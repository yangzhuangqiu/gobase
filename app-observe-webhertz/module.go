package webhertzobserve

import (
	"context"
	"errors"
	"net/http"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
	"github.com/prometheus/client_golang/prometheus"
	appwebhertz "github.com/yangzhuangqiu/gobase/app-web-hertz"
	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/ioc"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
	"go.opentelemetry.io/otel/propagation"
)

const (
	// moduleName 是应用运行时识别重复观测适配模块的稳定名称。
	moduleName = "app-observe-webhertz"
	// observationBeanName 是全部观测接口与 owned Metrics Resource 共享的原子 managed bean 名称。
	observationBeanName = "WebHertzObservation"
)

// prometheusRegistryMode 描述 Module 对 Registry 的所有权选择。
type prometheusRegistryMode uint8

const (
	// prometheusRegistryPrivate 表示 managed bean 创建并拥有私有 Registry。
	prometheusRegistryPrivate prometheusRegistryMode = iota
	// prometheusRegistryShared 表示 managed bean 注入并借用调用方 Registry。
	prometheusRegistryShared
)

// ModuleOption 定义可选信号适配 bean 的纯声明合同。
type ModuleOption func(options *moduleOptions) error

// moduleOptions 保存 Module 注册前已冻结的能力选择。
type moduleOptions struct {
	// prometheusDeclared 标记调用方是否显式覆盖默认 private Registry 声明，用于拒绝互斥重复声明。
	prometheusDeclared bool
	// prometheusMode 决定私有 owned 或共享 borrowed Registry。
	prometheusMode prometheusRegistryMode
	// tracingEnabled 决定是否声明原子 InstrumentationBundle bean。
	tracingEnabled bool
	// tracingCodeOptions 是 Formatter/Enricher 等不可序列化代码扩展声明。
	tracingCodeOptions tracingCodeOptions
	// configScope 是统一可观测快照在 IOC Init 阶段读取一次的配置路径。
	configScope string
}

// Module 定义按需注册 Metrics/Tracing 适配 bean 的合同。
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

// WithPrivatePrometheus 声明 owned-private Registry、Collector registration 和 Handler bean。
func WithPrivatePrometheus() ModuleOption {
	return declarePrometheusModule(prometheusRegistryPrivate)
}

// WithSharedPrometheus 声明借用 PrometheusRegistry 并拥有自身 Collector registration 的 bean。
func WithSharedPrometheus() ModuleOption {
	return declarePrometheusModule(prometheusRegistryShared)
}

// WithConfigScope 设置统一可观测启动快照的单次读取路径。
//
// 参数:
// - configScope: trim 后为空时回退 DefaultConfigScope；自定义路径缺失时启动失败。
//
// 说明:
// - 多次声明时最后一次生效；
// - 本 Option 不读取 ConfigManager，也不创建 listener。
func WithConfigScope(configScope string) ModuleOption {
	return func(target *moduleOptions) error {
		target.configScope = resolveConfigLocator(configScope).configScope
		return nil
	}
}

// WithOpenTelemetry 声明借用 TracerProvider/Propagator 的 OTel Hertz Instrumentation Bundle。
//
// 参数:
// - options: span 命名和安全 Attribute Enricher 代码扩展；不得包含 nil。
//
// 说明:
// - 本能力不创建/关闭 Provider 或 Exporter，也不访问 OTel global；
// - 重复声明在 Registry 修改前 fail-fast。
func WithOpenTelemetry(options ...TracingOption) ModuleOption {
	return func(target *moduleOptions) error {
		if target.tracingEnabled {
			return buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.module result=failed stage=declare_tracing reason=duplicate_capability")
		}
		resolved, err := resolveTracingCodeOptions(options)
		if err != nil {
			return err
		}
		target.tracingEnabled = true
		target.tracingCodeOptions = resolved
		return nil
	}
}

// declarePrometheusModule 创建互斥 Registry 所有权声明；指标运行策略统一由 Config 快照提供。
func declarePrometheusModule(mode prometheusRegistryMode) ModuleOption {
	return func(target *moduleOptions) error {
		if target.prometheusDeclared {
			return buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.module result=failed stage=declare_prometheus reason=registry_mode_conflict")
		}
		target.prometheusMode = mode
		target.prometheusDeclared = true
		return nil
	}
}

