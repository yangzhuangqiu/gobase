package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	webhertzobserve "github.com/yangzhuangqiu/gobase/app-observe-webhertz"
	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
	"github.com/yangzhuangqiu/gobase/ioc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	// applicationTracingModuleName 是 quickstart Provider owner 的稳定模块名。
	applicationTracingModuleName = "quickstart-application-tracing"
	// applicationTracerProviderBeanName 是 borrowed Web adapter 依赖的稳定 bean 名称。
	applicationTracerProviderBeanName = "QuickstartApplicationTracerProvider"
	// defaultApplicationSamplingPolicyReference 对应 ParentBased(AlwaysSample) 的应用策略版本。
	defaultApplicationSamplingPolicyReference = "parentbased_always_on"
)

// applicationTracingConfig 描述 quickstart 应用拥有的 Provider 策略。
type applicationTracingConfig struct {
	// ServiceName 写入 SDK Resource，必填且不会从 Web 请求读取。
	ServiceName string
	// SamplingPolicyReference 是 Provider 实际 Sampler 的稳定语义引用。
	SamplingPolicyReference string
}

// applicationTracerProvider 创建并拥有 SDK Provider、Batch Processor 和 Exporter。
type applicationTracerProvider struct {
	// config 是 Module 注册时冻结的 Provider 策略。
	config applicationTracingConfig
	// propagator 是显式 TraceContext+Baggage 传播策略，不修改 OTel global。
	propagator propagation.TextMapPropagator
	// exporter 由当前 bean 拥有；测试可在 Init 前注入记录实现。
	exporter sdktrace.SpanExporter
	// provider 在 Init 创建，在 Close 中 ForceFlush 后 Shutdown。
	provider *sdktrace.TracerProvider
}

// stdoutSpanExporter 把最小 Trace 关联摘要写到 stdout，不输出 Attribute、Header、Body 或 query。
type stdoutSpanExporter struct {
	// mu 串行保护 Encoder 和关闭状态。
	mu sync.Mutex
	// encoder 复用单个 JSON Encoder，输出每 Span 一行。
	encoder *json.Encoder
	// shutdown 阻止 Provider 关闭后继续导出。
	shutdown bool
}

// stdoutSpanRecord 是 quickstart stdout Exporter 的安全最小事件结构。
type stdoutSpanRecord struct {
	// Event 是日志检索使用的固定事件名。
	Event string `json:"event"`
	// Name 是受控 `METHOD route` Server Span 名称。
	Name string `json:"name"`
	// TraceID 是跨服务关联标识。
	TraceID string `json:"trace_id"`
	// SpanID 是当前服务处理阶段标识。
	SpanID string `json:"span_id"`
	// ParentSpanID 是入站父 Span 标识；根 Span 时为空。
	ParentSpanID string `json:"parent_span_id,omitempty"`
	// Status 是 OTel 稳定状态码文本。
	Status string `json:"status"`
}

var (
	_ webhertzobserve.TracerProvider = (*applicationTracerProvider)(nil)
	_ propagation.TextMapPropagator  = (*applicationTracerProvider)(nil)
	_ ioc.Initializer                = (*applicationTracerProvider)(nil)
	_ ioc.Closer                     = (*applicationTracerProvider)(nil)
	_ sdktrace.SpanExporter          = (*stdoutSpanExporter)(nil)
)

// newApplicationTracingModule 创建应用 owned Provider 的可重放声明，不在 Register 获取资源。
func newApplicationTracingModule(config applicationTracingConfig) appruntime.Module {
	resolved := config
	resolved.ServiceName = strings.TrimSpace(config.ServiceName)
	resolved.SamplingPolicyReference = strings.TrimSpace(config.SamplingPolicyReference)
	return appruntime.NewModule(applicationTracingModuleName, func(registry ioc.Registry) error {
		bean := &applicationTracerProvider{
			config:     resolved,
			propagator: defaultApplicationPropagator(),
		}
		if err := registry.Register(
			bean,
			ioc.WithName(applicationTracerProviderBeanName),
			ioc.WithDeclare[webhertzobserve.TracerProvider](),
			ioc.WithDeclare[propagation.TextMapPropagator](),
		); err != nil {
			return fmt.Errorf("op=quickstart.tracing result=failed stage=register_provider reason=ioc_register_failed: %w", err)
		}
		return nil
	})
}

