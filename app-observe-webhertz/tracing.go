package webhertzobserve

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
	hertztracer "github.com/cloudwego/hertz/pkg/common/tracer"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	appwebhertz "github.com/yangzhuangqiu/gobase/app-web-hertz"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	// tracingInstrumentationName 是 OTel Scope 中识别本适配器的稳定名称。
	tracingInstrumentationName = "github.com/yangzhuangqiu/gobase/app-observe-webhertz"
	// tracingInstrumentationVersion 与首个公开 Trace 合同同步，供 Exporter 区分语义版本。
	tracingInstrumentationVersion = "0.1.0"
)

// TracingOption 定义 OTel Hertz 插桩的纯值策略声明。
type TracingOption func(options *tracingOptions) error

// tracingOptions 保存构造前已冻结的 Trace 策略。
type tracingOptions struct {
	// ignoredPaths 是不创建 Span 且不提取传播上下文的精确路径集合。
	ignoredPaths map[string]struct{}
	// samplingPolicyReference 是调用方期望 Provider 已采用的策略引用。
	samplingPolicyReference string
	// spanNameFormatter 只接收安全路由快照，不接触原始 URL。
	spanNameFormatter ServerSpanNameFormatter
	// enrichers 按声明顺序追加经安全过滤的自定义 Attribute。
	enrichers []ServerSpanAttributeEnricher
}

// openTelemetryBundle 是不可变的 OTel Hertz Option/Middleware 配对结果。
type openTelemetryBundle struct {
	// hertzOptions 安装与 Server Middleware 配对的 Hertz internal tracer。
	hertzOptions []hertzconfig.Option
	// middleware 提取传播上下文并创建 HTTP server span。
	middleware []app.HandlerFunc
}

// hertzServerTracer 在 Hertz 请求边界创建 carrier，并在响应完成后补齐最终 Attribute/Status。
type hertzServerTracer struct {
	// tracer 从 borrowed Provider 创建，不读取 OTel global。
	tracer trace.Tracer
	// propagator 是 borrowed 入站传播策略。
	propagator propagation.TextMapPropagator
	// options 是构造期冻结且请求期只读的策略。
	options tracingOptions
}

// serverSpanState 跨 Hertz tracer 与 Middleware 共享单请求 Span 状态。
type serverSpanState struct {
	// mu 保护异常展开和 Hertz Finish 可能跨执行边界访问状态。
	mu sync.Mutex
	// span 是 Middleware 创建、Tracer Finish 终结的当前 Server Span。
	span trace.Span
	// panicked 标记未被内层 Recovery 消化的 panic。
	panicked bool
}

// serverSpanStateContextKey 隔离本包在标准 context 中保存的请求状态。
type serverSpanStateContextKey struct{}

// hertzRequestHeaderCarrier 把 Hertz Header 窄适配为 OTel TextMapCarrier。
type hertzRequestHeaderCarrier struct {
	// header 指向当前请求期 Header，只在同步 Extract 调用内使用。
	header *protocol.RequestHeader
}

var (
	_ appwebhertz.InstrumentationBundle           = (*openTelemetryBundle)(nil)
	_ webhertz.RequestObservationMetadataProvider = (*openTelemetryBundle)(nil)
	_ hertztracer.Tracer                          = (*hertzServerTracer)(nil)
)

// WithTracingIgnoredPaths 声明不创建 Server Span 的精确路径。
//
// 参数:
// - paths: 必须是以 `/` 开头且不含 query/fragment 的绝对路径；重复值自动去重。
func WithTracingIgnoredPaths(paths ...string) TracingOption {
	return func(options *tracingOptions) error {
		for index, path := range paths {
			path = strings.TrimSpace(path)
			if path == "" || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
				return buildError(ErrCodeDeclarationInvalid, messagef("op=app_observe_webhertz.tracing result=failed stage=declare_ignore_paths reason=path_invalid path_index=%d", index))
			}
			options.ignoredPaths[path] = struct{}{}
		}
		return nil
	}
}

// WithSamplingPolicyReference 要求 borrowed Provider 的采样策略引用与声明一致。
//
// 说明:
// - 本 Option 不创建或动态修改 Sampler；采样实现仍由 Provider owner 决定；
// - 空白引用非法，匹配失败在 adapter Init 阶段 fail-fast。
func WithSamplingPolicyReference(reference string) TracingOption {
	return func(options *tracingOptions) error {
		reference = strings.TrimSpace(reference)
		if reference == "" {
			return buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.tracing result=failed stage=declare_sampling_policy reason=reference_required")
		}
		options.samplingPolicyReference = reference
		return nil
	}
}

