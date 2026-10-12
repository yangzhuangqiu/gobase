package app

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/yangzhuangqiu/gobase/errcode"
)

type customStringValue struct {
	// value 保存自定义 Pflag Value 的解析结果。
	value string
}

// String 返回自定义 Flag 当前文本值。
func (this *customStringValue) String() string {
	return this.value
}

// Set 记录自定义 Flag 的解析输入。
func (this *customStringValue) Set(value string) error {
	this.value = value
	return nil
}

// Type 返回自定义 Flag 类型名，用于覆盖未知类型回退分支。
func (this *customStringValue) Type() string {
	return "custom"
}

// TestReadFlagValueCoversAllTypeBranches 验证 readFlagValue 能按 flag 类型正确读取值并覆盖主分支。
//
// 测试方法：
// 1. 构造含多种类型 flag 的 FlagSet 并赋值；
// 2. 调用 readFlagValue 读取每个类型；
// 3. 断言返回值类型与语义符合预期。
func TestReadFlagValueCoversAllTypeBranches(t *testing.T) {
	fs := pflag.NewFlagSet("read-flag", pflag.ContinueOnError)
	fs.Bool("b", false, "")
	fs.Int("i", 0, "")
	fs.Int8("i8", 0, "")
	fs.Int16("i16", 0, "")
	fs.Int32("i32", 0, "")
	fs.Int64("i64", 0, "")
	fs.Uint("u", 0, "")
	fs.Uint16("u16", 0, "")
	fs.Uint32("u32", 0, "")
	fs.Uint64("u64", 0, "")
	fs.Float32("f32", 0, "")
	fs.Float64("f64", 0, "")
	fs.Duration("d", 0, "")
	fs.StringSlice("ss", nil, "")
	fs.StringArray("sa", nil, "")
	fs.IntSlice("is", nil, "")
	fs.BoolSlice("bs", nil, "")
	fs.String("s", "", "")
	fs.Count("count", "")
	custom := &customStringValue{}
	fs.Var(custom, "custom", "")

	_ = fs.Set("b", "true")
	_ = fs.Set("i", "1")
	_ = fs.Set("i8", "2")
	_ = fs.Set("i16", "3")
	_ = fs.Set("i32", "4")
	_ = fs.Set("i64", "5")
	_ = fs.Set("u", "6")
	_ = fs.Set("u16", "7")
	_ = fs.Set("u32", "8")
	_ = fs.Set("u64", "9")
	_ = fs.Set("f32", "1.5")
	_ = fs.Set("f64", "2.5")
	_ = fs.Set("d", "4s")
	_ = fs.Set("ss", "a,b")
	_ = fs.Set("sa", "x")
	_ = fs.Set("sa", "y")
	_ = fs.Set("is", "1,2")
	_ = fs.Set("bs", "true,false")
	_ = fs.Set("s", "hello")
	_ = fs.Set("count", "2")
	_ = fs.Set("custom", "custom-v")

	check := func(name, typ string) any {
		value, err := readFlagValue(fs, name, typ)
		if err != nil {
			t.Fatalf("readFlagValue(%s,%s) failed: %v", name, typ, err)
		}
		return value
	}
	if check("b", "bool") != true {
		t.Fatalf("bool branch mismatch")
	}
	if check("i", "int") != 1 {
		t.Fatalf("int branch mismatch")
	}
	if check("i8", "int8") != 2 || check("i16", "int16") != 3 || check("i32", "int32") != 4 {
		t.Fatalf("int8/int16/int32 branch mismatch")
	}
	if check("i64", "int64") != int64(5) {
		t.Fatalf("int64 branch mismatch")
	}
	if check("u", "uint") != uint(6) || check("u16", "uint16") != uint(7) || check("u32", "uint32") != uint(8) {
		t.Fatalf("uint branch mismatch")
	}
	if check("u64", "uint64") != uint64(9) {
		t.Fatalf("uint64 branch mismatch")
	}
	if check("f32", "float32") != float64(1.5) || check("f64", "float64") != 2.5 {
		t.Fatalf("float branch mismatch")
	}
	if check("d", "duration") != 4*time.Second {
		t.Fatalf("duration branch mismatch")
	}
	ss := check("ss", "stringSlice").([]string)
	if len(ss) != 2 || ss[0] != "a" || ss[1] != "b" {
		t.Fatalf("stringSlice mismatch: %#v", ss)
	}
	sa := check("sa", "stringArray").([]string)
	if len(sa) != 2 || sa[0] != "x" || sa[1] != "y" {
		t.Fatalf("stringArray mismatch: %#v", sa)
	}
	is := check("is", "intSlice").([]int)
	if len(is) != 2 || is[0] != 1 || is[1] != 2 {
		t.Fatalf("intSlice mismatch: %#v", is)
	}
	bs := check("bs", "boolSlice").([]bool)
	if len(bs) != 2 || bs[0] != true || bs[1] != false {
		t.Fatalf("boolSlice mismatch: %#v", bs)
	}
	if check("s", "string") != "hello" {
		t.Fatalf("string mismatch")
	}
	if check("count", "count") != 2 {
		t.Fatalf("count fallback mismatch")
	}
	if check("custom", "custom") != "custom-v" {
		t.Fatalf("custom fallback mismatch")
	}
}

// TestTryParseByTypeUnsupported 验证 tryParseByType 对不支持类型会返回可分支错误码。
//
// 测试方法：
// 1. 调用 tryParseByType 传入未知类型；
// 2. 断言返回 ErrCodeAppCLIFlagTypeUnsupported。
func TestTryParseByTypeUnsupported(t *testing.T) {
	_, err := tryParseByType("v", "unknown")
	if err == nil {
		t.Fatal("expected unsupported type error")
	}
	if !errcode.IsCode[ErrorCode](err, ErrCodeAppCLIFlagTypeUnsupported) {
		t.Fatalf("expected unsupported type code, got: %v", err)
	}
}
