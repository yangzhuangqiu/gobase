package randx

import "testing"

const (
	// minInt64 是极端区间回归测试使用的 int64 最小边界。
	minInt64 = -1 << 63
	// maxInt64 是极端区间回归测试使用的 int64 最大边界。
	maxInt64 = 1<<63 - 1
)

// TestMathIntRangeSupportsExtremeBounds 验证伪随机 int 入口支持当前架构完整半开区间。
// 测试方法：在 [MinInt,MaxInt) 多次取样，断言不报溢出且结果始终在范围内。
func TestMathIntRangeSupportsExtremeBounds(t *testing.T) {
	minInt := -int(^uint(0)>>1) - 1
	maxInt := int(^uint(0) >> 1)
	randomizer := NewMath(42)
	for index := 0; index < 128; index++ {
		value, err := randomizer.IntRange(minInt, maxInt)
		if err != nil {
			t.Fatalf("Math.IntRange error: %v", err)
		}
		if value < minInt || value >= maxInt {
			t.Fatalf("Math.IntRange out of range: %d", value)
		}
	}
}

// TestCryptoIntRangeSupportsExtremeBounds 验证安全随机 int 入口不会因完整区间宽度溢出。
// 测试方法：在 [MinInt,MaxInt) 多次取样，断言结果始终位于半开区间。
func TestCryptoIntRangeSupportsExtremeBounds(t *testing.T) {
	minInt := -int(^uint(0)>>1) - 1
	maxInt := int(^uint(0) >> 1)
	for index := 0; index < 32; index++ {
		value, err := IntRangeByCrypto(minInt, maxInt)
		if err != nil {
			t.Fatalf("IntRangeByCrypto error: %v", err)
		}
		if value < minInt || value >= maxInt {
			t.Fatalf("IntRangeByCrypto out of range: %d", value)
		}
	}
}

// TestMathInt64RangeSupportsExtremeBounds 验证伪随机入口可处理跨越 int64 全域的区间；固定 seed 多次取样并断言半开区间边界。
func TestMathInt64RangeSupportsExtremeBounds(t *testing.T) {
	randomizer := NewMath(42)
	for i := 0; i < 128; i++ {
		value, err := randomizer.Int64Range(minInt64, maxInt64)
		if err != nil {
			t.Fatalf("Math.Int64Range error: %v", err)
		}
		if value < minInt64 || value >= maxInt64 {
			t.Fatalf("Math.Int64Range out of range: %d", value)
		}
	}
}

// TestCryptoInt64RangeSupportsExtremeBounds 验证安全随机入口不会因极端宽度 panic；多次取样并断言结果始终位于半开区间。
func TestCryptoInt64RangeSupportsExtremeBounds(t *testing.T) {
	for i := 0; i < 32; i++ {
		value, err := Int64RangeByCrypto(minInt64, maxInt64)
		if err != nil {
			t.Fatalf("Int64RangeByCrypto error: %v", err)
		}
		if value < minInt64 || value >= maxInt64 {
			t.Fatalf("Int64RangeByCrypto out of range: %d", value)
		}
	}
}

// TestMathInt64RangeKeepsSingleValueBoundary 验证最小值附近的单元素区间；调用后断言唯一合法结果为 MinInt64。
func TestMathInt64RangeKeepsSingleValueBoundary(t *testing.T) {
	value, err := NewMath(7).Int64Range(minInt64, minInt64+1)
	if err != nil {
		t.Fatalf("Math.Int64Range error: %v", err)
	}
	if value != minInt64 {
		t.Fatalf("expected %d, got %d", int64(minInt64), value)
	}
}

// TestCryptoInt64RangeKeepsSingleValueBoundary 验证最大值附近的单元素区间；调用后断言唯一合法结果为 MaxInt64-1。
func TestCryptoInt64RangeKeepsSingleValueBoundary(t *testing.T) {
	value, err := Int64RangeByCrypto(maxInt64-1, maxInt64)
	if err != nil {
		t.Fatalf("Int64RangeByCrypto error: %v", err)
	}
	if value != maxInt64-1 {
		t.Fatalf("expected %d, got %d", int64(maxInt64-1), value)
	}
}
