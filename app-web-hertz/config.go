package appwebhertz

import (
	"context"
	"strings"

	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/errcode"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

const (
	// DefaultConfigScope 是 IOC Module 默认读取的独立 Hertz 配置路径。
	DefaultConfigScope = "web.hertz"
)

// Config 定义 app-web-hertz 单个运行周期的 Server 与标准 Middleware 装配意图。
//
// 语义:
// 1. Server 在 YAML 中内联，兼容既有 web.hertz.address/readTimeout 等扁平字段；
// 2. Middleware 各能力默认关闭，只有 enabled=true 才进入请求链；
// 3. 配置只在 IOC Init 阶段解析一次，不支持运行期热更新。
type Config struct {
	// Server 保留 web-hertz 的启动配置合同；YAML 解码时内联到当前 config scope。
	Server webhertz.Config `yaml:",inline"`
	// Middleware 描述标准请求链装配；默认全部关闭且不持有外部资源。
	Middleware MiddlewareConfig `yaml:"middleware"`
}

// MiddlewareConfig 定义 app-web-hertz 固定阶段中的标准 Middleware 配置。
//
// 顺序：Request ID -> completion Observation -> Recovery -> CORS -> Gzip -> 全局/分组应用 Filters。
type MiddlewareConfig struct {
	// Recovery 控制 panic 恢复及内建 500 响应策略。
	Recovery RecoveryConfig `yaml:"recovery"`
	// RequestID 控制请求关联标识的生成、信任和响应回写。
	RequestID RequestIDConfig `yaml:"requestId"`
	// AccessLog 控制内建结构化请求日志是否消费完成态快照及其过滤策略。
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

// resolveConfiguredConfig 冻结 Server 与标准 Middleware 配置，供 Snapshot 加载和私有装配核心共用。
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

// resolveConfigScope 归一化 IOC Module 的独立配置路径。
// 阶段角色：空白声明回退稳定默认值，非空值原样作为 dotted config scope 使用。
func resolveConfigScope(configScope string) string {
	configScope = strings.TrimSpace(configScope)
	if configScope == "" {
		return DefaultConfigScope
	}
	return configScope
}

// loadConfig 从 Manager 当前快照读取并校验一次 Server/标准 Middleware 启动配置。
//
// 阶段角色：IOC Server bean 初始化时读取一次；监听地址等启动配置不订阅热更新。
// 语义：DefaultConfigScope 缺失时使用 DefaultConfig；自定义 config scope 缺失或任何已存在配置非法时 fail-fast。
func loadConfig(ctx context.Context, manager baseconfig.Manager, configScope string) (Config, error) {
	if ctx == nil {
		return Config{}, errorForConfigLoadFailed(
			messagef("op=app_web_hertz.load_config result=failed stage=precheck scope=%s reason=context_required", configScope),
		)
	}
	if isNilInterface(manager) {
		return Config{}, errorForConfigLoadFailed(
			messagef("op=app_web_hertz.load_config result=failed stage=precheck scope=%s reason=manager_required", configScope),
		)
	}
	if err := ctx.Err(); err != nil {
		return Config{}, errorForConfigLoadFailed(
			messagef("op=app_web_hertz.load_config result=failed stage=precheck scope=%s reason=context_done", configScope),
			err,
		)
	}
	snapshot := manager.Current()
	if snapshot == nil {
		return Config{}, errorForConfigLoadFailed(
			messagef("op=app_web_hertz.load_config result=failed stage=current scope=%s reason=snapshot_not_initialized", configScope),
		)
	}

	// 第一阶段：默认 config scope 缺失表示应用接受内建配置；自定义 config scope 缺失仍视为部署拼写错误。
	var input Config
	if err := snapshot.Decode(configScope, &input); err != nil {
		if configScope == DefaultConfigScope && errcode.IsCode[baseconfig.ErrorCode](err, baseconfig.ErrCodeConfigDecodeScopeNotFound) {
			return DefaultConfig(), nil
		}
		return Config{}, errorForConfigLoadFailed(
			messagef("op=app_web_hertz.load_config result=failed stage=decode scope=%s reason=decode_failed", configScope),
			err,
		)
	}
	// 第二阶段：从同一 Snapshot 同时冻结 provider 与标准 Middleware 配置。
	resolved, err := resolveConfiguredConfig(input)
	if err != nil {
		return Config{}, errorForConfigLoadFailed(
			messagef("op=app_web_hertz.load_config result=failed stage=validate_config scope=%s reason=config_invalid", configScope),
			err,
		)
	}
	return resolved, nil
}
