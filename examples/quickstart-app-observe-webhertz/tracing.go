package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	webhertzobserve "github.com/yangzhuangqiu/gobase/app-observe-webhertz"
	"github.com/yangzhuangqiu/gobase/ioc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// traceProviderOwner 创建并拥有示例 OTel SDK Provider，生命周期覆盖全部 Web adapter 和 Server。
type traceProviderOwner struct {
	// provider 在 Init 完整构造后发布，并在 IOC 逆序关闭末段 Flush/Shutdown。
	provider *sdktrace.TracerProvider
}

var (
	_ ioc.Initializer                = (*traceProviderOwner)(nil)
	_ ioc.Closer                     = (*traceProviderOwner)(nil)
	_ webhertzobserve.TracerProvider = (*traceProviderOwner)(nil)
	_ sdktrace.SpanExporter          = (*consoleSpanExporter)(nil)
)

// Init 构造 AlwaysSample Provider 和同步控制台 Exporter，便于本地直接观察完整 Span。
func (this *traceProviderOwner) Init(_ context.Context) error {
	this.provider = sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(&consoleSpanExporter{})),
	)
	return nil
}

// OTelTracerProvider 返回由当前 bean 拥有且已初始化的 SDK Provider。
func (this *traceProviderOwner) OTelTracerProvider() trace.TracerProvider {
	if this == nil {
		return nil
	}
	return this.provider
}

// SamplingPolicyReference 返回与 adapter 启动期声明一致的采样策略引用。
func (this *traceProviderOwner) SamplingPolicyReference() string {
	return alwaysOnSamplingPolicy
}

// Close 在全部 Web Server 和 adapter 停止后 Flush 并关闭 SDK Provider。
func (this *traceProviderOwner) Close(ctx context.Context) error {
	if this == nil || this.provider == nil {
		return nil
	}
	// Flush 与 Shutdown 均执行，避免一个阶段失败后跳过后续资源收敛。
	return errors.Join(this.provider.ForceFlush(ctx), this.provider.Shutdown(ctx))
}

// consoleSpanExporter 把教学 Span 的稳定关联字段写到 stdout，不输出 Header、Body 或 query。
type consoleSpanExporter struct {
	// mu 保证并发请求导出时每条 Span 文本保持完整。
	mu sync.Mutex
}

// ExportSpans 同步输出 Span 名称和标识；生产环境应替换为受管理的批处理远程 Exporter。
func (this *consoleSpanExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	this.mu.Lock()
	defer this.mu.Unlock()
	for _, span := range spans {
		_, _ = fmt.Fprintf(
			os.Stdout,
			"trace span=%q trace_id=%s span_id=%s status=%s\n",
			span.Name(), span.SpanContext().TraceID(), span.SpanContext().SpanID(), span.Status().Code,
		)
	}
	return nil
}

// Shutdown 完成控制台 Exporter 的无资源关闭合同。
func (this *consoleSpanExporter) Shutdown(context.Context) error { return nil }
