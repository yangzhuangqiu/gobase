package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

const (
	// diagnosticsConfigScope 是本示例私有的诊断配置路径，不构成框架固定 scope。
	diagnosticsConfigScope = "quickstart.diagnostics"
	// defaultMetricsPath 是诊断端口的 Prometheus 抓取路径。
	defaultMetricsPath = "/metrics"
	// diagnosticsPprofPrefix 是标准库 pprof 的固定独立路由前缀。
	diagnosticsPprofPrefix = "/debug/pprof/"
	// defaultDiagnosticsReadHeaderTimeout 限制慢 Header 攻击占用诊断连接的时间。
	defaultDiagnosticsReadHeaderTimeout = 5 * time.Second
)

// diagnosticsConfig 描述当前应用的独立诊断 HTTP 工作负载。
type diagnosticsConfig struct {
	// Enabled 决定是否创建诊断 Server；默认 false，不产生 listener 或 goroutine。
	Enabled bool `yaml:"enabled"`
	// Address 是诊断监听地址；空值回退仅 loopback 的默认地址。
	Address string `yaml:"address"`
	// MetricsEnabled 决定是否注册 Prometheus Handler。
	MetricsEnabled bool `yaml:"metricsEnabled"`
	// MetricsPath 是 Metrics 精确路径；空值回退 /metrics。
	MetricsPath string `yaml:"metricsPath"`
	// PprofEnabled 决定是否在诊断 mux 注册标准库 pprof；默认 false。
	PprofEnabled bool `yaml:"pprofEnabled"`
	// ReadHeaderTimeout 是诊断 Server 读取请求头的上限，必须大于零。
	ReadHeaderTimeout time.Duration `yaml:"readHeaderTimeout"`
}

// resolveDiagnosticsConfig 应用安全默认值并在获取 listener 前校验配置。
func resolveDiagnosticsConfig(input diagnosticsConfig) (diagnosticsConfig, error) {
	resolved := input
	resolved.Address = strings.TrimSpace(resolved.Address)
	if resolved.Address == "" {
		resolved.Address = defaultQuickstartDiagnosticsAddress
	}
	resolved.MetricsPath = strings.TrimSpace(resolved.MetricsPath)
	if resolved.MetricsPath == "" {
		resolved.MetricsPath = defaultMetricsPath
	}
	if resolved.ReadHeaderTimeout == 0 {
		resolved.ReadHeaderTimeout = defaultDiagnosticsReadHeaderTimeout
	}
	if !resolved.Enabled {
		return resolved, nil
	}

	// 启用后先收口 listener 与超时语义，避免启动业务 Server 后才发现诊断声明非法。
	if _, _, err := net.SplitHostPort(resolved.Address); err != nil {
		return diagnosticsConfig{}, fmt.Errorf("op=quickstart.diagnostics result=failed stage=validate_config reason=address_invalid address=%s: %w", resolved.Address, err)
	}
	if resolved.ReadHeaderTimeout <= 0 {
		return diagnosticsConfig{}, fmt.Errorf("op=quickstart.diagnostics result=failed stage=validate_config reason=read_header_timeout_not_positive timeout=%s", resolved.ReadHeaderTimeout)
	}
	if !resolved.MetricsEnabled && !resolved.PprofEnabled {
		return diagnosticsConfig{}, fmt.Errorf("op=quickstart.diagnostics result=failed stage=validate_config reason=endpoint_required")
	}
	if resolved.MetricsEnabled {
		if err := validateDiagnosticsPath(resolved.MetricsPath); err != nil {
			return diagnosticsConfig{}, err
		}
		if resolved.PprofEnabled && (resolved.MetricsPath == strings.TrimSuffix(diagnosticsPprofPrefix, "/") || strings.HasPrefix(resolved.MetricsPath, diagnosticsPprofPrefix)) {
			return diagnosticsConfig{}, fmt.Errorf("op=quickstart.diagnostics result=failed stage=validate_config reason=metrics_path_conflicts_pprof path=%s", resolved.MetricsPath)
		}
	}
	return resolved, nil
}

// validateDiagnosticsPath 拒绝非绝对、带 query/fragment 或根路径的 Metrics 声明。
func validateDiagnosticsPath(path string) error {
	parsed, err := url.ParseRequestURI(path)
	if err != nil || !strings.HasPrefix(path, "/") || path == "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("op=quickstart.diagnostics result=failed stage=validate_config reason=metrics_path_invalid path=%s", path)
	}
	return nil
}
