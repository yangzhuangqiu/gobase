package webhertz

import (
	"reflect"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/yangzhuangqiu/gobase/errcode"
)

// TestResolveConfigUsesModuleDefaults 验证零值配置只使用 web-hertz 自有默认值。
// 测试方法：解析 Config 零值，逐项断言完整结果与 DefaultConfig 一致。
func TestResolveConfigUsesModuleDefaults(t *testing.T) {
	resolved, err := ResolveConfig(Config{})
	if err != nil {
		t.Fatalf("resolve default config: %v", err)
	}
	if !reflect.DeepEqual(resolved, DefaultConfig()) {
		t.Fatalf("unexpected default config: %#v", resolved)
	}
}

// TestResolveConfigAppliesExplicitValues 验证显式 typed Config 覆盖默认值并 trim 地址。
// 测试方法：为全部字段提供非零输入，断言解析结果保持调用方语义。
func TestResolveConfigAppliesExplicitValues(t *testing.T) {
	input := Config{
		Address:            " 127.0.0.1:18080 ",
		ReadTimeout:        time.Second,
		IdleTimeout:        2 * time.Second,
		MaxRequestBodySize: 1024,
		MaxHeaderBytes:     2048,
		ExitWaitTime:       3 * time.Second,
		Router: RouterConfig{
			RedirectTrailingSlash:  optionalBool(false),
			RedirectFixedPath:      optionalBool(true),
			HandleMethodNotAllowed: optionalBool(true),
			DisablePrintRoute:      optionalBool(true),
		},
	}
	resolved, err := ResolveConfig(input)
	if err != nil {
		t.Fatalf("resolve explicit config: %v", err)
	}
	input.Address = "127.0.0.1:18080"
	if !reflect.DeepEqual(resolved, input) {
		t.Fatalf("unexpected resolved config: %#v", resolved)
	}
	if resolved.Router.RedirectFixedPath == input.Router.RedirectFixedPath {
		t.Fatal("expected resolved router config to detach caller pointers")
	}
}

// TestResolveConfigRejectsNegativeValues 验证负数不会穿透到 Hertz 运行阶段。
// 测试方法：分别注入负 duration/body size，断言统一返回可分支配置错误码。
func TestResolveConfigRejectsNegativeValues(t *testing.T) {
	tests := map[string]Config{
		"read timeout":          {ReadTimeout: -time.Second},
		"idle timeout":          {IdleTimeout: -time.Second},
		"max request body size": {MaxRequestBodySize: -1},
		"max header bytes":      {MaxHeaderBytes: -1},
		"exit wait time":        {ExitWaitTime: -time.Second},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ResolveConfig(input)
			if !errcode.IsCode[ErrorCode](err, ErrCodeConfigInvalid) {
				t.Fatalf("expected config error, got=%v", err)
			}
		})
	}
}

// TestNewAppliesNativeOptionsAfterTypedConfig 验证 Hertz 原生 Option 拥有最终优先级。
// 测试方法：typed Config 与原生 Option 设置冲突值，检查 Engine 最终 Options 和诊断地址。
func TestNewAppliesNativeOptionsAfterTypedConfig(t *testing.T) {
	instance, err := New(
		Config{
			Address:        "127.0.0.1:18080",
			ReadTimeout:    time.Second,
			MaxHeaderBytes: 2048,
			Router: RouterConfig{
				RedirectTrailingSlash:  optionalBool(false),
				RedirectFixedPath:      optionalBool(true),
				HandleMethodNotAllowed: optionalBool(true),
				DisablePrintRoute:      optionalBool(true),
			},
		},
		WithHertzOptions(
			server.WithHostPorts("127.0.0.1:28080"),
			server.WithReadTimeout(2*time.Second),
			server.WithMaxHeaderBytes(4096),
			server.WithRedirectTrailingSlash(true),
			server.WithRedirectFixedPath(false),
			server.WithHandleMethodNotAllowed(false),
			server.WithDisablePrintRoute(false),
		),
	)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	options := instance.engine.GetOptions()
	if options.Addr != "127.0.0.1:28080" || instance.address != options.Addr {
		t.Fatalf("unexpected final address: options=%q diagnostic=%q", options.Addr, instance.address)
	}
	if options.ReadTimeout != 2*time.Second {
		t.Fatalf("unexpected read timeout: %s", options.ReadTimeout)
	}
	if options.MaxHeaderBytes != 4096 {
		t.Fatalf("unexpected max header bytes: %d", options.MaxHeaderBytes)
	}
	if !options.RedirectTrailingSlash || options.RedirectFixedPath || options.HandleMethodNotAllowed || options.DisablePrintRoute {
		t.Fatalf("unexpected final router options: %+v", options)
	}
}
