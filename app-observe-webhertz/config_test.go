package webhertzobserve

import (
	"context"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/errcode"
	"github.com/yangzhuangqiu/gobase/ioc"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// TestDefaultConfigReturnsCompleteIndependentSnapshot 验证无外部配置时即可获得完整且彼此隔离的启动快照。
// 测试方法：断言指标、分桶、Metrics Server 和 Trace 默认值；修改一个快照的 slice 后确认另一个快照不受影响。
func TestDefaultConfigReturnsCompleteIndependentSnapshot(t *testing.T) {
	first := DefaultConfig()
	second := DefaultConfig()
	if first.Prometheus.Namespace != defaultPrometheusNamespace || first.Prometheus.Subsystem != defaultPrometheusSubsystem {
		t.Fatalf("unexpected prometheus defaults: %+v", first.Prometheus)
	}
	if first.Prometheus.Server != DefaultMetricsServerConfig() || len(first.Prometheus.Buckets.Duration) == 0 {
		t.Fatalf("incomplete prometheus defaults: %+v", first.Prometheus)
	}
	if len(first.Tracing.IgnoredPaths) != 0 || first.Tracing.SamplingPolicyReference != "" {
		t.Fatalf("unexpected tracing defaults: %+v", first.Tracing)
	}
	first.Prometheus.Buckets.Duration[0] = -1
	if second.Prometheus.Buckets.Duration[0] <= 0 {
		t.Fatal("default config snapshots must not share bucket slices")
	}
}

// TestLoadConfigUsesDefaultsAndAppliesPartialOverride 验证统一 scope 以完整默认值为基底做字段级覆盖。
// 测试方法：先用 nil Manager 和缺失默认 scope 断言完整默认值，再提供部分指标、Server、Trace 配置并断言未配置字段保持默认。
func TestLoadConfigUsesDefaultsAndAppliesPartialOverride(t *testing.T) {
	// 第一阶段：无 Manager 和默认 scope 缺失都必须产生相同完整默认快照。
	resolved, err := loadConfig(context.Background(), nil, resolveConfigLocator(DefaultConfigScope))
	if err != nil || !reflect.DeepEqual(resolved, DefaultConfig()) {
		t.Fatalf("load defaults without manager: config=%+v err=%v", resolved, err)
	}

	emptyManager := newObservationTestManager(t, map[string]any{})
	t.Cleanup(func() { _ = emptyManager.Close() })
	resolved, err = loadConfig(context.Background(), emptyManager, resolveConfigLocator(DefaultConfigScope))
	if err != nil || !reflect.DeepEqual(resolved, DefaultConfig()) {
		t.Fatalf("load defaults for missing default scope: config=%+v err=%v", resolved, err)
	}

	// 第二阶段：部分部署配置只覆盖显式字段，slice 同时完成清理、去重与复制。
	manager := newObservationTestManager(t, map[string]any{
		"observability": map[string]any{"webHertz": map[string]any{
			"prometheus": map[string]any{
				"namespace": "orders",
				"buckets":   map[string]any{"duration": []float64{0.01, 0.1, 1}},
				"server":    map[string]any{"address": "127.0.0.1:0", "path": "/internal/metrics", "readHeaderTimeout": "2s"},
			},
			"tracing": map[string]any{"ignoredPaths": []string{" /healthz ", "/healthz", "/readyz"}, "samplingPolicyReference": " always_on "},
		}},
	})
	t.Cleanup(func() { _ = manager.Close() })
	resolved, err = loadConfig(context.Background(), manager, resolveConfigLocator(DefaultConfigScope))
	if err != nil {
		t.Fatalf("load partial unified config: %v", err)
	}
	if resolved.Prometheus.Namespace != "orders" || resolved.Prometheus.Subsystem != defaultPrometheusSubsystem {
		t.Fatalf("unexpected metric identity: %+v", resolved.Prometheus)
	}
	if !reflect.DeepEqual(resolved.Prometheus.Buckets.Duration, []float64{0.01, 0.1, 1}) || len(resolved.Prometheus.Buckets.RequestBytes) == 0 {
		t.Fatalf("unexpected bucket merge: %+v", resolved.Prometheus.Buckets)
	}
	if resolved.Prometheus.Server.Path != "/internal/metrics" || resolved.Prometheus.Server.ReadHeaderTimeout != 2*time.Second {
		t.Fatalf("unexpected server config: %+v", resolved.Prometheus.Server)
	}
	if !reflect.DeepEqual(resolved.Tracing.IgnoredPaths, []string{"/healthz", "/readyz"}) || resolved.Tracing.SamplingPolicyReference != "always_on" {
		t.Fatalf("unexpected tracing config: %+v", resolved.Tracing)
	}
}

// TestLoadConfigRejectsMissingCustomScopeAndInvalidValues 验证自定义 scope 缺失和非法统一配置均在 IOC Init 前 fail-fast。
// 测试方法：读取空 Manager 的自定义路径断言 load_failed，再直接解析非法路径、bucket、地址并断言 config_invalid。
func TestLoadConfigRejectsMissingCustomScopeAndInvalidValues(t *testing.T) {
	// 第一阶段：自定义路径代表强声明，缺失时不能静默使用框架默认值。
	manager := newObservationTestManager(t, map[string]any{})
	t.Cleanup(func() { _ = manager.Close() })
	_, err := loadConfig(context.Background(), manager, resolveConfigLocator("custom.observability"))
	if !errcode.IsCode[ErrorCode](err, ErrCodeConfigLoadFailed) {
		t.Fatalf("expected custom scope load error, got=%v", err)
	}
	invalidManager := newObservationTestManager(t, map[string]any{
		"observability": map[string]any{"webHertz": map[string]any{
			"tracing": map[string]any{"ignoredPaths": []string{"healthz"}},
		}},
	})
	t.Cleanup(func() { _ = invalidManager.Close() })
	_, err = loadConfig(context.Background(), invalidManager, resolveConfigLocator(DefaultConfigScope))
	if !errcode.IsCode[ErrorCode](err, ErrCodeConfigLoadFailed) || !errcode.IsCode[ErrorCode](err, ErrCodeConfigInvalid) {
		t.Fatalf("expected load error preserving config-invalid cause, got=%v", err)
	}

	// 第二阶段：各子配置共用 config_invalid 语义，便于部署侧统一分支处理。
	for name, mutate := range map[string]func(*Config){
		"ignored path": func(config *Config) { config.Tracing.IgnoredPaths = []string{"healthz"} },
		"bucket":       func(config *Config) { config.Prometheus.Buckets.Duration = []float64{1, 1} },
		"address":      func(config *Config) { config.Prometheus.Server.Address = "127.0.0.1" },
	} {
		t.Run(name, func(t *testing.T) {
			config := DefaultConfig()
			mutate(&config)
			if _, err := resolveConfig(config); !errcode.IsCode[ErrorCode](err, ErrCodeConfigInvalid) {
				t.Fatalf("expected config-invalid error, got=%v", err)
			}
		})
	}
}

// TestModulePublishesMetricsServerResourceFromUnifiedScope 验证统一 Config 同时驱动指标命名和独立 Server Resource。
// 测试方法：注册自定义统一 scope，Build 后断言预绑定端点、Handler 可用且输出自定义 namespace。
func TestModulePublishesMetricsServerResourceFromUnifiedScope(t *testing.T) {
	// 第一阶段：统一 scope 同时覆盖指标身份和抓取端点，Module Init 原子获取资源。
	manager := newObservationTestManager(t, map[string]any{
		"custom": map[string]any{"observe": map[string]any{
			"prometheus": map[string]any{
				"namespace": "custom",
				"server":    map[string]any{"address": "127.0.0.1:0", "path": "/internal/metrics", "readHeaderTimeout": "2s"},
			},
		}},
	})
	t.Cleanup(func() { _ = manager.Close() })
	container := ioc.New()
	registerObservationTestManager(t, container, manager)
	if err := NewModule(WithConfigScope("custom.observe")).Register(container); err != nil {
		t.Fatalf("register observe module: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build observe module: %v", err)
	}
	t.Cleanup(func() { _ = container.Close(context.Background()) })

	// 第二阶段：从同一 managed bean 验证端点与 Collector 都消费了该快照。
	resource, err := ioc.ResolveAs[metricsServerResource](container)
	if err != nil {
		t.Fatalf("resolve metrics server resource: %v", err)
	}
	address, path := resource.endpoint()
	if !strings.HasPrefix(address, "127.0.0.1:") || path != "/internal/metrics" {
		t.Fatalf("unexpected configured resource: address=%q path=%q", address, path)
	}
	observer, err := ioc.ResolveAs[webhertz.RequestObserver](container)
	if err != nil {
		t.Fatalf("resolve configured observer: %v", err)
	}
	observer.(webhertz.RequestLifecycleObserver).RequestStarted(context.Background(), "GET")
	observer.Observe(context.Background(), webhertz.HTTPRequestObservation{
		Method: "GET", RoutePattern: "/ready", Status: 200, Outcome: webhertz.RequestOutcomeSuccess,
	})
	handler, err := ioc.ResolveAs[PrometheusHandler](container)
	if err != nil {
		t.Fatalf("resolve configured handler: %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
	if !strings.Contains(recorder.Body.String(), "custom_http_server_requests_total") {
		t.Fatalf("configured namespace not applied: %s", recorder.Body.String())
	}
}

// newObservationTestManager 创建禁用文件发现且只包含给定默认值的测试 Manager。
// 阶段角色：配置和 Module 测试自行关闭返回对象，adapter 只借用。
func newObservationTestManager(t *testing.T, defaults map[string]any) baseconfig.Manager {
	t.Helper()
	manager, err := baseconfig.New(baseconfig.File(baseconfig.FileDisabled()), baseconfig.Defaults(defaults)).Build(context.Background())
	if err != nil {
		t.Fatalf("build observation test manager: %v", err)
	}
	return manager
}

// registerObservationTestManager 把 borrowed ConfigManager 发布为统一配置入口。
// 阶段角色：Module Build 前准备 ConfigManager 依赖，不转移其关闭所有权。
func registerObservationTestManager(t *testing.T, container ioc.Registry, manager baseconfig.Manager) {
	t.Helper()
	if err := container.Register(manager, ioc.WithName("ConfigManager"), ioc.WithDeclare[baseconfig.Manager](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register config manager: %v", err)
	}
}
