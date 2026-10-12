package main

import (
	"context"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestApplicationMetricsRegistryRequiresExplicitGoProcessSelection 验证 Go/Process/业务 Collector 不会隐式进入 Registry。
// 测试方法：先只注册业务 Counter，再显式启用 Go/Process，分别 Gather 并检查指标族名称。
func TestApplicationMetricsRegistryRequiresExplicitGoProcessSelection(t *testing.T) {
	// 第一阶段：只声明业务 Counter，证明 Go/Process 不会因使用 Prometheus SDK 被隐式带入。
	business := prometheus.NewCounter(prometheus.CounterOpts{Name: "quickstart_business_events_total"})
	business.Inc()
	minimal := &applicationMetricsRegistry{
		registry: prometheus.NewRegistry(),
		config:   applicationMetricsConfig{BusinessCollectors: []prometheus.Collector{business}},
	}
	if err := minimal.Init(context.Background()); err != nil {
		t.Fatalf("init minimal registry: %v", err)
	}
	minimalNames := gatherMetricNames(t, minimal)
	if !strings.Contains(minimalNames, "quickstart_business_events_total") || strings.Contains(minimalNames, "go_gc_duration_seconds") || strings.Contains(minimalNames, "process_cpu_seconds_total") {
		t.Fatalf("unexpected minimal metric families: %s", minimalNames)
	}
	if err := minimal.Close(context.Background()); err != nil {
		t.Fatalf("close minimal registry: %v", err)
	}

	// 第二阶段：显式打开两个 runtime Collector 后，指标族才出现在同一私有 Registry。
	explicit := &applicationMetricsRegistry{
		registry: prometheus.NewRegistry(),
		config:   applicationMetricsConfig{IncludeGo: true, IncludeProcess: true},
	}
	if err := explicit.Init(context.Background()); err != nil {
		t.Fatalf("init explicit runtime registry: %v", err)
	}
	explicitNames := gatherMetricNames(t, explicit)
	if !strings.Contains(explicitNames, "go_gc_duration_seconds") || !strings.Contains(explicitNames, "process_cpu_seconds_total") {
		t.Fatalf("expected explicit Go/Process metrics: %s", explicitNames)
	}
	if err := explicit.Close(context.Background()); err != nil {
		t.Fatalf("close explicit runtime registry: %v", err)
	}
}

// TestApplicationMetricsRegistryRollsBackDuplicateCollector 验证聚合注册失败不会留下前置业务 Collector。
// 测试方法：声明两个同名 Counter 触发第二次注册失败，再 Gather 断言 Registry 已为空。
func TestApplicationMetricsRegistryRollsBackDuplicateCollector(t *testing.T) {
	first := prometheus.NewCounter(prometheus.CounterOpts{Name: "duplicate_business_total"})
	second := prometheus.NewCounter(prometheus.CounterOpts{Name: "duplicate_business_total"})
	registry := &applicationMetricsRegistry{
		registry: prometheus.NewRegistry(),
		config: applicationMetricsConfig{
			BusinessCollectors: []prometheus.Collector{first, second},
		},
	}
	if err := registry.Init(context.Background()); err == nil || !strings.Contains(err.Error(), "reason=registry_rejected") {
		t.Fatalf("expected duplicate registration failure, got=%v", err)
	}
	if families, err := registry.Gather(); err != nil || len(families) != 0 {
		t.Fatalf("expected rollback to empty registry: families=%d err=%v", len(families), err)
	}
}

// gatherMetricNames 把 Gather 结果压缩为可搜索名称集合。
func gatherMetricNames(t *testing.T, gatherer prometheus.Gatherer) string {
	t.Helper()
	families, err := gatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.GetName())
	}
	return strings.Join(names, "\n")
}
