package webhertzobserve

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/errcode"
)

const (
	// DefaultMetricsServerScope 是可选 Metrics HTTP Server 默认读取的独立配置路径。
	DefaultMetricsServerScope = "observability.metrics.server"
	// defaultMetricsServerAddress 默认只监听回环地址，防止本地开箱能力意外暴露到外部网络。
	defaultMetricsServerAddress = "127.0.0.1:9099"
	// defaultMetricsServerPath 是 Prometheus 抓取 Handler 的稳定默认路径。
	defaultMetricsServerPath = "/metrics"
	// defaultMetricsServerReadHeaderTimeout 限制诊断端口读取请求头的最长时间。
	defaultMetricsServerReadHeaderTimeout = 3 * time.Second
	// defaultMetricsServerShutdownTimeout 是两个工作负载统一收敛的独立宽限时间。
	defaultMetricsServerShutdownTimeout = 5 * time.Second
)

// MetricsServerConfig 定义独立 Prometheus 抓取端口的单次启动配置。
//
// 语义:
// 1. 配置只在 IOC Init 阶段读取一次，不支持运行期切换监听地址；
// 2. 空值回退安全默认值，非法非空值启动失败；
// 3. Server 只挂载 Path，不隐式开放 health、pprof 或业务路由。
type MetricsServerConfig struct {
	// Address 是 TCP 监听地址；默认 127.0.0.1:9099，生产容器需要显式配置可抓取地址。
	Address string `yaml:"address"`
	// Path 是 Prometheus Handler 的绝对路径；默认 /metrics。
	Path string `yaml:"path"`
	// ReadHeaderTimeout 限制读取 HTTP Header 的时长；默认 3s。
	ReadHeaderTimeout time.Duration `yaml:"readHeaderTimeout"`
	// ShutdownTimeout 限制业务 Server 与 Metrics Server 的统一关闭等待；默认 5s。
	ShutdownTimeout time.Duration `yaml:"shutdownTimeout"`
}

// MetricsServerOption 定义可选 Metrics HTTP Server 的纯声明策略。
type MetricsServerOption func(options *metricsServerOptions) error

// metricsServerOptions 保存 Module 注册前冻结的配置路径声明。
type metricsServerOptions struct {
	// scope 是 IOC Init 阶段读取一次的 dotted 配置路径。
	scope string
	// customScope 标记调用方是否显式选择非默认路径；缺失时必须 fail-fast。
	customScope bool
}

// DefaultMetricsServerConfig 返回只监听回环地址的独立配置快照。
func DefaultMetricsServerConfig() MetricsServerConfig {
	return MetricsServerConfig{
		Address:           defaultMetricsServerAddress,
		Path:              defaultMetricsServerPath,
		ReadHeaderTimeout: defaultMetricsServerReadHeaderTimeout,
		ShutdownTimeout:   defaultMetricsServerShutdownTimeout,
	}
}

// WithMetricsServerScope 设置 Metrics HTTP Server 单次读取的独立配置路径。
//
// 参数:
// - scope: trim 后为空时回退 observability.metrics.server；非空路径缺失时启动失败。
//
// 说明:
// - 多次声明时最后一次生效；
// - 本 Option 不读取 ConfigManager，也不创建 listener。
func WithMetricsServerScope(scope string) MetricsServerOption {
	return func(options *metricsServerOptions) error {
		scope = strings.TrimSpace(scope)
		options.scope = DefaultMetricsServerScope
		options.customScope = false
		if scope != "" {
			options.scope = scope
			options.customScope = scope != DefaultMetricsServerScope
		}
		return nil
	}
}

// resolveMetricsServerOptions 顺序应用声明并形成不可变配置定位策略。
// 阶段角色：Module 修改 IOC Registry 前拒绝 nil Option。
func resolveMetricsServerOptions(options []MetricsServerOption) (metricsServerOptions, error) {
	resolved := metricsServerOptions{scope: DefaultMetricsServerScope}
	for index, option := range options {
		if option == nil {
			return metricsServerOptions{}, errorForDeclarationInvalid(messagef(
				"op=app_observe_webhertz.metrics_server result=failed stage=resolve_options reason=option_required option_index=%d",
				index,
			))
		}
		if err := option(&resolved); err != nil {
			return metricsServerOptions{}, err
		}
	}
	return resolved, nil
}

