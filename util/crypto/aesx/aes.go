package aesx

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"io"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

const (
	// IVSize 是 AES-CBC 的 IV 长度（单位: byte）。
	IVSize = aes.BlockSize
	// NonceSize 是 GCM 标准 nonce 长度（单位: byte）。
	NonceSize = 12
)

// mode 表示内部 AES 分派模式；公共调用方只能使用模式专用入口。
type mode uint8

const (
	// modeGCM 表示带认证的 Galois/Counter Mode。
	modeGCM mode = iota
	// modeCBC 表示兼容协议使用的 Cipher Block Chaining。
	modeCBC
	// modeECB 表示仅兼容遗留协议的 Electronic Codebook。
	modeECB
)

// Padding 表示分组加解密时的填充策略。
type Padding string

const (
	// PaddingNone 不做填充。
	PaddingNone Padding = "NONE"
	// PaddingPKCS7 使用 PKCS7 填充。
	PaddingPKCS7 Padding = "PKCS7"
	// PaddingPKCS5 在 AES 场景等价于 PKCS7（业界常见兼容写法）。
	PaddingPKCS5 Padding = "PKCS5"
	// PaddingZero 使用 Zero Padding（有歧义，不建议文本协议场景）。
	PaddingZero Padding = "ZERO"
)

// Encoding 表示密文文本编码格式。
type Encoding string

const (
	// EncodingBase64 使用标准带 padding 的 Base64 文本。
	EncodingBase64 Encoding = "BASE64"
	// EncodingHex 使用小写十六进制文本。
	EncodingHex Encoding = "HEX"
)

// GenerateKey 生成 AES 对称密钥（16/24/32 字节）。
func GenerateKey(size int) ([]byte, error) {
	if size != 16 && size != 24 && size != 32 {
		return nil, errorsx.Newf("aesx.GenerateKey", KindInvalidKey, "size=%d expected=16_or_24_or_32", size)
	}
	return generateRandomBytes(size, "aesx.GenerateKey")
}

// GenerateIV 生成 CBC 模式使用的随机 IV（16 字节）。
func GenerateIV() ([]byte, error) {
	return generateRandomBytes(IVSize, "aesx.GenerateIV")
}

// GenerateNonce 生成 GCM 模式使用的随机 nonce（12 字节）。
func GenerateNonce() ([]byte, error) {
	return generateRandomBytes(NonceSize, "aesx.GenerateNonce")
}

// encrypt 执行模式专用入口已经约束后的共享加密流程。
// 阶段角色：统一 key 校验、primitive 初始化和 block 模式分派，不对外暴露非法组合。
func encrypt(plaintext, key, ivOrNonce []byte, selectedMode mode, padding Padding, op string) ([]byte, error) {
	if err := validateAESKey(key); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errorsx.Wrap(err, op, KindInternal)
	}

	// 模式阶段：公共 wrapper 已固定模式和必要参数，仅共享底层内存处理。
	switch selectedMode {
	case modeGCM:
		if padding != PaddingNone {
			return nil, errorsx.New(op, KindInvalidPadding)
		}
		if err := validateNonce(ivOrNonce); err != nil {
			return nil, err
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, errorsx.Wrap(err, op, KindInternal)
		}
		return aead.Seal(nil, ivOrNonce, plaintext, nil), nil
	case modeCBC:
		if err := validateIV(ivOrNonce); err != nil {
			return nil, err
		}
		padded, err := applyPadding(plaintext, block.BlockSize(), padding)
		if err != nil {
			return nil, err
		}
		cipherBytes := make([]byte, len(padded))
		cipher.NewCBCEncrypter(block, ivOrNonce).CryptBlocks(cipherBytes, padded)
		return cipherBytes, nil
	case modeECB:
		padded, err := applyPadding(plaintext, block.BlockSize(), padding)
		if err != nil {
			return nil, err
		}
		return encryptECB(block, padded), nil
	default:
		return nil, errorsx.Newf(op, KindInternal, "mode=%d", selectedMode)
	}
}

// EncryptGCM 使用 AES-GCM 加密（固定无填充）。
func EncryptGCM(plaintext, key, nonce []byte) ([]byte, error) {
	return encrypt(plaintext, key, nonce, modeGCM, PaddingNone, "aesx.EncryptGCM")
}

// EncryptCBC 使用 AES-CBC 加密。
func EncryptCBC(plaintext, key, iv []byte, padding Padding) ([]byte, error) {
	return encrypt(plaintext, key, iv, modeCBC, padding, "aesx.EncryptCBC")
}

