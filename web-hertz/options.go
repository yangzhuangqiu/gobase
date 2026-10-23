package webhertz

import (
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

// Option 定义 web-hertz 构造阶段的可选声明合同。
//
// 语义:
// 1. Hertz Option、Middleware 和 RouteFunc 分别保持声明顺序；
// 2. 所有 Middleware 总是在 RouteFunc 前注册；
// 3. nil Option 在 New 阶段 fail-fast。
type Option func(options *buildOptions) error

// buildOptions 保存构造期分类后的原生扩展声明。
type buildOptions struct {
	// logger 是上层为当前 Server 实例选择的诊断出口；缺失时回退 Hertz SystemLogger。
	logger baselog.Logger
	// hertzOptions 在 typed Config 之后应用，允许显式覆盖 provider 默认值。
	hertzOptions []config.Option
	// middleware 按声明顺序在所有路由前注册。
	middleware []app.HandlerFunc
	// routes 按声明顺序执行并共享同一个 Hertz Router。
	routes []RouteFunc
	// noRoute 按声明顺序组成未匹配路由的处理链，并继承全局 Middleware。
	noRoute []app.HandlerFunc
	// noMethod 按声明顺序组成 Method 不匹配处理链，并强制开启 405 路由扫描。
	noMethod []app.HandlerFunc
}

// WithLogger 注入当前 Server 实例使用的结构化诊断 Logger。
//
// 典型调用场景：app-web-hertz 从 IOC LoggerFactory 派生稳定命名 Logger 后传入，使 provider 自有日志不依赖进程全局 hlog。
//
// 参数:
// - logger: 必填且不得为 typed nil；所有权仍属于调用方，Server 不执行 Close。
func WithLogger(logger baselog.Logger) Option {
	return func(target *buildOptions) error {
		if isNilLogger(logger) {
			return errorForDeclarationInvalid(messagef("op=web_hertz.option result=failed stage=declare_logger reason=logger_required"))
		}
		target.logger = logger
		return nil
	}
}

// WithHertzOptions 追加 Hertz 原生构造 Option。
//
// 参数:
// - options: 按声明顺序应用；零值 Option 的 F 为 nil，构造时返回声明错误。
func WithHertzOptions(options ...config.Option) Option {
	return func(target *buildOptions) error {
		for index, option := range options {
			if option.F == nil {
				return errorForDeclarationInvalid(messagef("op=web_hertz.option result=failed stage=declare_hertz_option reason=option_required option_index=%d", index))
			}
			target.hertzOptions = append(target.hertzOptions, option)
		}
		return nil
	}
}

// WithMiddleware 追加 Hertz 原生全局 Middleware。
//
// 参数:
// - handlers: 按声明顺序执行；nil Handler 在构造阶段 fail-fast。
func WithMiddleware(handlers ...app.HandlerFunc) Option {
	return func(target *buildOptions) error {
		for index, handler := range handlers {
			if handler == nil {
				return errorForDeclarationInvalid(messagef("op=web_hertz.option result=failed stage=declare_middleware reason=handler_required handler_index=%d", index))
			}
			target.middleware = append(target.middleware, handler)
		}
		return nil
	}
}

// WithRoutes 追加构造阶段的显式路由注册函数。
//
// 参数:
// - routes: 按声明顺序执行；nil RouteFunc 在构造阶段 fail-fast。
func WithRoutes(routes ...RouteFunc) Option {
	return func(target *buildOptions) error {
		for index, route := range routes {
			if route == nil {
				return errorForDeclarationInvalid(messagef("op=web_hertz.option result=failed stage=declare_routes reason=route_required route_index=%d", index))
			}
			target.routes = append(target.routes, route)
		}
		return nil
	}
}
