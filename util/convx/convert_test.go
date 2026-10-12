package convx

import (
	"math"
	"testing"
	"time"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

// TestToInt64 验证十进制文本能够无损转换为 int64。
// 测试方法：输入 "123"，断言结果为 123 且没有错误。
func TestToInt64(t *testing.T) {
	value, err := ToInt64("123")
	if err != nil {
		t.Fatalf("ToInt64 error: %v", err)
	}
	if value != 123 {
		t.Fatalf("ToInt64 mismatch: %d", value)
	}
}

// TestToFloat64 验证小数文本能够按 float64 语义解析。
// 测试方法：输入 "12.5"，断言结果和值均符合预期。
func TestToFloat64(t *testing.T) {
	value, err := ToFloat64("12.5")
	if err != nil {
		t.Fatalf("ToFloat64 error: %v", err)
	}
	if value != 12.5 {
		t.Fatalf("ToFloat64 mismatch: %f", value)
	}
}

// TestToFloat64Uint64Large 验证 uint64 转浮点遵循 Go 的有损转换而不误报 int64 溢出。
// 测试方法：输入 MaxUint64，断言结果等于原生 float64 转换结果。
func TestToFloat64Uint64Large(t *testing.T) {
	value, err := ToFloat64(uint64(math.MaxUint64))
	if err != nil {
		t.Fatalf("ToFloat64 error: %v", err)
	}
	if value != float64(uint64(math.MaxUint64)) {
		t.Fatalf("ToFloat64 mismatch: got=%f", value)
	}
}

// TestToBool 验证文本和整数 0/1 的布尔转换语义。
// 测试方法：分别输入 "true"、有符号 0 和无符号 1，断言对应布尔结果。
func TestToBool(t *testing.T) {
	// 文本与整数阶段分别验证标准解析和严格 0/1 语义。
	trueValue, err := ToBool("true")
	if err != nil {
		t.Fatalf("ToBool true error: %v", err)
	}
	if !trueValue {
		t.Fatal("ToBool true mismatch")
	}

	falseValue, err := ToBool(0)
	if err != nil {
		t.Fatalf("ToBool zero error: %v", err)
	}
	if falseValue {
		t.Fatal("ToBool false mismatch")
	}

	unsignedTrue, err := ToBool(uint64(1))
	if err != nil {
		t.Fatalf("ToBool uint64 one error: %v", err)
	}
	if !unsignedTrue {
		t.Fatal("ToBool uint64 one mismatch")
	}
}

// TestToBoolInvalidInt 验证非 0/1 整数不会被静默转换为布尔值。
// 测试方法：输入整数 2，断言返回 KindInvalidValue。
func TestToBoolInvalidInt(t *testing.T) {
	if _, err := ToBool(2); err == nil {
		t.Fatal("expected error for invalid bool integer")
	} else if !errorsx.IsKind(err, KindInvalidValue) {
		t.Fatalf("expected kind %s, got err=%v", KindInvalidValue, err)
	}
}

// TestToDuration 验证 duration 支持标准单位、min 扩展和毫秒整数语义。
// 测试方法：表驱动输入 string、[]byte 和 int64，断言转换后的时长。
func TestToDuration(t *testing.T) {
	// 准备阶段：覆盖标准单位、min 扩展、纯数字毫秒、整数和 bytes。
	cases := []struct {
		input any
		want  time.Duration
	}{
		{input: "1h", want: time.Hour},
		{input: "1min", want: time.Minute},
		{input: "1s", want: time.Second},
		{input: "1ms", want: time.Millisecond},
		{input: "1500", want: 1500 * time.Millisecond},
		{input: int64(1500), want: 1500 * time.Millisecond},
		{input: []byte("2s"), want: 2 * time.Second},
	}

	for _, testCase := range cases {
		got, err := ToDuration(testCase.input)
		if err != nil {
			t.Fatalf("ToDuration error input=%v err=%v", testCase.input, err)
		}
		if got != testCase.want {
			t.Fatalf("ToDuration mismatch input=%v got=%s want=%s", testCase.input, got, testCase.want)
		}
	}
}

// TestToDurationRejectsMillisecondOverflow 验证毫秒乘法在超出 time.Duration 范围前失败。
// 测试方法：分别输入越界整数和纯数字文本，断言返回 KindOverflow 而不是环绕值。
func TestToDurationRejectsMillisecondOverflow(t *testing.T) {
	overflowMillis := int64(math.MaxInt64/int64(time.Millisecond)) + 1
	for _, input := range []any{overflowMillis, "9223372036855"} {
		if _, err := ToDuration(input); err == nil {
			t.Fatalf("expected overflow for input=%v", input)
		} else if !errorsx.IsKind(err, KindOverflow) {
			t.Fatalf("expected kind %s input=%v err=%v", KindOverflow, input, err)
		}
	}
}

// TestToDurationRejectsNumericStringOverflow 验证超出 int64 的纯数字文本不会降级为普通解析错误。
// 测试方法：输入超大毫秒数字文本，断言返回 KindOverflow。
func TestToDurationRejectsNumericStringOverflow(t *testing.T) {
	_, err := ToDuration("999999999999999999999999")
	if !errorsx.IsKind(err, KindOverflow) {
		t.Fatalf("expected KindOverflow, got %v", err)
	}
}

// TestToDurationAllowsMillisecondBoundary 验证最大安全毫秒值仍可成功转换。
// 测试方法：输入 MaxInt64/time.Millisecond，断言结果等于精确乘积。
func TestToDurationAllowsMillisecondBoundary(t *testing.T) {
	maxMillis := int64(math.MaxInt64 / int64(time.Millisecond))
	value, err := ToDuration(maxMillis)
	if err != nil {
		t.Fatalf("ToDuration boundary error: %v", err)
	}
	if value != time.Duration(maxMillis)*time.Millisecond {
		t.Fatalf("ToDuration boundary mismatch: %s", value)
	}
}

// TestToDurationInvalid 验证非法 duration 文本返回稳定解析 Kind。
// 测试方法：输入 "bad"，断言返回 KindParseError。
func TestToDurationInvalid(t *testing.T) {
	if _, err := ToDuration("bad"); err == nil {
		t.Fatal("expected parse error for invalid duration string")
	} else if !errorsx.IsKind(err, KindParseError) {
		t.Fatalf("expected kind %s, got err=%v", KindParseError, err)
	}
}

// TestErrorKinds 验证转换失败被归入调用方可处理的稳定 Kind。
// 测试方法：覆盖解析失败、不合法值、不支持类型和空文本四条失败路径。
func TestErrorKinds(t *testing.T) {
	// 断言阶段：每类失败只检查调用方可采取动作的稳定 Kind。
	if _, err := ToInt64("bad"); err == nil {
		t.Fatal("expected parse error")
	} else if !errorsx.IsKind(err, KindParseError) {
		t.Fatalf("expected kind %s, got err=%v", KindParseError, err)
	}

	if _, err := ToBool(2); err == nil {
		t.Fatal("expected invalid integer error")
	} else if !errorsx.IsKind(err, KindInvalidValue) {
		t.Fatalf("expected kind %s, got err=%v", KindInvalidValue, err)
	}

	if _, err := ToDuration(struct{}{}); err == nil {
		t.Fatal("expected unsupported type error")
	} else if !errorsx.IsKind(err, KindUnsupportedType) {
		t.Fatalf("expected kind %s, got err=%v", KindUnsupportedType, err)
	}

	if _, err := ToDuration("   "); err == nil {
		t.Fatal("expected empty string error")
	} else if !errorsx.IsKind(err, KindInvalidValue) {
		t.Fatalf("expected kind %s, got err=%v", KindInvalidValue, err)
	}
}
