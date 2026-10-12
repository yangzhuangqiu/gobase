package nacos

import (
	"context"
	"testing"

	"github.com/yangzhuangqiu/gobase/config/provider"
)

// TestUseCaseBootstrapEnvToSession 演示调用方通过环境变量构造 bootstrap 并打开 Provider Session。
func TestUseCaseBootstrapEnvToSession(t *testing.T) {
	t.Setenv("APP_NACOS_ADDRESS", "127.0.0.1:8848")
	t.Setenv("APP_NACOS_SOURCE_0_GROUP_ID", "g1")
	t.Setenv("APP_NACOS_SOURCE_0_DATA_ID", "d1.yaml")
	t.Setenv("APP_NACOS_SOURCE_0_PRIORITY", "100")

	runtimeProvider := New()
	values := provider.LoadBootstrapFromEnvBySchema("APP_", runtimeProvider.BootstrapSchema())
	values = mergeBootstrapMaps(values, runtimeProvider.LoadBootstrapFromEnv("APP_"))
	if err := runtimeProvider.ValidateBootstrap(values); err != nil {
		t.Fatalf("validate bootstrap failed: %v", err)
	}

	opened, err := runtimeProvider.Open(context.Background(), values)
	if err != nil {
		t.Fatalf("build plan from bootstrap failed: %v", err)
	}
	runtimeSession := opened.(*session)
	if runtimeSession.client == nil {
		t.Fatalf("expected non-nil plan client")
	}
	if len(runtimeSession.sources) != 1 {
		t.Fatalf("expected one source, got: %d", len(runtimeSession.sources))
	}
	if runtimeSession.sources[0].Info().Name != "g1/d1.yaml" {
		t.Fatalf("expected source name g1/d1.yaml, got: %s", runtimeSession.sources[0].Info().Name)
	}
}

// TestUseCaseValidateBootstrapRejectsLegacyStringSource 演示旧字符串 source 声明会 fail-fast。
func TestUseCaseValidateBootstrapRejectsLegacyStringSource(t *testing.T) {
	runtimeProvider := New()
	err := runtimeProvider.ValidateBootstrap(map[string]any{
		"nacos": map[string]any{
			"sources": []any{"name=legacy,group=g,data=d.yaml"},
		},
	})
	if err == nil {
		t.Fatalf("expected validate bootstrap error for legacy source format")
	}
	assertErrorCode(t, err, ErrCodeNacosInvalidSource)
	assertErrorContains(t, err, "expect map")
}

// TestUseCaseOpenRequiresSources 演示仅配置连接器不配置 source 会失败。
func TestUseCaseOpenRequiresSources(t *testing.T) {
	runtimeProvider := New()
	_, err := runtimeProvider.Open(context.Background(), map[string]any{
		"nacos": map[string]any{
			"address": "127.0.0.1:8848",
		},
	})
	if err == nil {
		t.Fatalf("expected build plan error when sources are missing")
	}
	assertErrorCode(t, err, ErrCodeNacosInvalidBinding)
	assertErrorToken(t, err, "reason", "nacos_sources_required_when_connector_configured")
}

func mergeBootstrapMaps(base map[string]any, overlay map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(overlay))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range overlay {
		overlayMap, overlayIsMap := value.(map[string]any)
		baseMap, baseIsMap := result[key].(map[string]any)
		if overlayIsMap && baseIsMap {
			result[key] = mergeBootstrapMaps(baseMap, overlayMap)
			continue
		}
		result[key] = value
	}
	return result
}
