package webhertzobserve

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultMetricsServerAddress 默认只监听回环地址，防止本地开箱能力意外暴露到外部网络。
	defaultMetricsServerAddress = "127.0.0.1:9099"
	// defaultMetricsServerPath 是 Prometheus 抓取 Handler 的稳定默认路径。
	defaultMetricsServerPath = "/metrics"
	// defaultMetricsServerReadHeaderTimeout 限制诊断端口读取请求头的最长时间。
	defaultMetricsServerReadHeaderTimeout = 3 * time.Second
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
}

// DefaultMetricsServerConfig 返回只监听回环地址的独立配置快照。
func DefaultMetricsServerConfig() MetricsServerConfig {
	return MetricsServerConfig{
		Address:           defaultMetricsServerAddress,
		Path:              defaultMetricsServerPath,
		ReadHeaderTimeout: defaultMetricsServerReadHeaderTimeout,
	}
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

	if err := validateMetricsServerAddress(resolved.Address); err != nil {
		return MetricsServerConfig{}, err
	}
	parsedPath, err := url.ParseRequestURI(resolved.Path)
	if err != nil || parsedPath.Path != resolved.Path || parsedPath.RawQuery != "" || parsedPath.Fragment != "" {
		return MetricsServerConfig{}, errorForConfigInvalid(
			messagef(
				"op=app_observe_webhertz.metrics_server result=failed stage=validate_path reason=absolute_path_required path=%s",
				resolved.Path,
			),
			err,
		)
	}
	if resolved.Path == "/" {
		return MetricsServerConfig{}, errorForConfigInvalid(
			"op=app_observe_webhertz.metrics_server result=failed stage=validate_path reason=root_path_forbidden",
		)
	}
	if resolved.ReadHeaderTimeout <= 0 {
		return MetricsServerConfig{}, errorForConfigInvalid(
			"op=app_observe_webhertz.metrics_server result=failed stage=validate_timeout reason=positive_duration_required",
		)
	}
	return resolved, nil
}

// validateMetricsServerAddress 校验 host:port 结构和端口范围，不执行 DNS 或占用端口。
// 阶段角色：配置冻结阶段排除确定性错误，真实端口冲突留给 Module Init 预绑定处理。
func validateMetricsServerAddress(address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return errorForConfigInvalid(
			messagef(
				"op=app_observe_webhertz.metrics_server result=failed stage=validate_address reason=host_port_required address=%s",
				address,
			),
			err,
		)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 0 || portNumber > 65535 {
		return errorForConfigInvalid(
			messagef(
				"op=app_observe_webhertz.metrics_server result=failed stage=validate_address reason=port_invalid address=%s",
				address,
			),
			err,
		)
	}
	return nil
}
