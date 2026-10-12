package radixx

import (
	"math"
	"testing"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

// TestBase62EncodeDecodeInt64 验证默认 Base62 对正数、零和负数可逆。
// 测试方法：表驱动编码多个 int64，再解码并与原值比较。
func TestBase62EncodeDecodeInt64(t *testing.T) {
	values := []int64{0, 1, 61, 62, 9999, -99}
	for _, value := range values {
		encoded := Base62EncodeInt64(value, 0)
		decoded, err := Base62DecodeInt64(encoded)
		if err != nil {
			t.Fatalf("decode error value=%d encoded=%s err=%v", value, encoded, err)
		}
		if decoded != value {
			t.Fatalf("roundtrip mismatch value=%d decoded=%d", value, decoded)
		}
	}
}

// TestBase62Padding 验证非负编码按最小宽度使用 alphabet 零位补齐。
// 测试方法：编码 10 且 pad=4，断言结果为 000a。
func TestBase62Padding(t *testing.T) {
	encoded := Base62EncodeInt64(10, 4)
	if encoded != "000a" {
		t.Fatalf("unexpected padded value: %s", encoded)
	}
}

// TestBase62DecodeInvalid 验证 alphabet 之外的 byte 被归为无效值。
// 测试方法：解码 @@，断言返回 KindInvalidValue。
func TestBase62DecodeInvalid(t *testing.T) {
	if _, err := Base62DecodeInt64("@@"); err == nil {
		t.Fatal("expected invalid char error")
	} else if !errorsx.IsKind(err, KindInvalidValue) {
		t.Fatalf("expected kind %s, got err=%v", KindInvalidValue, err)
	}
}

// TestEncodeDecodeWithCustomDigits 验证自定义二进制 alphabet 编解码可逆。
// 测试方法：用 digits=01 编码并解码十进制 10，断言文本 1010 和原值。
func TestEncodeDecodeWithCustomDigits(t *testing.T) {
	// 编码阶段：先检查文本表达，再用同一 alphabet 解码验证可逆性。
	encoded, err := EncodeInt64(10, "01", 0)
	if err != nil {
		t.Fatalf("EncodeInt64 error: %v", err)
	}
	if encoded != "1010" {
		t.Fatalf("unexpected encoded value: %s", encoded)
	}

	decoded, err := DecodeInt64(encoded, "01")
	if err != nil {
		t.Fatalf("DecodeInt64 error: %v", err)
	}
	if decoded != 10 {
		t.Fatalf("unexpected decoded value: %d", decoded)
	}
}

// TestCustomDigitsValidation 验证过短和重复 digits 统一归入配置错误。
// 测试方法：分别传入 "0" 和 "001"，断言 KindInvalidDigits。
func TestCustomDigitsValidation(t *testing.T) {
	if _, err := EncodeInt64(10, "0", 0); err == nil {
		t.Fatal("expected digits too short error")
	} else if !errorsx.IsKind(err, KindInvalidDigits) {
		t.Fatalf("expected kind %s, got err=%v", KindInvalidDigits, err)
	}
	if _, err := EncodeInt64(10, "001", 0); err == nil {
		t.Fatal("expected duplicate digit error")
	} else if !errorsx.IsKind(err, KindInvalidDigits) {
		t.Fatalf("expected kind %s, got err=%v", KindInvalidDigits, err)
	}
}

// TestCustomDigitsRejectReservedSign 验证自定义 alphabet 不能占用负号协议字符。
// 测试方法：使用包含 '-' 的 digits 编解码，断言均返回 KindInvalidDigits。
func TestCustomDigitsRejectReservedSign(t *testing.T) {
	if _, err := EncodeInt64(1, "0-1", 0); !errorsx.IsKind(err, KindInvalidDigits) {
		t.Fatalf("expected encode KindInvalidDigits, got %v", err)
	}
	if _, err := DecodeInt64("1", "0-1"); !errorsx.IsKind(err, KindInvalidDigits) {
		t.Fatalf("expected decode KindInvalidDigits, got %v", err)
	}
}

// TestBase62EncodeDecodeMinInt64 验证 MinInt64 不会在绝对值转换时溢出。
// 测试方法：编码并解码 math.MinInt64，断言完整往返。
func TestBase62EncodeDecodeMinInt64(t *testing.T) {
	encoded := Base62EncodeInt64(math.MinInt64, 0)
	decoded, err := Base62DecodeInt64(encoded)
	if err != nil {
		t.Fatalf("decode error value=%d encoded=%s err=%v", math.MinInt64, encoded, err)
	}
	if decoded != math.MinInt64 {
		t.Fatalf("roundtrip mismatch value=%d decoded=%d", math.MinInt64, decoded)
	}
}

// TestDecodeInt64Overflow 验证正负越界文本都返回稳定溢出 Kind。
// 测试方法：用十进制 alphabet 解码 MaxInt64+1 和 MinInt64-1，断言 KindOverflow。
func TestDecodeInt64Overflow(t *testing.T) {
	// 正负边界阶段分别构造越界一位的十进制文本。
	_, err := DecodeInt64("9223372036854775808", "0123456789")
	if err == nil {
		t.Fatal("expected positive overflow error")
	}
	if !errorsx.IsKind(err, KindOverflow) {
		t.Fatalf("expected kind %s, got err=%v", KindOverflow, err)
	}

	_, err = DecodeInt64("-9223372036854775809", "0123456789")
	if err == nil {
		t.Fatal("expected negative overflow error")
	}
	if !errorsx.IsKind(err, KindOverflow) {
		t.Fatalf("expected kind %s, got err=%v", KindOverflow, err)
	}
}