// NewModule 创建默认启用 private Prometheus 与独立 Metrics Server 的可重放模块声明。
// 典型调用场景：与 app-web-hertz Module 一起传给 app.WithModules；主动运行由两个独立 Workload 显式声明。
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
	// 单一 managed bean 原子发布 Observer、Handler、Metrics Server Resource 与可选 Trace Bundle。
	bean := &managedObservation{
		prometheusMode:     resolved.prometheusMode,
		tracingEnabled:     resolved.tracingEnabled,
		tracingCodeOptions: resolved.tracingCodeOptions,
		configScope:        resolved.configScope,
	}
	registerOptions := []ioc.RegisterOption{
		ioc.WithName(observationBeanName),
		ioc.WithDeclare[webhertz.RequestObserver](),
		ioc.WithDeclare[PrometheusHandler](),
		ioc.WithDeclare[metricsServerResource](),
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
	// 第一阶段：零参数 Module 直接形成 private Prometheus + 默认统一配置路径。
	resolved := moduleOptions{
		prometheusMode: prometheusRegistryPrivate,
		configScope:    DefaultConfigScope,
	}
	// 第二阶段：显式高级声明按顺序覆盖默认值，nil 和互斥能力在注册 IOC 前失败。
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

// managedObservation 在一个 IOC 节点内原子发布 Prometheus/可选 Trace 适配能力，并拥有独立 Metrics Server Resource。
type managedObservation struct {
	// Manager 在统一可观测快照初始化时读取并借用；缺失时默认 scope 使用完整默认值。
	Manager baseconfig.Manager `ioc:"required=false"`
	// Registry 只在 shared Prometheus 模式由 IOC 注入并借用。
	Registry PrometheusRegistry `ioc:"required=false"`
	// TraceProvider 只在 OTel 启用时注入并借用；Provider owner 负责最终 ForceFlush/Shutdown。
	TraceProvider TracerProvider `ioc:"required=false"`
	// Propagator 只在 OTel 启用时注入并借用；缺失时不回退全局传播器。
	Propagator propagation.TextMapPropagator `ioc:"required=false"`
	// prometheusMode 决定 Registry 所有权。
	prometheusMode prometheusRegistryMode
	// tracingEnabled 决定是否构造并发布原子插桩 Bundle。
	tracingEnabled bool
	// tracingCodeOptions 保存不可序列化的 OTel Formatter/Enricher 扩展。
	tracingCodeOptions tracingCodeOptions
	// configScope 保存统一可观测启动快照的单次配置路径。
	configScope string
	// prometheusRuntime 在完整 Init 成功后发布并由 Close 注销。
	prometheusRuntime *PrometheusRuntime
	// instrumentationBundle 在完整 Init 成功后发布，只借用 Provider/Propagator。
	instrumentationBundle appwebhertz.InstrumentationBundle
	// metricsServer 是 Init 成功后发布给独立 Workload 的主动 Resource。
	metricsServer *managedMetricsServer
}

var (
	_ ioc.Initializer                             = (*managedObservation)(nil)
	_ ioc.Closer                                  = (*managedObservation)(nil)
	_ webhertz.RequestObserver                    = (*managedObservation)(nil)
	_ webhertz.RequestLifecycleObserver           = (*managedObservation)(nil)
	_ PrometheusHandler                           = (*managedObservation)(nil)
	_ appwebhertz.InstrumentationBundle           = (*managedObservation)(nil)
	_ webhertz.RequestObservationMetadataProvider = (*managedObservation)(nil)
	_ metricsServerResource                       = (*managedObservation)(nil)
)

// Init 构造 Trace/Prometheus adapter，并在任何 Workload 启动前预绑定 Metrics listener。
func (this *managedObservation) Init(ctx context.Context) error {
	var prometheusRuntime *PrometheusRuntime
	var instrumentationBundle appwebhertz.InstrumentationBundle
	var err error
	// 第一阶段：读取并冻结统一配置快照，尚未获取 listener 或 Collector registration。
	config, err := loadConfig(
		ctx,
		this.Manager,
		resolveConfigLocator(this.configScope),
	)
	if err != nil {
		return err
	}
	// 第二阶段：Tracing 只构造借用依赖的无资源 Bundle，必须先于 Prometheus registration 完成。
	if this.tracingEnabled {
		tracingOptions := buildTracingOptions(config.Tracing, this.tracingCodeOptions)
		instrumentationBundle, err = newOpenTelemetryBundleFromConfig(this.TraceProvider, this.Propagator, tracingOptions)
		if err != nil {
			return err
		}
	}
	// 第三阶段：注册 Prometheus Collector；后续 listener 失败必须显式回滚该 registration。
	prometheusOptions := prometheusOptionsFromConfig(config.Prometheus)
	if this.prometheusMode == prometheusRegistryPrivate {
		prometheusRuntime, err = newPrometheusRuntimeFromConfig(prometheus.NewRegistry(), prometheusOptions)
	} else if this.prometheusMode == prometheusRegistryShared {
		prometheusRuntime, err = newPrometheusRuntimeFromConfig(this.Registry, prometheusOptions)
	}
	if err != nil {
		return err
	}
	// 第四阶段：预绑定独立 Metrics 端口，端口冲突必须发生在任一 Workload 启动之前。
	metricsServer, err := newManagedMetricsServer(config.Prometheus.Server, prometheusRuntime)
	if err != nil {
		return errors.Join(err, prometheusRuntime.Close())
	}
	// 第五阶段：全部构造成功后原子发布，IOC 后续 Server bean 和两个 Workload 才能使用。
	this.prometheusRuntime = prometheusRuntime
	this.instrumentationBundle = instrumentationBundle
	this.metricsServer = metricsServer
	return nil
}

// HertzOptions 委托已初始化 Bundle 返回原生 tracer Option；未声明接口时不会被 app-web-hertz 注入。
func (this *managedObservation) HertzOptions() []hertzconfig.Option {
	if this.instrumentationBundle == nil {
		return nil
	}
	return this.instrumentationBundle.HertzOptions()
}

// Middleware 委托已初始化 Bundle 返回配对 Server Middleware。
func (this *managedObservation) Middleware() []hertzapp.HandlerFunc {
	if this.instrumentationBundle == nil {
		return nil
	}
	return this.instrumentationBundle.Middleware()
}

// RequestObservationMetadata 委托 Bundle 复制 Trace ID；未初始化时返回空元数据。
func (this *managedObservation) RequestObservationMetadata(ctx context.Context, request *hertzapp.RequestContext) webhertz.RequestObservationMetadata {
	provider, ok := this.instrumentationBundle.(webhertz.RequestObservationMetadataProvider)
	if !ok || isNilValue(provider) {
		return webhertz.RequestObservationMetadata{}
	}
	return provider.RequestObservationMetadata(ctx, request)
}

// RequestStarted 委托已初始化 Runtime 更新 in-flight Gauge。
func (this *managedObservation) RequestStarted(ctx context.Context, method string) {
	if this.prometheusRuntime != nil {
		this.prometheusRuntime.RequestStarted(ctx, method)
	}
}

// Observe 委托已初始化 Runtime 更新完成态指标。
func (this *managedObservation) Observe(ctx context.Context, observation webhertz.HTTPRequestObservation) {
	if this.prometheusRuntime != nil {
		this.prometheusRuntime.Observe(ctx, observation)
	}
}

// ServeHTTP 暴露已初始化 Gatherer；异常提前调用时返回稳定 503，不启动隐藏 Server。
func (this *managedObservation) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if this.prometheusRuntime == nil {
		http.Error(writer, "prometheus runtime not initialized", http.StatusServiceUnavailable)
		return
	}
	this.prometheusRuntime.ServeHTTP(writer, request)
}

