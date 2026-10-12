package webhertz

import (
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/config"
)

const (
	// defaultAddress 是未显式配置时的 Hertz 监听地址。
	defaultAddress = ":8888"
	// defaultReadTimeout 是底层读取阶段默认超时。
	defaultReadTimeout = 3 * time.Minute
	// defaultIdleTimeout 是无请求连接的默认空闲超时。
	defaultIdleTimeout = 3 * time.Minute
	// defaultMaxRequestBodySize 是默认请求体上限，单位为 byte。
	defaultMaxRequestBodySize = 4 << 20
	// defaultMaxHeaderBytes 是默认请求头上限，单位为 byte。
	defaultMaxHeaderBytes = 1 << 20
	// defaultExitWaitTime 是单次 Hertz 优雅停止的最大等待时间。
	defaultExitWaitTime = 5 * time.Second
	// defaultRedirectTrailingSlash 表示默认修正仅缺少或多余尾斜杠的路径。
	defaultRedirectTrailingSlash = true
	// defaultRedirectFixedPath 表示默认不执行大小写和冗余路径修正。
	defaultRedirectFixedPath = false
	// defaultHandleMethodNotAllowed 表示默认不扫描其他 Method，未匹配请求返回 404。
	defaultHandleMethodNotAllowed = false
	// defaultDisablePrintRoute 表示默认保留 Hertz 路由打印行为。
	defaultDisablePrintRoute = false
)

// RouterConfig 定义 Hertz Router 行为与路由诊断的构造期配置。
//
// 语义:
// 1. nil 表示使用 web-hertz 固化默认值，非 nil 可显式表达 true 或 false；
// 2. 配置只在 Server 构造阶段冻结，不支持运行期热更新；
// 3. WithNoMethod 为保证自定义 405 Handler 可达，会最终启用 HandleMethodNotAllowed。
type RouterConfig struct {
	// RedirectTrailingSlash 控制尾斜杠自动重定向；默认 true。
	RedirectTrailingSlash *bool `yaml:"redirectTrailingSlash"`
	// RedirectFixedPath 控制大小写和冗余路径自动修正；默认 false。
	RedirectFixedPath *bool `yaml:"redirectFixedPath"`
	// HandleMethodNotAllowed 控制路由存在但 Method 不匹配时是否返回 405；默认 false。
	HandleMethodNotAllowed *bool `yaml:"handleMethodNotAllowed"`
	// DisablePrintRoute 控制构造时是否禁止 Hertz 打印路由；默认 false。
	DisablePrintRoute *bool `yaml:"disablePrintRoute"`
}

// Config 定义 web-hertz 独立拥有的 Hertz 启动配置。
//
// 语义:
// 1. 零值字段由 ResolveConfig 使用本模块默认值补齐；
// 2. 该结构不嵌入根 web 配置，也不承诺与其他 Web provider 对齐字段；
// 3. 配置只在 Server 构造阶段解析，运行期不热更新。
type Config struct {
	// Address 是 Hertz 监听地址；trim 后为空时使用 :8888。
	Address string `yaml:"address"`
	// ReadTimeout 是底层读取超时；零值使用 3m，负值非法。
	ReadTimeout time.Duration `yaml:"readTimeout"`
	// IdleTimeout 是无请求连接空闲超时；零值使用 3m，负值非法。
	IdleTimeout time.Duration `yaml:"idleTimeout"`
	// MaxRequestBodySize 是请求体上限，单位 byte；零值使用 4 MiB，负值非法。
	MaxRequestBodySize int `yaml:"maxRequestBodySize"`
	// MaxHeaderBytes 是请求头上限，单位 byte；零值使用 1 MiB，负值非法。
	MaxHeaderBytes int `yaml:"maxHeaderBytes"`
	// ExitWaitTime 是 Hertz 单次优雅停止预算；零值使用 5s，负值非法。
	ExitWaitTime time.Duration `yaml:"exitWaitTime"`
	// Router 描述路由匹配、405 和路由打印行为；字段 nil 时使用模块默认值。
	Router RouterConfig `yaml:"router"`
}

// DefaultConfig 返回 web-hertz 固化的推荐默认配置。
//
// 返回:
// - Config: 与 Hertz v0.10.5 默认值对齐的完整配置副本。
func DefaultConfig() Config {
	return Config{
		Address:            defaultAddress,
		ReadTimeout:        defaultReadTimeout,
		IdleTimeout:        defaultIdleTimeout,
		MaxRequestBodySize: defaultMaxRequestBodySize,
		MaxHeaderBytes:     defaultMaxHeaderBytes,
		ExitWaitTime:       defaultExitWaitTime,
		Router: RouterConfig{
			RedirectTrailingSlash:  optionalBool(defaultRedirectTrailingSlash),
			RedirectFixedPath:      optionalBool(defaultRedirectFixedPath),
			HandleMethodNotAllowed: optionalBool(defaultHandleMethodNotAllowed),
			DisablePrintRoute:      optionalBool(defaultDisablePrintRoute),
		},
	}
}

