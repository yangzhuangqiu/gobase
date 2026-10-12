package webhertzobserve

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/yangzhuangqiu/gobase/errcode"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// tracerProviderStub 把 SDK Provider 与稳定采样策略引用适配到 borrowed Provider 合同。
type tracerProviderStub struct {
	// provider 是测试拥有并在结束时关闭的 SDK Provider。
	provider trace.TracerProvider
	// samplingPolicyReference 是适配器启动期应匹配的策略名。
	samplingPolicyReference string
}

// OTelTracerProvider 返回测试拥有的 SDK Provider。
func (this *tracerProviderStub) OTelTracerProvider() trace.TracerProvider {
	return this.provider
}

// SamplingPolicyReference 返回测试声明的稳定策略引用。
func (this *tracerProviderStub) SamplingPolicyReference() string {
	return this.samplingPolicyReference
}

// serverSpanAttributeEnricherStub 返回固定 Attribute，便于验证安全过滤。
type serverSpanAttributeEnricherStub struct {
	// attributes 是 Enricher 声明的允许与拒绝字段集合。
	attributes []attribute.KeyValue
}

// EnrichServerSpan 返回固定 Attribute 副本。
func (this *serverSpanAttributeEnricherStub) EnrichServerSpan(_ context.Context, _ ServerSpanSnapshot) []attribute.KeyValue {
	return append([]attribute.KeyValue{}, this.attributes...)
}

// TestOpenTelemetryBundlePropagatesAndRecordsSafeFinalSpan 验证远端传播、最终 route/status、Trace ID 桥接和敏感字段白名单。
// 测试方法：使用 SpanRecorder 和固定 traceparent 执行完整 Tracer/Middleware 链，断言 parent、名称、状态、允许字段及敏感字段缺失。
func TestOpenTelemetryBundlePropagatesAndRecordsSafeFinalSpan(t *testing.T) {
	// 第一阶段：创建测试拥有的 Provider，并构造显式 Provider/Propagator Bundle。
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	enricher := &serverSpanAttributeEnricherStub{attributes: []attribute.KeyValue{
		attribute.String("deployment.environment", "test"),
		attribute.String("http.route", "/overridden"),
		attribute.String("http.request.header.authorization", "secret"),
		attribute.String("request.query", "token=secret"),
	}}
	bundleValue, err := NewOpenTelemetryBundle(
		&tracerProviderStub{provider: provider, samplingPolicyReference: "always_on"},
		propagation.TraceContext{},
		WithSamplingPolicyReference("always_on"),
		WithServerSpanAttributeEnrichers(enricher),
	)
	if err != nil {
		t.Fatalf("build tracing bundle: %v", err)
	}
	bundle := bundleValue.(*openTelemetryBundle)
	tracer := extractHertzServerTracer(t, bundle)

	// 第二阶段：执行真实 Hertz Next 链；输入包含 query/Authorization/Cookie/Body 以证明默认不会导出。
	request := newTracingTestRequest("GET", "/orders/42?token=secret")
	request.Request.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	request.Request.Header.Set("Authorization", "Bearer private")
	request.Request.Header.Set("Cookie", "session=private")
	request.Request.SetBodyString("private body")
	var handlerContext context.Context
	request.SetHandlers([]app.HandlerFunc{
		bundle.middleware[0],
		func(ctx context.Context, request *app.RequestContext) {
			handlerContext = ctx
			request.SetFullPath("/orders/:id")
			request.SetStatusCode(consts.StatusCreated)
		},
	})
	ctx := tracer.Start(context.Background(), request)
	request.Next(ctx)
	tracer.Finish(ctx, request)

	// 第三阶段：断言传播关系、完成态语义和安全 Attribute 集合。
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected one ended span, got=%d", len(spans))
	}
	span := spans[0]
	if span.Parent().TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" || span.SpanKind() != trace.SpanKindServer {
		t.Fatalf("unexpected propagation: parent=%s kind=%s", span.Parent().TraceID(), span.SpanKind())
	}
	if span.Name() != "GET /orders/:id" || span.Status().Code != codes.Unset {
		t.Fatalf("unexpected final span: name=%q status=%v", span.Name(), span.Status())
	}
	attributes := spanAttributeMap(span.Attributes())
	if attributes["http.route"] != "/orders/:id" || attributes["http.response.status_code"] != int64(consts.StatusCreated) || attributes["deployment.environment"] != "test" {
		t.Fatalf("unexpected semantic attributes: %v", attributes)
	}
	for _, forbidden := range []string{"url.full", "url.query", "http.request.header.authorization", "request.query", "http.request.body", "http.response.body"} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("forbidden attribute exported: key=%s attributes=%v", forbidden, attributes)
		}
	}
	metadata := bundle.RequestObservationMetadata(handlerContext, request)
	if metadata.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("unexpected metadata trace id: %s", metadata.TraceID)
	}
}