// run 委托独立 Metrics Server Resource 阻塞运行，不接触业务 Hertz Server。
func (this *managedObservation) run(ctx context.Context) error {
	if this.metricsServer == nil {
		return errorForMetricsServerRunFailed(
			"op=app_observe_webhertz.metrics_server result=failed stage=run reason=resource_not_initialized",
		)
	}
	return this.metricsServer.run(ctx)
}

// shutdown 委托独立 Metrics Server Resource 执行 graceful→force。
func (this *managedObservation) shutdown(ctx context.Context) error {
	if this.metricsServer == nil {
		return nil
	}
	return this.metricsServer.shutdown(ctx)
}

// endpoint 返回实际监听地址和 Prometheus 路由，供 Metrics Workload 输出启动日志。
func (this *managedObservation) endpoint() (string, string) {
	if this.metricsServer == nil {
		return "", ""
	}
	return this.metricsServer.endpoint()
}

// Close 兜底停止 Metrics Server，再注销本 bean 的 Collector registration。
func (this *managedObservation) Close(ctx context.Context) error {
	var metricsErr error
	if this.metricsServer != nil {
		metricsErr = this.metricsServer.shutdown(ctx)
	}
	var prometheusErr error
	if this.prometheusRuntime != nil {
		prometheusErr = this.prometheusRuntime.Close()
	}
	return errors.Join(metricsErr, prometheusErr)
}
