package appwebhertz

import webhertz "github.com/yangzhuangqiu/gobase/web-hertz"

// serverComponents 保存 IOC 为单个 Server 聚合的完整应用组件集合。
// 阶段角色：managedServer 完成依赖注入后构造该快照，后续装配不再读取 IOC。
type serverComponents struct {
	// controllers 按 IOC 注册顺序声明逻辑分组和业务路由。
	controllers []Controller
	// filters 按 IOC 注册顺序包含全局与分组 Filter，具体作用域由接口类型决定。
	filters []Filter
	// noRouteHandlers 按 IOC 注册顺序声明全局 404 fallback 链。
	noRouteHandlers []NoRouteHandler
	// noMethodHandlers 按 IOC 注册顺序声明全局 405 fallback 链。
	noMethodHandlers []NoMethodHandler
	// loggers 是 IOC 根 LoggerFactory 在 Init 阶段派生的命名 Logger 快照。
	loggers serverLoggers
	// requestObservers 无需配置开关，接收 Metrics 等通用完成态快照。
	requestObservers []webhertz.RequestObserver
	// instrumentationBundles 成对提供原生 Hertz Option、Middleware 和可选元数据。
	instrumentationBundles []InstrumentationBundle
}
