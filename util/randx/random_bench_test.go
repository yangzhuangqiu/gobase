package randx

import (
	"strings"
	"testing"
)

// BenchmarkAlphanumericDefaultLocked 衡量包级 mutex 保护下的伪随机字符串成本。
func BenchmarkAlphanumericDefaultLocked(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		value, err := Alphanumeric(16)
		if err != nil {
			b.Fatalf("Alphanumeric error: %v", err)
		}
		if len(value) != 16 {
			b.Fatalf("unexpected length: %d", len(value))
		}
	}
}

// BenchmarkAlphanumericNewMathReuse 衡量调用方复用私有 Math 实例的热点成本。
func BenchmarkAlphanumericNewMathReuse(b *testing.B) {
	randomizer := NewMath(42)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		value, err := randomizer.Alphanumeric(16)
		if err != nil {
			b.Fatalf("Math.Alphanumeric error: %v", err)
		}
		if len(value) != 16 {
			b.Fatalf("unexpected length: %d", len(value))
		}
	}
}

// BenchmarkAlphanumericNewMathPerCall 衡量每次创建 Math 的分配代价，防止误用为推荐路径。
func BenchmarkAlphanumericNewMathPerCall(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		randomizer := NewMath(int64(index) + 1)
		value, err := randomizer.Alphanumeric(16)
		if err != nil {
			b.Fatalf("Math.Alphanumeric error: %v", err)
		}
		if len(value) != 16 {
			b.Fatalf("unexpected length: %d", len(value))
		}
	}
}

// BenchmarkStringFromCharsetByCrypto 衡量常规 byte alphabet 的批量安全随机取样成本。
func BenchmarkStringFromCharsetByCrypto(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		value, err := StringFromCharsetByCrypto(32, alphanumericCharset)
		if err != nil {
			b.Fatalf("StringFromCharsetByCrypto error: %v", err)
		}
		if len(value) != 32 {
			b.Fatalf("unexpected length: %d", len(value))
		}
	}
}

// BenchmarkStringFromCharsetByCryptoLargeCharset 衡量超过 256-byte alphabet 的逐位 big.Int 路径成本。
func BenchmarkStringFromCharsetByCryptoLargeCharset(b *testing.B) {
	largeCharset := buildLargeCharset(260)
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		value, err := StringFromCharsetByCrypto(32, largeCharset)
		if err != nil {
			b.Fatalf("StringFromCharsetByCrypto error: %v", err)
		}
		if len(value) != 32 {
			b.Fatalf("unexpected length: %d", len(value))
		}
	}
}

// buildLargeCharset 构造指定长度的重复 byte alphabet，仅用于覆盖大 charset 基准分支。
func buildLargeCharset(length int) string {
	base := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()_+-=[]{}|;:,.<>?/`~"
	if length <= len(base) {
		return base[:length]
	}
	repeat := length/len(base) + 1
	return strings.Repeat(base, repeat)[:length]
}
