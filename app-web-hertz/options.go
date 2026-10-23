package appwebhertz

import (
	"reflect"

	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// Option 定义 app-web-hertz 构造阶段的可选装配合同。
//
// 语义:
// 1. Option 只保存声明，不获取配置、IOC 或网络资源；
// 2. nil Option 在 Server 构造阶段返回声明错误；
// 3. scope 与路径风格使用最后一次有效声明，web-hertz Option 按调用方声明顺序保存。
type Option func(options *buildOptions) error

// buildOptions 保存一次 Server 构造已经归一化的应用装配声明。
type buildOptions struct {
	// scope 是 Server bean Init 阶段单次读取的独立配置路径。
	scope string
	// serverOptions 在 typed Config 后按声明顺序传给 web-hertz。
	serverOptions []webhertz.Option
	// routePathStyle 决定 Controller 方法后缀到 URL 的自动映射风格。
	routePathStyle RoutePathStyle
	// recoveryHandler 替换配置启用后的内建 panic 响应和诊断策略。
	recoveryHandler RecoveryHandler
	// requestIDGenerator 替换配置启用后的默认随机标识生成器。
	requestIDGenerator RequestIDGenerator
	// corsOriginValidator 扩展配置启用后的静态 Origin 白名单。
	corsOriginValidator CORSOriginValidator
	// gzipPredicate 在基础协议与容量检查后决定是否压缩最终响应。
	gzipPredicate GzipPredicate
}

// defaultOptions 创建使用默认配置路径和路径风格的纯构造声明。
func defaultOptions() *buildOptions {
	return &buildOptions{scope: DefaultScope, routePathStyle: RoutePathStyleKebab}
}

// WithScope 设置 Server bean 单次读取的独立配置路径。
//
// 参数:
// - scope: trim 后为空时回退 `web.hertz`；非空值原样作为 config dotted path。
//
// 说明:
// - 多次声明时最后一次生效；本 Option 不读取 ConfigManager。
func WithScope(scope string) Option {
	return func(options *buildOptions) error {
		options.scope = resolveScope(scope)
		return nil
	}
}

// WithServerOptions 追加 web-hertz 原生 provider 扩展声明。
//
// 参数:
// - serverOptions: 按声明顺序应用；nil Option 在应用装配边界 fail-fast。
func WithServerOptions(serverOptions ...webhertz.Option) Option {
	return func(options *buildOptions) error {
		for index, item := range serverOptions {
			if item == nil {
				return errorForDeclarationInvalid(messagef("op=app_web_hertz.option result=failed stage=declare_server_options reason=option_required option_index=%d", index))
			}
			options.serverOptions = append(options.serverOptions, item)
		}
		return nil
	}
}

// WithRoutePathStyle 设置 Controller 方法名自动映射 URL 的命名风格。
//
// 参数:
// - style: 支持 Kebab、Camel 或 Snake；非法值在读取配置和创建 Server 前 fail-fast。
//
// 说明:
// - 本 Option 不影响 GroupPath 和 RegisterRoutes 中显式声明的路径。
func WithRoutePathStyle(style RoutePathStyle) Option {
	return func(options *buildOptions) error {
		if !style.valid() {
			return errorForDeclarationInvalid(messagef("op=app_web_hertz.option result=failed stage=declare_route_path_style reason=style_unsupported style=%d", style))
		}
		options.routePathStyle = style
		return nil
	}
}

// WithRecoveryHandler 替换配置启用后的内建 Recovery 处理策略。
//
// 参数:
// - handler: 必填；负责受控日志和最终响应，nil 在读取配置前 fail-fast。
func WithRecoveryHandler(handler RecoveryHandler) Option {
	return func(options *buildOptions) error {
		if handler == nil {
			return errorForDeclarationInvalid("op=app_web_hertz.option result=failed stage=declare_recovery_handler reason=handler_required")
		}
		options.recoveryHandler = handler
		return nil
	}
}

// WithRequestIDGenerator 替换配置启用后的默认随机 Request ID 生成器。
//
// 参数:
// - generator: 必填；返回值和错误在请求阶段按 Request ID 合同处理。
func WithRequestIDGenerator(generator RequestIDGenerator) Option {
	return func(options *buildOptions) error {
		if generator == nil {
			return errorForDeclarationInvalid("op=app_web_hertz.option result=failed stage=declare_request_id_generator reason=generator_required")
		}
		options.requestIDGenerator = generator
		return nil
	}
}

// WithCORSOriginValidator 扩展配置启用后的静态 Origin 白名单。
//
// 参数:
// - validator: 必填；可作为未配置静态 Origin 时的唯一判定策略。
func WithCORSOriginValidator(validator CORSOriginValidator) Option {
	return func(options *buildOptions) error {
		if validator == nil {
			return errorForDeclarationInvalid("op=app_web_hertz.option result=failed stage=declare_cors_origin_validator reason=validator_required")
		}
		options.corsOriginValidator = validator
		return nil
	}
}

// WithGzipPredicate 增加配置启用后的响应压缩判定策略。
//
// 参数:
// - predicate: 必填；只在协议、状态、容量和媒体类型检查通过后调用。
func WithGzipPredicate(predicate GzipPredicate) Option {
	return func(options *buildOptions) error {
		if predicate == nil {
			return errorForDeclarationInvalid("op=app_web_hertz.option result=failed stage=declare_gzip_predicate reason=predicate_required")
		}
		options.gzipPredicate = predicate
		return nil
	}
}

// valid 判断路径风格是否属于公开支持集合。
// 阶段角色：Option 解析期拒绝未来值，防止自动路由退化为非预期默认值。
func (this RoutePathStyle) valid() bool {
	return this >= RoutePathStyleKebab && this <= RoutePathStyleSnake
}

// resolveOptions 顺序应用构造声明并拒绝 nil Option。
// 阶段角色：所有配置读取、IOC 注册和 provider 构造前的纯内存校验。
func resolveOptions(options []Option) (*buildOptions, error) {
	result := defaultOptions()
	for index, option := range options {
		if option == nil {
			return nil, errorForDeclarationInvalid(messagef("op=app_web_hertz.options result=failed stage=resolve reason=option_required option_index=%d", index))
		}
		if err := option(result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// isNilInterface 判断接口是否为 nil 或携带可空类型的 typed nil。
// 阶段角色：统一保护应用组件、Manager 和 Registry 声明边界。
func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
