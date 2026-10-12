package webhertzobserve

import (
	"context"
	"net/http"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
	"github.com/prometheus/client_golang/prometheus"
	appwebhertz "github.com/yangzhuangqiu/gobase/app-web-hertz"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
	"go.opentelemetry.io/otel/propagation"
)

const (
	// moduleName 是应用运行时识别重复观测适配模块的稳定名称。
	moduleName = "app-observe-webhertz"
	// adaptersBeanName 是全部按需信号接口共享的原子 managed bean 名称。
	adaptersBeanName = "WebHertzObservationAdapters"
)

// prometheusRegistryMode 描述 Module 对 Registry 的所有权选择。
type prometheusRegistryMode uint8

const (
	// prometheusRegistryDisabled 表示 Module 不声明 Prometheus 能力。
	prometheusRegistryDisabled prometheusRegistryMode = iota
	// prometheusRegistryPrivate 表示 managed bean 创建并拥有私有 Registry。
	prometheusRegistryPrivate
	// prometheusRegistryShared 表示 managed bean 注入并借用调用方 Registry。
	prometheusRegistryShared
)

// ModuleOption 定义可选信号适配 bean 的纯声明合同。
type ModuleOption func(options *moduleOptions) error

// moduleOptions 保存 Module 注册前已冻结的能力选择。
type moduleOptions struct {
	// requestLogEnabled 决定是否声明 RequestLogObserver bean。
	requestLogEnabled bool
	// requestLogConfig 是冻结的无资源日志适配策略。
	requestLogConfig requestLogOptions
	// prometheusMode 决定私有 owned 或共享 borrowed Registry。
	prometheusMode prometheusRegistryMode
	// prometheusConfig 是冻结的 HTTP Collector 纯值配置。
	prometheusConfig prometheusOptions
	// tracingEnabled 决定是否声明原子 InstrumentationBundle bean。
	tracingEnabled bool
	// tracingConfig 是冻结的 OTel Hertz 插桩策略。
	tracingConfig tracingOptions
}

// Module 定义按需注册 Request Log/Prometheus 适配 bean 的合同。
type Module interface {
	// Name 返回应用运行时用于重复检测的稳定模块名。
	Name() string
	// Register 在 IOC Build 前声明适配 bean，不读取配置、不注册 Collector 或启动 Server。
	Register(registry ioc.Registry) error
}

// module 是可跨 Container 重放的纯声明对象。
type module struct {
	// options 保存构造时复制的 Module Option。
	options []ModuleOption
}

// WithRequestLog 声明借用 LoggerFactory 的 Request Log Adapter bean。
func WithRequestLog(options ...RequestLogOption) ModuleOption {
	return func(target *moduleOptions) error {
		if target.requestLogEnabled {
			return buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.module result=failed stage=declare_request_log reason=duplicate_capability")
		}
		resolved, err := resolveRequestLogOptions(options)
		if err != nil {
			return err
		}
		target.requestLogEnabled = true
		target.requestLogConfig = resolved
		return nil
	}
}

// WithPrivatePrometheus 声明 owned-private Registry、Collector registration 和 Handler bean。
func WithPrivatePrometheus(options ...PrometheusOption) ModuleOption {
	return declarePrometheusModule(prometheusRegistryPrivate, options)
}

// WithSharedPrometheus 声明借用 PrometheusRegistry 并拥有自身 Collector registration 的 bean。
func WithSharedPrometheus(options ...PrometheusOption) ModuleOption {
	return declarePrometheusModule(prometheusRegistryShared, options)
}

// WithOpenTelemetry 声明借用 TracerProvider/Propagator 的 OTel Hertz Instrumentation Bundle。
//
// 参数:
// - options: span 命名、忽略路径、采样策略引用和安全 Attribute Enricher；不得包含 nil。
//
// 说明:
// - 本能力不创建/关闭 Provider 或 Exporter，也不访问 OTel global；
// - 重复声明在 Registry 修改前 fail-fast。
func WithOpenTelemetry(options ...TracingOption) ModuleOption {
	return func(target *moduleOptions) error {
		if target.tracingEnabled {
			return buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.module result=failed stage=declare_tracing reason=duplicate_capability")
		}
		resolved, err := resolveTracingOptions(options)
		if err != nil {
			return err
		}
		target.tracingEnabled = true
		target.tracingConfig = resolved
		return nil
	}
}

// declarePrometheusModule 创建互斥 Registry 所有权声明，并在 Registry 修改前校验指标配置。
func declarePrometheusModule(mode prometheusRegistryMode, options []PrometheusOption) ModuleOption {
	return func(target *moduleOptions) error {
		if target.prometheusMode != prometheusRegistryDisabled {
			return buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.module result=failed stage=declare_prometheus reason=registry_mode_conflict")
		}
		resolved, err := resolvePrometheusOptions(options)
		if err != nil {
			return err
		}
		target.prometheusMode = mode
		target.prometheusConfig = resolved
		return nil
	}
}

