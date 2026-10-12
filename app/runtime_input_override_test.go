package app

import (
	"reflect"
	"testing"
)

// TestBuildPinnedValuesExplicitFlagsWin 验证显式 Config flags 高于通用 --override。
//
// 测试方法：
// 1. 为同一 dotted key 构造 override 与 explicit 输入；
// 2. 调用 buildPinnedValues；
// 3. 断言显式值覆盖 override，同时保留其他 override。
func TestBuildPinnedValuesExplicitFlagsWin(t *testing.T) {
	explicit := map[string]any{"service": map[string]any{"name": "explicit"}}
	overrides := []keyValueOverride{
		{Key: "service.name", Val: "override"},
		{Key: "service.port", Val: int64(8080)},
	}
	values, err := buildPinnedValues(explicit, overrides)
	if err != nil {
		t.Fatalf("build pinned failed: %v", err)
	}
	expect := map[string]any{"service": map[string]any{"name": "explicit", "port": int64(8080)}}
	if !reflect.DeepEqual(values, expect) {
		t.Fatalf("unexpected pinned values: expect=%#v actual=%#v", expect, values)
	}
}

// TestParseLiteralValuePreservesSemanticTypes 验证 CLI 字面量的类型恢复与前导零保留规则。
//
// 测试方法：
// 1. 输入 bool、整数、浮点、null、前导零和普通字符串；
// 2. 调用 parseLiteralValue；
// 3. 断言得到稳定业务类型。
func TestParseLiteralValuePreservesSemanticTypes(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		expect any
	}{
		{name: "bool", raw: "true", expect: true},
		{name: "integer", raw: "42", expect: int64(42)},
		{name: "float", raw: "1.5", expect: 1.5},
		{name: "null", raw: "null", expect: nil},
		{name: "leading-zero", raw: "001", expect: "001"},
		{name: "string", raw: "demo", expect: "demo"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := parseLiteralValue(test.raw); !reflect.DeepEqual(actual, test.expect) {
				t.Fatalf("unexpected literal: raw=%s expect=%#v actual=%#v", test.raw, test.expect, actual)
			}
		})
	}
}
