package nacos

import (
	"context"
	"reflect"
	"testing"

	"github.com/yangzhuangqiu/gobase/config/provider"
)

// TestParseBootstrapSupportsStructuredSources 验证 sources 结构化声明可被解析。
func TestParseBootstrapSupportsStructuredSources(t *testing.T) {
	runtimeProvider := New()
	binding, err := runtimeProvider.parseBootstrap(map[string]any{
		"nacos": map[string]any{
			"address": "127.0.0.1:8848",
			"sources": []any{
				map[string]any{"groupId": "g", "dataId": "d.yaml", "priority": 20, "optional": true},
			},
		},
	})
	if err != nil {
		t.Fatalf("parse bootstrap failed: %v", err)
	}
	if binding == nil {
		t.Fatalf("expected non-nil binding")
	}
	if binding.ClientOptions == nil || binding.ClientOptions.Address != "127.0.0.1:8848" {
		t.Fatalf("expected sdk address, got: %#v", binding.ClientOptions)
	}
	if len(binding.Sources) != 1 {
		t.Fatalf("expected one source, got: %d", len(binding.Sources))
	}
	item := binding.Sources[0]
	if item.Info().Name != "g/d.yaml" || item.GroupID != "g" || item.DataID != "d.yaml" || item.Priority != 20 || !item.Optional {
		t.Fatalf("unexpected source parsed: %#v", item)
	}
}

// TestParseBootstrapRejectsStringSourceSpec 验证字符串 source 声明已不再支持。
func TestParseBootstrapRejectsStringSourceSpec(t *testing.T) {
	runtimeProvider := New()
	_, err := runtimeProvider.parseBootstrap(map[string]any{
		"nacos": map[string]any{
			"sources": []any{"name=app,group=g,data=d.yaml"},
		},
	})
	if err == nil {
		t.Fatalf("expected parse bootstrap error")
	}
	assertErrorCode(t, err, ErrCodeNacosInvalidSource)
	assertErrorContains(t, err, "expect map")
}

// TestParseBootstrapRejectsNameFieldInSource 验证 source.name 字段已被移除。
func TestParseBootstrapRejectsNameFieldInSource(t *testing.T) {
	runtimeProvider := New()
	_, err := runtimeProvider.parseBootstrap(map[string]any{
		"nacos": map[string]any{
			"sources": []any{
				map[string]any{
					"name":    "app",
					"groupId": "g",
					"dataId":  "d.yaml",
				},
			},
		},
	})
	if err == nil {
		t.Fatalf("expected parse bootstrap error")
	}
	assertErrorCode(t, err, ErrCodeNacosInvalidSource)
	assertErrorContains(t, err, "invalid nacos source field name")
}

// TestParseBootstrapFailsOnUnknownField 验证未知 bootstrap 字段 fail fast。
func TestParseBootstrapFailsOnUnknownField(t *testing.T) {
	runtimeProvider := New()
	_, err := runtimeProvider.parseBootstrap(map[string]any{
		"nacos": map[string]any{"invalidField": "x"},
	})
	if err == nil {
		t.Fatalf("expected parse bootstrap error")
	}
	assertErrorCode(t, err, ErrCodeNacosInvalidBootstrap)
	assertErrorContains(t, err, "invalid nacos bootstrap field")
}

// TestOpenRejectsMultipleConnectors 验证 Client 与 SDKOptions 互斥校验。
func TestOpenRejectsMultipleConnectors(t *testing.T) {
	runtimeProvider := New()
	_, err := runtimeProvider.buildSession(&binding{
		Sources:       []BootstrapSource{{GroupID: "g", DataID: "d"}},
		Client:        &fakeProviderClient{},
		ClientOptions: &ClientOptions{Address: "127.0.0.1:8848"},
	})
	if err == nil {
		t.Fatalf("expected build plan error")
	}
	assertErrorCode(t, err, ErrCodeNacosInvalidBinding)
}

// TestOpenReturnsClientWhenExplicitInjected 验证显式注入 client 会被直接透传。
func TestOpenReturnsClientWhenExplicitInjected(t *testing.T) {
	client := &fakeProviderClient{}
	runtimeProvider := New()
	opened, err := runtimeProvider.buildSession(&binding{
		Sources: []BootstrapSource{{GroupID: "g", DataID: "d"}},
		Client:  client,
	})
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}
	runtimeSession := opened.(*session)
	if runtimeSession.client != client {
		t.Fatalf("expected same client instance")
	}
	if len(runtimeSession.sources) != 1 || runtimeSession.sources[0].Info().Name != "g/d" {
		t.Fatalf("expected derived source name g/d, got: %#v", runtimeSession.sources)
	}
}

