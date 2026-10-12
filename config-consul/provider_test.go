package consul

import (
	"context"
	"reflect"
	"testing"

	"github.com/yangzhuangqiu/gobase/config/provider"
)

// TestParseBootstrapSupportsMapSources 验证 map 形式 sources 可被解析。
func TestParseBootstrapSupportsMapSources(t *testing.T) {
	runtimeProvider := New()
	binding, err := runtimeProvider.parseBootstrap(map[string]any{
		"consul": map[string]any{
			"address": "127.0.0.1:8500",
			"sources": []any{
				map[string]any{"name": "app", "prefix": "config", "key": "app.yaml", "priority": 20, "optional": true},
			},
		},
	})
	if err != nil {
		t.Fatalf("parse bootstrap failed: %v", err)
	}
	if binding == nil {
		t.Fatalf("expected non-nil binding")
	}
	if binding.ClientOptions == nil || binding.ClientOptions.Address != "127.0.0.1:8500" {
		t.Fatalf("unexpected options parsed: %#v", binding.ClientOptions)
	}
	if len(binding.Sources) != 1 {
		t.Fatalf("expected one source, got: %d", len(binding.Sources))
	}
	item := binding.Sources[0]
	if item.Name != "app" || item.Prefix != "config" || item.Key != "app.yaml" || item.Priority != 20 || !item.Optional {
		t.Fatalf("unexpected source parsed: %#v", item)
	}
}

// TestParseBootstrapRejectsStringSourceSpec 验证字符串 source 声明已不再支持。
func TestParseBootstrapRejectsStringSourceSpec(t *testing.T) {
	runtimeProvider := New()
	_, err := runtimeProvider.parseBootstrap(map[string]any{
		"consul": map[string]any{
			"sources": []any{"name=app,prefix=config,key=app.yaml"},
		},
	})
	if err == nil {
		t.Fatalf("expected parse bootstrap error")
	}
	assertErrorCode(t, err, ErrCodeConsulInvalidSource)
	assertErrorContains(t, err, "expect map")
}

// TestParseBootstrapFailsOnUnknownField 验证未知 bootstrap 字段 fail fast。
func TestParseBootstrapFailsOnUnknownField(t *testing.T) {
	runtimeProvider := New()
	_, err := runtimeProvider.parseBootstrap(map[string]any{
		"consul": map[string]any{"invalidField": "x"},
	})
	if err == nil {
		t.Fatalf("expected parse bootstrap error")
	}
	assertErrorCode(t, err, ErrCodeConsulInvalidBootstrap)
	assertErrorContains(t, err, "invalid consul bootstrap field")
}

// TestOpenRejectsMultipleConnectors 验证 Client 与 ClientOptions 互斥校验。
func TestOpenRejectsMultipleConnectors(t *testing.T) {
	runtimeProvider := New()
	_, err := runtimeProvider.buildSession(&binding{
		Sources:       []BootstrapSource{{Name: "app", Prefix: "config", Key: "app.yaml"}},
		Client:        &fakeProviderClient{},
		ClientOptions: &ClientOptions{Address: "127.0.0.1:8500"},
	})
	if err == nil {
		t.Fatalf("expected build plan error")
	}
	assertErrorCode(t, err, ErrCodeConsulInvalidBinding)
}

// TestOpenReturnsClientWhenExplicitInjected 验证显式注入 client 会被直接透传。
func TestOpenReturnsClientWhenExplicitInjected(t *testing.T) {
	client := &fakeProviderClient{}
	runtimeProvider := New()
	opened, err := runtimeProvider.buildSession(&binding{
		Sources: []BootstrapSource{{Name: "app", Prefix: "config", Key: "app.yaml"}},
		Client:  client,
	})
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}
	if opened.(*session).client != client {
		t.Fatalf("expected same client instance")
	}
}

