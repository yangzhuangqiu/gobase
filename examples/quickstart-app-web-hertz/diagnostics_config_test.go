package main

import (
	"strings"
	"testing"
	"time"
)

// TestResolveDiagnosticsConfigKeepsDisabledSafeDefaults 验证零值不启用诊断工作负载且默认地址只绑定 loopback。
// 测试方法：解析零值配置，断言 Enabled/pprof 保持关闭，并检查地址、Metrics 路径和 Header timeout 默认值。
func TestResolveDiagnosticsConfigKeepsDisabledSafeDefaults(t *testing.T) {
	resolved, err := resolveDiagnosticsConfig(diagnosticsConfig{})
	if err != nil {
		t.Fatalf("resolve disabled diagnostics: %v", err)
	}
	if resolved.Enabled || resolved.PprofEnabled || resolved.Address != defaultQuickstartDiagnosticsAddress || resolved.MetricsPath != defaultMetricsPath || resolved.ReadHeaderTimeout != defaultDiagnosticsReadHeaderTimeout {
		t.Fatalf("unexpected safe defaults: %+v", resolved)
	}
}

// TestResolveDiagnosticsConfigRejectsUnsafeDeclarations 验证启用后的监听、路由和 timeout 在获取资源前 fail-fast。
// 测试方法：表驱动输入非法地址、空端点、根 Metrics 路径、pprof 冲突及负 timeout，断言稳定 reason。
func TestResolveDiagnosticsConfigRejectsUnsafeDeclarations(t *testing.T) {
	// 使用不同失败维度验证所有错误都发生在 listener 获取之前。
	tests := []struct {
		name   string
		config diagnosticsConfig
		reason string
	}{
		{name: "address", config: diagnosticsConfig{Enabled: true, Address: "127.0.0.1", MetricsEnabled: true}, reason: "address_invalid"},
		{name: "endpoint", config: diagnosticsConfig{Enabled: true, Address: "127.0.0.1:0"}, reason: "endpoint_required"},
		{name: "metrics_root", config: diagnosticsConfig{Enabled: true, Address: "127.0.0.1:0", MetricsEnabled: true, MetricsPath: "/"}, reason: "metrics_path_invalid"},
		{name: "pprof_conflict", config: diagnosticsConfig{Enabled: true, Address: "127.0.0.1:0", MetricsEnabled: true, MetricsPath: "/debug/pprof/heap", PprofEnabled: true}, reason: "metrics_path_conflicts_pprof"},
		{name: "timeout", config: diagnosticsConfig{Enabled: true, Address: "127.0.0.1:0", MetricsEnabled: true, ReadHeaderTimeout: -time.Second}, reason: "read_header_timeout_not_positive"},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			if _, err := resolveDiagnosticsConfig(item.config); err == nil || !strings.Contains(err.Error(), "reason="+item.reason) {
				t.Fatalf("expected reason %s, got=%v", item.reason, err)
			}
		})
	}
}

// TestLoadDiagnosticsAddressUsesLoopbackDefaultAndEnvironmentOverride 验证应用可显式覆盖诊断地址但默认不对外网暴露。
// 测试方法：分别设置空白和显式环境变量，断言 trim 后的最终地址。
func TestLoadDiagnosticsAddressUsesLoopbackDefaultAndEnvironmentOverride(t *testing.T) {
	t.Setenv(quickstartDiagnosticsAddressEnv, "  ")
	if actual := loadDiagnosticsAddress(); actual != defaultQuickstartDiagnosticsAddress {
		t.Fatalf("unexpected diagnostics default: %q", actual)
	}
	t.Setenv(quickstartDiagnosticsAddressEnv, " 0.0.0.0:29090 ")
	if actual := loadDiagnosticsAddress(); actual != "0.0.0.0:29090" {
		t.Fatalf("unexpected diagnostics override: %q", actual)
	}
}