// TestOpenRejectsSourceWithoutRequiredKeys 验证显式 binding 缺少 groupId/dataId 会失败。
func TestOpenRejectsSourceWithoutRequiredKeys(t *testing.T) {
	runtimeProvider := New()
	_, err := runtimeProvider.buildSession(&binding{
		Sources: []BootstrapSource{{GroupID: "", DataID: "d"}},
		Client:  &fakeProviderClient{},
	})
	if err == nil {
		t.Fatalf("expected build plan error")
	}
	assertErrorCode(t, err, ErrCodeNacosInvalidSource)
}

// TestNewWithClientRejectsTypedNilClient 验证已有连接入口不会接受装入接口的 typed nil。
// 测试方法：传入 nil *fakeProviderClient 和有效 Source，调用 Open 并断言稳定 binding 错误。
func TestNewWithClientRejectsTypedNilClient(t *testing.T) {
	var client *fakeProviderClient
	_, err := NewWithClient(client, BootstrapSource{GroupID: "g", DataID: "d"}).Open(context.Background(), nil)
	assertErrorCode(t, err, ErrCodeNacosInvalidBinding)
	assertErrorContains(t, err, "reason=client_required")
}

// TestNewWithClientRejectsBootstrapConnector 验证显式 Client 与 bootstrap.nacos 不会形成静默覆盖。
// 测试方法：使用 borrowed Client 创建 Provider，再提供 nacos bootstrap，断言 validate 阶段返回互斥错误。
func TestNewWithClientRejectsBootstrapConnector(t *testing.T) {
	item := NewWithClient(&fakeProviderClient{}, BootstrapSource{GroupID: "g", DataID: "d"})
	err := item.ValidateBootstrap(map[string]any{"nacos": map[string]any{"address": "127.0.0.1:8848"}})
	assertErrorCode(t, err, ErrCodeNacosInvalidBinding)
	assertErrorContains(t, err, "reason=bootstrap_not_allowed_with_client")
}

// TestLoadBootstrapFromEnvBySchemaLoadsNacosScalars 验证 schema 自动映射会加载标量字段。
func TestLoadBootstrapFromEnvBySchemaLoadsNacosScalars(t *testing.T) {
	t.Setenv("APP_NACOS_ADDRESS", "127.0.0.1:8848")
	t.Setenv("APP_NACOS_TIMEOUT_MS", "10001")
	t.Setenv("APP_NACOS_LOG_LEVEL", "info")

	runtimeProvider := New()
	values := provider.LoadBootstrapFromEnvBySchema("APP_", runtimeProvider.BootstrapSchema())

	nacosBlock, ok := values["nacos"].(map[string]any)
	if !ok {
		t.Fatalf("expected nacos block, got: %#v", values["nacos"])
	}
	if nacosBlock["address"] != "127.0.0.1:8848" {
		t.Fatalf("expected address from schema env mapper, got: %#v", nacosBlock["address"])
	}
	if nacosBlock["timeoutMs"] != uint64(10001) {
		t.Fatalf("expected timeoutMs uint64 value, got: %#v", nacosBlock["timeoutMs"])
	}
	if nacosBlock["logLevel"] != "info" {
		t.Fatalf("expected logLevel from schema env mapper, got: %#v", nacosBlock["logLevel"])
	}
}