// WithServerSpanNameFormatter 替换默认 `METHOD route` Span 命名策略。
func WithServerSpanNameFormatter(formatter ServerSpanNameFormatter) TracingOption {
	return func(options *tracingOptions) error {
		if formatter == nil {
			return buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.tracing result=failed stage=declare_span_name reason=formatter_required")
		}
		options.spanNameFormatter = formatter
		return nil
	}
}

// WithServerSpanAttributeEnrichers 追加只接收安全完成态快照的 Attribute Enricher。
func WithServerSpanAttributeEnrichers(enrichers ...ServerSpanAttributeEnricher) TracingOption {
	return func(options *tracingOptions) error {
		for index, enricher := range enrichers {
			if isNilValue(enricher) {
				return buildError(ErrCodeDeclarationInvalid, messagef("op=app_observe_webhertz.tracing result=failed stage=declare_enrichers reason=enricher_required enricher_index=%d", index))
			}
			options.enrichers = append(options.enrichers, enricher)
		}
		return nil
	}
}

// NewOpenTelemetryBundle 使用 borrowed Provider/Propagator 构造原子 Hertz 插桩 Bundle。
//
// 参数:
// - provider: 必填；由外部 managed bean 拥有并覆盖 Web Server 生命周期；
// - propagator: 必填；不会回退到 OTel global；
// - options: span 命名、忽略路径、采样策略引用和 Attribute 扩展。
//
// 返回:
// - InstrumentationBundle: 无资源、可并发复用的配对声明；
// - error: 依赖或策略非法，不返回半初始化 Bundle。
func NewOpenTelemetryBundle(
	provider TracerProvider,
	propagator propagation.TextMapPropagator,
	options ...TracingOption,
) (appwebhertz.InstrumentationBundle, error) {
	resolved, err := resolveTracingOptions(options)
	if err != nil {
		return nil, err
	}
	return newOpenTelemetryBundleFromConfig(provider, propagator, resolved)
}

// newOpenTelemetryBundleFromConfig 使用 Module 已冻结的策略构造 Bundle，避免 Init 重放用户 Option。
// 阶段角色：显式构造和 IOC 构造共享同一依赖校验、采样引用匹配和无全局 fallback 语义。
func newOpenTelemetryBundleFromConfig(
	provider TracerProvider,
	propagator propagation.TextMapPropagator,
	resolved tracingOptions,
) (appwebhertz.InstrumentationBundle, error) {
	if isNilValue(provider) || isNilValue(propagator) {
		return nil, buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.tracing result=failed stage=build_bundle reason=provider_and_propagator_required")
	}
	otelProvider := provider.OTelTracerProvider()
	if isNilValue(otelProvider) {
		return nil, buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.tracing result=failed stage=build_bundle reason=otel_provider_required")
	}
	if resolved.samplingPolicyReference != "" && strings.TrimSpace(provider.SamplingPolicyReference()) != resolved.samplingPolicyReference {
		return nil, buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.tracing result=failed stage=build_bundle reason=sampling_policy_mismatch")
	}

	// Provider 与 Propagator 均已显式解析；构造过程不触碰任何 OTel global 状态。
	adapter := &hertzServerTracer{
		tracer: otelProvider.Tracer(
			tracingInstrumentationName,
			trace.WithInstrumentationVersion(tracingInstrumentationVersion),
		),
		propagator: propagator,
		options:    resolved,
	}
	return &openTelemetryBundle{
		hertzOptions: []hertzconfig.Option{server.WithTracer(adapter)},
		middleware:   []app.HandlerFunc{adapter.middleware()},
	}, nil
}

// NewTraceAlignedRequestIDGenerator 创建“有效 Trace ID 优先、显式 fallback 兜底”的 Request ID 策略。
//
// 说明:
// - 默认 Request ID 仍与 Trace ID 独立；只有调用方显式安装本 Generator 才对齐；
// - 当前 context 无有效 Span（通常包括未安装 Trace）时调用 fallback，不改变响应 Header 合同；
// - 有效但未采样的 SpanContext 仍具有 Trace ID，因此仍执行对齐。
func NewTraceAlignedRequestIDGenerator(fallback appwebhertz.RequestIDGenerator) (appwebhertz.RequestIDGenerator, error) {
	if fallback == nil {
		return nil, buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.tracing result=failed stage=build_request_id_generator reason=fallback_required")
	}
	return func(ctx context.Context, request *app.RequestContext) (string, error) {
		spanContext := trace.SpanContextFromContext(ctx)
		if spanContext.IsValid() {
			return spanContext.TraceID().String(), nil
		}
		return fallback(ctx, request)
	}, nil
}