// TestOpenTelemetryBundleIgnoresExactPathAndNeverUsesGlobalFallback 验证忽略路径不创建 Span，缺少 borrowed 依赖时不会读取 OTel global。
// 测试方法：先用 nil Provider/Propagator 断言构造失败，再对精确忽略路径执行配对链并断言 SpanRecorder 为空。
func TestOpenTelemetryBundleIgnoresExactPathAndNeverUsesGlobalFallback(t *testing.T) {
	// 第一阶段：缺少任一 borrowed 依赖都必须在 Bundle 构造期失败。
	if _, err := NewOpenTelemetryBundle(nil, propagation.TraceContext{}); !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected provider-required error, got=%v", err)
	}
	provider := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	bundleValue, err := NewOpenTelemetryBundle(
		&tracerProviderStub{provider: provider},
		nil,
	)
	if !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) || bundleValue != nil {
		t.Fatalf("expected propagator-required error: bundle=%v err=%v", bundleValue, err)
	}

	// 第二阶段：使用 Recorder 验证精确 ignore 不创建 Span，邻近路径仍可被正常插桩。
	recorder := tracetest.NewSpanRecorder()
	recordingProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = recordingProvider.Shutdown(context.Background()) })
	bundleValue, err = NewOpenTelemetryBundle(
		&tracerProviderStub{provider: recordingProvider},
		propagation.TraceContext{},
		WithTracingIgnoredPaths("/healthz"),
	)
	if err != nil {
		t.Fatalf("build ignored bundle: %v", err)
	}
	bundle := bundleValue.(*openTelemetryBundle)
	tracer := extractHertzServerTracer(t, bundle)
	request := newTracingTestRequest("GET", "/healthz")
	request.SetHandlers(bundle.middleware)
	ctx := tracer.Start(context.Background(), request)
	request.Next(ctx)
	tracer.Finish(ctx, request)
	if spans := recorder.Ended(); len(spans) != 0 {
		t.Fatalf("expected ignored request without spans, got=%d", len(spans))
	}
}

// TestOpenTelemetryBundleMarksPanicAndServerError 验证穿透 panic 使用稳定 500/error.type 且不记录 panic 文本。
// 测试方法：下游 panic 后恢复测试进程，再调用 Hertz Finish，断言 Span 错误状态、稳定类型及属性中无私密 panic 值。
func TestOpenTelemetryBundleMarksPanicAndServerError(t *testing.T) {
	// 第一阶段：安装会 panic 的自定义命名器，验证策略故障不会破坏业务请求。
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	bundleValue, err := NewOpenTelemetryBundle(
		&tracerProviderStub{provider: provider},
		propagation.TraceContext{},
		WithServerSpanNameFormatter(func(_ ServerSpanSnapshot) string { panic("formatter private value") }),
	)
	if err != nil {
		t.Fatalf("build panic bundle: %v", err)
	}
	bundle := bundleValue.(*openTelemetryBundle)
	tracer := extractHertzServerTracer(t, bundle)
	request := newTracingTestRequest("POST", "/panic")
	request.SetFullPath("/panic")
	request.SetHandlers([]app.HandlerFunc{bundle.middleware[0], func(_ context.Context, _ *app.RequestContext) { panic("private panic value") }})
	ctx := tracer.Start(context.Background(), request)
	// 第二阶段：保留原始 panic 语义供测试恢复，再让 Hertz tracer 完成稳定错误 Span。
	func() {
		defer func() { _ = recover() }()
		request.Next(ctx)
	}()
	tracer.Finish(ctx, request)

	span := recorder.Ended()[0]
	attributes := spanAttributeMap(span.Attributes())
	if span.Status().Code != codes.Error || span.Status().Description != "panic" || attributes["error.type"] != "panic" || attributes["http.response.status_code"] != int64(500) {
		t.Fatalf("unexpected panic span: status=%v attributes=%v", span.Status(), attributes)
	}
	if span.Name() != "POST /panic" || strings.Contains(strings.ToLower(span.Status().Description), "private") || strings.Contains(strings.ToLower(span.Name()), "private") {
		t.Fatalf("panic value leaked into span: name=%q status=%v", span.Name(), span.Status())
	}
}

