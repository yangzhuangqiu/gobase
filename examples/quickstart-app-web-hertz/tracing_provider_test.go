package main

import (
	"context"
	"sync"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// recordingSpanExporter 记录 Flush 导出与 Shutdown 次序，验证 Provider owner 生命周期。
type recordingSpanExporter struct {
	// mu 保护 batch worker 与测试断言并发访问。
	mu sync.Mutex
	// events 保存 exporter 收到的阶段顺序。
	events []string
}

// ExportSpans 记录导出阶段，不持有 SDK Span。
func (this *recordingSpanExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	this.mu.Lock()
	defer this.mu.Unlock()
	if len(spans) > 0 {
		this.events = append(this.events, "export")
	}
	return nil
}

// Shutdown 记录 Provider 已终止 exporter。
func (this *recordingSpanExporter) Shutdown(_ context.Context) error {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.events = append(this.events, "shutdown")
	return nil
}

// TestApplicationTracerProviderFlushesBeforeShutdown 验证 owned Provider 在关闭时先导出缓存 Span，再关闭 Exporter。
// 测试方法：注入记录 Exporter，Init 后创建并结束 Span，调用 Close 并断言 export、shutdown 顺序及 Provider 不再发布。
func TestApplicationTracerProviderFlushesBeforeShutdown(t *testing.T) {
	// 第一阶段：用记录 Exporter 初始化真实 Batch Processor，并产生一个已结束 Span。
	exporter := &recordingSpanExporter{}
	provider := &applicationTracerProvider{
		config: applicationTracingConfig{
			ServiceName:             "test-service",
			SamplingPolicyReference: defaultApplicationSamplingPolicyReference,
		},
		propagator: newApplicationTracerProviderForTest().propagator,
		exporter:   exporter,
	}
	if err := provider.Init(context.Background()); err != nil {
		t.Fatalf("init tracer provider: %v", err)
	}
	_, span := provider.OTelTracerProvider().Tracer("test").Start(context.Background(), "operation", trace.WithSpanKind(trace.SpanKindServer))
	span.End()
	// 第二阶段：Close 内的 ForceFlush 应触发 export，随后 SDK Shutdown 再关闭 Exporter。
	if err := provider.Close(context.Background()); err != nil {
		t.Fatalf("close tracer provider: %v", err)
	}
	exporter.mu.Lock()
	events := append([]string{}, exporter.events...)
	exporter.mu.Unlock()
	if len(events) != 2 || events[0] != "export" || events[1] != "shutdown" {
		t.Fatalf("unexpected provider close order: %v", events)
	}
	if provider.OTelTracerProvider() != nil {
		t.Fatal("expected provider reference cleared after shutdown")
	}
}

// TestLoadTracingEnabledDefaultsOffAndRejectsInvalid 验证 tracing 默认关闭且非法环境值在应用构造前失败。
// 测试方法：依次设置空、true 和非法值，断言 false、true 与带阶段语义错误。
func TestLoadTracingEnabledDefaultsOffAndRejectsInvalid(t *testing.T) {
	// 第一阶段：环境未声明时不创建 Trace 资源，显式 true 才启用。
	t.Setenv(quickstartTracingEnabledEnv, "")
	if enabled, err := loadTracingEnabled(); err != nil || enabled {
		t.Fatalf("expected tracing disabled by default: enabled=%t err=%v", enabled, err)
	}
	t.Setenv(quickstartTracingEnabledEnv, "true")
	if enabled, err := loadTracingEnabled(); err != nil || !enabled {
		t.Fatalf("expected tracing enabled: enabled=%t err=%v", enabled, err)
	}
	// 第二阶段：非法值不可静默回退 false，避免运维误以为 Trace 已开启。
	t.Setenv(quickstartTracingEnabledEnv, "sometimes")
	if enabled, err := loadTracingEnabled(); err == nil || enabled {
		t.Fatalf("expected invalid tracing switch: enabled=%t err=%v", enabled, err)
	}
}

// TestApplicationTracerProviderRejectsUnsupportedSamplingReference 验证 Provider 发布的策略引用一定对应真实 Sampler。
// 测试方法：构造非支持引用并调用 Init，断言启动失败且没有发布 SDK Provider。
func TestApplicationTracerProviderRejectsUnsupportedSamplingReference(t *testing.T) {
	provider := &applicationTracerProvider{
		config: applicationTracingConfig{
			ServiceName:             "test-service",
			SamplingPolicyReference: "ratio_50",
		},
		propagator: defaultApplicationPropagator(),
	}
	if err := provider.Init(context.Background()); err == nil {
		t.Fatal("expected unsupported sampling policy error")
	}
	if provider.OTelTracerProvider() != nil {
		t.Fatal("expected no provider published after invalid sampling policy")
	}
}

// newApplicationTracerProviderForTest 创建带显式传播器的无资源 Provider 声明。
func newApplicationTracerProviderForTest() *applicationTracerProvider {
	return &applicationTracerProvider{propagator: defaultApplicationPropagator()}
}