// resolveTracingOptions 顺序应用纯声明并复制 slice/map，形成请求期不可变策略。
func resolveTracingOptions(options []TracingOption) (tracingOptions, error) {
	resolved := tracingOptions{ignoredPaths: make(map[string]struct{})}
	for index, option := range options {
		if option == nil {
			return tracingOptions{}, buildError(ErrCodeDeclarationInvalid, messagef("op=app_observe_webhertz.tracing result=failed stage=resolve_options reason=option_required option_index=%d", index))
		}
		if err := option(&resolved); err != nil {
			return tracingOptions{}, err
		}
	}
	resolved.enrichers = append([]ServerSpanAttributeEnricher{}, resolved.enrichers...)
	return resolved, nil
}

// HertzOptions 返回当前 Bundle 的 Hertz tracer Option 副本。
func (this *openTelemetryBundle) HertzOptions() []hertzconfig.Option {
	return append([]hertzconfig.Option{}, this.hertzOptions...)
}

// Middleware 返回当前 Bundle 的 Server Middleware 副本。
func (this *openTelemetryBundle) Middleware() []app.HandlerFunc {
	return append([]app.HandlerFunc{}, this.middleware...)
}

// RequestObservationMetadata 从当前请求 context 复制有效 Trace ID；无有效 Span 时返回空值。
func (this *openTelemetryBundle) RequestObservationMetadata(ctx context.Context, _ *app.RequestContext) webhertz.RequestObservationMetadata {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return webhertz.RequestObservationMetadata{}
	}
	return webhertz.RequestObservationMetadata{TraceID: spanContext.TraceID().String()}
}

// Start 为单次请求创建跨 Hertz Tracer/Middleware 共享的空状态。
func (this *hertzServerTracer) Start(ctx context.Context, _ *app.RequestContext) context.Context {
	return context.WithValue(ctx, serverSpanStateContextKey{}, &serverSpanState{})
}

// Finish 在请求完成后写入最终 route/status、运行安全 Enricher 并终结 Span。
func (this *hertzServerTracer) Finish(ctx context.Context, request *app.RequestContext) {
	state, _ := ctx.Value(serverSpanStateContextKey{}).(*serverSpanState)
	if state == nil {
		return
	}
	state.mu.Lock()
	span := state.span
	panicked := state.panicked || appwebhertz.RequestPanicked(request)
	state.mu.Unlock()
	if span == nil {
		return
	}

	// 第一阶段：只从完成态请求复制有限字段；未匹配路由绝不回退到原始 path。
	status := request.Response.StatusCode()
	if status == 0 {
		status = consts.StatusOK
	}
	if panicked && status < consts.StatusInternalServerError {
		status = consts.StatusInternalServerError
	}
	snapshot := ServerSpanSnapshot{
		Method:       strings.ToUpper(strings.TrimSpace(string(request.Method()))),
		RoutePattern: safeRoutePattern(request.FullPath()),
		Status:       status,
		Panicked:     panicked,
	}
	span.SetName(resolveServerSpanName(this.options.spanNameFormatter, snapshot))
	span.SetAttributes(
		semconv.HTTPRequestMethodKey.String(snapshot.Method),
		semconv.HTTPRouteKey.String(snapshot.RoutePattern),
		semconv.HTTPResponseStatusCodeKey.Int(snapshot.Status),
	)

	// 第二阶段：5xx 和穿透 panic 使用稳定原因，不记录错误文本、panic 值或 Stack。
	if snapshot.Panicked {
		span.SetStatus(codes.Error, "panic")
		span.SetAttributes(attribute.String("error.type", "panic"))
	} else if snapshot.Status >= consts.StatusInternalServerError {
		span.SetStatus(codes.Error, "http_server_error")
		span.SetAttributes(attribute.String("error.type", "http_server_error"))
	}

	// 第三阶段：Enricher 只接收安全快照；panic 和保留/敏感键由适配器隔离。
	enrichmentContext := trace.ContextWithSpan(ctx, span)
	for _, enricher := range this.options.enrichers {
		span.SetAttributes(safeEnrichedAttributes(enrichmentContext, enricher, snapshot)...)
	}
	span.End()
}

