package appwebhertz

import webhertz "github.com/yangzhuangqiu/gobase/web-hertz"

// Config 定义 app-web-hertz 单个运行周期的 Server 与标准 Middleware 装配意图。
//
// 语义:
// 1. Server 在 YAML 中内联，兼容既有 web.hertz.address/readTimeout 等扁平字段；
// 2. Middleware 各能力默认关闭，只有 enabled=true 才进入请求链；
// 3. 配置只在显式构造或 IOC Init 阶段解析一次，不支持运行期热更新。
type Config struct {
	// Server 保留 web-hertz 的启动配置合同；YAML 解码时内联到当前 scope。
	Server webhertz.Config `yaml:",inline"`
	// Middleware 描述标准请求链装配；默认全部关闭且不持有外部资源。
	Middleware MiddlewareConfig `yaml:"middleware"`
}

// MiddlewareConfig 定义 app-web-hertz 固定阶段中的标准 Middleware 配置。
//
// 顺序：Request ID -> completion Observation -> Recovery -> CORS -> Gzip -> application Filters。
type MiddlewareConfig struct {
	// Recovery 控制 panic 恢复及内建 500 响应策略。
	Recovery RecoveryConfig `yaml:"recovery"`
	// RequestID 控制请求关联标识的生成、信任和响应回写。
	RequestID RequestIDConfig `yaml:"requestId"`
	// AccessLog 控制 Request Log Observer 是否接收完成态快照及其过滤策略。
	AccessLog AccessLogConfig `yaml:"accessLog"`
	// CORS 控制静态跨域白名单和预检响应。
	CORS CORSConfig `yaml:"cors"`
	// Gzip 控制满足条件的普通响应压缩。
	Gzip GzipConfig `yaml:"gzip"`
}

// DefaultConfig 返回 Server 完整默认值与全部关闭的标准 Middleware 配置。
//
// 返回:
// - Config: 每次调用均与其他配置快照隔离。
func DefaultConfig() Config {
	// Server 与各能力策略均返回独立快照；enabled 保持 false，策略默认值只为未来显式启用做准备。
	return Config{
		Server: webhertz.DefaultConfig(),
		Middleware: MiddlewareConfig{
			Recovery:  RecoveryConfig{Response: RecoveryResponseJSON},
			RequestID: RequestIDConfig{Header: defaultRequestIDHeader},
			AccessLog: AccessLogConfig{SkipPaths: []string{}},
			CORS: CORSConfig{
				AllowOrigins:  []string{},
				AllowMethods:  append([]string{}, defaultCORSMethods...),
				AllowHeaders:  append([]string{}, defaultCORSHeaders...),
				ExposeHeaders: []string{},
				MaxAge:        defaultCORSMaxAge,
			},
			Gzip: GzipConfig{
				Level:                defaultGzipLevel,
				MinLength:            defaultGzipMinLength,
				ExcludedContentTypes: append([]string{}, defaultGzipExcludedContentTypes...),
			},
		},
	}
}

// resolveConfiguredConfig 冻结 Server 与标准 Middleware 配置，供显式和 IOC 构造共用。
// 阶段角色：任何 Handler、Router 或 Hertz Engine 创建前完成默认值、复制和组合校验。
func resolveConfiguredConfig(input Config) (Config, error) {
	serverConfig, err := webhertz.ResolveConfig(input.Server)
	if err != nil {
		return Config{}, errorForConfigInvalid(
			"op=app_web_hertz.resolve_config result=failed stage=validate_server reason=provider_config_invalid",
			err,
		)
	}
	middlewareConfig, err := resolveMiddlewareConfig(input.Middleware)
	if err != nil {
		return Config{}, err
	}
	return Config{Server: serverConfig, Middleware: middlewareConfig}, nil
}