// TestOpenBuildsClientWhenUsingOptions 验证 ClientOptions 路径会创建 Consul SDK client。
func TestOpenBuildsClientWhenUsingOptions(t *testing.T) {
	runtimeProvider := New()
	opened, err := runtimeProvider.buildSession(&binding{
		Sources:       []BootstrapSource{{Name: "app", Prefix: "config", Key: "app.yaml"}},
		ClientOptions: &ClientOptions{Address: "127.0.0.1:8500"},
	})
	if err != nil {
		t.Fatalf("expected build plan success, got: %v", err)
	}
	if opened.(*session).client == nil {
		t.Fatalf("expected consul client to be built")
	}
}

// TestNewWithClientRejectsTypedNilClient 验证已有连接入口不会接受装入接口的 typed nil。
// 测试方法：传入 nil *fakeProviderClient 和有效 Source，调用 Open 并断言稳定 binding 错误。
func TestNewWithClientRejectsTypedNilClient(t *testing.T) {
	var client *fakeProviderClient
	_, err := NewWithClient(client, BootstrapSource{Prefix: "p", Key: "k"}).Open(context.Background(), nil)
	assertErrorCode(t, err, ErrCodeConsulInvalidBinding)
	assertErrorContains(t, err, "reason=client_required")
}

// TestNewWithClientRejectsBootstrapConnector 验证显式 Client 与 bootstrap.consul 不会形成静默覆盖。
// 测试方法：使用 borrowed Client 创建 Provider，再提供 consul bootstrap，断言 validate 阶段返回互斥错误。
func TestNewWithClientRejectsBootstrapConnector(t *testing.T) {
	item := NewWithClient(&fakeProviderClient{}, BootstrapSource{Prefix: "p", Key: "k"})
	err := item.ValidateBootstrap(map[string]any{"consul": map[string]any{"address": "127.0.0.1:8500"}})
	assertErrorCode(t, err, ErrCodeConsulInvalidBinding)
	assertErrorContains(t, err, "reason=bootstrap_not_allowed_with_client")
}

// TestLoadBootstrapFromEnvBySchemaLoadsConsulScalars 验证 schema 自动映射会加载标量字段。
func TestLoadBootstrapFromEnvBySchemaLoadsConsulScalars(t *testing.T) {
	t.Setenv("APP_CONSUL_ADDRESS", "127.0.0.1:8500")
	t.Setenv("APP_CONSUL_WAIT_TIME_MS", "45000")
	t.Setenv("APP_CONSUL_NAMESPACE", "prod")

	runtimeProvider := New()
	values := provider.LoadBootstrapFromEnvBySchema("APP_", runtimeProvider.BootstrapSchema())

	consulBlock, ok := values["consul"].(map[string]any)
	if !ok {
		t.Fatalf("expected consul block, got: %#v", values["consul"])
	}
	if consulBlock["address"] != "127.0.0.1:8500" {
		t.Fatalf("expected address from schema env mapper, got: %#v", consulBlock["address"])
	}
	if consulBlock["waitTimeMs"] != uint64(45000) {
		t.Fatalf("expected waitTimeMs uint64 value, got: %#v", consulBlock["waitTimeMs"])
	}
	if consulBlock["namespace"] != "prod" {
		t.Fatalf("expected namespace from schema env mapper, got: %#v", consulBlock["namespace"])
	}
}

