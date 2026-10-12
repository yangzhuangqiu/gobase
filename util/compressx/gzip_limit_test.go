package compressx

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

type zeroReader struct{}

// Read 为默认上限测试持续生成零字节，避免为测试源数据额外分配 64 MiB。
// 阶段角色：仅作为 gzip 压缩输入，不参与生产逻辑。
func (this zeroReader) Read(data []byte) (int, error) {
	clear(data)
	return len(data), nil
}

// TestDefaultGunzipMaxOutputBytes 验证模块默认上限是稳定公开契约；直接断言默认值为 64 MiB，防止无意移除或改变容量边界。
func TestDefaultGunzipMaxOutputBytes(t *testing.T) {
	// expected 固定首次发布的默认解压容量契约（单位：byte）。
	const expected int64 = 64 * 1024 * 1024
	if DefaultGunzipMaxOutputBytes != expected {
		t.Fatalf("DefaultGunzipMaxOutputBytes=%d, want=%d", DefaultGunzipMaxOutputBytes, expected)
	}
}

// TestGunzipAppliesDefaultOutputLimit 验证默认入口确实应用模块上限；流式生成超限一字节的高压缩比数据并断言超限错误 Kind。
func TestGunzipAppliesDefaultOutputLimit(t *testing.T) {
	// 准备阶段：流式构造 64 MiB+1 的解压结果，避免同时持有同等大小的测试源切片。
	var compressed bytes.Buffer
	writer, err := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
	if err != nil {
		t.Fatalf("gzip.NewWriterLevel error: %v", err)
	}
	if _, err = io.CopyN(writer, zeroReader{}, DefaultGunzipMaxOutputBytes+1); err != nil {
		t.Fatalf("io.CopyN error: %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("gzip writer close error: %v", err)
	}

	// 验证阶段：默认入口必须拒绝超限结果，而不是继续无界读取。
	_, err = Gunzip(compressed.Bytes())
	if !errorsx.IsKind(err, KindOutputLimitExceeded) {
		t.Fatalf("expected kind %s, got err=%v", KindOutputLimitExceeded, err)
	}
}

// TestGunzipLimitAllowsExactBoundary 验证输出恰好达到上限仍成功；通过压缩固定长度数据并断言解压结果完整来验证边界语义。
func TestGunzipLimitAllowsExactBoundary(t *testing.T) {
	source := bytes.Repeat([]byte("a"), 1024)
	zipped, err := Gzip(source)
	if err != nil {
		t.Fatalf("Gzip error: %v", err)
	}

	restored, err := GunzipLimit(zipped, int64(len(source)))
	if err != nil {
		t.Fatalf("GunzipLimit error: %v", err)
	}
	if !bytes.Equal(restored, source) {
		t.Fatal("GunzipLimit exact-boundary result mismatch")
	}
}

// TestGunzipLimitChecksTrailerAtExactBoundary 验证达到上限时仍校验 gzip 尾部；篡改 trailer 后按精确上限解压并断言输入无效 Kind。
func TestGunzipLimitChecksTrailerAtExactBoundary(t *testing.T) {
	source := bytes.Repeat([]byte("c"), 256)
	zipped, err := Gzip(source)
	if err != nil {
		t.Fatalf("Gzip error: %v", err)
	}
	zipped[len(zipped)-1] ^= 0xff

	_, err = GunzipLimit(zipped, int64(len(source)))
	if !errorsx.IsKind(err, KindInvalidData) {
		t.Fatalf("expected kind %s, got err=%v", KindInvalidData, err)
	}
}

// TestGunzipFromBase64LimitValidatesLimitFirst 验证配置错误优先于载荷解码错误。
// 测试方法：同时传入非法 Base64 和零上限，断言返回 KindInvalidOutputLimit。
func TestGunzipFromBase64LimitValidatesLimitFirst(t *testing.T) {
	_, err := GunzipFromBase64Limit("%%%", 0)
	if !errorsx.IsKind(err, KindInvalidOutputLimit) {
		t.Fatalf("expected kind %s, got err=%v", KindInvalidOutputLimit, err)
	}
}

// TestGunzipLimitRejectsOutputOverLimit 验证压缩炸弹防护分支；用高压缩比数据触发上限并断言稳定错误 Kind。
func TestGunzipLimitRejectsOutputOverLimit(t *testing.T) {
	zipped, err := Gzip(bytes.Repeat([]byte("a"), 4096))
	if err != nil {
		t.Fatalf("Gzip error: %v", err)
	}

	_, err = GunzipLimit(zipped, 128)
	if !errorsx.IsKind(err, KindOutputLimitExceeded) {
		t.Fatalf("expected kind %s, got err=%v", KindOutputLimitExceeded, err)
	}
}

// TestGunzipLimitRejectsInvalidLimit 验证非正输出上限不可绕过保护；分别输入零值和负值并断言参数错误 Kind。
func TestGunzipLimitRejectsInvalidLimit(t *testing.T) {
	for _, limit := range []int64{0, -1} {
		_, err := GunzipLimit(nil, limit)
		if !errorsx.IsKind(err, KindInvalidOutputLimit) {
			t.Fatalf("limit=%d: expected kind %s, got err=%v", limit, KindInvalidOutputLimit, err)
		}
	}
}

// TestGunzipFromBase64Limit 验证组合入口同样执行输出限制；先生成合法 Base64 gzip，再以较小上限断言拒绝。
func TestGunzipFromBase64Limit(t *testing.T) {
	encoded, err := GzipToBase64(bytes.Repeat([]byte("b"), 512))
	if err != nil {
		t.Fatalf("GzipToBase64 error: %v", err)
	}

	_, err = GunzipFromBase64Limit(encoded, 64)
	if !errorsx.IsKind(err, KindOutputLimitExceeded) {
		t.Fatalf("expected kind %s, got err=%v", KindOutputLimitExceeded, err)
	}
}