// TestLoadBootstrapFromEnvLoadsNacosSources 验证可选 loader 负责 indexed source 映射。
func TestLoadBootstrapFromEnvLoadsNacosSources(t *testing.T) {
	t.Setenv("APP_NACOS_SOURCE_0_GROUP_ID", "g1")
	t.Setenv("APP_NACOS_SOURCE_0_DATA_ID", "d1.yaml")
	t.Setenv("APP_NACOS_SOURCE_0_PRIORITY", "10")
	t.Setenv("APP_NACOS_SOURCE_0_OPTIONAL", "true")

	runtimeProvider := New()
	values := runtimeProvider.LoadBootstrapFromEnv("APP_")

	nacosBlock, ok := values["nacos"].(map[string]any)
	if !ok {
		t.Fatalf("expected nacos block, got: %#v", values["nacos"])
	}
	if _, exists := nacosBlock["address"]; exists {
		t.Fatalf("expected scalar field address to be handled by schema mapper, got: %#v", nacosBlock["address"])
	}
	rawSources, ok := nacosBlock["sources"].([]any)
	if !ok {
		t.Fatalf("expected []any sources, got: %#v", nacosBlock["sources"])
	}
	if len(rawSources) != 1 {
		t.Fatalf("expected one env source, got: %d", len(rawSources))
	}
	first, ok := rawSources[0].(map[string]any)
	if !ok {
		t.Fatalf("expected source map, got: %#v", rawSources[0])
	}
	if first["groupId"] != "g1" || first["dataId"] != "d1.yaml" || first["priority"] != "10" || first["optional"] != "true" {
		t.Fatalf("unexpected env source map: %#v", first)
	}
}

// TestCollectEnvNacosSourcesSupportsIndexedKeys 验证编号环境变量映射语义。
func TestCollectEnvNacosSourcesSupportsIndexedKeys(t *testing.T) {
	t.Setenv("APP_NACOS_SOURCE_0_GROUP_ID", "g1")
	t.Setenv("APP_NACOS_SOURCE_0_DATA_ID", "d1.yaml")
	t.Setenv("APP_NACOS_SOURCE_1_GROUP_ID", "g2")
	t.Setenv("APP_NACOS_SOURCE_1_DATA_ID", "d2.yaml")
	t.Setenv("APP_NACOS_SOURCE_3_GROUP_ID", "ignored")
	t.Setenv("APP_NACOS_SOURCE_3_DATA_ID", "ignored.yaml")

	got := collectEnvNacosSources("APP_")
	want := []map[string]any{
		{"groupId": "g1", "dataId": "d1.yaml"},
		{"groupId": "g2", "dataId": "d2.yaml"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected indexed sources, want=%#v got=%#v", want, got)
	}
}

// TestLoadBootstrapFromEnvIgnoresEmptyValues 验证空环境变量不会污染结果。
func TestLoadBootstrapFromEnvIgnoresEmptyValues(t *testing.T) {
	t.Setenv("APP_NACOS_SOURCE_0_GROUP_ID", "")
	runtimeProvider := New()
	values := runtimeProvider.LoadBootstrapFromEnv("APP_")
	if _, ok := values["nacos"]; ok {
		t.Fatalf("expected empty env to be ignored")
	}
}

// TestValidateBootstrapRejectsUnknownField 验证 ValidateBootstrap 走结构校验并返回错误。
func TestValidateBootstrapRejectsUnknownField(t *testing.T) {
	runtimeProvider := New()
	err := runtimeProvider.ValidateBootstrap(map[string]any{
		"nacos": map[string]any{"unknownField": "x"},
	})
	if err == nil {
		t.Fatalf("expected validate bootstrap error")
	}
	assertErrorCode(t, err, ErrCodeNacosInvalidBootstrap)
	assertErrorContains(t, err, "invalid nacos bootstrap field")
}

// TestOpenBuildsSDKClient 验证 Open 会构建可用 plan。
func TestOpenBuildsSDKClient(t *testing.T) {
	runtimeProvider := New()
	opened, err := runtimeProvider.Open(context.Background(), map[string]any{
		"nacos": map[string]any{
			"address": "127.0.0.1:8848",
			"sources": []any{
				map[string]any{
					"groupId": "g1",
					"dataId":  "d1.yaml",
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
	if len(runtimeSession.sources) != 1 || runtimeSession.sources[0].Info().Name != "g1/d1.yaml" {
		t.Fatalf("unexpected sources: %#v", runtimeSession.sources)
	}
}

// TestOpenBuildsClientWhenUsingSDKOptions 验证 SDKOptions 路径会创建 SDK client。
func TestOpenBuildsClientWhenUsingSDKOptions(t *testing.T) {
	runtimeProvider := New()
	opened, err := runtimeProvider.buildSession(&binding{
		Sources:       []BootstrapSource{{GroupID: "g", DataID: "d"}},
		ClientOptions: &ClientOptions{Address: "127.0.0.1:8848"},
	})
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}
	if opened.(*session).client == nil {
		t.Fatalf("expected nacos sdk client to be built")
	}
}