// TestLoadBootstrapFromEnvLoadsConsulSources 验证可选 loader 负责 indexed source 映射。
func TestLoadBootstrapFromEnvLoadsConsulSources(t *testing.T) {
	t.Setenv("APP_CONSUL_SOURCE_0_PREFIX", "p1")
	t.Setenv("APP_CONSUL_SOURCE_0_KEY", "k1.yaml")
	t.Setenv("APP_CONSUL_SOURCE_0_NAME", "a")
	t.Setenv("APP_CONSUL_SOURCE_1_PREFIX", "p2")
	t.Setenv("APP_CONSUL_SOURCE_1_KEY", "k2.yaml")
	t.Setenv("APP_CONSUL_SOURCE_1_NAME", "b")
	runtimeProvider := New()
	values := runtimeProvider.LoadBootstrapFromEnv("APP_")

	consulBlock, ok := values["consul"].(map[string]any)
	if !ok {
		t.Fatalf("expected consul block, got: %#v", values["consul"])
	}
	if _, exists := consulBlock["address"]; exists {
		t.Fatalf("expected scalar field address to be handled by schema mapper, got: %#v", consulBlock["address"])
	}
	rawSources, ok := consulBlock["sources"].([]any)
	if !ok {
		t.Fatalf("expected []any sources, got: %#v", consulBlock["sources"])
	}
	sources := make([]map[string]any, 0, len(rawSources))
	for _, item := range rawSources {
		typed, cast := item.(map[string]any)
		if !cast {
			t.Fatalf("expected source map, got: %#v", item)
		}
		sources = append(sources, typed)
	}
	want := []map[string]any{
		{"name": "a", "prefix": "p1", "key": "k1.yaml"},
		{"name": "b", "prefix": "p2", "key": "k2.yaml"},
	}
	if !reflect.DeepEqual(sources, want) {
		t.Fatalf("unexpected env sources: %#v", sources)
	}
}

// TestCollectEnvConsulSourcesSupportsIndexedKeys 验证编号环境变量结构化映射语义。
func TestCollectEnvConsulSourcesSupportsIndexedKeys(t *testing.T) {
	t.Setenv("APP_CONSUL_SOURCE_0_PREFIX", "p1")
	t.Setenv("APP_CONSUL_SOURCE_0_KEY", "k1.yaml")
	t.Setenv("APP_CONSUL_SOURCE_1_PREFIX", "p2")
	t.Setenv("APP_CONSUL_SOURCE_1_KEY", "k2.yaml")
	t.Setenv("APP_CONSUL_SOURCE_3_PREFIX", "ignored")
	t.Setenv("APP_CONSUL_SOURCE_3_KEY", "ignored.yaml")
	got := collectEnvConsulSources("APP_")
	want := []map[string]any{
		{"prefix": "p1", "key": "k1.yaml"},
		{"prefix": "p2", "key": "k2.yaml"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected indexed sources, want=%#v got=%#v", want, got)
	}
}

// TestLoadBootstrapFromEnvIgnoresEmptyValues 验证空环境变量不会污染结果。
func TestLoadBootstrapFromEnvIgnoresEmptyValues(t *testing.T) {
	t.Setenv("APP_CONSUL_SOURCE_0_PREFIX", "")
	runtimeProvider := New()
	values := runtimeProvider.LoadBootstrapFromEnv("APP_")
	if _, ok := values["consul"]; ok {
		t.Fatalf("expected empty env to be ignored")
	}
}

// TestValidateBootstrapRejectsUnknownField 验证 ValidateBootstrap 走结构校验并返回错误。
func TestValidateBootstrapRejectsUnknownField(t *testing.T) {
	runtimeProvider := New()
	err := runtimeProvider.ValidateBootstrap(map[string]any{
		"consul": map[string]any{"unknownField": "x"},
	})
	if err == nil {
		t.Fatalf("expected validate bootstrap error")
	}
	assertErrorCode(t, err, ErrCodeConsulInvalidBootstrap)
	assertErrorContains(t, err, "invalid consul bootstrap field")
}

// TestOpenBuildsSDKClient 验证 Open 会构建可用 plan。
func TestOpenBuildsSDKClient(t *testing.T) {
	runtimeProvider := New()
	opened, err := runtimeProvider.Open(context.Background(), map[string]any{
		"consul": map[string]any{
			"address": "127.0.0.1:8500",
			"sources": []any{
				map[string]any{
					"prefix": "config",
					"key":    "app.yaml",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("build plan from bootstrap failed: %v", err)
	}
	runtimeSession := opened.(*session)
	if runtimeSession.client == nil {
		t.Fatalf("expected non-nil client in plan")
	}
	if len(runtimeSession.sources) != 1 || runtimeSession.sources[0].Info().Name != "config/app.yaml" {
		t.Fatalf("unexpected sources: %#v", runtimeSession.sources)
	}
}
