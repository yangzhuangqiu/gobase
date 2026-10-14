package nacos

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseOneAddressDefaultsPortForAddressWithoutPort 验证无端口地址走默认端口。
func TestParseOneAddressDefaultsPortForAddressWithoutPort(t *testing.T) {
	cases := []struct {
		name         string
		raw          string
		expectedHost string
	}{
		{name: "ipv4_no_port", raw: "127.0.0.1", expectedHost: "127.0.0.1"},
		{name: "ipv6_no_port", raw: "[::1]", expectedHost: "::1"},
	}

	for _, item := range cases {
		item := item
		t.Run(item.name, func(t *testing.T) {
			parsed, err := parseOneAddress(item.raw)
			if err != nil {
				t.Fatalf("parseOneAddress(%s) failed: %v", item.raw, err)
			}
			if parsed.Port != DefaultNacosPort {
				t.Fatalf("expected default port %d, got: %d", DefaultNacosPort, parsed.Port)
			}
			if parsed.IpAddr != item.expectedHost {
				t.Fatalf("expected host %s, got: %s", item.expectedHost, parsed.IpAddr)
			}
		})
	}
}

// TestParseOneAddressKeepsExplicitPort 验证显式端口不会被默认值覆盖。
func TestParseOneAddressKeepsExplicitPort(t *testing.T) {
	cases := []struct {
		name         string
		raw          string
		expectedHost string
		expectedPort uint64
	}{
		{name: "ipv4_with_port", raw: "127.0.0.1:18848", expectedHost: "127.0.0.1", expectedPort: 18848},
		{name: "ipv6_with_port", raw: "[::1]:18849", expectedHost: "::1", expectedPort: 18849},
	}

	for _, item := range cases {
		item := item
		t.Run(item.name, func(t *testing.T) {
			parsed, err := parseOneAddress(item.raw)
			if err != nil {
				t.Fatalf("parseOneAddress(%s) failed: %v", item.raw, err)
			}
			if parsed.Port != item.expectedPort {
				t.Fatalf("expected port %d, got: %d", item.expectedPort, parsed.Port)
			}
			if parsed.IpAddr != item.expectedHost {
				t.Fatalf("expected host %s, got: %s", item.expectedHost, parsed.IpAddr)
			}
		})
	}
}

