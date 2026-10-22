package appwebhertz

import (
	"context"

	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/ioc"
	baseweb "github.com/yangzhuangqiu/gobase/web"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// managedServer 是 IOC Build 阶段自动聚合应用组件的单周期 Server bean。
type managedServer struct {
	// Manager 是 app-runtime 或调用方注册的配置唯一事实源，必填且只借用。
	Manager baseconfig.Manager
	// Controllers 由 IOC 按接口声明和注册顺序注入；缺失表示应用没有 Controller。
	Controllers []Controller `ioc:"required=false"`
	// Filters 由 IOC 按接口声明和注册顺序注入；普通类型全局生效，GroupedFilter 绑定逻辑分组。
	Filters []Filter `ioc:"required=false"`
	// NoRouteHandlers 由 IOC 注入并按 Order 冻结为全局 404 fallback 链。
	NoRouteHandlers []NoRouteHandler `ioc:"required=false"`
	// NoMethodHandlers 由 IOC 注入并按 Order 冻结为全局 405 fallback 链。
	NoMethodHandlers []NoMethodHandler `ioc:"required=false"`
	// RequestLogObservers 由 IOC 按语义接口注入；只有配置启用 AccessLog 时进入完成态 fan-out。
	RequestLogObservers []RequestLogObserver `ioc:"required=false"`
	// RequestObservers 由 IOC 注入 Metrics 等通用完成态观察器，不依赖 YAML 总开关。
	RequestObservers []webhertz.RequestObserver `ioc:"required=false"`
	// InstrumentationBundles 由 IOC 注入成对 Option/Middleware；缺失表示不启用 Tracing 等插桩。
	InstrumentationBundles []InstrumentationBundle `ioc:"required=false"`

	// scope 是 Init 阶段读取一次的独立 Hertz 配置路径。
	scope string
	// options 是 Module 注册前已校验并冻结的应用装配声明。
	options *buildOptions
	// server 是 Init 成功后发布的底层单周期实现。
	server baseweb.Server
}

var (
	_ baseweb.Server  = (*managedServer)(nil)
	_ ioc.Initializer = (*managedServer)(nil)
	_ ioc.Closer      = (*managedServer)(nil)
)

// Init 在 IOC 依赖完成注入和初始化后构造底层 Hertz Server。
//
// 参数:
// - ctx: IOC Build 上下文；取消时停止配置读取和 Server 构造。
//
// 返回:
// - nil: 配置、应用组件和 provider 已冻结；
// - error: 配置或构造失败，IOC Build 将回滚此前已初始化 bean。
func (this *managedServer) Init(ctx context.Context) error {
	// 第一阶段：读取并校验本运行周期的独立启动配置，不订阅热更新。
	input, err := loadConfig(ctx, this.Manager, this.scope)
	if err != nil {
		return err
	}
	// 第二阶段：把 IOC 注入结果冻结为完整组件快照，统一装配核心负责全部转换和校验。
	components := serverComponents{
		controllers:            this.Controllers,
		filters:                this.Filters,
		noRouteHandlers:        this.NoRouteHandlers,
		noMethodHandlers:       this.NoMethodHandlers,
		requestLogObservers:    this.RequestLogObservers,
		requestObservers:       this.RequestObservers,
		instrumentationBundles: this.InstrumentationBundles,
	}
	server, err := buildResolvedServer(input, components, this.options)
	if err != nil {
		return err
	}
	// 第三阶段：只在完整构造成功后发布委托，避免半初始化 Server 可见。
	this.server = server
	return nil
}

// Run 启动 IOC Build 已构造的底层 Server，并阻塞到其运行周期收敛。
//
// 参数:
// - ctx: 必填；取消时请求底层 Server 优雅停止。
//
// 返回:
// - nil: Server 正常停止；
// - error: IOC 尚未完成 Init，或底层运行/停止失败。
func (this *managedServer) Run(ctx context.Context) error {
	if this == nil || this.server == nil {
		return errorForServerStateInvalid("op=app_web_hertz.server.run result=failed stage=precheck reason=server_not_initialized")
	}
	return this.server.Run(ctx)
}

// Shutdown 请求 IOC Build 已构造的底层 Server 优雅停止。
//
// 参数:
// - ctx: 必填；只约束当前调用者等待。
//
// 返回:
// - nil: Server 已停止；
// - error: IOC 尚未完成 Init，或底层停止失败。
func (this *managedServer) Shutdown(ctx context.Context) error {
	if this == nil || this.server == nil {
		return errorForServerStateInvalid("op=app_web_hertz.server.shutdown result=failed stage=precheck reason=server_not_initialized")
	}
	return this.server.Shutdown(ctx)
}

// Close 在 IOC 逆序关闭阶段停止底层 Server，不关闭 Manager、Controller 或 Filter。
//
// 参数:
// - ctx: IOC Close 为当前 bean 分配的停止上下文。
//
// 返回:
// - nil: 未取得 Server 或 Server 已停止；
// - error: 底层优雅停止失败。
func (this *managedServer) Close(ctx context.Context) error {
	if this == nil || this.server == nil {
		return nil
	}
	return this.server.Shutdown(ctx)
}
