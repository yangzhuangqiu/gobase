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
			parsed, err := parseOneAddress(item.raw, ClientOptions{})
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
			parsed, err := parseOneAddress(item.raw, ClientOptions{})
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

// TestParseOneAddressRejectsMalformedPort 验证非法端口格式会直接报错。
func TestParseOneAddressRejectsMalformedPort(t *testing.T) {
	_, err := parseOneAddress("127.0.0.1:abc", ClientOptions{})
	if err == nil {
		t.Fatalf("expected invalid port error")
	}
	assertErrorContains(t, err, "parse nacos address")
}

// TestParseOneAddressRejectsOverflowPort 验证超大端口字符串会报 invalid port。
func TestParseOneAddressRejectsOverflowPort(t *testing.T) {
	_, err := parseOneAddress("127.0.0.1:18446744073709551616", ClientOptions{})
	if err == nil {
		t.Fatalf("expected invalid port error")
	}
	assertErrorContains(t, err, "invalid nacos port")
}

// TestParseOneAddressRejectsEmptyHost 验证地址缺失 host 时 fail fast。
func TestParseOneAddressRejectsEmptyHost(t *testing.T) {
	_, err := parseOneAddress(":8848", ClientOptions{})
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
			_, err := parseOneAddress(raw, ClientOptions{})
			if err == nil {
				t.Fatalf("expected out-of-range port error")
			}
			assertErrorContains(t, err, "invalid nacos port")
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

	if got := chooseScheme("", ""); got != "http" {
		t.Fatalf("expected default scheme http, got: %s", got)
	}
	if got := chooseScheme("", "https"); got != "https" {
		t.Fatalf("expected fallback scheme https, got: %s", got)
	}
	if got := chooseScheme("http", "https"); got != "http" {
		t.Fatalf("expected preferred scheme http, got: %s", got)
	}

	if got := chooseContextPath(""); got != "/nacos" {
		t.Fatalf("expected default context path /nacos, got: %s", got)
	}
	if got := chooseContextPath("nacos"); got != "/nacos" {
		t.Fatalf("expected normalized context path /nacos, got: %s", got)
	}

	if got := chooseLogLevel(""); got != "warn" {
		t.Fatalf("expected default log level warn, got: %s", got)
	}
	if got := chooseLogLevel("debug"); got != "debug" {
		t.Fatalf("expected log level debug, got: %s", got)
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