// TestParseServerConfigsPrefersStructuredServers 验证非空 servers 会整体覆盖 address。
// 测试方法：同时声明非法 address 与两个结构化节点，断言仅解析 servers 且保留节点级差异。
func TestParseServerConfigsPrefersStructuredServers(t *testing.T) {
	// 同时提供无效 address 与两个有效 server，证明实现不会触碰被覆盖的 address。
	configs, err := parseServerConfigs(ClientOptions{
		Address: "invalid-address:bad-port",
		Servers: []Server{
			{Host: "10.0.0.1", Port: 8848, GrpcPort: 9848},
			{Host: "10.0.0.2", Port: 18848, Scheme: "https", ContextPath: "custom", GrpcPort: 19848},
		},
	})
	if err != nil {
		t.Fatalf("parse server configs failed: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("expected two server configs, got: %d", len(configs))
	}
	// 分别断言默认值与节点级覆盖，避免退化为全局 gRPC 端口。
	if configs[0].IpAddr != "10.0.0.1" || configs[0].GrpcPort != 9848 || configs[0].Scheme != "http" || configs[0].ContextPath != "/nacos" {
		t.Fatalf("unexpected first server config: %#v", configs[0])
	}
	if configs[1].IpAddr != "10.0.0.2" || configs[1].GrpcPort != 19848 || configs[1].Scheme != "https" || configs[1].ContextPath != "/custom" {
		t.Fatalf("unexpected second server config: %#v", configs[1])
	}
}

// TestParseServerConfigsRejectsOutOfRangeGrpcPort 验证节点级 gRPC 端口必须落在有效范围。
// 测试方法：在第 2 个 server 传入 65536，断言错误携带节点索引和原因。
func TestParseServerConfigsRejectsOutOfRangeGrpcPort(t *testing.T) {
	_, err := parseServerConfigs(ClientOptions{Servers: []Server{{Host: "ok"}, {Host: "bad", GrpcPort: 65536}}})
	if err == nil {
		t.Fatalf("expected out-of-range grpc port error")
	}
	assertErrorContains(t, err, "server_index=1")
	assertErrorContains(t, err, "grpc_port_out_of_range")
}

// TestParseServerConfigsLeavesGrpcPortForSDKDerivation 验证省略节点 gRPC 端口时保留 SDK 推导语义。
// 测试方法：声明非默认 HTTP 端口且不声明 grpcPort，断言 ServerConfig 保持 0，由 SDK 推导为 port+1000。
func TestParseServerConfigsLeavesGrpcPortForSDKDerivation(t *testing.T) {
	configs, err := parseServerConfigs(ClientOptions{Servers: []Server{{Host: "127.0.0.1", Port: 18848}}})
	if err != nil {
		t.Fatalf("parse structured server failed: %v", err)
	}
	if len(configs) != 1 || configs[0].Port != 18848 || configs[0].GrpcPort != 0 {
		t.Fatalf("expected SDK grpc derivation input, got: %#v", configs)
	}
}

// TestParseOneAddressRejectsMalformedPort 验证非法端口格式会直接报错。
func TestParseOneAddressRejectsMalformedPort(t *testing.T) {
	_, err := parseOneAddress("127.0.0.1:abc")
	if err == nil {
		t.Fatalf("expected invalid port error")
	}
	assertErrorContains(t, err, "parse nacos address")
}

// TestParseOneAddressRejectsOverflowPort 验证超大端口字符串会报 invalid port。
func TestParseOneAddressRejectsOverflowPort(t *testing.T) {
	_, err := parseOneAddress("127.0.0.1:18446744073709551616")
	if err == nil {
		t.Fatalf("expected invalid port error")
	}
	assertErrorContains(t, err, "invalid nacos port")
}

// TestParseOneAddressRejectsEmptyHost 验证地址缺失 host 时 fail fast。
func TestParseOneAddressRejectsEmptyHost(t *testing.T) {
	_, err := parseOneAddress(":8848")
	if err == nil {
		t.Fatalf("expected empty host error")
	}
	assertErrorContains(t, err, "empty host")
}

// TestParseOneAddressRejectsOutOfRangePort 验证端口必须落在 [1,65535]。
func TestParseOneAddressRejectsOutOfRangePort(t *testing.T) {
	cases := []string{"127.0.0.1:0", "127.0.0.1:65536"}
	for _, raw := range cases {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			_, err := parseOneAddress(raw)
			if err == nil {
				t.Fatalf("expected out-of-range port error")
			}
			assertErrorContains(t, err, "invalid nacos port")
		})
	}
}

// TestParseOneAddressRejectsNonStandardFields 验证 address 只承载标准 host/port 简写。
// 测试方法：输入携带 scheme 或 context path 的地址，断言提示改用结构化 servers。
func TestParseOneAddressRejectsNonStandardFields(t *testing.T) {
	for _, raw := range []string{"https://127.0.0.1:8848", "127.0.0.1:8848/custom"} {
		t.Run(raw, func(t *testing.T) {
			_, err := parseOneAddress(raw)
			assertErrorCode(t, err, ErrCodeNacosSDKInvalidInput)
			assertErrorContains(t, err, "structured servers")
		})
	}
}

// TestParseServerConfigsRejectsHostWithConnectionParts 验证 servers.host 只接受纯主机信息。
// 测试方法：分别混入 scheme、port 和 path，断言错误携带 invalid_host 语义。
func TestParseServerConfigsRejectsHostWithConnectionParts(t *testing.T) {
	for _, host := range []string{"https://nacos", "nacos:8848", "nacos/path"} {
		t.Run(host, func(t *testing.T) {
			_, err := parseServerConfigs(ClientOptions{Servers: []Server{{Host: host}}})
			assertErrorCode(t, err, ErrCodeNacosSDKInvalidInput)
			assertErrorContains(t, err, "reason=invalid_host")
		})
	}
}