// TestTraceAlignedRequestIDGeneratorUsesExplicitFallback 验证对齐策略只在有效 Span 时使用 Trace ID，否则保持显式 fallback。
// 测试方法：分别传入无 Span context 和固定有效 SpanContext，断言 fallback 调用次数与返回值。
func TestTraceAlignedRequestIDGeneratorUsesExplicitFallback(t *testing.T) {
	// 第一阶段：无有效 Span 时只能调用显式 fallback，不能生成隐藏默认值。
	fallbackCalls := 0
	generator, err := NewTraceAlignedRequestIDGenerator(func(_ context.Context, _ *app.RequestContext) (string, error) {
		fallbackCalls++
		return "fallback-id", nil
	})
	if err != nil {
		t.Fatalf("build aligned generator: %v", err)
	}
	request := app.NewContext(0)
	value, err := generator(context.Background(), request)
	if err != nil || value != "fallback-id" || fallbackCalls != 1 {
		t.Fatalf("unexpected fallback result: value=%q calls=%d err=%v", value, fallbackCalls, err)
	}
	// 第二阶段：有效 SpanContext 直接提供 Trace ID，并保持 fallback 调用次数不变。
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID})
	value, err = generator(trace.ContextWithSpanContext(context.Background(), spanContext), request)
	if err != nil || value != traceID.String() || fallbackCalls != 1 {
		t.Fatalf("unexpected aligned result: value=%q calls=%d err=%v", value, fallbackCalls, err)
	}
	if _, err := NewTraceAlignedRequestIDGenerator(nil); !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected fallback-required error, got=%v", err)
	}
}

// TestTracingOptionsRejectInvalidAndMismatchedPolicies 验证路径、typed Enricher 和采样策略引用在构造前 fail-fast。
// 测试方法：表驱动非法 Option，再用不同 Provider policy 构造 Bundle，断言均为 declaration_invalid。
func TestTracingOptionsRejectInvalidAndMismatchedPolicies(t *testing.T) {
	// 第一阶段：逐类拒绝会破坏安全边界或导致运行期空引用的 Option。
	var typedNil *serverSpanAttributeEnricherStub
	for name, option := range map[string]TracingOption{
		"nil option":         nil,
		"relative path":      WithTracingIgnoredPaths("healthz"),
		"path with query":    WithTracingIgnoredPaths("/healthz?full=1"),
		"empty policy":       WithSamplingPolicyReference(" "),
		"nil formatter":      WithServerSpanNameFormatter(nil),
		"typed nil enricher": WithServerSpanAttributeEnrichers(typedNil),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := resolveTracingOptions([]TracingOption{option}); !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
				t.Fatalf("expected declaration error, got=%v", err)
			}
		})
	}
	// 第二阶段：adapter 声明必须匹配 Provider owner 发布的实际采样策略引用。
	provider := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	_, err := NewOpenTelemetryBundle(
		&tracerProviderStub{provider: provider, samplingPolicyReference: "parent_ratio_10"},
		propagation.TraceContext{},
		WithSamplingPolicyReference("always_on"),
	)
	if !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected sampling mismatch error, got=%v", err)
	}
}

// extractHertzServerTracer 从 Bundle 原生 Option 中解析唯一 Hertz tracer，验证 Option/Middleware 确实成对。
func extractHertzServerTracer(t *testing.T, bundle *openTelemetryBundle) *hertzServerTracer {
	t.Helper()
	options := hertzconfig.NewOptions(bundle.hertzOptions)
	if len(options.Tracers) != 1 {
		t.Fatalf("expected one Hertz tracer, got=%d", len(options.Tracers))
	}
	tracer, ok := options.Tracers[0].(*hertzServerTracer)
	if !ok {
		t.Fatalf("unexpected Hertz tracer type: %T", options.Tracers[0])
	}
	return tracer
}

// newTracingTestRequest 创建具有标准初始 handler index 的 Hertz 请求上下文。
func newTracingTestRequest(method string, uri string) *app.RequestContext {
	request := app.NewContext(0)
	request.Request.SetMethod(method)
	request.Request.SetRequestURI(uri)
	return request
}

// spanAttributeMap 把 OTel Attribute 转为便于断言的值映射。
func spanAttributeMap(attributes []attribute.KeyValue) map[string]any {
	result := make(map[string]any, len(attributes))
	for _, item := range attributes {
		result[string(item.Key)] = item.Value.AsInterface()
	}
	return result
}
