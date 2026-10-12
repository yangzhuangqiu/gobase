package hashx

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
)

// MD5Hex 计算 MD5 十六进制摘要。
// 说明: MD5 不适用于抗碰撞安全场景，仅用于兼容协议。
func MD5Hex(message string) string {
	return MD5HexBytes([]byte(message))
}

// MD5HexBytes 计算字节数组的 MD5 十六进制摘要。
// 说明: MD5 不适用于抗碰撞安全场景，仅用于兼容协议。
func MD5HexBytes(message []byte) string {
	sum := md5.Sum(message)
	return hex.EncodeToString(sum[:])
}

// MD5Base64 计算 MD5 Base64 摘要。
// 说明: MD5 不适用于抗碰撞安全场景，仅用于兼容协议。
func MD5Base64(message string) string {
	return MD5Base64Bytes([]byte(message))
}

// MD5Base64Bytes 计算字节数组的 MD5 Base64 摘要。
// 说明: MD5 不适用于抗碰撞安全场景，仅用于兼容协议。
func MD5Base64Bytes(message []byte) string {
	sum := md5.Sum(message)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// SHA1Hex 计算 SHA-1 十六进制摘要。
// 说明: SHA-1 不适用于抗碰撞安全场景，仅用于兼容协议。
func SHA1Hex(message string) string {
	return SHA1HexBytes([]byte(message))
}

// SHA1HexBytes 计算字节数组的 SHA-1 十六进制摘要。
// 说明: SHA-1 不适用于抗碰撞安全场景，仅用于兼容协议。
func SHA1HexBytes(message []byte) string {
	sum := sha1.Sum(message)
	return hex.EncodeToString(sum[:])
}

// SHA1Base64 计算 SHA-1 Base64 摘要。
// 说明: SHA-1 不适用于抗碰撞安全场景，仅用于兼容协议。
func SHA1Base64(message string) string {
	return SHA1Base64Bytes([]byte(message))
}

// SHA1Base64Bytes 计算字节数组的 SHA-1 Base64 摘要。
// 说明: SHA-1 不适用于抗碰撞安全场景，仅用于兼容协议。
func SHA1Base64Bytes(message []byte) string {
	sum := sha1.Sum(message)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// SHA256Hex 计算字符串的 SHA-256 十六进制摘要。
func SHA256Hex(message string) string {
	return SHA256HexBytes([]byte(message))
}

// SHA256HexBytes 计算字节数组的 SHA-256 十六进制摘要。
func SHA256HexBytes(message []byte) string {
	sum := sha256.Sum256(message)
	return hex.EncodeToString(sum[:])
}

// SHA256Base64 计算 SHA-256 Base64 摘要。
func SHA256Base64(message string) string {
	return SHA256Base64Bytes([]byte(message))
}

// SHA256Base64Bytes 计算字节数组的 SHA-256 Base64 摘要。
func SHA256Base64Bytes(message []byte) string {
	sum := sha256.Sum256(message)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// SHA256BytesRaw 计算字节数组的 SHA-256 原始摘要字节。
func SHA256BytesRaw(message []byte) []byte {
	sum := sha256.Sum256(message)
	output := make([]byte, len(sum))
	copy(output, sum[:])
	return output
}

// SHA512Hex 计算字符串的 SHA-512 十六进制摘要。
func SHA512Hex(message string) string {
	return SHA512HexBytes([]byte(message))
}

// SHA512HexBytes 计算字节数组的 SHA-512 十六进制摘要。
func SHA512HexBytes(message []byte) string {
	sum := sha512.Sum512(message)
	return hex.EncodeToString(sum[:])
}

// SHA512Base64 计算 SHA-512 Base64 摘要。
func SHA512Base64(message string) string {
	return SHA512Base64Bytes([]byte(message))
}

// SHA512Base64Bytes 计算字节数组的 SHA-512 Base64 摘要。
func SHA512Base64Bytes(message []byte) string {
	sum := sha512.Sum512(message)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// HMACSHA1Hex 计算 HMAC-SHA1 的十六进制摘要。
// 说明: HMAC-SHA1 仅建议用于兼容要求 SHA-1 的老协议。
// 参数顺序遵循标准密码学 API：key 在前，message 在后。
func HMACSHA1Hex(key, message string) string {
	return HMACSHA1HexBytes([]byte(key), []byte(message))
}

// HMACSHA1Base64 计算 HMAC-SHA1 的 Base64 摘要。
// 参数顺序遵循标准密码学 API：key 在前，message 在后。
func HMACSHA1Base64(key, message string) string {
	return HMACSHA1Base64Bytes([]byte(key), []byte(message))
}

// HMACSHA256Hex 计算 HMAC-SHA256 的十六进制摘要。
// 参数顺序遵循标准密码学 API：key 在前，message 在后。
func HMACSHA256Hex(key, message string) string {
	return HMACSHA256HexBytes([]byte(key), []byte(message))
}

// HMACSHA256Base64 计算 HMAC-SHA256 的 Base64 摘要。
// 参数顺序遵循标准密码学 API：key 在前，message 在后。
func HMACSHA256Base64(key, message string) string {
	return HMACSHA256Base64Bytes([]byte(key), []byte(message))
}

// HMACSHA512Hex 计算 HMAC-SHA512 的十六进制摘要。
// 参数顺序遵循标准密码学 API：key 在前，message 在后。
func HMACSHA512Hex(key, message string) string {
	return HMACSHA512HexBytes([]byte(key), []byte(message))
}

// HMACSHA512Base64 计算 HMAC-SHA512 的 Base64 摘要。
// 参数顺序遵循标准密码学 API：key 在前，message 在后。
func HMACSHA512Base64(key, message string) string {
	return HMACSHA512Base64Bytes([]byte(key), []byte(message))
}

// HMACSHA1HexBytes 计算字节数组 HMAC-SHA1 的十六进制摘要。
// 参数顺序遵循标准密码学 API：key 在前，message 在后。
func HMACSHA1HexBytes(key, message []byte) string {
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(message)
	return hex.EncodeToString(mac.Sum(nil))
}

// HMACSHA1Base64Bytes 计算字节数组 HMAC-SHA1 的 Base64 摘要。
// 参数顺序遵循标准密码学 API：key 在前，message 在后。
func HMACSHA1Base64Bytes(key, message []byte) string {
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(message)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// HMACSHA256HexBytes 计算字节数组 HMAC-SHA256 的十六进制摘要。
// 参数顺序遵循标准密码学 API：key 在前，message 在后。
func HMACSHA256HexBytes(key, message []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return hex.EncodeToString(mac.Sum(nil))
}

// HMACSHA256Base64Bytes 计算字节数组 HMAC-SHA256 的 Base64 摘要。
// 参数顺序遵循标准密码学 API：key 在前，message 在后。
func HMACSHA256Base64Bytes(key, message []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// HMACSHA512HexBytes 计算字节数组 HMAC-SHA512 的十六进制摘要。
// 参数顺序遵循标准密码学 API：key 在前，message 在后。
func HMACSHA512HexBytes(key, message []byte) string {
	mac := hmac.New(sha512.New, key)
	_, _ = mac.Write(message)
	return hex.EncodeToString(mac.Sum(nil))
}

// HMACSHA512Base64Bytes 计算字节数组 HMAC-SHA512 的 Base64 摘要。
// 参数顺序遵循标准密码学 API：key 在前，message 在后。
func HMACSHA512Base64Bytes(key, message []byte) string {
	mac := hmac.New(sha512.New, key)
	_, _ = mac.Write(message)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
