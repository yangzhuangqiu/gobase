package compressx

import (
	"bytes"
	"testing"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

// TestGzipAndGunzip 验证 bytes 压缩、magic header 和解压往返。
// 测试方法：压缩固定文本，检查 IsGzip 后解压并逐字节比较。
func TestGzipAndGunzip(t *testing.T) {
	// 执行阶段：先验证压缩产物形态，再验证完整解压内容。
	source := []byte("hello compressx")
	zipped, err := Gzip(source)
	if err != nil {
		t.Fatalf("Gzip error: %v", err)
	}
	if !IsGzip(zipped) {
		t.Fatal("expected gzip header")
	}

	restored, err := Gunzip(zipped)
	if err != nil {
		t.Fatalf("Gunzip error: %v", err)
	}
	if !bytes.Equal(restored, source) {
		t.Fatalf("roundtrip mismatch: got=%q want=%q", string(restored), string(source))
	}
}

// TestGzipStringAndGunzipToString 验证 string 便捷入口保持文本内容。
// 测试方法：压缩 abc-123 后通过文本入口解压并比较。
func TestGzipStringAndGunzipToString(t *testing.T) {
	zipped, err := GzipString("abc-123")
	if err != nil {
		t.Fatalf("GzipString error: %v", err)
	}
	restored, err := GunzipToString(zipped)
	if err != nil {
		t.Fatalf("GunzipToString error: %v", err)
	}
	if restored != "abc-123" {
		t.Fatalf("string mismatch: %q", restored)
	}
}

// TestGzipToBase64AndBack 验证 gzip 与 Base64 组合入口可往返。
// 测试方法：编码 payload 后使用默认上限解压并比较结果。
func TestGzipToBase64AndBack(t *testing.T) {
	base64Value, err := GzipToBase64([]byte("payload"))
	if err != nil {
		t.Fatalf("GzipToBase64 error: %v", err)
	}
	restored, err := GunzipFromBase64(base64Value)
	if err != nil {
		t.Fatalf("GunzipFromBase64 error: %v", err)
	}
	if string(restored) != "payload" {
		t.Fatalf("payload mismatch: %q", string(restored))
	}
}

// TestGunzipInvalidData 验证损坏 gzip 和 Base64 统一归入不可接受输入。
// 测试方法：分别传入非法 bytes 与文本，断言 KindInvalidData。
func TestGunzipInvalidData(t *testing.T) {
	if _, err := Gunzip([]byte("not-gzip")); err == nil {
		t.Fatal("expected gunzip error for invalid data")
	} else if !errorsx.IsKind(err, KindInvalidData) {
		t.Fatalf("expected kind %s, got err=%v", KindInvalidData, err)
	}
	if _, err := GunzipFromBase64("%%%"); err == nil {
		t.Fatal("expected base64 decode error")
	} else if !errorsx.IsKind(err, KindInvalidData) {
		t.Fatalf("expected kind %s, got err=%v", KindInvalidData, err)
	}
}
