package appwebhertz

import (
	hertzapp "github.com/cloudwego/hertz/pkg/app"
	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
	baseweb "github.com/yangzhuangqiu/gobase/web"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// instrumentationPlan 保存全部 Bundle 校验后的原子 Hertz 构造片段。
type instrumentationPlan struct {
	// hertzOptions 先于调用方原生高级 Option 应用。
	hertzOptions []hertzconfig.Option
	// middleware 位于 advanced outer 与标准 Middleware 之间。
	middleware []hertzapp.HandlerFunc
	// metadataProviders 把 Trace ID 等完成态关联值桥接给 Request Observer。
	metadataProviders []webhertz.RequestObservationMetadataProvider
}

// buildServer 执行配置校验和 web-hertz Server 构造事务。
//
// 流程:
// 1. 校验 Server/Middleware Config；
// 2. 按固定阶段把完整应用组件、标准能力和原生 Option 转换为构造计划；
// 3. 构建并返回唯一的 provider Server。
func buildServer(
	input Config,
	components serverComponents,
	options *buildOptions,
) (baseweb.Server, error) {
	// 第一阶段：同时固定 provider 和标准 Middleware 配置，避免构造一半后发现非法组合。
	resolvedConfig, err := resolveConfiguredConfig(input)
	if err != nil {
		return nil, errorForServerBuildFailed(
			"op=app_web_hertz.build result=failed stage=validate_config reason=config_invalid",
			err,
		)
	}
	return buildResolvedServer(resolvedConfig, components, options)
}

// buildResolvedServer 把已冻结配置和应用组件转换为唯一 provider Server。
//
// 流程:
// 1. 把完整组件集合转换为原生路由、Middleware、fallback 和 Observer；
// 2. 按 advanced、standard、application 固定阶段合并 Middleware；
// 3. 构建 provider，成功后不再持有配置来源或 IOC 对象。
func buildResolvedServer(
	input Config,
	components serverComponents,
	options *buildOptions,
) (baseweb.Server, error) {
	// 第二阶段：先冻结完整组件集合，任何 typed nil 或非法声明都不进入 provider。
	filterPlan, err := prepareFilters(components.filters)
	if err != nil {
		return nil, err
	}
	routes, err := prepareRoutes(components.controllers, filterPlan.groupedFilters, options.routePathStyle)
	if err != nil {
		return nil, err
	}
	noRouteHandlers, err := prepareNoRouteHandlers(components.noRouteHandlers)
	if err != nil {
		return nil, err
	}
	noMethodHandlers, err := prepareNoMethodHandlers(components.noMethodHandlers)
	if err != nil {
		return nil, err
	}
	instrumentation, err := prepareInstrumentationBundles(components.instrumentationBundles)
	if err != nil {
		return nil, err
	}
	standardPlan, err := prepareStandardMiddleware(input.Middleware, options, components.loggers.middleware)
	if err != nil {
		return nil, err
	}
	requestObservers, err := prepareRequestObservers(
		input.Middleware.AccessLog,
		components.loggers.request,
		components.requestObservers,
		instrumentation.metadataProviders,
	)
	if err != nil {
		return nil, err
	}

	// 第三阶段：Bundle 原生 Option 先应用，advanced outer 保持最外；插桩位于其后、Request ID 之前。
	serverOptions := make([]webhertz.Option, 0, len(options.serverOptions)+6)
	serverOptions = append(serverOptions, webhertz.WithLogger(components.loggers.provider))
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
	if len(filterPlan.globalHandlers) > 0 {
		serverOptions = append(serverOptions, webhertz.WithMiddleware(filterPlan.globalHandlers...))
	}
	if len(noRouteHandlers) > 0 {
		serverOptions = append(serverOptions, webhertz.WithNoRoute(noRouteHandlers...))
	}
	if len(noMethodHandlers) > 0 {
		serverOptions = append(serverOptions, webhertz.WithNoMethod(noMethodHandlers...))
	}
	if len(routes) > 0 {
		serverOptions = append(serverOptions, webhertz.WithRoutes(routes...))
	}
	server, err := webhertz.New(input.Server, serverOptions...)
	if err != nil {
		return nil, errorForServerBuildFailed(
			"op=app_web_hertz.build result=failed stage=build_server reason=provider_build_failed",
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
			return instrumentationPlan{}, errorForDeclarationInvalid(messagef("op=app_web_hertz.build result=failed stage=validate_instrumentation reason=bundle_required bundle_index=%d", bundleIndex))
		}
		// 先完整校验当前 Bundle，再合并到计划；失败时不会发布当前 Bundle 的任何一半。
		nativeOptions := bundle.HertzOptions()
		for optionIndex, option := range nativeOptions {
			if option.F == nil {
				return instrumentationPlan{}, errorForDeclarationInvalid(messagef("op=app_web_hertz.build result=failed stage=validate_instrumentation reason=hertz_option_required bundle_index=%d option_index=%d", bundleIndex, optionIndex))
			}
		}
		middleware := bundle.Middleware()
		for handlerIndex, handler := range middleware {
			if handler == nil {
				return instrumentationPlan{}, errorForDeclarationInvalid(messagef("op=app_web_hertz.build result=failed stage=validate_instrumentation reason=middleware_required bundle_index=%d handler_index=%d", bundleIndex, handlerIndex))
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
