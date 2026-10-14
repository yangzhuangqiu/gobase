package nacos

import (
	"reflect"
	"testing"
)

// TestBootstrapEncodeFromSchema 验证结构化 bootstrap 可正确转换为 Provider 内部默认值 map。
// 测试方法：编码包含连接字段和 Sources 的 Bootstrap，断言映射结构完整且字段语义不变。
func TestBootstrapEncodeFromSchema(t *testing.T) {
	spec := Bootstrap{
		Address: "127.0.0.1:8848",
		Servers: []Server{
			{Host: "nacos-a", Port: 18848, Scheme: "https", ContextPath: "/custom", GrpcPort: 19848},
		},
		NamespaceID:        "public",
		TimeoutMs:          3000,
		DisableUseSnapshot: true,
		Sources: []BootstrapSource{
			{GroupID: "g1", DataID: "d1.yaml", Priority: 20, Optional: true},
			{GroupID: "g2", DataID: "d2.yaml"},
		},
	}

	got := newBootstrapCodec().Encode(spec)
	nacos, ok := got[ProviderName].(map[string]any)
	if !ok {
		t.Fatalf("expected nacos block, got: %#v", got[ProviderName])
	}
	if nacos["address"] != "127.0.0.1:8848" || nacos["namespace"] != "public" || nacos["timeoutMs"] != uint64(3000) || nacos["disableUseSnapshot"] != true {
		t.Fatalf("unexpected sdk options map: %#v", nacos)
	}
	wantServers := []any{map[string]any{"host": "nacos-a", "port": uint64(18848), "scheme": "https", "contextPath": "/custom", "grpcPort": uint64(19848)}}
	if !reflect.DeepEqual(nacos["servers"], wantServers) {
		t.Fatalf("unexpected servers map, want=%#v got=%#v", wantServers, nacos["servers"])
	}
	sources, ok := nacos["sources"].([]any)
	if !ok {
		t.Fatalf("expected sources []any, got: %#v", nacos["sources"])
	}
	wantSources := []any{
		map[string]any{"groupId": "g1", "dataId": "d1.yaml", "priority": 20, "optional": true},
		map[string]any{"groupId": "g2", "dataId": "d2.yaml"},
	}
	if !reflect.DeepEqual(sources, wantSources) {
		t.Fatalf("unexpected sources map, want=%#v got=%#v", wantSources, sources)
	}
}

// TestProviderConstructorRejectsIncompleteSource 验证 typed Source 缺少定位字段时 fail-fast。
// 测试方法：传入缺少 GroupID 的 Source，调用校验并断言稳定 source 错误码。
func TestProviderConstructorRejectsIncompleteSource(t *testing.T) {
	err := New(Bootstrap{
		Sources: []BootstrapSource{
			{GroupID: "", DataID: "d.yaml"},
		},
	}).ValidateBootstrap(nil)
	assertErrorCode(t, err, ErrCodeNacosInvalidSource)
}

// TestProviderConstructorBootstrapCanBeOverridden 验证构造 Bootstrap 是可被外部输入覆盖的默认值。
// 测试方法：构造 address 和 Sources 默认值，再用外部 address 覆盖，断言覆盖生效且未覆盖的 Sources 保留。
func TestProviderConstructorBootstrapCanBeOverridden(t *testing.T) {
	runtimeProvider := New(Bootstrap{
		Address: "code:8848",
		Sources: []BootstrapSource{{GroupID: "code", DataID: "application.yaml"}},
	})
	values := runtimeProvider.effectiveBootstrap(map[string]any{
		ProviderName: map[string]any{"address": "input:8848"},
	})
	spec, err := newBootstrapCodec().Decode(values)
	if err != nil {
		t.Fatalf("decode effective bootstrap failed: %v", err)
	}
	if spec == nil || spec.Address != "input:8848" {
		t.Fatalf("expected input address override, got=%#v", spec)
	}
	if len(spec.Sources) != 1 || spec.Sources[0].GroupID != "code" {
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

// TestBootstrapDecodeRejectsLossyTypes 验证 schema 声明的 string/int/bool/uint64 不接受隐式有损强转。
// 测试方法：分别输入非 string 定位字段、小数 priority/timeout 和数字 bool，断言 Decode 全部 fail-fast。
func TestBootstrapDecodeRejectsLossyTypes(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]any
	}{
		{name: "address_not_string", values: map[string]any{ProviderName: map[string]any{"address": true}}},
		{name: "source_id_not_string", values: map[string]any{ProviderName: map[string]any{"sources": []any{map[string]any{"groupId": 1, "dataId": "d"}}}}},
		{name: "priority_fraction", values: map[string]any{ProviderName: map[string]any{"sources": []any{map[string]any{"groupId": "g", "dataId": "d", "priority": 1.5}}}}},
		{name: "optional_number", values: map[string]any{ProviderName: map[string]any{"sources": []any{map[string]any{"groupId": "g", "dataId": "d", "optional": 1}}}}},
		{name: "timeout_fraction", values: map[string]any{ProviderName: map[string]any{"timeoutMs": 1.5}}},
		{name: "disable_snapshot_number", values: map[string]any{ProviderName: map[string]any{"disableUseSnapshot": 1}}},
		{name: "server_grpc_port_fraction", values: map[string]any{ProviderName: map[string]any{"servers": []any{map[string]any{"host": "127.0.0.1", "grpcPort": 1.5}}}}},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			if _, err := newBootstrapCodec().Decode(item.values); err == nil {
				t.Fatalf("expected strict bootstrap type error")
			}
		})
	}
}

// TestBootstrapDecodeRejectsInvalidServers 验证结构化节点在 bootstrap 阶段严格校验。
// 测试方法：覆盖缺少 host、未知字段、非法协议、端口越界及派生 gRPC 端口越界，断言统一返回 bootstrap 错误。
func TestBootstrapDecodeRejectsInvalidServers(t *testing.T) {
	// 构造覆盖结构、枚举和端口边界的无效节点集合。
	tests := []struct {
		name   string
		server map[string]any
	}{
		{name: "host_required", server: map[string]any{"port": 8848}},
		{name: "unknown_field", server: map[string]any{"host": "nacos", "zone": "a"}},
		{name: "unsupported_scheme", server: map[string]any{"host": "nacos", "scheme": "ftp"}},
		{name: "http_port_out_of_range", server: map[string]any{"host": "nacos", "port": 65536}},
		{name: "grpc_port_out_of_range", server: map[string]any{"host": "nacos", "grpcPort": 65536}},
		{name: "derived_grpc_port_out_of_range", server: map[string]any{"host": "nacos", "port": 65000}},
	}
	// 逐项通过 Decode 验证错误在 bootstrap 阶段收口，而不是延迟到 SDK 建连。
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			_, err := newBootstrapCodec().Decode(map[string]any{ProviderName: map[string]any{"servers": []any{item.server}}})
			assertErrorCode(t, err, ErrCodeNacosInvalidBootstrap)
		})
	}
}
