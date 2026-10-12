package consul

import (
	"reflect"
	"testing"
)

// TestBootstrapEncodeFromSchema 验证 typed bootstrap 可正确转换为 Provider 内部默认值 map。
// 测试方法：编码完整 Bootstrap，断言连接字段和 Sources 的映射结果。
func TestBootstrapEncodeFromSchema(t *testing.T) {
	spec := Bootstrap{
		Address:    "127.0.0.1:8500",
		Scheme:     "https",
		Datacenter: "dc1",
		WaitTimeMs: 1500,
		Sources: []BootstrapSource{
			{Name: "app", Prefix: "config", Key: "app.yaml", Priority: 20, Optional: true},
			{Prefix: "feature", Key: "flags.yaml"},
		},
	}

	got := newBootstrapCodec().Encode(spec)
	consul, ok := got[ProviderName].(map[string]any)
	if !ok {
		t.Fatalf("expected consul block, got: %#v", got[ProviderName])
	}
	if consul["address"] != "127.0.0.1:8500" || consul["scheme"] != "https" || consul["datacenter"] != "dc1" {
		t.Fatalf("unexpected consul options map: %#v", consul)
	}
	if consul["waitTimeMs"] != uint64(1500) {
		t.Fatalf("unexpected waitTimeMs: %#v", consul["waitTimeMs"])
	}
	sources, ok := consul["sources"].([]any)
	if !ok {
		t.Fatalf("expected sources []any, got: %#v", consul["sources"])
	}
	wantSources := []any{
		map[string]any{"name": "app", "prefix": "config", "key": "app.yaml", "priority": 20, "optional": true},
		map[string]any{"name": "feature/flags.yaml", "prefix": "feature", "key": "flags.yaml"},
	}
	if !reflect.DeepEqual(sources, wantSources) {
		t.Fatalf("unexpected sources map, want=%#v got=%#v", wantSources, sources)
	}
}

// TestProviderConstructorRejectsIncompleteSource 验证 typed Source 缺少定位字段时 fail-fast。
// 测试方法：传入缺少 Prefix 的 Source，调用校验并断言稳定 source 错误码。
func TestProviderConstructorRejectsIncompleteSource(t *testing.T) {
	err := New(Bootstrap{
		Sources: []BootstrapSource{
			{Prefix: "", Key: "app.yaml"},
		},
	}).ValidateBootstrap(nil)
	assertErrorCode(t, err, ErrCodeConsulInvalidSource)
}

// TestProviderConstructorBootstrapCanBeOverridden 验证构造 Bootstrap 是可被外部输入覆盖的默认值。
// 测试方法：构造 address 和 Sources 默认值，再用外部 address 覆盖，断言覆盖生效且未覆盖的 Sources 保留。
func TestProviderConstructorBootstrapCanBeOverridden(t *testing.T) {
	runtimeProvider := New(Bootstrap{
		Address: "code:8500",
		Sources: []BootstrapSource{{Prefix: "code", Key: "application.yaml"}},
	})
	values := runtimeProvider.effectiveBootstrap(map[string]any{
		ProviderName: map[string]any{"address": "input:8500"},
	})
	spec, err := newBootstrapCodec().Decode(values)
	if err != nil {
		t.Fatalf("decode effective bootstrap failed: %v", err)
	}
	if spec == nil || spec.Address != "input:8500" {
		t.Fatalf("expected input address override, got=%#v", spec)
	}
	if len(spec.Sources) != 1 || spec.Sources[0].Prefix != "code" {
		t.Fatalf("expected constructor sources defaults to remain, got=%#v", spec.Sources)
	}
}

// TestProviderConstructorRejectsMultipleBootstrapValues 验证构造器最多接收一个 typed bootstrap。
// 测试方法：传入两个 Bootstrap，调用校验并断言声明错误在建连前返回。
func TestProviderConstructorRejectsMultipleBootstrapValues(t *testing.T) {
	err := New(Bootstrap{}, Bootstrap{}).ValidateBootstrap(nil)
	if err == nil {
		t.Fatalf("expected multiple bootstrap declaration error")
	}
}

// TestBootstrapDecodeRejectsLossyTypes 验证 Consul bootstrap 不接受与 schema 不一致或会溢出的值。
// 测试方法：输入非 string 字段、小数 priority/wait、数字 bool 和超大 wait，断言 Decode 全部 fail-fast。
func TestBootstrapDecodeRejectsLossyTypes(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]any
	}{
		{name: "address_not_string", values: map[string]any{ProviderName: map[string]any{"address": true}}},
		{name: "source_key_not_string", values: map[string]any{ProviderName: map[string]any{"sources": []any{map[string]any{"prefix": "p", "key": 1}}}}},
		{name: "priority_fraction", values: map[string]any{ProviderName: map[string]any{"sources": []any{map[string]any{"prefix": "p", "key": "k", "priority": 1.5}}}}},
		{name: "optional_number", values: map[string]any{ProviderName: map[string]any{"sources": []any{map[string]any{"prefix": "p", "key": "k", "optional": 1}}}}},
		{name: "wait_fraction", values: map[string]any{ProviderName: map[string]any{"waitTimeMs": 1.5}}},
		{name: "wait_overflow", values: map[string]any{ProviderName: map[string]any{"waitTimeMs": ^uint64(0)}}},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			if _, err := newBootstrapCodec().Decode(item.values); err == nil {
				t.Fatalf("expected strict bootstrap type error")
			}
		})
	}
}
