package aesx

import (
	"bytes"
	"testing"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

// TestEncryptDecryptGCM 验证模式专用 GCM API 完成认证加密往返。
// 测试方法：使用固定 key/nonce 加密 hello，再解密并比较明文。
func TestEncryptDecryptGCM(t *testing.T) {
	// 执行阶段：固定材料只用于验证认证加密往返，不作为生产密钥示例。
	key := []byte("1234567890abcdef")
	nonce := []byte("123456789012")
	ciphertext, err := EncryptGCM([]byte("hello"), key, nonce)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}

	plaintext, err := DecryptGCM(ciphertext, key, nonce)
	if err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}
	if string(plaintext) != "hello" {
		t.Fatalf("plaintext mismatch: %s", string(plaintext))
	}
}

// TestEncryptDecryptCBCPKCS7 验证 CBC bytes API 可与独立 Base64 编解码组合。
// 测试方法：加密 hello、Base64 往返编码，再使用 PKCS7 解密并比较明文。
func TestEncryptDecryptCBCPKCS7(t *testing.T) {
	// 加密阶段：模式专用 bytes API 与文本编码保持正交组合。
	key := []byte("1234567890abcdef")
	iv := []byte("1234567890abcdef")
	cipherBytes, err := EncryptCBC([]byte("hello"), key, iv, PaddingPKCS7)
	if err != nil {
		t.Fatalf("EncryptCBC error: %v", err)
	}
	cipherText, err := Encode(cipherBytes, EncodingBase64)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	decodedCipher, err := Decode(cipherText, EncodingBase64)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	plainBytes, err := DecryptCBC(decodedCipher, key, iv, PaddingPKCS7)
	if err != nil {
		t.Fatalf("DecryptCBC error: %v", err)
	}
	if string(plainBytes) != "hello" {
		t.Fatalf("plaintext mismatch: %s", string(plainBytes))
	}
}

// TestEncryptDecryptECBPKCS5 验证遗留 ECB/PKCS5 兼容入口仍可往返。
// 测试方法：调用模式专用 ECB API 加解密 hello 并比较结果。
func TestEncryptDecryptECBPKCS5(t *testing.T) {
	key := []byte("1234567890abcdef")
	cipherBytes, err := EncryptECB([]byte("hello"), key, PaddingPKCS5)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	plainBytes, err := DecryptECB(cipherBytes, key, PaddingPKCS5)
	if err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}
	if string(plainBytes) != "hello" {
		t.Fatalf("plainText mismatch: %s", string(plainBytes))
	}
}

// TestNonePaddingValidation 验证未对齐明文不能使用 PaddingNone。
// 测试方法：用 CBC 加密 5-byte 明文，断言返回 KindInvalidPadding。
func TestNonePaddingValidation(t *testing.T) {
	key := []byte("1234567890abcdef")
	iv := []byte("1234567890abcdef")
	_, err := EncryptCBC([]byte("hello"), key, iv, PaddingNone)
	if err == nil {
		t.Fatal("expected none padding aligned error")
	}
	if !errorsx.IsKind(err, KindInvalidPadding) {
		t.Fatalf("expected kind %s, got err=%v", KindInvalidPadding, err)
	}
}

// TestHexEncoding 验证 GCM bytes API 可与独立 Hex 编解码组合。
// 测试方法：加密 hello，执行 Hex 往返后解密并比较明文。
func TestHexEncoding(t *testing.T) {
	// 组合阶段：先完成 GCM 加密，再单独验证 Hex 编解码层。
	key := []byte("1234567890abcdef")
	nonce := []byte("123456789012")
	cipherBytes, err := EncryptGCM([]byte("hello"), key, nonce)
	if err != nil {
		t.Fatalf("EncryptGCM error: %v", err)
	}
	cipherText, err := Encode(cipherBytes, EncodingHex)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	decodedCipher, err := Decode(cipherText, EncodingHex)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	plainBytes, err := DecryptGCM(decodedCipher, key, nonce)
	if err != nil {
		t.Fatalf("DecryptGCM error: %v", err)
	}
	if string(plainBytes) != "hello" {
		t.Fatalf("plaintext mismatch: %s", string(plainBytes))
	}
}

// TestGenerateKeyAndNonce 验证随机 key、IV 和 nonce 的公开长度契约。
// 测试方法：分别生成三类材料并断言 32、IVSize 和 NonceSize。
func TestGenerateKeyAndNonce(t *testing.T) {
	// 生成阶段：依次验证 key、IV、nonce 三类安全随机材料的固定长度。
	key, err := GenerateKey(32)
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("unexpected key length: %d", len(key))
	}
	iv, err := GenerateIV()
	if err != nil {
		t.Fatalf("GenerateIV error: %v", err)
	}
	if len(iv) != IVSize {
		t.Fatalf("unexpected IV length: %d", len(iv))
	}
	nonce, err := GenerateNonce()
	if err != nil {
		t.Fatalf("GenerateNonce error: %v", err)
	}
	if len(nonce) != NonceSize {
		t.Fatalf("unexpected nonce length: %d", len(nonce))
	}
}

