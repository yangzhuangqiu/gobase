package webhertzobserve

import (
	"context"
	"testing"
	"time"

	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/errcode"
	"github.com/yangzhuangqiu/gobase/ioc"
)

// TestMetricsServerConfigUsesSafeDefaultsAndRejectsInvalidValues 验证开箱默认值与网络配置边界。
//
// 测试方法：先解析零值并断言回环地址、固定路径和正读取预算；再表驱动输入非法地址、路径和时长，断言稳定配置错误码。
func TestMetricsServerConfigUsesSafeDefaultsAndRejectsInvalidValues(t *testing.T) {
	resolved, err := resolveMetricsServerConfig(MetricsServerConfig{})
	if err != nil {
		t.Fatalf("resolve default metrics server config: %v", err)
	}
	expected := DefaultMetricsServerConfig()
	if resolved != expected {
		t.Fatalf("unexpected defaults: expected=%+v actual=%+v", expected, resolved)
	}

	tests := map[string]MetricsServerConfig{
		"address":   {Address: "127.0.0.1"},
		"path":      {Path: "metrics"},
		"root path": {Path: "/"},
		"read timeout": {
			ReadHeaderTimeout: -time.Second,
		},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := resolveMetricsServerConfig(input)
			if !errcode.IsCode[ErrorCode](err, ErrCodeMetricsServerConfigInvalid) {
				t.Fatalf("expected config-invalid error, got=%v", err)
			}
		})
	}
}

// TestMetricsServerConfigLoadsDefaultsAndRequiresCustomScope 验证默认 scope 缺失与自定义 scope 缺失具有不同语义。
//
// 测试方法：用空 Snapshot 读取默认 scope 并断言安全默认值；再读取不存在的自定义 scope，断言 config-load-failed。
func TestMetricsServerConfigLoadsDefaultsAndRequiresCustomScope(t *testing.T) {
	manager := newMetricsTestManager(t, map[string]any{})
	t.Cleanup(func() { _ = manager.Close() })

	resolved, err := loadMetricsServerConfig(
		context.Background(),
		manager,
		metricsServerOptions{scope: DefaultMetricsServerScope},
	)
	if err != nil {
		t.Fatalf("load default metrics server config: %v", err)
	}
	if resolved != DefaultMetricsServerConfig() {
		t.Fatalf("unexpected default config: %+v", resolved)
	}

	_, err = loadMetricsServerConfig(
		context.Background(),
		manager,
		metricsServerOptions{scope: "custom.metrics.server", customScope: true},
	)
	if !errcode.IsCode[ErrorCode](err, ErrCodeMetricsServerConfigLoadFailed) {
		t.Fatalf("expected custom scope load error, got=%v", err)
	}
}

// TestModulePublishesMetricsServerPlanFromConfiguredScope 验证 Server 计划与 Prometheus Handler 由同一 managed bean 原子发布。
//
// 测试方法：注册自定义配置和私有 Prometheus，Build 后解析内部计划，断言配置覆盖与 Handler 非空。
func TestModulePublishesMetricsServerPlanFromConfiguredScope(t *testing.T) {
	manager := newMetricsTestManager(t, map[string]any{
		"custom": map[string]any{
			"metrics": map[string]any{
				"address":           "127.0.0.1:0",
				"path":              "/internal/metrics",
				"readHeaderTimeout": "2s",
			},
		},
	})
	t.Cleanup(func() { _ = manager.Close() })

	container := ioc.New()
	if err := container.Register(
		manager,
		ioc.WithName("ConfigManager"),
		ioc.WithDeclare[baseconfig.Manager](),
		ioc.WithBorrowed(),
	); err != nil {
		t.Fatalf("register config manager: %v", err)
	}
	module := NewModule(
		WithPrivatePrometheus(),
		WithMetricsServer(WithMetricsServerScope("custom.metrics")),
	)
	if err := module.Register(container); err != nil {
		t.Fatalf("register metrics server module: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build metrics server module: %v", err)
	}
	t.Cleanup(func() { _ = container.Close(context.Background()) })

	plans, err := ioc.ResolveAllAs[metricsServerPlan](container)
	if err != nil || len(plans) != 1 {
		t.Fatalf("resolve metrics server plan: count=%d err=%v", len(plans), err)
	}
	config := plans[0].metricsConfig()
	if config.Address != "127.0.0.1:0" || config.Path != "/internal/metrics" ||
		config.ReadHeaderTimeout != 2*time.Second {
		t.Fatalf("unexpected configured plan: %+v", config)
	}
	if isNilValue(plans[0].metricsHandler()) {
		t.Fatal("expected plan to publish prometheus handler")
	}
}

// newMetricsTestManager 创建禁用文件发现且只包含给定默认值的测试 Manager。
// 阶段角色：配置和 Module 测试自行关闭返回对象，adapter 只借用。
func newMetricsTestManager(t *testing.T, defaults map[string]any) baseconfig.Manager {
	t.Helper()
	manager, err := baseconfig.New(
		baseconfig.File(baseconfig.FileDisabled()),
		baseconfig.Defaults(defaults),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build metrics test manager: %v", err)
	}
	return manager
}
