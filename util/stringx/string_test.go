package stringx

import "testing"

type testStringer struct{}

// String 返回固定文本，验证 StringOf 优先遵循 fmt.Stringer。
func (this testStringer) String() string {
	return "stringer-value"
}

// TestStringOf 验证常见基础类型和 fmt.Stringer 的文本转换。
// 测试方法：分别输入 nil、整数、bytes 和 Stringer，断言固定结果。
func TestStringOf(t *testing.T) {
	if got := StringOf(nil); got != "" {
		t.Fatalf("StringOf nil mismatch: %q", got)
	}
	if got := StringOf(123); got != "123" {
		t.Fatalf("StringOf int mismatch: %q", got)
	}
	if got := StringOf([]byte("abc")); got != "abc" {
		t.Fatalf("StringOf []byte mismatch: %q", got)
	}
	if got := StringOf(testStringer{}); got != "stringer-value" {
		t.Fatalf("StringOf stringer mismatch: %q", got)
	}
}

// TestBeforeAfter 验证首次和末次分隔符的四种截取语义。
// 测试方法：使用邮箱与路径文本，断言分隔符前后结果。
func TestBeforeAfter(t *testing.T) {
	if got := Before("a@b.com", "@"); got != "a" {
		t.Fatalf("Before mismatch: %s", got)
	}
	if got := After("a@b.com", "@"); got != "b.com" {
		t.Fatalf("After mismatch: %s", got)
	}
	if got := AfterLast("a/b/c", "/"); got != "c" {
		t.Fatalf("AfterLast mismatch: %s", got)
	}
	if got := BeforeLast("a/b/c", "/"); got != "a/b" {
		t.Fatalf("BeforeLast mismatch: %s", got)
	}
}

// TestHasAnyEmpty 验证多参数空字符串检测。
// 测试方法：分别传入含空值和全非空集合，断言布尔结果。
func TestHasAnyEmpty(t *testing.T) {
	if !HasAnyEmpty("a", "", "c") {
		t.Fatal("expected true when empty exists")
	}
	if HasAnyEmpty("a", "b") {
		t.Fatal("expected false when no empty string")
	}
}

// TestBlankAndDefault 验证 Unicode 空白判断和默认值回退。
// 测试方法：输入空白、非空白文本，断言 IsBlank 与 DefaultIfBlank。
func TestBlankAndDefault(t *testing.T) {
	if !IsBlank("  \t") {
		t.Fatal("expected blank for whitespace")
	}
	if IsBlank(" a ") {
		t.Fatal("non-empty string should not be blank")
	}
	if got := DefaultIfBlank("   ", "x"); got != "x" {
		t.Fatalf("DefaultIfBlank mismatch: %s", got)
	}
	if got := DefaultIfBlank("ok", "x"); got != "ok" {
		t.Fatalf("DefaultIfBlank mismatch: %s", got)
	}
}

// TestParseWithError 验证非法数值文本不会返回伪默认值。
// 测试方法：分别解析非法 int 与 float 文本，断言均返回错误。
func TestParseWithError(t *testing.T) {
	if _, err := ParseInt("not-int"); err == nil {
		t.Fatal("expected ParseInt error")
	}
	if _, err := ParseFloat64("not-float"); err == nil {
		t.Fatal("expected ParseFloat64 error")
	}
}

// TestCamelCaseToSep 验证连续大写字母按当前 byte/rune 规则逐个拆分。
// 测试方法：输入 SendMasterID 和下划线，断言得到 send_master_i_d。
func TestCamelCaseToSep(t *testing.T) {
	got := CamelCaseToSep("SendMasterID", '_')
	if got != "send_master_i_d" {
		t.Fatalf("CamelCaseToSep mismatch: %s", got)
	}
}

// TestCopyStrings 验证切片副本不共享可修改元素槽位。
// 测试方法：修改复制结果首项，断言原切片保持不变。
func TestCopyStrings(t *testing.T) {
	original := []string{"a", "b", "c"}
	copied := CopyStrings(original)
	if len(copied) != len(original) {
		t.Fatalf("length mismatch: %d", len(copied))
	}
	copied[0] = "changed"
	if original[0] == copied[0] {
		t.Fatal("CopyStrings should return an independent slice")
	}
}

// TestJoinNonEmpty 验证空字符串会在拼接前被过滤。
// 测试方法：输入包含空项的切片，断言逗号拼接结果没有空段。
func TestJoinNonEmpty(t *testing.T) {
	got := JoinNonEmpty([]string{"a", "", "b", "c"}, ",")
	if got != "a,b,c" {
		t.Fatalf("JoinNonEmpty mismatch: %s", got)
	}
}

// TestJSONFallbackHelpers 验证显式吞错 JSON 入口的成功与失败返回值。
// 测试方法：序列化普通 map 后断言非空，再序列化 func 并断言空字符串/nil。
func TestJSONFallbackHelpers(t *testing.T) {
	// 成功与失败阶段分别验证非空输出和显式 fallback 值。
	raw := ToJSON(map[string]any{"a": 1})
	if raw == "" {
		t.Fatal("ToJSON should not be empty for valid object")
	}

	bytes := ToJSONBytes(map[string]any{"a": 1})
	if len(bytes) == 0 {
		t.Fatal("ToJSONBytes should not be empty for valid object")
	}
	if ToJSON(func() {}) != "" {
		t.Fatal("ToJSON should return empty string on marshal error")
	}
	if ToJSONBytes(func() {}) != nil {
		t.Fatal("ToJSONBytes should return nil on marshal error")
	}
}
