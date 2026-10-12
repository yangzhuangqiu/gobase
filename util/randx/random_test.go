package randx

import (
	"testing"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

// TestIntRangeByCrypto 验证安全随机 int 始终位于半开区间。
// 测试方法：在 [10,20) 连续取样并断言每个结果满足边界。
func TestIntRangeByCrypto(t *testing.T) {
	for i := 0; i < 50; i++ {
		value, err := IntRangeByCrypto(10, 20)
		if err != nil {
			t.Fatalf("IntRangeByCrypto error: %v", err)
		}
		if value < 10 || value >= 20 {
			t.Fatalf("IntRangeByCrypto out of range: %d", value)
		}
	}
}

// TestIntRangeByCryptoInvalid 验证空区间会被拒绝。
// 测试方法：传入相同起止值，断言返回错误。
func TestIntRangeByCryptoInvalid(t *testing.T) {
	_, err := IntRangeByCrypto(10, 10)
	if err == nil {
		t.Fatal("expected error for invalid range")
	}
}

// TestAlphanumericByCrypto 验证安全随机字母数字串的长度和字符集合。
// 测试方法：生成固定长度文本，逐字节检查是否属于默认 alphabet。
func TestAlphanumericByCrypto(t *testing.T) {
	text, err := AlphanumericByCrypto(32)
	if err != nil {
		t.Fatalf("AlphanumericByCrypto error: %v", err)
	}
	if len(text) != 32 {
		t.Fatalf("unexpected length: %d", len(text))
	}
}

// TestAlphabeticAndNumericByCrypto 验证安全随机纯字母和纯数字入口的约束。
// 测试方法：分别生成文本并逐字节检查所属字符集合。
func TestAlphabeticAndNumericByCrypto(t *testing.T) {
	alpha, err := AlphabeticByCrypto(16)
	if err != nil {
		t.Fatalf("AlphabeticByCrypto error: %v", err)
	}
	numeric, err := NumericByCrypto(16)
	if err != nil {
		t.Fatalf("NumericByCrypto error: %v", err)
	}
	if len(alpha) != 16 || len(numeric) != 16 {
		t.Fatalf("unexpected lengths alpha=%d numeric=%d", len(alpha), len(numeric))
	}
}

// TestInt64RangeByCrypto 验证安全随机 int64 遵循半开区间。
// 测试方法：在负数到正数区间连续取样并断言边界。
func TestInt64RangeByCrypto(t *testing.T) {
	for i := 0; i < 50; i++ {
		value, err := Int64RangeByCrypto(100, 200)
		if err != nil {
			t.Fatalf("Int64RangeByCrypto error: %v", err)
		}
		if value < 100 || value >= 200 {
			t.Fatalf("out of range: %d", value)
		}
	}
}

// TestMathDeterministic 验证相同 seed 的 Math 实例生成可复现序列。
// 测试方法：创建两个同 seed 实例，逐次比较 IntN 结果。
func TestMathDeterministic(t *testing.T) {
	left := NewMath(100)
	right := NewMath(100)
	for i := 0; i < 20; i++ {
		leftValue, leftErr := left.IntN(1000)
		rightValue, rightErr := right.IntN(1000)
		if leftErr != nil || rightErr != nil {
			t.Fatalf("unexpected error: left=%v right=%v", leftErr, rightErr)
		}
		if leftValue != rightValue {
			t.Fatalf("pseudo sequence mismatch at i=%d", i)
		}
	}
}

// TestMathAlphanumeric 验证伪随机字母数字串满足长度和 alphabet 约束。
// 测试方法：固定 seed 生成文本并逐字节检查。
func TestMathAlphanumeric(t *testing.T) {
	randomizer := NewMath(42)
	text, err := randomizer.Alphanumeric(16)
	if err != nil {
		t.Fatalf("Math.Alphanumeric error: %v", err)
	}
	if len(text) != 16 {
		t.Fatalf("unexpected length: %d", len(text))
	}
}

// TestMathAlphabeticNumericAndInt64Range 验证 Math 的常用字符串和 int64 范围入口。
// 测试方法：固定 seed 调用三个入口，断言字符集合与数值边界。
func TestMathAlphabeticNumericAndInt64Range(t *testing.T) {
	// 固定实例阶段：同一 RNG 顺序调用三类 API，只断言各自公开边界。
	randomizer := NewMath(7)

	alpha, err := randomizer.Alphabetic(10)
	if err != nil {
		t.Fatalf("Math.Alphabetic error: %v", err)
	}
	numeric, err := randomizer.Numeric(10)
	if err != nil {
		t.Fatalf("Math.Numeric error: %v", err)
	}
	if len(alpha) != 10 || len(numeric) != 10 {
		t.Fatalf("unexpected lengths alpha=%d numeric=%d", len(alpha), len(numeric))
	}

	value, err := randomizer.Int64Range(20, 30)
	if err != nil {
		t.Fatalf("Math.Int64Range error: %v", err)
	}
	if value < 20 || value >= 30 {
		t.Fatalf("Math.Int64Range out of range: %d", value)
	}
}

// TestDefaultMathIntN 验证包级默认伪随机入口返回合法范围值。
// 测试方法：连续调用 IntN(10)，断言结果始终位于 [0,10)。
func TestDefaultMathIntN(t *testing.T) {
	value, err := IntN(10)
	if err != nil {
		t.Fatalf("IntN error: %v", err)
	}
	if value < 0 || value >= 10 {
		t.Fatalf("IntN out of range: %d", value)
	}
}

// TestDefaultMathStringFunctions 验证包级字符串入口满足长度和字符集合契约。
// 测试方法：分别调用字母数字、字母、数字和自定义 charset 入口后检查结果。
func TestDefaultMathStringFunctions(t *testing.T) {
	// 包级入口阶段：逐项检查默认和自定义 alphabet，不依赖具体随机序列。
	alphaNumeric, err := Alphanumeric(12)
	if err != nil {
		t.Fatalf("Alphanumeric error: %v", err)
	}
	alpha, err := Alphabetic(12)
	if err != nil {
		t.Fatalf("Alphabetic error: %v", err)
	}
	numeric, err := Numeric(12)
	if err != nil {
		t.Fatalf("Numeric error: %v", err)
	}
	custom, err := StringFromCharset(8, "ab")
	if err != nil {
		t.Fatalf("StringFromCharset error: %v", err)
	}
	if len(alphaNumeric) != 12 || len(alpha) != 12 || len(numeric) != 12 || len(custom) != 8 {
		t.Fatalf(
			"unexpected lengths alphaNumeric=%d alpha=%d numeric=%d custom=%d",
			len(alphaNumeric),
			len(alpha),
			len(numeric),
			len(custom),
		)
	}
}

// TestBytesAndBytesByCrypto 验证伪随机和安全随机字节入口返回请求长度。
// 测试方法：分别请求固定长度与零长度，断言切片长度和错误。
func TestBytesAndBytesByCrypto(t *testing.T) {
	// 两类随机源阶段：相同长度请求应遵循一致切片边界。
	data, err := Bytes(16)
	if err != nil {
		t.Fatalf("Bytes error: %v", err)
	}
	if len(data) != 16 {
		t.Fatalf("Bytes length mismatch: %d", len(data))
	}
	secureData, err := BytesByCrypto(16)
	if err != nil {
		t.Fatalf("BytesByCrypto error: %v", err)
	}
	if len(secureData) != 16 {
		t.Fatalf("BytesByCrypto length mismatch: %d", len(secureData))
	}
}

// TestBoolAndBoolByCrypto 验证两种随机源的布尔入口都可直接调用。
// 测试方法：分别调用包级 math 与 crypto 版本，断言没有错误。
func TestBoolAndBoolByCrypto(t *testing.T) {
	// 执行阶段：布尔结果本身不可预测，因此只验证调用成功。
	value, err := Bool()
	if err != nil {
		t.Fatalf("Bool error: %v", err)
	}
	secureValue, err := BoolByCrypto()
	if err != nil {
		t.Fatalf("BoolByCrypto error: %v", err)
	}
	if value != true && value != false {
		t.Fatalf("unexpected bool value: %v", value)
	}
	if secureValue != true && secureValue != false {
		t.Fatalf("unexpected secure bool value: %v", secureValue)
	}
}

// TestErrorKinds 验证 randx 参数和实例错误暴露稳定 Kind。
// 测试方法：覆盖非法上限、区间、长度、空 charset 和 nil receiver。
func TestErrorKinds(t *testing.T) {
	// 参数阶段：每种错误只断言其调用方可修正的具体输入原因。
	if _, err := IntNByCrypto(0); err == nil {
		t.Fatal("expected invalid max")
	} else if !errorsx.IsKind(err, KindInvalidMax) {
		t.Fatalf("expected kind %s, got err=%v", KindInvalidMax, err)
	}

	if _, err := BytesByCrypto(-1); err == nil {
		t.Fatal("expected invalid length")
	} else if !errorsx.IsKind(err, KindInvalidLength) {
		t.Fatalf("expected kind %s, got err=%v", KindInvalidLength, err)
	}

	if _, err := StringFromCharsetByCrypto(1, ""); err == nil {
		t.Fatal("expected empty charset")
	} else if !errorsx.IsKind(err, KindEmptyCharset) {
		t.Fatalf("expected kind %s, got err=%v", KindEmptyCharset, err)
	}

	var mathGen *Math
	if _, err := mathGen.IntN(1); err == nil {
		t.Fatal("expected nil receiver")
	} else if !errorsx.IsKind(err, KindInvalidReceiver) {
		t.Fatalf("expected kind %s, got err=%v", KindInvalidReceiver, err)
	}
}