// ResolveConfig 合并默认值并校验 Hertz 启动配置。
//
// 参数:
// - input: 调用方显式配置；零值字段表示使用模块默认值。
//
// 返回:
// - Config: 已完成 trim、默认值回填和校验的不可变使用快照；
// - error: duration、body size 或 header size 为负值时返回可分支配置错误。
func ResolveConfig(input Config) (Config, error) {
	if input.ReadTimeout < 0 {
		return Config{}, errorForConfigInvalid("op=web_hertz.resolve_config result=failed stage=validate reason=read_timeout_negative")
	}
	if input.IdleTimeout < 0 {
		return Config{}, errorForConfigInvalid("op=web_hertz.resolve_config result=failed stage=validate reason=idle_timeout_negative")
	}
	if input.MaxRequestBodySize < 0 {
		return Config{}, errorForConfigInvalid("op=web_hertz.resolve_config result=failed stage=validate reason=max_request_body_size_negative")
	}
	if input.MaxHeaderBytes < 0 {
		return Config{}, errorForConfigInvalid("op=web_hertz.resolve_config result=failed stage=validate reason=max_header_bytes_negative")
	}
	if input.ExitWaitTime < 0 {
		return Config{}, errorForConfigInvalid("op=web_hertz.resolve_config result=failed stage=validate reason=exit_wait_time_negative")
	}

	result := DefaultConfig()
	if address := strings.TrimSpace(input.Address); address != "" {
		result.Address = address
	}
	if input.ReadTimeout > 0 {
		result.ReadTimeout = input.ReadTimeout
	}
	if input.IdleTimeout > 0 {
		result.IdleTimeout = input.IdleTimeout
	}
	if input.MaxRequestBodySize > 0 {
		result.MaxRequestBodySize = input.MaxRequestBodySize
	}
	if input.MaxHeaderBytes > 0 {
		result.MaxHeaderBytes = input.MaxHeaderBytes
	}
	if input.ExitWaitTime > 0 {
		result.ExitWaitTime = input.ExitWaitTime
	}
	result.Router = resolveRouterConfig(input.Router)
	return result, nil
}

// resolveRouterConfig 把三态输入解析为与调用方指针隔离的完整路由配置。
// 阶段角色：Config 校验完成后冻结 Router 默认值和显式 false，供 Hertz Option 映射。
func resolveRouterConfig(input RouterConfig) RouterConfig {
	return RouterConfig{
		RedirectTrailingSlash:  resolveOptionalBool(input.RedirectTrailingSlash, defaultRedirectTrailingSlash),
		RedirectFixedPath:      resolveOptionalBool(input.RedirectFixedPath, defaultRedirectFixedPath),
		HandleMethodNotAllowed: resolveOptionalBool(input.HandleMethodNotAllowed, defaultHandleMethodNotAllowed),
		DisablePrintRoute:      resolveOptionalBool(input.DisablePrintRoute, defaultDisablePrintRoute),
	}
}

// resolveOptionalBool 复制显式布尔值或返回模块默认值，避免解析结果持有调用方指针。
// 阶段角色：Router Config 构造期三态归一化。
func resolveOptionalBool(input *bool, fallback bool) *bool {
	if input == nil {
		return optionalBool(fallback)
	}
	return optionalBool(*input)
}

// optionalBool 创建只属于当前 Config 快照的布尔指针。
// 阶段角色：DefaultConfig 和 ResolveConfig 输出隔离，防止跨实例修改共享默认值。
func optionalBool(value bool) *bool {
	return &value
}

// hertzOptions 把已解析 Config 映射为 Hertz 原生构造 Option。
// 阶段角色：固定 typed Config 低于 WithHertzOptions 的优先级。
func (this Config) hertzOptions() []config.Option {
	return []config.Option{
		server.WithHostPorts(this.Address),
		server.WithReadTimeout(this.ReadTimeout),
		server.WithIdleTimeout(this.IdleTimeout),
		server.WithMaxRequestBodySize(this.MaxRequestBodySize),
		server.WithMaxHeaderBytes(this.MaxHeaderBytes),
		server.WithExitWaitTime(this.ExitWaitTime),
		server.WithRedirectTrailingSlash(*this.Router.RedirectTrailingSlash),
		server.WithRedirectFixedPath(*this.Router.RedirectFixedPath),
		server.WithHandleMethodNotAllowed(*this.Router.HandleMethodNotAllowed),
		server.WithDisablePrintRoute(*this.Router.DisablePrintRoute),
	}
}
