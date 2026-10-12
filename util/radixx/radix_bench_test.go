package radixx

import "testing"

// BenchmarkBase62EncodeInt64 衡量默认 Base62 编码的分配和吞吐。
func BenchmarkBase62EncodeInt64(b *testing.B) {
	b.ReportAllocs()
	var value int64 = 123456789
	for index := 0; index < b.N; index++ {
		_ = Base62EncodeInt64(value+int64(index), 0)
	}
}

// BenchmarkBase62DecodeInt64 衡量预构建索引下的默认 Base62 解码成本。
func BenchmarkBase62DecodeInt64(b *testing.B) {
	encoded := Base62EncodeInt64(123456789, 0)
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		value, err := Base62DecodeInt64(encoded)
		if err != nil {
			b.Fatalf("Base62DecodeInt64 error: %v", err)
		}
		if value != 123456789 {
			b.Fatalf("unexpected decoded value: %d", value)
		}
	}
}

// BenchmarkEncodeInt64CustomDigits 衡量每次校验自定义 alphabet 的编码成本。
func BenchmarkEncodeInt64CustomDigits(b *testing.B) {
	digits := "0123456789abcdef"
	b.ReportAllocs()
	var value int64 = 0x7fffffffffffffff
	for index := 0; index < b.N; index++ {
		encoded, err := EncodeInt64(value-int64(index%1024), digits, 0)
		if err != nil {
			b.Fatalf("EncodeInt64 error: %v", err)
		}
		if encoded == "" {
			b.Fatal("empty encoded value")
		}
	}
}
