package hashx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestSHA256Hex 验证 SHA-256 十六进制输出符合标准测试向量。
// 测试方法：计算 "abc" 摘要并与公开固定值比较。
func TestSHA256Hex(t *testing.T) {
	got := SHA256Hex("abc")
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Fatalf("SHA256Hex mismatch: got=%s want=%s", got, want)
	}
}

// TestMD5AndSHA1 验证兼容摘要和 SHA-256 Base64 的固定编码结果。
// 测试方法：使用 "abc" 标准向量分别比较 Hex 与 Base64 文本。
func TestMD5AndSHA1(t *testing.T) {
	// 兼容算法阶段：固定向量仅验证协议兼容输出，不表达安全推荐。
	if got := MD5Hex("abc"); got != "900150983cd24fb0d6963f7d28e17f72" {
		t.Fatalf("MD5Hex mismatch: %s", got)
	}
	if got := MD5Base64("abc"); got != "kAFQmDzST7DWlj99KOF/cg==" {
		t.Fatalf("MD5Base64 mismatch: %s", got)
	}
	if got := SHA1Hex("abc"); got != "a9993e364706816aba3e25717850c26c9cd0d89d" {
		t.Fatalf("SHA1Hex mismatch: %s", got)
	}
	if got := SHA1Base64("abc"); got != "qZk+NkcGgWq6PiVxeFDCbJzQ2J0=" {
		t.Fatalf("SHA1Base64 mismatch: %s", got)
	}
	if got := SHA256Base64("abc"); got != "ungWv48Bz+pBQUDeXa4iI7ADYaOWF3qctBD/YfIAFa0=" {
		t.Fatalf("SHA256Base64 mismatch: %s", got)
	}
}

// TestHMACSHA256Output 验证 HMAC-SHA256 采用 key-first 参数契约。
// 测试方法：用标准库按 key="secret"、message="hello" 计算期望值并比较。
func TestHMACSHA256Output(t *testing.T) {
	// 标准库基线阶段：先按 key-first 构造期望摘要，再验证便捷编码入口。
	standard := hmac.New(sha256.New, []byte("secret"))
	_, _ = standard.Write([]byte("hello"))
	wantHex := hex.EncodeToString(standard.Sum(nil))
	hexDigest := HMACSHA256Hex("secret", "hello")
	base64Digest := HMACSHA256Base64("secret", "hello")
	if hexDigest != wantHex {
		t.Fatalf("HMACSHA256 key-first mismatch: got=%s want=%s", hexDigest, wantHex)
	}
	if len(hexDigest) != 64 {
		t.Fatalf("unexpected hex digest length: %d", len(hexDigest))
	}
	if base64Digest == "" {
		t.Fatal("base64 digest should not be empty")
	}
}

// TestHMACSHA1Output 验证兼容 HMAC-SHA1 的两种编码入口可用。
// 测试方法：按 key-first 调用 Hex/Base64，断言摘要长度和非空结果。
func TestHMACSHA1Output(t *testing.T) {
	hexDigest := HMACSHA1Hex("secret", "hello")
	base64Digest := HMACSHA1Base64("secret", "hello")
	if len(hexDigest) != 40 {
		t.Fatalf("unexpected hex digest length: %d", len(hexDigest))
	}
	if base64Digest == "" {
		t.Fatal("base64 digest should not be empty")
	}
}

// TestByteInputVariants 验证 string 与 []byte 入口对相同内容产生一致结果。
// 测试方法：比较 SHA-256 与 key-first HMAC 的两类输入版本。
func TestByteInputVariants(t *testing.T) {
	if SHA256HexBytes([]byte("abc")) != SHA256Hex("abc") {
		t.Fatal("SHA256 hex bytes/string mismatch")
	}
	if SHA256Base64Bytes([]byte("abc")) != SHA256Base64("abc") {
		t.Fatal("SHA256 base64 bytes/string mismatch")
	}

	if HMACSHA256HexBytes([]byte("secret"), []byte("hello")) != HMACSHA256Hex("secret", "hello") {
		t.Fatal("HMACSHA256 hex bytes/string mismatch")
	}
	if HMACSHA1Base64Bytes([]byte("secret"), []byte("hello")) != HMACSHA1Base64("secret", "hello") {
		t.Fatal("HMACSHA1 base64 bytes/string mismatch")
	}
}

// TestSHA512AndHMACSHA512 验证 SHA-512/HMAC-SHA512 的 string 与 bytes 入口一致。
// 测试方法：对固定消息和 key 比较两类输入版本的 Hex/Base64 结果。
func TestSHA512AndHMACSHA512(t *testing.T) {
	if SHA512Hex("abc") != SHA512HexBytes([]byte("abc")) {
		t.Fatal("SHA512 hex bytes/string mismatch")
	}
	if SHA512Base64("abc") != SHA512Base64Bytes([]byte("abc")) {
		t.Fatal("SHA512 base64 bytes/string mismatch")
	}
	if HMACSHA512Hex("secret", "hello") != HMACSHA512HexBytes([]byte("secret"), []byte("hello")) {
		t.Fatal("HMACSHA512 hex bytes/string mismatch")
	}
	if HMACSHA512Base64("secret", "hello") != HMACSHA512Base64Bytes([]byte("secret"), []byte("hello")) {
		t.Fatal("HMACSHA512 base64 bytes/string mismatch")
	}
}

// TestSHA256BytesRaw 验证原始 SHA-256 bytes 与十六进制入口表达同一摘要。
// 测试方法：检查原始长度并编码为 Hex 后与 SHA256Hex 比较。
func TestSHA256BytesRaw(t *testing.T) {
	raw := SHA256BytesRaw([]byte("abc"))
	if len(raw) != 32 {
		t.Fatalf("unexpected raw digest length: %d", len(raw))
	}
	if hex.EncodeToString(raw) != SHA256Hex("abc") {
		t.Fatal("SHA256BytesRaw mismatch with SHA256Hex")
	}
}
