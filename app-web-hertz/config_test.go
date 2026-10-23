package appwebhertz

import (
	"context"
	"reflect"
	"testing"
	"time"

	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/errcode"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// countingManager 包装真实 Manager，用于验证 app-web-hertz 只读取一次 Snapshot 且不建立订阅。
type countingManager struct {
	baseconfig.Manager
	// currentCalls 记录 Current 调用次数。
	currentCalls int
	// subscribeCalls 记录 Subscribe 调用次数。
	subscribeCalls int
}

// Current 记录 pull 快照读取并委托真实 Manager。
func (this *countingManager) Current() baseconfig.Snapshot {
	this.currentCalls++
	return this.Manager.Current()
}

// Subscribe 记录任何意外的 push 订阅并委托真实 Manager。
func (this *countingManager) Subscribe(subscriber baseconfig.Subscriber) (baseconfig.Subscription, error) {
	this.subscribeCalls++
	return this.Manager.Subscribe(subscriber)
}

// TestLoadConfigPreservesFlatScopeAndDecodesRouterConfig 验证旧扁平 Server 配置继续可用，并能从同一 scope 解码新增 Router 配置。
// 测试方法：构造包含旧 address 字段和嵌套 router 显式 false/true 的 Snapshot，调用 loadConfig 后断言默认回填与三态值。
func TestLoadConfigPreservesFlatScopeAndDecodesRouterConfig(t *testing.T) {
	// 第一阶段：构造同时包含旧扁平 Server 字段与新增 Middleware 的单一配置快照。
	manager := newTestManager(t, map[string]any{
		"web": map[string]any{
			"hertz": map[string]any{
				"address":        "127.0.0.1:18080",
				"maxHeaderBytes": 2048,
				"router": map[string]any{
					"redirectTrailingSlash":  false,
					"handleMethodNotAllowed": true,
				},
				"middleware": map[string]any{
					"requestId": map[string]any{"enabled": true, "header": "X-Correlation-ID", "trustIncoming": true},
					"accessLog": map[string]any{"enabled": true, "skipPaths": []string{"/healthz", "/readyz"}, "slowThreshold": "250ms"},
					"cors": map[string]any{
						"enabled":          true,
						"allowOrigins":     []string{"https://example.com"},
						"allowCredentials": true,
					},
				},
			},
		},
	})
	defer func() { _ = manager.Close() }()

	// 第二阶段：通过生产 loadConfig 入口解析，验证兼容字段、三态路由值与 Middleware 同步生效。
	resolved, err := loadConfig(context.Background(), manager, DefaultScope)
	if err != nil {
		t.Fatalf("load configured Hertz server: %v", err)
	}
	if resolved.Server.Address != "127.0.0.1:18080" || resolved.Server.MaxHeaderBytes != 2048 {
		t.Fatalf("unexpected flat server config: %+v", resolved)
	}
	if resolved.Server.Router.RedirectTrailingSlash == nil || *resolved.Server.Router.RedirectTrailingSlash {
		t.Fatalf("expected explicit redirectTrailingSlash=false: %+v", resolved.Server.Router)
	}
	if resolved.Server.Router.HandleMethodNotAllowed == nil || !*resolved.Server.Router.HandleMethodNotAllowed {
		t.Fatalf("expected explicit handleMethodNotAllowed=true: %+v", resolved.Server.Router)
	}
	if resolved.Server.Router.RedirectFixedPath == nil || *resolved.Server.Router.RedirectFixedPath {
		t.Fatalf("expected default redirectFixedPath=false: %+v", resolved.Server.Router)
	}
	if !resolved.Middleware.RequestID.Enabled || resolved.Middleware.RequestID.Header != "X-Correlation-ID" || !resolved.Middleware.RequestID.TrustIncoming {
		t.Fatalf("unexpected request ID config: %+v", resolved.Middleware.RequestID)
	}
	if !resolved.Middleware.AccessLog.Enabled || resolved.Middleware.AccessLog.SlowThreshold != 250*time.Millisecond || !reflect.DeepEqual(resolved.Middleware.AccessLog.SkipPaths, []string{"/healthz", "/readyz"}) {
		t.Fatalf("unexpected access log config: %+v", resolved.Middleware.AccessLog)
	}
	if !resolved.Middleware.CORS.Enabled || !resolved.Middleware.CORS.AllowCredentials || !reflect.DeepEqual(resolved.Middleware.CORS.AllowOrigins, []string{"https://example.com"}) {
		t.Fatalf("unexpected CORS config: %+v", resolved.Middleware.CORS)
	}
}

// TestResolveConfiguredConfigUsesDisabledDefaultsAndDetachesSlices 验证旧入口等价配置不会隐式安装能力，且默认集合不跨实例共享。
// 测试方法：解析两个零值 Config，断言全部 enabled=false、默认策略完整；修改第一份 slice 后确认第二份不变。
func TestResolveConfiguredConfigUsesDisabledDefaultsAndDetachesSlices(t *testing.T) {
	// 第一阶段：分别解析两个零值配置，验证默认值完整且标准能力均保持关闭。
	first, err := resolveConfiguredConfig(Config{})
	if err != nil {
		t.Fatalf("resolve first default config: %v", err)
	}
	second, err := resolveConfiguredConfig(Config{})
	if err != nil {
		t.Fatalf("resolve second default config: %v", err)
	}
	if first.Middleware.Recovery.Enabled || first.Middleware.RequestID.Enabled || first.Middleware.AccessLog.Enabled || first.Middleware.CORS.Enabled || first.Middleware.Gzip.Enabled {
		t.Fatalf("expected all middleware disabled: %+v", first.Middleware)
	}
	if first.Middleware.Recovery.Response != RecoveryResponseJSON || first.Middleware.RequestID.Header != defaultRequestIDHeader || first.Middleware.CORS.MaxAge != defaultCORSMaxAge || first.Middleware.Gzip.MinLength != defaultGzipMinLength {
		t.Fatalf("unexpected middleware defaults: %+v", first.Middleware)
	}
	if !reflect.DeepEqual(first, DefaultConfig()) {
		t.Fatalf("expected zero input to resolve to DefaultConfig: %+v", first)
	}
	// 第二阶段：禁用配置不得生成 Handler，且默认集合必须按实例复制，避免跨 Server 污染。
	plan, err := prepareStandardMiddleware(first.Middleware, defaultOptions(), testServerLoggers().middleware)
	if err != nil || len(plan.beforeObservation) != 0 || len(plan.afterObservation) != 0 {
		t.Fatalf("expected disabled middleware to build no handlers: before=%d after=%d err=%v", len(plan.beforeObservation), len(plan.afterObservation), err)
	}
	first.Middleware.CORS.AllowMethods[0] = "BROKEN"
	first.Middleware.Gzip.ExcludedContentTypes[0] = "broken/type"
	if second.Middleware.CORS.AllowMethods[0] == "BROKEN" || second.Middleware.Gzip.ExcludedContentTypes[0] == "broken/type" {
		t.Fatal("expected resolved config slices to be detached")
	}
	if !reflect.DeepEqual(first.Server, webhertz.DefaultConfig()) {
		t.Fatal("expected provider defaults to remain aligned")
	}
}

// TestResolveConfiguredConfigRejectsInvalidMiddleware 验证非法配置在创建 Handler/Engine 前以稳定配置错误失败。
// 测试方法：表驱动输入未知 Recovery、非法 Header/CORS/Gzip 组合，断言 config_invalid；再经私有 builder 验证外层 build 错误。
func TestResolveConfiguredConfigRejectsInvalidMiddleware(t *testing.T) {
	// 第一阶段：覆盖各标准 Middleware 的非法枚举、范围及安全组合。
	tests := map[string]MiddlewareConfig{
		"recovery response": {Recovery: RecoveryConfig{Response: "xml"}},
		"request ID header": {RequestID: RequestIDConfig{Header: "bad header"}},
		"access log slow":   {AccessLog: AccessLogConfig{SlowThreshold: -time.Second}},
		"access log path":   {AccessLog: AccessLogConfig{SkipPaths: []string{"healthz"}}},
		"CORS max age":      {CORS: CORSConfig{MaxAge: -time.Second}},
		"CORS origin":       {CORS: CORSConfig{AllowOrigins: []string{"file://unsafe"}}},
		"CORS credentials":  {CORS: CORSConfig{AllowOrigins: []string{"*"}, AllowCredentials: true}},
		"CORS method":       {CORS: CORSConfig{AllowMethods: []string{"GET BAD"}}},
		"CORS header":       {CORS: CORSConfig{AllowHeaders: []string{"Bad Header"}}},
		"gzip level":        {Gzip: GzipConfig{Level: 10}},
		"gzip min length":   {Gzip: GzipConfig{MinLength: -1}},
	}
	for name, middleware := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := resolveConfiguredConfig(Config{Middleware: middleware})
			if !errcode.IsCode[ErrorCode](err, ErrCodeConfigInvalid) {
				t.Fatalf("expected config error, got=%v", err)
			}
		})
	}

	// 第二阶段：私有统一装配核心应在稳定配置错误外保留 Server 构建阶段语义。
	_, err := buildServer(Config{Middleware: tests["CORS credentials"]}, serverComponents{loggers: testServerLoggers()}, defaultOptions())
	if !errcode.IsCode[ErrorCode](err, ErrCodeServerBuildFailed) || !errcode.IsCode[ErrorCode](err, ErrCodeConfigInvalid) {
		t.Fatalf("expected layered build config error, got=%v", err)
	}
}

