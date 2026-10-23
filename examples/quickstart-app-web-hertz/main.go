package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	baseapp "github.com/yangzhuangqiu/gobase/app"
	applogzap "github.com/yangzhuangqiu/gobase/app-log-zap"
	appwebhertz "github.com/yangzhuangqiu/gobase/app-web-hertz"
	baseconfig "github.com/yangzhuangqiu/gobase/config"
	apphlogzap "github.com/yangzhuangqiu/gobase/hlog-zap/app"
	"github.com/yangzhuangqiu/gobase/ioc"
)

const (
	// quickstartAddressEnv 是示例监听地址的可选环境变量。
	quickstartAddressEnv = "APP_WEB_HERTZ_ADDRESS"
	// defaultQuickstartAddress 只用于本地教学，避免占用 provider Quickstart 端口。
	defaultQuickstartAddress = "127.0.0.1:18081"
)

// greetingResponse 是自动和手动路由共用的教学响应结构。
type greetingResponse struct {
	// Message 表示本次请求生成的问候内容。
	Message string `json:"message"`
	// Method 表示命中 Controller 的 HTTP method。
	Method string `json:"method"`
}

// errorResponse 是 fallback handler 返回的稳定错误结构。
type errorResponse struct {
	// Code 是应用可检索的错误类别。
	Code string `json:"code"`
	// Message 是面向调用方的安全错误说明。
	Message string `json:"message"`
}

// GreetingController 演示逻辑分组、自动多 method 路由和手动动态路由。
type GreetingController struct{}

// GroupPath 把当前 Controller 的全部路由绑定到共享逻辑分组 `/api`。
func (this *GreetingController) GroupPath() string { return "/api" }

// GetOrPostHello 通过方法名自动同时注册 GET/POST `/api/hello`。
func (this *GreetingController) GetOrPostHello(_ context.Context, request *hertzapp.RequestContext) {
	request.JSON(consts.StatusOK, greetingResponse{
		Message: "hello from automatic route",
		Method:  string(request.Method()),
	})
}

// RegisterRoutes 在 Controller 无路径子分组上注册含路径参数的原生路由。
func (this *GreetingController) RegisterRoutes(router *route.RouterGroup) error {
	router.GET("/users/:name", func(_ context.Context, request *hertzapp.RequestContext) {
		request.JSON(consts.StatusOK, greetingResponse{
			Message: "hello, " + request.Param("name"),
			Method:  string(request.Method()),
		})
	})
	return nil
}

// globalHeaderFilter 演示注册到 Hertz Engine 的全局 Filter。
type globalHeaderFilter struct{}

// Order 把全局 Header Filter 放在同作用域较早位置。
func (this *globalHeaderFilter) Order() int { return 10 }

// DoFilter 为全部请求写入教学 Header，并继续执行后续链。
func (this *globalHeaderFilter) DoFilter(ctx context.Context, request *hertzapp.RequestContext) {
	request.Response.Header.Set("X-Quickstart-Global", "true")
	request.Next(ctx)
}

// apiHeaderFilter 演示只作用于 `/api` 逻辑分组的 Filter。
type apiHeaderFilter struct{}

// Order 把分组 Header Filter 放在 `/api` 作用域较早位置。
func (this *apiHeaderFilter) Order() int { return 10 }

// GroupPath 把当前 Filter 绑定到 `/api` 共享基础分组。
func (this *apiHeaderFilter) GroupPath() string { return "/api" }

// DoFilter 为 `/api` Controller 响应写入教学 Header，并继续执行后续链。
func (this *apiHeaderFilter) DoFilter(ctx context.Context, request *hertzapp.RequestContext) {
	request.Response.Header.Set("X-Quickstart-Group", "api")
	request.Next(ctx)
}

// jsonNoRouteHandler 为未匹配路由返回统一 JSON 404。
type jsonNoRouteHandler struct{}

// Order 返回 404 fallback 链中的稳定顺序。
func (this *jsonNoRouteHandler) Order() int { return 0 }

// HandleNoRoute 写入最终 404 响应，不继续 fallback 链。
func (this *jsonNoRouteHandler) HandleNoRoute(_ context.Context, request *hertzapp.RequestContext) {
	request.JSON(consts.StatusNotFound, errorResponse{Code: "route_not_found", Message: "route not found"})
}

// jsonNoMethodHandler 为 Method 不匹配请求返回统一 JSON 405。
type jsonNoMethodHandler struct{}

// Order 返回 405 fallback 链中的稳定顺序。
func (this *jsonNoMethodHandler) Order() int { return 0 }

// HandleNoMethod 写入最终 405 响应，不继续 fallback 链。
func (this *jsonNoMethodHandler) HandleNoMethod(_ context.Context, request *hertzapp.RequestContext) {
	request.JSON(consts.StatusMethodNotAllowed, errorResponse{Code: "method_not_allowed", Message: "method not allowed"})
}

// applicationDefinitions 返回由 app Runtime 原样注册的完整 Web 组件声明。
func applicationDefinitions() []ioc.Definition {
	return []ioc.Definition{
		appwebhertz.DefineController(&GreetingController{}),
		appwebhertz.DefineFilter(&globalHeaderFilter{}),
		// 分组 Filter 同样声明为 Filter；装配阶段由 GroupedFilter 能力决定作用域。
		appwebhertz.DefineFilter(&apiHeaderFilter{}),
		appwebhertz.DefineNoRouteHandler(&jsonNoRouteHandler{}),
		appwebhertz.DefineNoMethodHandler(&jsonNoMethodHandler{}),
	}
}

// main 是应用级 Quickstart 入口，统一向 stderr 报告最终失败。
func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "quickstart-app-web-hertz failed: %v\n", err)
		os.Exit(1)
	}
}

// run 使用 app Runtime、IOC Module 和 Runner 构建并运行唯一 Hertz Server。
func run() error {
	return baseapp.Run(
		// 第一阶段：app 创建并拥有 Config、Logging 与 IOC Runtime。
		baseapp.WithName("quickstart-app-web-hertz"),
		baseapp.WithDescription("app-web-hertz IOC server assembly quickstart"),
		baseapp.WithConfig(
			baseconfig.File(baseconfig.FileDisabled()),
			baseconfig.Defaults(defaultConfig()),
		),
		// 日志 Provider 在 IOC Build 前同时安装可恢复的 Hertz hlog，框架与应用共享同一 Zap 底层。
		baseapp.WithLogging(apphlogzap.Wrap(applogzap.New())),
		// 第二阶段：Definition 发布组件语义，Module 在 IOC Init 聚合并构造 web.Server。
		baseapp.WithBeanDefinitions(applicationDefinitions()...),
		baseapp.WithModules(appwebhertz.NewModule()),
		// 第三阶段：Runner 解析唯一 Server；取消与 Runtime Close 由 app 统一编排。
		baseapp.WithRunner(appwebhertz.Runner()),
	)
}

// defaultConfig 返回示例进程的最低优先级配置树。
func defaultConfig() map[string]any {
	// 唯一配置 scope 同时描述 provider Router 与 app-web-hertz 标准 Middleware。
	return map[string]any{
		"web": map[string]any{
			"hertz": map[string]any{
				"address": loadAddress(),
				"router": map[string]any{
					"handleMethodNotAllowed": true,
				},
				"middleware": map[string]any{
					"recovery":  map[string]any{"enabled": true, "response": "json"},
					"requestId": map[string]any{"enabled": true, "header": "X-Request-ID"},
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