// TestChooseNacosSDKOptionDefaults 验证 SDK 可选参数默认值语义。
func TestChooseNacosSDKOptionDefaults(t *testing.T) {
	if got := chooseTimeout(0); got != 10_000 {
		t.Fatalf("expected default timeout 10000, got: %d", got)
	}
	if got := chooseTimeout(3000); got != 3000 {
		t.Fatalf("expected timeout 3000, got: %d", got)
	}

	if got, err := chooseServerScheme(""); err != nil || got != "http" {
		t.Fatalf("expected default scheme http, got=%s err=%v", got, err)
	}
	if got, err := chooseServerScheme("https"); err != nil || got != "https" {
		t.Fatalf("expected explicit scheme https, got=%s err=%v", got, err)
	}

	if got := chooseContextPath(""); got != "/nacos" {
		t.Fatalf("expected default context path /nacos, got: %s", got)
	}
	if got := chooseContextPath("nacos"); got != "/nacos" {
		t.Fatalf("expected normalized context path /nacos, got: %s", got)
	}

	gotLogLevel, err := chooseLogLevel("")
	if err != nil {
		t.Fatalf("choose default log level failed: %v", err)
	}
	if gotLogLevel != "warn" {
		t.Fatalf("expected default log level warn, got: %s", gotLogLevel)
	}
	gotLogLevel, err = chooseLogLevel("debug")
	if err != nil {
		t.Fatalf("choose explicit log level failed: %v", err)
	}
	if gotLogLevel != "debug" {
		t.Fatalf("expected log level debug, got: %s", gotLogLevel)
	}

	expectedLogDir := filepath.Join(os.TempDir(), "nacos", "log")
	if got := chooseLogDir(""); got != expectedLogDir {
		t.Fatalf("expected default log dir %s, got: %s", expectedLogDir, got)
	}
	if got := chooseLogDir("/custom/log"); got != "/custom/log" {
		t.Fatalf("expected custom log dir /custom/log, got: %s", got)
	}

	expectedCacheDir := filepath.Join(os.TempDir(), "nacos", "cache")
	if got := chooseCacheDir(""); got != expectedCacheDir {
		t.Fatalf("expected default cache dir %s, got: %s", expectedCacheDir, got)
	}
	if got := chooseCacheDir("/custom/cache"); got != "/custom/cache" {
		t.Fatalf("expected custom cache dir /custom/cache, got: %s", got)
	}
}

// TestChooseServerSchemeRejectsUnsupported 验证节点协议只接受小写 http/https。
// 测试方法：输入未知协议与大小写变体，断言 SDK 创建前返回稳定参数错误。
func TestChooseServerSchemeRejectsUnsupported(t *testing.T) {
	for _, scheme := range []string{"ftp", "HTTPS"} {
		t.Run(scheme, func(t *testing.T) {
			_, err := chooseServerScheme(scheme)
			assertErrorCode(t, err, ErrCodeNacosSDKInvalidInput)
			assertErrorContains(t, err, "expected http|https")
		})
	}
}

// TestParseServerConfigsRejectsInvalidDerivedGrpcPort 验证省略 grpcPort 时派生端口不得越界。
// 测试方法：声明 HTTP 端口 65000，断言 Port+1000 在 SDK 建连前被拒绝。
func TestParseServerConfigsRejectsInvalidDerivedGrpcPort(t *testing.T) {
	_, err := parseServerConfigs(ClientOptions{Servers: []Server{{Host: "127.0.0.1", Port: 65000}}})
	assertErrorCode(t, err, ErrCodeNacosSDKInvalidInput)
	assertErrorContains(t, err, "derived_grpc_port_out_of_range")
}

// TestChooseNacosSDKLogLevelRejectsUnsupported 验证 SDK 日志级别只接受明确的小写枚举。
// 测试方法：分别输入未知值和大小写不匹配值，断言在 SDK 初始化前返回稳定参数错误。
func TestChooseNacosSDKLogLevelRejectsUnsupported(t *testing.T) {
	for _, level := range []string{"verbose", "WARN"} {
		t.Run(level, func(t *testing.T) {
			_, err := chooseLogLevel(level)
			assertErrorCode(t, err, ErrCodeNacosSDKInvalidInput)
			assertErrorContains(t, err, "expected debug|info|warn|error")
		})
	}
}

// TestChooseNacosSDKLogLevelAcceptsSupported 验证四个 SDK 日志级别枚举保持原值。
// 测试方法：逐一输入支持值，断言无错误且不会发生大小写或级别转换。
func TestChooseNacosSDKLogLevelAcceptsSupported(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		t.Run(level, func(t *testing.T) {
			actual, err := chooseLogLevel(level)
			if err != nil {
				t.Fatalf("choose supported log level failed: %v", err)
			}
			if actual != level {
				t.Fatalf("expected log level %s, got: %s", level, actual)
			}
		})
	}
}