// NewModule 创建不持有 Logger、Registry、Collector 或 Handler 的可重放适配模块声明。
func NewModule(options ...ModuleOption) Module {
	return &module{options: append([]ModuleOption{}, options...)}
}

// Name 返回应用运行时识别的稳定模块名。
func (this *module) Name() string {
	return moduleName
}

// Register 校验全部能力声明后，把适配接口作为单个 IOC bean 原子发布。
func (this *module) Register(registry ioc.Registry) error {
	if this == nil || isNilValue(registry) {
		return buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.module result=failed stage=precheck reason=module_or_registry_required")
	}
	resolved, err := resolveModuleOptions(this.options)
	if err != nil {
		return err
	}
	if !resolved.requestLogEnabled && resolved.prometheusMode == prometheusRegistryDisabled && !resolved.tracingEnabled {
		return buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.module result=failed stage=resolve_options reason=capability_required")
	}

	// 单一 managed bean 按需声明多个语义接口，使 Registry 修改和 Init 资源创建都具备原子边界。
	bean := &managedAdapters{
		requestLogEnabled: resolved.requestLogEnabled,
		requestLogConfig:  resolved.requestLogConfig,
		prometheusMode:    resolved.prometheusMode,
		prometheusConfig:  resolved.prometheusConfig,
		tracingEnabled:    resolved.tracingEnabled,
		tracingConfig:     resolved.tracingConfig,
	}
	registerOptions := []ioc.RegisterOption{ioc.WithName(adaptersBeanName)}
	if resolved.requestLogEnabled {
		registerOptions = append(registerOptions, ioc.WithDeclare[appwebhertz.RequestLogObserver]())
	}
	if resolved.prometheusMode != prometheusRegistryDisabled {
		registerOptions = append(registerOptions, ioc.WithDeclare[webhertz.RequestObserver](), ioc.WithDeclare[PrometheusHandler]())
	}
	if resolved.tracingEnabled {
		registerOptions = append(registerOptions, ioc.WithDeclare[appwebhertz.InstrumentationBundle]())
	}
	if err := registry.Register(bean, registerOptions...); err != nil {
		return buildError(ErrCodeModuleRegisterFailed, "op=app_observe_webhertz.module result=failed stage=register_adapters reason=ioc_register_failed", err)
	}
	return nil
}

// resolveModuleOptions 顺序应用非空 Option 并形成单次注册计划。
func resolveModuleOptions(options []ModuleOption) (moduleOptions, error) {
	resolved := moduleOptions{}
	for index, option := range options {
		if option == nil {
			return moduleOptions{}, buildError(ErrCodeDeclarationInvalid, messagef("op=app_observe_webhertz.module result=failed stage=resolve_options reason=option_required option_index=%d", index))
		}
		if err := option(&resolved); err != nil {
			return moduleOptions{}, err
		}
	}
	return resolved, nil
}

// managedAdapters 在一个 IOC 节点内原子发布可选日志、Prometheus 与 Trace 适配能力。
type managedAdapters struct {
	// Factory 只在 Request Log 启用时借用；optional 避免 Prometheus-only 模式产生虚假依赖。
	Factory baselog.LoggerFactory `ioc:"required=false"`
	// Registry 只在 shared Prometheus 模式由 IOC 注入并借用。
	Registry PrometheusRegistry `ioc:"required=false"`
	// TraceProvider 只在 OTel 启用时注入并借用；Provider owner 负责最终 ForceFlush/Shutdown。
	TraceProvider TracerProvider `ioc:"required=false"`
	// Propagator 只在 OTel 启用时注入并借用；缺失时不回退全局传播器。
	Propagator propagation.TextMapPropagator `ioc:"required=false"`
	// requestLogEnabled 决定是否构造日志委托。
	requestLogEnabled bool
	// requestLogConfig 是冻结的日志纯值策略。
	requestLogConfig requestLogOptions
	// prometheusMode 决定 Registry 所有权。
	prometheusMode prometheusRegistryMode
	// prometheusConfig 是冻结的指标纯值策略。
	prometheusConfig prometheusOptions
	// tracingEnabled 决定是否构造并发布原子插桩 Bundle。
	tracingEnabled bool
	// tracingConfig 是冻结的 OTel Hertz 策略。
	tracingConfig tracingOptions
	// requestLogObserver 在完整 Init 成功后发布。
	requestLogObserver appwebhertz.RequestLogObserver
	// prometheusRuntime 在完整 Init 成功后发布并由 Close 注销。
	prometheusRuntime *PrometheusRuntime
	// instrumentationBundle 在完整 Init 成功后发布，只借用 Provider/Propagator。
	instrumentationBundle appwebhertz.InstrumentationBundle
}

