package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	baseapp "github.com/yangzhuangqiu/gobase/app"
	applogzap "github.com/yangzhuangqiu/gobase/app-log-zap"
	webhertzobserve "github.com/yangzhuangqiu/gobase/app-observe-webhertz"
	appwebhertz "github.com/yangzhuangqiu/gobase/app-web-hertz"
	baseconfig "github.com/yangzhuangqiu/gobase/config"
)

const (
	// quickstartAddressEnv 是示例监听地址的可选环境变量。
	quickstartAddressEnv = "APP_WEB_HERTZ_ADDRESS"
	// defaultQuickstartAddress 仅用于示例，避免占用 provider Quickstart 端口。
	defaultQuickstartAddress = "127.0.0.1:18081"
	// quickstartDiagnosticsAddressEnv 是诊断监听地址的应用级覆盖入口。
	quickstartDiagnosticsAddressEnv = "APP_WEB_HERTZ_DIAGNOSTICS_ADDRESS"
	// defaultQuickstartDiagnosticsAddress 默认只绑定 loopback，避免意外公开 Metrics/pprof。
	defaultQuickstartDiagnosticsAddress = "127.0.0.1:18082"
	// quickstartTracingEnabledEnv 显式控制是否创建 Provider/Exporter 和 Trace batch goroutine。
	quickstartTracingEnabledEnv = "APP_WEB_HERTZ_TRACING_ENABLED"
)

// greetingResponse 是自动和手动路由共用的教学响应结构。
type greetingResponse struct {
	// Message 表示本次请求生成的问候内容。
	Message string `json:"message"`
	// Method 表示命中 Controller 的 HTTP method。
	Method string `json:"method"`
}

// GreetingController 演示 Controller 分组、自动多 method 路由和手动动态路由。
type GreetingController struct{}

// GroupPath 把当前 Controller 的全部路由放入 `/api` 独占分组。
func (this *GreetingController) GroupPath() string {
	return "/api"
}

// GetOrPostHello 通过方法名自动同时注册 GET/POST `/api/hello`。
func (this *GreetingController) GetOrPostHello(_ context.Context, request *hertzapp.RequestContext) {
	request.JSON(consts.StatusOK, greetingResponse{
		Message: "hello from automatic route",
		Method:  string(request.Method()),
	})
}

// RegisterRoutes 在 GroupPath 已生效的 RouterGroup 上注册含路径参数的原生路由。
func (this *GreetingController) RegisterRoutes(router *route.RouterGroup) error {
	router.GET("/users/:name", func(_ context.Context, request *hertzapp.RequestContext) {
		request.JSON(consts.StatusOK, greetingResponse{
			Message: "hello, " + request.Param("name"),
			Method:  string(request.Method()),
		})
	})
	return nil
}

// main 是应用级 Quickstart 入口，统一向 stderr 报告最终失败。
func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "quickstart-app-web-hertz failed: %v\n", err)
		os.Exit(1)
	}
}

// run 通过 app 生命周期装配业务 Web、诊断 Server 和同一个工作负载监督器。
func run() error {
	modules, err := applicationModules()
	if err != nil {
		return err
	}
	workloads := &workloadAssembly{}
	return baseapp.Run(
		// 第一阶段：app 构建并拥有 Config、Logging 与 IOC Runtime。
		baseapp.WithName("quickstart-app-web-hertz"),
		baseapp.WithDescription("Hertz Web、请求观测与独立诊断端口 Quickstart"),
		baseapp.WithConfig(
			baseconfig.File(baseconfig.FileDisabled()),
			baseconfig.Defaults(defaultConfig()),
		),
		baseapp.WithLogging(applogzap.New()),
		baseapp.WithBeans(&GreetingController{}),
		// 第二阶段：应用 Registry 聚合 IOC/Web Collector，Web Module 只借用语义接口。
		baseapp.WithModules(modules...),
		// 第三阶段：同一个监督器实例依次进入 Prepare、Runner 和 Shutdown Hook。
		baseapp.WithBeforeRunHook(workloads.Prepare),
		baseapp.WithRunner(func(ctx context.Context, _ baseapp.Runtime) error {
			return workloads.Run(ctx)
		}),
		baseapp.WithShutdown(
			baseapp.WithShutdownHook("web-workloads", workloads.Shutdown),
		),
	)
}