// middleware 提取远端上下文、创建 Server Span，并把 Span context 传给内层标准链。
func (this *hertzServerTracer) middleware() app.HandlerFunc {
	return func(ctx context.Context, request *app.RequestContext) {
		if _, ignored := this.options.ignoredPaths[string(request.Path())]; ignored {
			request.Next(ctx)
			return
		}
		state, _ := ctx.Value(serverSpanStateContextKey{}).(*serverSpanState)
		if state == nil {
			// 只有绕过配对 Hertz Option 时才可能触发；保持业务可用但不静默创建孤立 Span。
			request.Next(ctx)
			return
		}

		// 传播提取先于 Span 创建，固定 Server kind；初始名称只使用 method/route 安全字段。
		parentContext := this.propagator.Extract(ctx, hertzRequestHeaderCarrier{header: &request.Request.Header})
		startSnapshot := ServerSpanSnapshot{
			Method:       strings.ToUpper(strings.TrimSpace(string(request.Method()))),
			RoutePattern: safeRoutePattern(request.FullPath()),
		}
		spanContext, span := this.tracer.Start(
			parentContext,
			resolveServerSpanName(this.options.spanNameFormatter, startSnapshot),
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(semconv.HTTPRequestMethodKey.String(startSnapshot.Method)),
		)
		state.mu.Lock()
		state.span = span
		state.mu.Unlock()

		// 穿透 panic 只保存布尔语义并原样抛出，具体 Recovery/响应仍由 app-web-hertz 负责。
		defer func() {
			if recovered := recover(); recovered != nil {
				state.mu.Lock()
				state.panicked = true
				state.mu.Unlock()
				panic(recovered)
			}
		}()
		request.Next(spanContext)
	}
}

// Get 从 Hertz 请求 Header 读取传播字段。
func (this hertzRequestHeaderCarrier) Get(key string) string {
	return this.header.Get(key)
}

// Set 写入 Hertz 请求 Header；Server Extract 路径通常不会调用。
func (this hertzRequestHeaderCarrier) Set(key string, value string) {
	this.header.Set(key, value)
}

// Keys 返回当前 Header key 的值副本，供组合 Propagator 枚举。
func (this hertzRequestHeaderCarrier) Keys() []string {
	keys := make([]string, 0, this.header.Len())
	this.header.VisitAll(func(key, _ []byte) {
		keys = append(keys, string(key))
	})
	return keys
}

// safeRoutePattern 把未匹配路由收敛到固定值，禁止原始路径进入 Span 名称和 Attribute。
func safeRoutePattern(route string) string {
	route = strings.TrimSpace(route)
	if route == "" {
		return webhertz.UnmatchedRoutePattern
	}
	return route
}

// resolveServerSpanName 执行可选 Formatter，并为空白结果提供稳定安全回退。
func resolveServerSpanName(formatter ServerSpanNameFormatter, snapshot ServerSpanSnapshot) (result string) {
	defer func() {
		if recover() != nil {
			result = strings.TrimSpace(fmt.Sprintf("%s %s", snapshot.Method, snapshot.RoutePattern))
		}
	}()
	if formatter != nil {
		if name := strings.TrimSpace(formatter(snapshot)); name != "" {
			return name
		}
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s", snapshot.Method, snapshot.RoutePattern))
}

// safeEnrichedAttributes 隔离 Enricher panic，并过滤保留键、空键和疑似敏感字段。
func safeEnrichedAttributes(ctx context.Context, enricher ServerSpanAttributeEnricher, snapshot ServerSpanSnapshot) (result []attribute.KeyValue) {
	defer func() {
		if recover() != nil {
			result = nil
		}
	}()
	for _, item := range enricher.EnrichServerSpan(ctx, snapshot) {
		key := strings.TrimSpace(string(item.Key))
		if key == "" || unsafeSpanAttributeKey(key) {
			continue
		}
		result = append(result, item)
	}
	return result
}

// unsafeSpanAttributeKey 判断核心 HTTP 合同与常见 Secret/载荷字段，阻止 Enricher 覆盖或泄露。
func unsafeSpanAttributeKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if _, reserved := reservedServerSpanAttributeKeys[normalized]; reserved {
		return true
	}
	for _, fragment := range sensitiveServerSpanAttributeKeyFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

// reservedServerSpanAttributeKeys 固定适配器拥有的标准 Attribute，Enricher 不得覆盖。
var reservedServerSpanAttributeKeys = map[string]struct{}{
	"http.request.method": {}, "http.route": {}, "http.response.status_code": {}, "error.type": {},
}

// sensitiveServerSpanAttributeKeyFragments 是默认拒绝的 Secret、Header 和载荷键片段。
var sensitiveServerSpanAttributeKeyFragments = []string{
	"authorization", "cookie", "credential", "password", "secret", "token", "request.body", "response.body", "query",
}