// loadMetricsServerConfig 从当前 Snapshot 读取并冻结一次监听配置。
// 阶段角色：默认 scope 缺失时提供安全开箱默认值，自定义 scope 缺失或配置非法时 fail-fast。
func loadMetricsServerConfig(
	ctx context.Context,
	manager baseconfig.Manager,
	options metricsServerOptions,
) (MetricsServerConfig, error) {
	if ctx == nil {
		return MetricsServerConfig{}, errorForMetricsServerConfigLoadFailed(
			"op=app_observe_webhertz.metrics_server result=failed stage=precheck reason=context_required",
		)
	}
	if isNilValue(manager) {
		return MetricsServerConfig{}, errorForMetricsServerConfigLoadFailed(
			"op=app_observe_webhertz.metrics_server result=failed stage=precheck reason=manager_required",
		)
	}
	if err := ctx.Err(); err != nil {
		return MetricsServerConfig{}, errorForMetricsServerConfigLoadFailed(
			"op=app_observe_webhertz.metrics_server result=failed stage=precheck reason=context_done",
			err,
		)
	}
	snapshot := manager.Current()
	if snapshot == nil {
		return MetricsServerConfig{}, errorForMetricsServerConfigLoadFailed(
			"op=app_observe_webhertz.metrics_server result=failed stage=current reason=snapshot_not_initialized",
		)
	}

	// 默认路径不存在时使用安全回环默认值；显式自定义路径缺失通常表示部署配置拼写错误。
	input := DefaultMetricsServerConfig()
	if err := snapshot.Decode(options.scope, &input); err != nil {
		missingDefault := !options.customScope &&
			errcode.IsCode[baseconfig.ErrorCode](err, baseconfig.ErrCodeConfigDecodeScopeNotFound)
		if !missingDefault {
			return MetricsServerConfig{}, errorForMetricsServerConfigLoadFailed(
				messagef(
					"op=app_observe_webhertz.metrics_server result=failed stage=decode scope=%s reason=decode_failed",
					options.scope,
				),
				err,
			)
		}
	}

	// 解码完成后统一补默认值和校验，避免 listener 才暴露稳定配置错误。
	resolved, err := resolveMetricsServerConfig(input)
	if err != nil {
		return MetricsServerConfig{}, errorForMetricsServerConfigLoadFailed(
			messagef(
				"op=app_observe_webhertz.metrics_server result=failed stage=validate_config scope=%s reason=config_invalid",
				options.scope,
			),
			err,
		)
	}
	return resolved, nil
}

// resolveMetricsServerConfig 补齐空值并校验监听地址、路径和时间预算。
// 阶段角色：网络资源创建前生成单次运行所需的完整纯值快照。
func resolveMetricsServerConfig(input MetricsServerConfig) (MetricsServerConfig, error) {
	resolved := input
	resolved.Address = strings.TrimSpace(resolved.Address)
	resolved.Path = strings.TrimSpace(resolved.Path)
	if resolved.Address == "" {
		resolved.Address = defaultMetricsServerAddress
	}
	if resolved.Path == "" {
		resolved.Path = defaultMetricsServerPath
	}
	if resolved.ReadHeaderTimeout == 0 {
		resolved.ReadHeaderTimeout = defaultMetricsServerReadHeaderTimeout
	}
	if resolved.ShutdownTimeout == 0 {
		resolved.ShutdownTimeout = defaultMetricsServerShutdownTimeout
	}

	if err := validateMetricsServerAddress(resolved.Address); err != nil {
		return MetricsServerConfig{}, err
	}
	parsedPath, err := url.ParseRequestURI(resolved.Path)
	if err != nil || parsedPath.Path != resolved.Path || parsedPath.RawQuery != "" || parsedPath.Fragment != "" {
		return MetricsServerConfig{}, errorForMetricsServerConfigInvalid(
			messagef(
				"op=app_observe_webhertz.metrics_server result=failed stage=validate_path reason=absolute_path_required path=%s",
				resolved.Path,
			),
			err,
		)
	}
	if resolved.Path == "/" {
		return MetricsServerConfig{}, errorForMetricsServerConfigInvalid(
			"op=app_observe_webhertz.metrics_server result=failed stage=validate_path reason=root_path_forbidden",
		)
	}
	if resolved.ReadHeaderTimeout <= 0 || resolved.ShutdownTimeout <= 0 {
		return MetricsServerConfig{}, errorForMetricsServerConfigInvalid(
			"op=app_observe_webhertz.metrics_server result=failed stage=validate_timeout reason=positive_duration_required",
		)
	}
	return resolved, nil
}

// validateMetricsServerAddress 校验 host:port 结构和端口范围，不执行 DNS 或占用端口。
// 阶段角色：配置冻结阶段排除确定性错误，真实端口冲突留给 Runner 预绑定处理。
func validateMetricsServerAddress(address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return errorForMetricsServerConfigInvalid(
			messagef(
				"op=app_observe_webhertz.metrics_server result=failed stage=validate_address reason=host_port_required address=%s",
				address,
			),
			err,
		)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 0 || portNumber > 65535 {
		return errorForMetricsServerConfigInvalid(
			messagef(
				"op=app_observe_webhertz.metrics_server result=failed stage=validate_address reason=port_invalid address=%s",
				address,
			),
			err,
		)
	}
	return nil
}