// defaultApplicationPropagator 创建应用显式拥有的 W3C TraceContext+Baggage 传播策略。
func defaultApplicationPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
}

// Init 创建应用独占 SDK Provider；只有显式启用 Trace 后 IOC 才会调用。
func (this *applicationTracerProvider) Init(_ context.Context) error {
	if this == nil || this.config.ServiceName == "" || this.config.SamplingPolicyReference == "" || this.propagator == nil {
		return fmt.Errorf("op=quickstart.tracing result=failed stage=init reason=config_and_propagator_required")
	}
	if this.config.SamplingPolicyReference != defaultApplicationSamplingPolicyReference {
		return fmt.Errorf("op=quickstart.tracing result=failed stage=init reason=sampling_policy_unsupported reference=%s", this.config.SamplingPolicyReference)
	}
	if this.exporter == nil {
		this.exporter = &stdoutSpanExporter{encoder: json.NewEncoder(os.Stdout)}
	}
	// Provider owner 同时固定 Resource、Sampler 和 Batch Exporter；Web adapter 只借用最终 Provider。
	this.provider = sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", this.config.ServiceName))),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
		sdktrace.WithBatcher(this.exporter),
	)
	return nil
}

// OTelTracerProvider 返回已初始化 SDK Provider；adapter Init 顺序由 IOC 依赖图保证。
func (this *applicationTracerProvider) OTelTracerProvider() trace.TracerProvider {
	if this == nil || this.provider == nil {
		return nil
	}
	return this.provider
}

// SamplingPolicyReference 返回 Provider 实际 Sampler 的应用级稳定引用。
func (this *applicationTracerProvider) SamplingPolicyReference() string {
	if this == nil {
		return ""
	}
	return this.config.SamplingPolicyReference
}

// Inject 委托显式组合 Propagator 写入载体，不访问 OTel global。
func (this *applicationTracerProvider) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	this.propagator.Inject(ctx, carrier)
}

// Extract 委托显式组合 Propagator 读取远端 Trace/Baggage。
func (this *applicationTracerProvider) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return this.propagator.Extract(ctx, carrier)
}

// Fields 返回组合 Propagator 可能读写的 Header key。
func (this *applicationTracerProvider) Fields() []string {
	return append([]string{}, this.propagator.Fields()...)
}

// Close 在 Web Server/adapter 关闭后先 ForceFlush 再 Shutdown，不关闭其他应用资源。
func (this *applicationTracerProvider) Close(ctx context.Context) error {
	if this == nil || this.provider == nil {
		return nil
	}
	// Flush 与 Shutdown 分别保留错误身份；Shutdown 即使在 Flush 失败时也必须执行。
	flushErr := this.provider.ForceFlush(ctx)
	shutdownErr := this.provider.Shutdown(ctx)
	this.provider = nil
	return errors.Join(flushErr, shutdownErr)
}

// ExportSpans 同步写出 Batch Processor 提交的最小 Span 摘要。
func (this *stdoutSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	this.mu.Lock()
	defer this.mu.Unlock()
	if this.shutdown {
		return fmt.Errorf("op=quickstart.tracing result=failed stage=export reason=exporter_shutdown")
	}
	// 每个 Span 独立编码；不输出 Attribute，避免示例 Exporter 意外扩大数据边界。
	for _, span := range spans {
		record := stdoutSpanRecord{
			Event:        "trace_span",
			Name:         span.Name(),
			TraceID:      span.SpanContext().TraceID().String(),
			SpanID:       span.SpanContext().SpanID().String(),
			ParentSpanID: span.Parent().SpanID().String(),
			Status:       span.Status().Code.String(),
		}
		if err := this.encoder.Encode(record); err != nil {
			return fmt.Errorf("op=quickstart.tracing result=failed stage=export reason=encode_failed: %w", err)
		}
	}
	return nil
}

// Shutdown 标记 Exporter 不再接受新 Span；底层 stdout 生命周期不归应用所有。
func (this *stdoutSpanExporter) Shutdown(_ context.Context) error {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.shutdown = true
	return nil
}