var (
	_ ioc.Initializer                             = (*managedAdapters)(nil)
	_ ioc.Closer                                  = (*managedAdapters)(nil)
	_ appwebhertz.RequestLogObserver              = (*managedAdapters)(nil)
	_ webhertz.RequestObserver                    = (*managedAdapters)(nil)
	_ webhertz.RequestLifecycleObserver           = (*managedAdapters)(nil)
	_ PrometheusHandler                           = (*managedAdapters)(nil)
	_ appwebhertz.InstrumentationBundle           = (*managedAdapters)(nil)
	_ webhertz.RequestObservationMetadataProvider = (*managedAdapters)(nil)
)

// Init 先构造无资源日志/Trace 适配，再注册 Prometheus；全部成功后一次性发布全部委托。
func (this *managedAdapters) Init(_ context.Context) error {
	var requestLogObserver appwebhertz.RequestLogObserver
	var prometheusRuntime *PrometheusRuntime
	var instrumentationBundle appwebhertz.InstrumentationBundle
	var err error
	// 第一阶段：日志适配只借用已初始化 Factory，不取得资源所有权。
	if this.requestLogEnabled {
		requestLogObserver, err = newRequestLogObserver(this.Factory, this.requestLogConfig)
		if err != nil {
			return err
		}
	}
	// 第二阶段：Tracing 只构造借用依赖的无资源 Bundle，必须先于 Prometheus registration 完成。
	if this.tracingEnabled {
		instrumentationBundle, err = newOpenTelemetryBundleFromConfig(this.TraceProvider, this.Propagator, this.tracingConfig)
		if err != nil {
			return err
		}
	}
	// 第三阶段：Prometheus registration 是唯一资源动作，失败时尚未发布任何委托。
	if this.prometheusMode == prometheusRegistryPrivate {
		prometheusRuntime, err = newPrometheusRuntimeFromConfig(prometheus.NewRegistry(), this.prometheusConfig)
	} else if this.prometheusMode == prometheusRegistryShared {
		prometheusRuntime, err = newPrometheusRuntimeFromConfig(this.Registry, this.prometheusConfig)
	}
	if err != nil {
		return err
	}
	// 第四阶段：全部构造成功后原子发布，IOC 后续 Server bean 才能开始使用。
	this.requestLogObserver = requestLogObserver
	this.prometheusRuntime = prometheusRuntime
	this.instrumentationBundle = instrumentationBundle
	return nil
}

// HertzOptions 委托已初始化 Bundle 返回原生 tracer Option；未声明接口时不会被 app-web-hertz 注入。
func (this *managedAdapters) HertzOptions() []hertzconfig.Option {
	if this.instrumentationBundle == nil {
		return nil
	}
	return this.instrumentationBundle.HertzOptions()
}

// Middleware 委托已初始化 Bundle 返回配对 Server Middleware。
func (this *managedAdapters) Middleware() []hertzapp.HandlerFunc {
	if this.instrumentationBundle == nil {
		return nil
	}
	return this.instrumentationBundle.Middleware()
}

// RequestObservationMetadata 委托 Bundle 复制 Trace ID；未初始化时返回空元数据。
func (this *managedAdapters) RequestObservationMetadata(ctx context.Context, request *hertzapp.RequestContext) webhertz.RequestObservationMetadata {
	provider, ok := this.instrumentationBundle.(webhertz.RequestObservationMetadataProvider)
	if !ok || isNilValue(provider) {
		return webhertz.RequestObservationMetadata{}
	}
	return provider.RequestObservationMetadata(ctx, request)
}

// ObserveRequestLog 委托已初始化日志适配器；未声明该接口时 IOC 不会把本 bean 注入请求链。
func (this *managedAdapters) ObserveRequestLog(ctx context.Context, observation webhertz.HTTPRequestObservation, slow bool) {
	if this.requestLogObserver != nil {
		this.requestLogObserver.ObserveRequestLog(ctx, observation, slow)
	}
}

// RequestStarted 委托已初始化 Runtime 更新 in-flight Gauge。
func (this *managedAdapters) RequestStarted(ctx context.Context, method string) {
	if this.prometheusRuntime != nil {
		this.prometheusRuntime.RequestStarted(ctx, method)
	}
}

// Observe 委托已初始化 Runtime 更新完成态指标。
func (this *managedAdapters) Observe(ctx context.Context, observation webhertz.HTTPRequestObservation) {
	if this.prometheusRuntime != nil {
		this.prometheusRuntime.Observe(ctx, observation)
	}
}

// ServeHTTP 暴露已初始化 Gatherer；异常提前调用时返回稳定 503，不启动隐藏 Server。
func (this *managedAdapters) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if this.prometheusRuntime == nil {
		http.Error(writer, "prometheus runtime not initialized", http.StatusServiceUnavailable)
		return
	}
	this.prometheusRuntime.ServeHTTP(writer, request)
}

// Close 注销本 bean 的 Collector，不关闭 shared Registry 或任何 HTTP Server。
func (this *managedAdapters) Close(_ context.Context) error {
	if this.prometheusRuntime == nil {
		return nil
	}
	return this.prometheusRuntime.Close()
}
