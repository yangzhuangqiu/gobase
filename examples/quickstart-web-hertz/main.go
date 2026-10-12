package main

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/middlewares/server/recovery"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

const (
	// quickstartAddressEnv 是示例监听地址的可选环境变量。
	quickstartAddressEnv = "WEB_HERTZ_ADDRESS"
	// defaultQuickstartAddress 仅用于示例，避免占用 provider 默认端口。
	defaultQuickstartAddress = "127.0.0.1:18080"
)

// healthResponse 是健康检查成功时返回的稳定 JSON 结构。
type healthResponse struct {
	// Status 表示当前进程可以处理 HTTP 请求。
	Status string `json:"status"`
}

// helloResponse 是带路径参数的教学响应结构。
type helloResponse struct {
	// Message 是根据请求路径 name 生成的问候语。
	Message string `json:"message"`
}

// main 是 quickstart 进程入口，统一记录无法恢复的构造或运行失败。
func main() {
	if err := run(); err != nil {
		hlog.SystemLogger().Errorf("op=quickstart_web_hertz.run result=failed reason=server_failed error=%v", err)
		os.Exit(1)
	}
}

// run 完成配置、原生扩展和进程信号到 web.Server 生命周期的装配。
func run() error {
	config := loadConfig()
	server, err := webhertz.New(
		config,
		// accessLog 在 recovery 外层，保证 panic 转换为 500 后仍能记录最终状态。
		webhertz.WithMiddleware(accessLog(), recovery.Recovery()),
		webhertz.WithNoRoute(notFoundHandler),
		webhertz.WithNoMethod(methodNotAllowedHandler),
		webhertz.WithRoutes(registerRoutes),
	)
	if err != nil {
		return err
	}

	ctx, stopSignal := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignal()
	hlog.SystemLogger().Infof(
		"op=quickstart_web_hertz.run result=starting address=%s reason=awaiting_requests",
		config.Address,
	)
	// Run 消费 context 取消并等待 Hertz 优雅停止，不需要示例再安装第二套信号处理。
	return server.Run(ctx)
}

// loadConfig 从示例专属环境变量读取监听地址，其余字段保留 web-hertz 默认值。
func loadConfig() webhertz.Config {
	address := strings.TrimSpace(os.Getenv(quickstartAddressEnv))
	if address == "" {
		address = defaultQuickstartAddress
	}
	return webhertz.Config{Address: address}
}

// registerRoutes 通过 Hertz 原生根 RouterGroup 显式声明路由和分组。
func registerRoutes(router *route.RouterGroup) error {
	router.GET("/healthz", healthHandler)
	api := router.Group("/api/v1")
	api.GET("/hello/:name", helloHandler)
	return nil
}

// accessLog 返回使用 Hertz 原生 RequestContext 的全局访问日志 Middleware。
func accessLog() app.HandlerFunc {
	return func(ctx context.Context, request *app.RequestContext) {
		// 第一阶段：记录入口时间并让 Hertz 执行 recovery 和最终业务 Handler。
		startedAt := time.Now()
		request.Next(ctx)

		// 第二阶段：按最终响应状态记录结果；未匹配路由回退到原始请求路径。
		status := request.Response.StatusCode()
		path := request.FullPath()
		if path == "" {
			path = string(request.Path())
		}
		if status >= consts.StatusInternalServerError {
			hlog.CtxErrorf(
				ctx,
				"op=quickstart_web_hertz.request result=failed reason=response_5xx method=%s path=%s status=%d duration_ms=%d",
				request.Method(), path, status, time.Since(startedAt).Milliseconds(),
			)
			return
		}
		hlog.CtxInfof(
			ctx,
			"op=quickstart_web_hertz.request result=ok method=%s path=%s status=%d duration_ms=%d",
			request.Method(), path, status, time.Since(startedAt).Milliseconds(),
		)
	}
}

// healthHandler 返回进程级存活状态，不执行外部依赖探测。
func healthHandler(_ context.Context, request *app.RequestContext) {
	request.JSON(consts.StatusOK, healthResponse{Status: "ok"})
}

// helloHandler 演示直接读取 Hertz 路径参数并返回 JSON。
func helloHandler(_ context.Context, request *app.RequestContext) {
	request.JSON(consts.StatusOK, helloResponse{Message: "hello, " + request.Param("name")})
}

// notFoundHandler 为未匹配路径返回稳定的 JSON 404，便于客户端按 code 分支。
func notFoundHandler(_ context.Context, request *app.RequestContext) {
	request.JSON(consts.StatusNotFound, map[string]string{"code": "route_not_found"})
}

// methodNotAllowedHandler 为路径存在但方法不匹配的请求返回稳定的 JSON 405。
func methodNotAllowedHandler(_ context.Context, request *app.RequestContext) {
	request.JSON(consts.StatusMethodNotAllowed, map[string]string{"code": "method_not_allowed"})
}