// applicationModules 依据显式环境开关组装应用 owned Provider、观测适配器和 Web Server 依赖图。
// 阶段角色：在 app Runtime 构造前决定是否启用 Trace；默认关闭时不创建 Provider、Exporter 或 goroutine。
func applicationModules() ([]baseapp.Module, error) {
	tracingEnabled, err := loadTracingEnabled()
	if err != nil {
		return nil, err
	}
	observeOptions := []webhertzobserve.ModuleOption{
		webhertzobserve.WithRequestLog(),
		webhertzobserve.WithSharedPrometheus(),
	}
	modules := []baseapp.Module{newApplicationMetricsModule(applicationMetricsConfig{})}
	if tracingEnabled {
		// Provider owner 先声明，adapter 借用并由 Web Server 反向依赖；IOC Close 自动按相反顺序收敛。
		modules = append(modules, newApplicationTracingModule(applicationTracingConfig{
			ServiceName:             "quickstart-app-web-hertz",
			SamplingPolicyReference: defaultApplicationSamplingPolicyReference,
		}))
		observeOptions = append(observeOptions, webhertzobserve.WithOpenTelemetry(
			webhertzobserve.WithSamplingPolicyReference(defaultApplicationSamplingPolicyReference),
			webhertzobserve.WithTracingIgnoredPaths("/healthz", "/readyz"),
		))
	}
	modules = append(modules, webhertzobserve.NewModule(observeOptions...), appwebhertz.NewModule(""))
	return modules, nil
}

// defaultConfig 返回示例进程的最低优先级配置树。
func defaultConfig() map[string]any {
	return map[string]any{
		// 诊断配置属于当前应用；不会成为 web-hertz 或观测适配包的固定 scope。
		"quickstart": map[string]any{
			"diagnostics": map[string]any{
				"enabled":           true,
				"address":           loadDiagnosticsAddress(),
				"metricsEnabled":    true,
				"metricsPath":       "/metrics",
				"pprofEnabled":      false,
				"readHeaderTimeout": "5s",
			},
		},
		// 业务 Server 与标准 Middleware 继续只读取既有 web.hertz scope。
		"web": map[string]any{
			"hertz": map[string]any{
				"address": loadAddress(),
				"middleware": map[string]any{
					"recovery":  map[string]any{"enabled": true, "response": "json"},
					"requestId": map[string]any{"enabled": true, "header": "X-Request-ID"},
					"accessLog": map[string]any{"enabled": true, "skipPaths": []string{"/healthz", "/readyz"}, "slowThreshold": "500ms"},
					"cors": map[string]any{
						"enabled":      true,
						"allowOrigins": []string{"http://localhost:3000"},
						"allowMethods": []string{"GET", "POST", "OPTIONS"},
					},
					"gzip": map[string]any{"enabled": true, "minLength": 1},
				},
			},
		},
	}
}

// loadAddress 读取并清理示例监听地址，空值回退固定教学端口。
func loadAddress() string {
	address := strings.TrimSpace(os.Getenv(quickstartAddressEnv))
	if address == "" {
		return defaultQuickstartAddress
	}
	return address
}

// loadDiagnosticsAddress 读取诊断监听地址；空值回退仅 loopback 的教学端口。
func loadDiagnosticsAddress() string {
	address := strings.TrimSpace(os.Getenv(quickstartDiagnosticsAddressEnv))
	if address == "" {
		return defaultQuickstartDiagnosticsAddress
	}
	return address
}

// loadTracingEnabled 解析显式 Trace 开关；空值保持默认关闭，非法值在 Runtime 构造前失败。
func loadTracingEnabled() (bool, error) {
	value := strings.TrimSpace(os.Getenv(quickstartTracingEnabledEnv))
	if value == "" {
		return false, nil
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("op=quickstart.tracing result=failed stage=resolve_enabled reason=value_invalid: %w", err)
	}
	return enabled, nil
}