// EncryptECB 使用 AES-ECB 加密。
func EncryptECB(plaintext, key []byte, padding Padding) ([]byte, error) {
	return encrypt(plaintext, key, nil, modeECB, padding, "aesx.EncryptECB")
}

// decrypt 执行模式专用入口已经约束后的共享解密流程。
// 阶段角色：统一 primitive 初始化、认证、block 长度和 padding 校验。
func decrypt(cipherBytes, key, ivOrNonce []byte, selectedMode mode, padding Padding, op string) ([]byte, error) {
	if err := validateAESKey(key); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errorsx.Wrap(err, op, KindInternal)
	}

	// 模式阶段：GCM 先认证再返回明文；CBC/ECB 先验证 block 长度再解密。
	switch selectedMode {
	case modeGCM:
		if padding != PaddingNone {
			return nil, errorsx.New(op, KindInvalidPadding)
		}
		if err := validateNonce(ivOrNonce); err != nil {
			return nil, err
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, errorsx.Wrap(err, op, KindInternal)
		}
		plaintext, err := aead.Open(nil, ivOrNonce, cipherBytes, nil)
		if err != nil {
			return nil, errorsx.Wrap(err, op, KindAuthenticationFailed)
		}
		return plaintext, nil
	case modeCBC:
		if err := validateIV(ivOrNonce); err != nil {
			return nil, err
		}
		if len(cipherBytes)%block.BlockSize() != 0 {
			return nil, errorsx.Newf(op, KindInvalidData, "ciphertext_len=%d block_size=%d", len(cipherBytes), block.BlockSize())
		}
		raw := make([]byte, len(cipherBytes))
		cipher.NewCBCDecrypter(block, ivOrNonce).CryptBlocks(raw, cipherBytes)
		return removePadding(raw, block.BlockSize(), padding)
	case modeECB:
		if len(cipherBytes)%block.BlockSize() != 0 {
			return nil, errorsx.Newf(op, KindInvalidData, "ciphertext_len=%d block_size=%d", len(cipherBytes), block.BlockSize())
		}
		raw := decryptECB(block, cipherBytes)
		return removePadding(raw, block.BlockSize(), padding)
	default:
		return nil, errorsx.Newf(op, KindInternal, "mode=%d", selectedMode)
	}
}

// DecryptGCM 使用 AES-GCM 解密（固定无填充）。
func DecryptGCM(cipherBytes, key, nonce []byte) ([]byte, error) {
	return decrypt(cipherBytes, key, nonce, modeGCM, PaddingNone, "aesx.DecryptGCM")
}

// DecryptCBC 使用 AES-CBC 解密。
func DecryptCBC(cipherBytes, key, iv []byte, padding Padding) ([]byte, error) {
	return decrypt(cipherBytes, key, iv, modeCBC, padding, "aesx.DecryptCBC")
}

// DecryptECB 使用 AES-ECB 解密。
func DecryptECB(cipherBytes, key []byte, padding Padding) ([]byte, error) {
	return decrypt(cipherBytes, key, nil, modeECB, padding, "aesx.DecryptECB")
}

// Encode 将字节按指定编码输出字符串。
func Encode(data []byte, encoding Encoding) (string, error) {
	switch encoding {
	case EncodingBase64:
		return base64.StdEncoding.EncodeToString(data), nil
	case EncodingHex:
		return hex.EncodeToString(data), nil
	default:
		return "", errorsx.Newf("aesx.Encode", KindInvalidEncoding, "encoding=%s", encoding)
	}
}

// Decode 将 Base64/Hex 字符串解码为字节。
func Decode(value string, encoding Encoding) ([]byte, error) {
	// 编码分派阶段：具体解析错误统一映射为调用方可修正的 KindInvalidEncoding。
	switch encoding {
	case EncodingBase64:
		data, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, errorsx.Wrapf(err, "aesx.Decode", KindInvalidEncoding, "encoding=base64 value_len=%d", len(value))
		}
		return data, nil
	case EncodingHex:
		data, err := hex.DecodeString(value)
		if err != nil {
			return nil, errorsx.Wrapf(err, "aesx.Decode", KindInvalidEncoding, "encoding=hex value_len=%d", len(value))
		}
		return data, nil
	default:
		return nil, errorsx.Newf("aesx.Decode", KindInvalidEncoding, "encoding=%s", encoding)
	}
}

// generateRandomBytes 生成指定长度的安全随机字节。
// 阶段角色：为 key、IV 和 nonce 入口统一分配与安全随机源错误映射。
func generateRandomBytes(size int, op string) ([]byte, error) {
	if size <= 0 {
		return nil, errorsx.Newf(op, KindInternal, "random_size=%d", size)
	}
	buffer := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, buffer); err != nil {
		return nil, errorsx.Wrap(err, op, KindRandomSourceFailed)
	}
	return buffer, nil
}

