package appwebhertz

import (
	"sort"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
	baseweb "github.com/yangzhuangqiu/gobase/web"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// filterEntry 保存 Filter 的稳定排序信息和已解析原生 Handler。
type filterEntry struct {
	// order 是 Filter 声明的全局 Middleware 优先级，数值越小越先执行。
	order int
	// inputIndex 是显式输入或 IOC 注册顺序，用于相同 order 的稳定排序。
	inputIndex int
	// handler 是 Filter.DoFilter 的原生 Hertz 方法值。
	handler hertzapp.HandlerFunc
}

// instrumentationPlan 保存全部 Bundle 校验后的原子 Hertz 构造片段。
type instrumentationPlan struct {
	// hertzOptions 先于调用方原生高级 Option 应用。
	hertzOptions []hertzconfig.Option
	// middleware 位于 advanced outer 与标准 Middleware 之间。
	middleware []hertzapp.HandlerFunc
	// metadataProviders 把 Trace ID 等完成态关联值桥接给 Request Observer。
	metadataProviders []webhertz.RequestObservationMetadataProvider
}

// New 使用显式配置、Controller 和 Filter 构造应用级 Web Server。
//
// 参数:
// - input: web-hertz 独立启动配置；
// - controllers: 按输入顺序注册路由；
// - filters: 按 Order 升序、相同 Order 输入顺序注册全局 Middleware；
// - options: 路径风格、标准 typed policy 和 web-hertz 原生扩展声明；标准能力未启用时 policy 不执行。
//
// 返回:
// - web.Server: 构造完成且尚未运行的单周期 Server；
// - error: 声明或 provider Server 构造失败，不返回半初始化对象。
func New(input webhertz.Config, controllers []Controller, filters []Filter, options ...Option) (baseweb.Server, error) {
	resolvedOptions, err := resolveOptions(options)
	if err != nil {
		return nil, err
	}
	return buildServer(Config{Server: input}, controllers, filters, resolvedOptions)
}

// NewConfigured 使用显式 Server/标准 Middleware 配置构造应用级 Web Server。
//
// 参数:
// - input: app-web-hertz 完整启动配置；Server 保留 web-hertz 合同；
// - controllers: 按输入顺序注册路由；
// - filters: 位于标准 Middleware 内层，并按 Order 稳定排序；
// - options: 路径风格、标准策略和 web-hertz 原生扩展声明。
//
// 返回:
// - web.Server: 配置和完整 Middleware/Route 链已冻结的单周期 Server；
// - error: 配置、typed policy、应用声明或 provider 构造失败。
func NewConfigured(input Config, controllers []Controller, filters []Filter, options ...Option) (baseweb.Server, error) {
	resolvedOptions, err := resolveOptions(options)
	if err != nil {
		return nil, err
	}
	return buildServer(input, controllers, filters, resolvedOptions)
}

// buildServer 执行显式值校验和 web-hertz Server 构造事务。
//
// 流程:
// 1. 校验 Server/Middleware Config；
// 2. 按固定阶段把标准能力、Controller/Filter 和原生 Option 转换为构造计划；
// 3. 构建并返回唯一的 provider Server。
func buildServer(
	input Config,
	controllers []Controller,
	filters []Filter,
	options *buildOptions,
) (baseweb.Server, error) {
	// 第一阶段：同时固定 provider 和标准 Middleware 配置，避免构造一半后发现非法组合。
	resolvedConfig, err := resolveConfiguredConfig(input)
	if err != nil {
		return nil, errorForServerBuildFailed(
			"op=app_web_hertz.new result=failed stage=validate_config reason=config_invalid",
			err,
		)
	}
	return buildResolvedServer(resolvedConfig, controllers, filters, options)
}

// buildResolvedServer 把已冻结配置和应用组件转换为唯一 provider Server。
//
// 流程:
// 1. 把 Controller/Filter 转换为原生路由和 Handler；
// 2. 按 advanced、standard、application 固定阶段合并 Middleware；
// 3. 构建 provider，成功后不再持有配置来源或 IOC 对象。
func buildResolvedServer(
	input Config,
	controllers []Controller,
	filters []Filter,
	options *buildOptions,
) (baseweb.Server, error) {
	// 第二阶段：把全部应用组件转换为不依赖 IOC 的不可变原生扩展。
	routes, err := prepareControllers(controllers, options.routePathStyle)
	if err != nil {
		return nil, err
	}
	handlers, err := prepareFilters(filters)
	if err != nil {
		return nil, err
	}
	instrumentation, err := prepareInstrumentationBundles(options.instrumentationBundles)
	if err != nil {
		return nil, err
	}
	standardPlan, err := prepareStandardMiddleware(input.Middleware, options)
	if err != nil {
		return nil, err
	}
	requestObservers, err := prepareRequestObservers(input.Middleware.AccessLog, options, instrumentation.metadataProviders)
	if err != nil {
		return nil, err
	}

	// 第三阶段：Bundle 原生 Option 先应用，advanced outer 保持最外；插桩位于其后、Request ID 之前。
	serverOptions := make([]webhertz.Option, 0, len(options.serverOptions)+6)
	if len(instrumentation.hertzOptions) > 0 {
		serverOptions = append(serverOptions, webhertz.WithHertzOptions(instrumentation.hertzOptions...))
	}
	serverOptions = append(serverOptions, options.serverOptions...)
	if len(instrumentation.middleware) > 0 {
		serverOptions = append(serverOptions, webhertz.WithMiddleware(instrumentation.middleware...))
	}
	if len(standardPlan.beforeObservation) > 0 {
		serverOptions = append(serverOptions, webhertz.WithMiddleware(standardPlan.beforeObservation...))
	}
	if len(requestObservers) > 0 {
		serverOptions = append(serverOptions, webhertz.WithRequestObservers(requestObservers...))
	}
	if len(standardPlan.afterObservation) > 0 {
		serverOptions = append(serverOptions, webhertz.WithMiddleware(standardPlan.afterObservation...))
	}
	if len(handlers) > 0 {
		serverOptions = append(serverOptions, webhertz.WithMiddleware(handlers...))
	}
	if len(routes) > 0 {
		serverOptions = append(serverOptions, webhertz.WithRoutes(routes...))
	}
	server, err := webhertz.New(input.Server, serverOptions...)
	if err != nil {
		return nil, errorForServerBuildFailed(
			"op=app_web_hertz.new result=failed stage=build_server reason=provider_build_failed",
			err,
		)
	}
	return server, nil
}

// prepareInstrumentationBundles 在 provider 构造前展开并验证全部原子插桩声明。
// 阶段角色：任何 Bundle 非法都会阻止全部 Hertz Option/Middleware 安装，避免半插桩 Server。
func prepareInstrumentationBundles(bundles []InstrumentationBundle) (instrumentationPlan, error) {
	plan := instrumentationPlan{}
	for bundleIndex, bundle := range bundles {
		if isNilInterface(bundle) {
			return instrumentationPlan{}, errorForDeclarationInvalid(messagef("op=app_web_hertz.new result=failed stage=validate_instrumentation reason=bundle_required bundle_index=%d", bundleIndex))
		}
		// 先完整校验当前 Bundle，再合并到计划；失败时不会发布当前 Bundle 的任何一半。
		nativeOptions := bundle.HertzOptions()
		for optionIndex, option := range nativeOptions {
			if option.F == nil {
				return instrumentationPlan{}, errorForDeclarationInvalid(messagef("op=app_web_hertz.new result=failed stage=validate_instrumentation reason=hertz_option_required bundle_index=%d option_index=%d", bundleIndex, optionIndex))
			}
		}
		middleware := bundle.Middleware()
		for handlerIndex, handler := range middleware {
			if handler == nil {
				return instrumentationPlan{}, errorForDeclarationInvalid(messagef("op=app_web_hertz.new result=failed stage=validate_instrumentation reason=middleware_required bundle_index=%d handler_index=%d", bundleIndex, handlerIndex))
			}
		}
		plan.hertzOptions = append(plan.hertzOptions, nativeOptions...)
		plan.middleware = append(plan.middleware, middleware...)
		if provider, ok := bundle.(webhertz.RequestObservationMetadataProvider); ok && !isNilInterface(provider) {
			plan.metadataProviders = append(plan.metadataProviders, provider)
		}
	}
	return plan, nil
}

// prepareFilters 校验 Filter 并按 Order 把直接方法值做稳定排序。
// 阶段角色：全部声明在 provider 构造前 fail-fast，输出直接交给 web-hertz Middleware。
func prepareFilters(filters []Filter) ([]hertzapp.HandlerFunc, error) {
	// 第一阶段：校验完整输入并保存原始索引，typed nil 不进入排序或 provider。
	entries := make([]filterEntry, 0, len(filters))
	for index, item := range filters {
		if isNilInterface(item) {
			return nil, errorForDeclarationInvalid(messagef("op=app_web_hertz.new result=failed stage=validate_filters reason=filter_required filter_index=%d", index))
		}
		entries = append(entries, filterEntry{order: item.Order(), inputIndex: index, handler: item.DoFilter})
	}
	// 第二阶段：Order 优先，输入/IOC 注册索引作为相同 Order 的稳定次序。
	sort.SliceStable(entries, func(left, right int) bool {
		if entries[left].order == entries[right].order {
			return entries[left].inputIndex < entries[right].inputIndex
		}
		return entries[left].order < entries[right].order
	})
	// 第三阶段：只投影原生方法值，请求期不增加适配对象。
	handlers := make([]hertzapp.HandlerFunc, 0, len(entries))
	for _, entry := range entries {
		handlers = append(handlers, entry.handler)
	}
	return handlers, nil
}