// TestGoValueAndSnapshotConfigResolveIdentically 验证 Go 配置值与 IOC Snapshot 共享同一个解析合同。
// 测试方法：同一语义分别构造 Go Config 和 Manager Snapshot，调用统一 resolve/load 后断言完整结果一致。
func TestGoValueAndSnapshotConfigResolveIdentically(t *testing.T) {
	// 第一阶段：解析完整 Go Config，作为统一配置合同的期望值。
	goValue := Config{
		Server: webhertz.Config{Address: "127.0.0.1:19090"},
		Middleware: MiddlewareConfig{
			Recovery:  RecoveryConfig{Enabled: true, Response: RecoveryResponseText},
			RequestID: RequestIDConfig{Enabled: true, Header: "X-Correlation-ID"},
		},
	}
	expected, err := resolveConfiguredConfig(goValue)
	if err != nil {
		t.Fatalf("resolve Go config value: %v", err)
	}
	// 第二阶段：从同语义 Snapshot 加载，验证 IOC 不另建一套默认值或校验规则。
	manager := newTestManager(t, map[string]any{"web": map[string]any{"hertz": map[string]any{
		"address": "127.0.0.1:19090",
		"middleware": map[string]any{
			"recovery":  map[string]any{"enabled": true, "response": "text"},
			"requestId": map[string]any{"enabled": true, "header": "X-Correlation-ID"},
		},
	}}})
	defer func() { _ = manager.Close() }()
	counted := &countingManager{Manager: manager}
	actual, err := loadConfig(context.Background(), counted, DefaultScope)
	if err != nil {
		t.Fatalf("load snapshot config: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("expected identical resolved config:\nactual=%+v\nexpected=%+v", actual, expected)
	}
	// 第三阶段：配置装配只允许一次 pull，不建立运行期订阅。
	if counted.currentCalls != 1 || counted.subscribeCalls != 0 {
		t.Fatalf("unexpected Manager usage: current=%d subscribe=%d", counted.currentCalls, counted.subscribeCalls)
	}
}
