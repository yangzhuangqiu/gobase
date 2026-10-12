package provider

import (
	"reflect"
	"testing"
)

func TestLoadBootstrapFromEnvBySchema(t *testing.T) {
	t.Setenv("APP_CONFIG", "application.yaml")
	t.Setenv("APP_NACOS_TIMEOUT_MS", "12000")
	t.Setenv("APP_NACOS_ENABLED", "true")
	t.Setenv("APP_NACOS_SOURCES", "[{\"groupId\":\"g\",\"dataId\":\"d.yaml\"}]")
	t.Setenv("APP_NACOS_SOURCE_0_GROUP_ID", "ignored_by_schema_auto_map")

	values := LoadBootstrapFromEnvBySchema("APP", []BootstrapKey{
		{Key: "config", Type: BootstrapSchemaTypeString},
		{Key: "nacos.timeoutMs", Type: BootstrapSchemaTypeUint64},
		{Key: "nacos.enabled", Type: BootstrapSchemaTypeBool},
		{Key: "nacos.sources", Type: BootstrapSchemaTypeList},
		{Key: "nacos.sources[].groupId", Type: BootstrapSchemaTypeString},
	})

	nacosBlock, ok := values["nacos"].(map[string]any)
	if !ok {
		t.Fatalf("expected nacos block, got: %#v", values["nacos"])
	}
	if values["config"] != "application.yaml" {
		t.Fatalf("expected config from env, got: %#v", values["config"])
	}
	if timeout, cast := nacosBlock["timeoutMs"].(uint64); !cast || timeout != 12000 {
		t.Fatalf("expected timeoutMs uint64=12000, got: %#v", nacosBlock["timeoutMs"])
	}
	if enabled, cast := nacosBlock["enabled"].(bool); !cast || !enabled {
		t.Fatalf("expected enabled bool=true, got: %#v", nacosBlock["enabled"])
	}
	rawSources, cast := nacosBlock["sources"].([]any)
	if !cast || len(rawSources) != 1 {
		t.Fatalf("expected parsed list sources, got: %#v", nacosBlock["sources"])
	}
	if _, exists := values["nacos.sources[]"]; exists {
		t.Fatalf("expected wildcard key to be ignored")
	}
}

func TestLoadBootstrapFromEnvBySchemaFallbacksRawStringOnTypeError(t *testing.T) {
	t.Setenv("APP_NACOS_ENABLED", "not_bool")

	values := LoadBootstrapFromEnvBySchema("APP", []BootstrapKey{
		{Key: "nacos.enabled", Type: BootstrapSchemaTypeBool},
	})

	nacosBlock, ok := values["nacos"].(map[string]any)
	if !ok {
		t.Fatalf("expected nacos block, got: %#v", values["nacos"])
	}
	if nacosBlock["enabled"] != "not_bool" {
		t.Fatalf("expected raw string fallback, got: %#v", nacosBlock["enabled"])
	}
}

func TestBootstrapKeyToEnvToken(t *testing.T) {
	cases := map[string]string{
		"config":                    "CONFIG",
		"nacos.timeoutMs":           "NACOS_TIMEOUT_MS",
		"consul.waitTimeMs":         "CONSUL_WAIT_TIME_MS",
		"demo.apiURL":               "DEMO_API_URL",
		"mock.provider.http2Config": "MOCK_PROVIDER_HTTP2_CONFIG",
	}
	for key, expect := range cases {
		if got := bootstrapKeyToEnvToken(key); got != expect {
			t.Fatalf("bootstrap key token mismatch key=%s expect=%s got=%s", key, expect, got)
		}
	}
}

func TestParseBootstrapEnvValueListAndMap(t *testing.T) {
	listValue := parseBootstrapEnvValue("[1,2,3]", BootstrapSchemaTypeList)
	if !reflect.DeepEqual(listValue, []any{1, 2, 3}) {
		t.Fatalf("unexpected list parse result: %#v", listValue)
	}

	mapValue := parseBootstrapEnvValue("{name: demo}", "object")
	expectedMap := map[string]any{"name": "demo"}
	if !reflect.DeepEqual(mapValue, expectedMap) {
		t.Fatalf("unexpected map parse result: %#v", mapValue)
	}
}