// TestModeSpecificEntrypoints 验证 GCM 与 CBC 模式专用入口均可独立往返。
// 测试方法：分别调用两组 Encrypt/Decrypt，断言明文保持一致。
func TestModeSpecificEntrypoints(t *testing.T) {
	// 模式阶段：GCM 与 CBC 分别走独立公开入口，不依赖通用 Mode 分派。
	key := []byte("1234567890abcdef")
	nonce := []byte("123456789012")
	iv := []byte("1234567890abcdef")

	gcmCipher, err := EncryptGCM([]byte("hello"), key, nonce)
	if err != nil {
		t.Fatalf("EncryptGCM error: %v", err)
	}
	gcmPlain, err := DecryptGCM(gcmCipher, key, nonce)
	if err != nil {
		t.Fatalf("DecryptGCM error: %v", err)
	}
	if string(gcmPlain) != "hello" {
		t.Fatalf("DecryptGCM mismatch: %s", string(gcmPlain))
	}

	cbcCipher, err := EncryptCBC([]byte("hello"), key, iv, PaddingPKCS7)
	if err != nil {
		t.Fatalf("EncryptCBC error: %v", err)
	}
	cbcPlain, err := DecryptCBC(cbcCipher, key, iv, PaddingPKCS7)
	if err != nil {
		t.Fatalf("DecryptCBC error: %v", err)
	}
	if string(cbcPlain) != "hello" {
		t.Fatalf("DecryptCBC mismatch: %s", string(cbcPlain))
	}

}

// TestGCMAuthenticationFailure 验证篡改密文只能返回认证失败而不会泄露明文。
// 测试方法：翻转 GCM 密文首字节，断言 DecryptGCM 返回 KindAuthenticationFailed。
func TestGCMAuthenticationFailure(t *testing.T) {
	key := []byte("1234567890abcdef")
	nonce := []byte("123456789012")
	cipherBytes, err := EncryptGCM([]byte("hello"), key, nonce)
	if err != nil {
		t.Fatalf("EncryptGCM error: %v", err)
	}
	cipherBytes[0] ^= 0xff
	if _, err := DecryptGCM(cipherBytes, key, nonce); err == nil {
		t.Fatal("expected authentication failure")
	} else if !errorsx.IsKind(err, KindAuthenticationFailed) {
		t.Fatalf("expected kind %s, got err=%v", KindAuthenticationFailed, err)
	}
}

// TestPublicInputKinds 验证 key、IV、nonce 和编码错误映射到调用方可处理的 Kind。
// 测试方法：逐项传入非法长度或文本，断言对应 Kind。
func TestPublicInputKinds(t *testing.T) {
	validKey := []byte("1234567890abcdef")
	if _, err := EncryptGCM([]byte("x"), []byte("short"), []byte("123456789012")); err == nil || !errorsx.IsKind(err, KindInvalidKey) {
		t.Fatalf("expected invalid key, got=%v", err)
	}
	if _, err := EncryptGCM([]byte("x"), validKey, []byte("short")); err == nil || !errorsx.IsKind(err, KindInvalidNonce) {
		t.Fatalf("expected invalid nonce, got=%v", err)
	}
	if _, err := EncryptCBC([]byte("x"), validKey, []byte("short"), PaddingPKCS7); err == nil || !errorsx.IsKind(err, KindInvalidIV) {
		t.Fatalf("expected invalid IV, got=%v", err)
	}
	if _, err := Decode("%%%", EncodingBase64); err == nil || !errorsx.IsKind(err, KindInvalidEncoding) {
		t.Fatalf("expected invalid encoding, got=%v", err)
	}
}

// TestEncryptCBCDoesNotMutateInputBackingArray 验证 padding 不会污染调用方切片的富余容量。
// 测试方法：使用带额外 capacity 的 hello，CBC 加密后检查扩展区域仍为零值。
func TestEncryptCBCDoesNotMutateInputBackingArray(t *testing.T) {
	key := []byte("1234567890abcdef")
	iv := []byte("1234567890abcdef")
	plaintext := make([]byte, 5, 32)
	copy(plaintext, []byte("hello"))

	if _, err := EncryptCBC(plaintext, key, iv, PaddingPKCS7); err != nil {
		t.Fatalf("EncryptCBC error: %v", err)
	}

	extended := plaintext[:16]
	if !bytes.Equal(extended[5:], make([]byte, 11)) {
		t.Fatalf("plaintext backing array should not be mutated, tail=%v", extended[5:])
	}
}
