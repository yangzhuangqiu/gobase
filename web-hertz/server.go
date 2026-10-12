package webhertz

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/app/server/registry"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/route"
	baseweb "github.com/yangzhuangqiu/gobase/web"
)

// Server 是持有 Hertz Engine 的单运行周期 Web Server。
//
// 语义:
// 1. 实现 web.Server，并把 Hertz Run/Shutdown 差异收敛为稳定生命周期；
// 2. 独占底层 *server.Hertz，不暴露绕过状态机的启动和停止入口；
// 3. 构造后配置、Middleware 和 Routes 均已冻结。
type Server struct {
	// engine 是本实例独占的 Hertz Server。
	engine *server.Hertz
	// address 是应用全部原生 Option 后的实际监听地址，用于生命周期诊断。
	address string
	// mu 串行保护生命周期状态、结果和 channel 关闭。
	mu sync.Mutex
	// state 表示当前单运行周期所处阶段。
	state lifecycleState
	// engineDone 在 Hertz Run goroutine 返回后关闭。
	engineDone chan struct{}
	// runningObservationDone 在运行状态观测 goroutine 退出后关闭，约束最终 hlog 访问边界。
	runningObservationDone chan struct{}
	// shutdownDone 在共享停止事务完成后关闭。
	shutdownDone chan struct{}
	// shutdownStarted 标记共享停止事务已被触发。
	shutdownStarted bool
	// shutdownCompleted 标记 shutdownDone 已关闭并允许读取 shutdownErr。
	shutdownCompleted bool
	// engineErr 保存 Hertz Run 的原始结果。
	engineErr error
	// shutdownErr 保存底层 Hertz Shutdown 的原始结果。
	shutdownErr error
}

var _ baseweb.Server = (*Server)(nil)

// New 解析配置并构造尚未监听的 Hertz Server。
//
// 参数:
// - input: web-hertz 独立 typed Config；
// - options: Hertz 原生 Option、Middleware、NoRoute/NoMethod 和 RouteFunc 声明。
//
// 返回:
// - *Server: 构造成功且尚未进入运行周期的实例；
// - error: 配置、声明、Registry 或显式路由注册失败，不返回半初始化 Server。
func New(input Config, options ...Option) (*Server, error) {
	resolvedConfig, err := ResolveConfig(input)
	if err != nil {
		return nil, err
	}

	// 第一阶段：分类聚合声明，保证 Middleware、fallback、Routes 的阶段顺序稳定。
	resolvedOptions := &buildOptions{}
	for index, option := range options {
		if option == nil {
			return nil, errorForDeclarationInvalid(messagef("op=web_hertz.new result=failed stage=declare reason=option_required option_index=%d", index))
		}
		if err := option(resolvedOptions); err != nil {
			return nil, err
		}
	}

	// 第二阶段：typed Config 先应用，原生 Option 按声明顺序覆盖；NoMethod 可达性是窄例外。
	nativeOptions := resolvedConfig.hertzOptions()
	nativeOptions = append(nativeOptions, resolvedOptions.hertzOptions...)
	if len(resolvedOptions.noMethod) > 0 {
		// 自定义 NoMethod 若不可达会造成静默错误，因此其窄合同最终覆盖关闭 405 的原生 Option。
		nativeOptions = append(nativeOptions, server.WithHandleMethodNotAllowed(true))
	}
	if err := validateHertzOptions(nativeOptions); err != nil {
		return nil, err
	}
	engine := server.New(nativeOptions...)
	finalOptions := engine.GetOptions()
	if err := validateRegistry(finalOptions); err != nil {
		return nil, err
	}

	// 第三阶段：先冻结全局 Middleware，再安装 fallback，最后执行显式路由事务。
	if len(resolvedOptions.middleware) > 0 {
		engine.Use(resolvedOptions.middleware...)
	}
	if len(resolvedOptions.noRoute) > 0 {
		engine.NoRoute(resolvedOptions.noRoute...)
	}
	if len(resolvedOptions.noMethod) > 0 {
		engine.NoMethod(resolvedOptions.noMethod...)
	}
	// RouteFunc 共享一个根路径 group，保留路由顺序但不暴露 Hertz 生命周期入口。
	router := engine.Group("/")
	if err := registerRoutes(router, resolvedOptions.routes); err != nil {
		return nil, err
	}

	return &Server{
		engine:                 engine,
		address:                finalOptions.Addr,
		state:                  lifecycleStateNew,
		engineDone:             make(chan struct{}),
		runningObservationDone: make(chan struct{}),
		shutdownDone:           make(chan struct{}),
	}, nil
}

// validateHertzOptions 拒绝 F 为空的 Hertz 原生 Option，避免 server.New panic。
// 阶段角色：在进入第三方构造器前完成可预测的输入校验。
func validateHertzOptions(options []config.Option) error {
	for index, option := range options {
		if option.F == nil {
			return errorForDeclarationInvalid(messagef("op=web_hertz.new result=failed stage=validate_hertz_options reason=option_required option_index=%d", index))
		}
	}
	return nil
}

// validateRegistry 检查最终 Hertz Options，阻止注册/注销生命周期不对称。
// 阶段角色：原生 Option 全部应用后按最终值做能力拒绝，而不是猜测 Option 来源。
func validateRegistry(options *config.Options) error {
	defaultRegistryType := reflect.TypeOf(registry.NoopRegistry)
	configuredRegistryType := reflect.TypeOf(options.Registry)
	if options.RegistryInfo == nil && configuredRegistryType == defaultRegistryType {
		return nil
	}
	return errorForRegistryUnsupported("op=web_hertz.new result=failed stage=validate_registry reason=spin_registration_hook_unavailable")
}

// registerRoutes 执行顺序确定的路由注册事务。
// 阶段角色：普通 error 停止构造；编程错误 panic 补充阶段语义后继续传播。
func registerRoutes(router *route.RouterGroup, routes []RouteFunc) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panic(&routeRegistrationPanic{cause: recovered})
		}
	}()
	for index, route := range routes {
		if err := route(router); err != nil {
			return errorForRouteRegistrationFailed(
				messagef("op=web_hertz.new result=failed stage=register_routes route_index=%d reason=route_error", index),
				err,
			)
		}
	}
	return nil
}

// routeRegistrationPanic 为 Hertz 路由编程错误补充稳定构造阶段。
type routeRegistrationPanic struct {
	// cause 保存原始 panic 值，便于测试和故障定位。
	cause any
}

// Error 返回包含 register_routes 阶段和原始 panic 的诊断文本。
func (this *routeRegistrationPanic) Error() string {
	return fmt.Sprintf("op=web_hertz.new result=panic stage=register_routes reason=%v", this.cause)
}

// Unwrap 在原始 panic 为 error 时保留其错误链身份。
func (this *routeRegistrationPanic) Unwrap() error {
	cause, _ := this.cause.(error)
	return cause
}