// validateAESKey 校验 AES-128/192/256 key 长度，防止 primitive 初始化阶段出现模糊错误。
func validateAESKey(key []byte) error {
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return errorsx.Newf("aesx.validateAESKey", KindInvalidKey, "len=%d expected=16_or_24_or_32", len(key))
	}
	return nil
}

// validateNonce 校验当前 GCM 公共契约固定使用的 12-byte nonce。
func validateNonce(nonce []byte) error {
	if len(nonce) != NonceSize {
		return errorsx.Newf("aesx.validateNonce", KindInvalidNonce, "len=%d expected=%d", len(nonce), NonceSize)
	}
	return nil
}

// validateIV 校验 CBC IV 与 AES block size 一致。
func validateIV(iv []byte) error {
	if len(iv) != IVSize {
		return errorsx.Newf("aesx.validateIV", KindInvalidIV, "len=%d expected=%d", len(iv), IVSize)
	}
	return nil
}

// applyPadding 在 block 加密前应用明确 padding，并保证不修改调用方底层数组。
// 阶段角色：统一 CBC/ECB 的对齐校验和新输出分配。
func applyPadding(data []byte, blockSize int, padding Padding) ([]byte, error) {
	// 填充分派阶段：None 只校验对齐；其他模式创建独立输出，避免修改调用方切片。
	switch padding {
	case PaddingNone:
		if len(data)%blockSize != 0 {
			return nil, errorsx.Newf("aesx.applyPadding", KindInvalidPadding, "padding=none data_len=%d block_size=%d", len(data), blockSize)
		}
		return data, nil
	case PaddingPKCS5, PaddingPKCS7:
		padLen := blockSize - len(data)%blockSize
		if padLen == 0 {
			padLen = blockSize
		}
		output := make([]byte, len(data)+padLen)
		copy(output, data)
		for index := len(data); index < len(output); index++ {
			output[index] = byte(padLen)
		}
		return output, nil
	case PaddingZero:
		padLen := blockSize - len(data)%blockSize
		if padLen == blockSize {
			return data, nil
		}
		output := make([]byte, len(data)+padLen)
		copy(output, data)
		return output, nil
	default:
		return nil, errorsx.Newf("aesx.applyPadding", KindInvalidPadding, "padding=%s", padding)
	}
}

// removePadding 校验并移除 block 解密结果中的 padding。
// 阶段角色：拒绝损坏或伪造的 padding，避免返回未经验证的明文尾部。
func removePadding(data []byte, blockSize int, padding Padding) ([]byte, error) {
	// 校验阶段：PKCS padding 必须同时满足长度字段和每个尾部 byte 一致。
	switch padding {
	case PaddingNone:
		return data, nil
	case PaddingPKCS5, PaddingPKCS7:
		if len(data) == 0 || len(data)%blockSize != 0 {
			return nil, errorsx.Newf("aesx.removePadding", KindInvalidData, "padded_len=%d block_size=%d", len(data), blockSize)
		}
		padLen := int(data[len(data)-1])
		if padLen <= 0 || padLen > blockSize || padLen > len(data) {
			return nil, errorsx.Newf("aesx.removePadding", KindInvalidData, "padding_len=%d", padLen)
		}
		for index := len(data) - padLen; index < len(data); index++ {
			if int(data[index]) != padLen {
				return nil, errorsx.New("aesx.removePadding", KindInvalidData)
			}
		}
		return data[:len(data)-padLen], nil
	case PaddingZero:
		return bytes.TrimRight(data, "\x00"), nil
	default:
		return nil, errorsx.Newf("aesx.removePadding", KindInvalidPadding, "padding=%s", padding)
	}
}

// encryptECB 逐 block 加密已完成对齐的明文。
func encryptECB(block cipher.Block, plaintext []byte) []byte {
	blockSize := block.BlockSize()
	cipherBytes := make([]byte, len(plaintext))
	for offset := 0; offset < len(plaintext); offset += blockSize {
		block.Encrypt(cipherBytes[offset:offset+blockSize], plaintext[offset:offset+blockSize])
	}
	return cipherBytes
}

// decryptECB 逐 block 解密已验证长度的 ECB 密文。
func decryptECB(block cipher.Block, cipherBytes []byte) []byte {
	blockSize := block.BlockSize()
	plaintext := make([]byte, len(cipherBytes))
	for offset := 0; offset < len(cipherBytes); offset += blockSize {
		block.Decrypt(plaintext[offset:offset+blockSize], cipherBytes[offset:offset+blockSize])
	}
	return plaintext
}
