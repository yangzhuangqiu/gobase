package appwebhertz

import (
	"context"
	"net"
	"testing"
	"time"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/network/standard"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/yangzhuangqiu/gobase/errcode"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
	baseweb "github.com/yangzhuangqiu/gobase/web"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// TestManagedServerLogsAssemblySummaryOnce 验证 IOC 聚合成功只发布一次高层 Info 摘要。
// 测试方法：注册基础依赖和空 Server Module，Build 后筛选 assembly Logger，断言固定 op/result、组件数量与能力开关。
func TestManagedServerLogsAssemblySummaryOnce(t *testing.T) {
	// 第一阶段：构建只启用 Recovery 的空应用 Server，使组装摘要字段保持可预测。
	manager := newTestManager(t, map[string]any{
		"web": map[string]any{"hertz": map[string]any{}},
	})
	defer func() { _ = manager.Close() }()
	container := ioc.New()
	loggerFactory := registerBorrowedManager(t, container, manager)
	if err := NewModule().Register(container); err != nil {
		t.Fatalf("register module: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build container: %v", err)
	}
	defer func() { _ = container.Close(context.Background()) }()

	// 第二阶段：复制日志快照并只筛选 assembly Logger，避免持锁执行断言。
	loggerFactory.mu.Lock()
	events := append([]recordedLogEvent{}, loggerFactory.events...)
	loggerFactory.mu.Unlock()
	assemblyEvents := make([]recordedLogEvent, 0, 1)
	for _, event := range events {
		if event.logger == assemblyLoggerName {
			assemblyEvents = append(assemblyEvents, event)
		}
	}
	if len(assemblyEvents) != 1 {
		t.Fatalf("expected one assembly event, got=%+v", assemblyEvents)
	}
	event := assemblyEvents[0]
	if event.level != baselog.LevelInfo || event.fields["op"] != "app_web_hertz.assemble" || event.fields["result"] != "ok" || event.fields["controller_count"] != 0 || event.fields["recovery_enabled"] != true {
		t.Fatalf("unexpected assembly summary: %+v", event)
	}
}

// TestContainerCloseStopsManagedRunningServer 验证 IOC 对 Server 的托管包含真实运行周期的关闭责任。
// 测试方法：用预绑定 listener 通过 Module 构造 Server，启动并确认可请求后关闭 Container，断言 Run 正常解除且 Manager 仍可用。
func TestContainerCloseStopsManagedRunningServer(t *testing.T) {
	// 第一阶段：预绑定 listener 并准备 Module 所需配置和 Controller。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test address: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	manager := newTestManager(t, map[string]any{
		"web": map[string]any{"hertz": map[string]any{"exitWaitTime": time.Second}},
	})
	defer func() { _ = manager.Close() }()

	container := ioc.New()
	registerBorrowedManager(t, container, manager)
	controller := controllerFunc(func(router *route.RouterGroup) error {
		router.GET("/ready", func(_ context.Context, request *hertzapp.RequestContext) {
			request.String(consts.StatusOK, "ready")
		})
		return nil
	})
	if err := container.Register(controller, ioc.WithDeclare[Controller]()); err != nil {
		t.Fatalf("register controller: %v", err)
	}
	module := NewModule(WithServerOptions(webhertz.WithHertzOptions(
		server.WithListener(listener),
		server.WithTransport(standard.NewTransporter),
	)))
	// 第二阶段：Build 后解析并启动 IOC 托管的唯一 Web Server。
	if err := module.Register(container); err != nil {
		t.Fatalf("register module: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build container: %v", err)
	}
	webServer, err := ioc.ResolveAs[baseweb.Server](container)
	if err != nil {
		t.Fatalf("resolve web server: %v", err)
	}
	runResult := make(chan error, 1)
	go func() { runResult <- webServer.Run(context.Background()) }()
	requestUntilReady(t, "http://"+listener.Addr().String()+"/ready")

	// 第三阶段：由 Container 触发 Shutdown，并验证 Run 和 borrowed Manager 结果。
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close container: %v", err)
	}
	if err := waitForLifecycleError(t, runResult); err != nil {
		t.Fatalf("managed run result: %v", err)
	}
	if manager.Current() == nil {
		t.Fatal("expected borrowed manager after managed server close")
	}
}

// TestManagedServerRejectsLifecycleBeforeInit 验证托管代理不会在 IOC Init 前发生 nil 委托 panic。
// 测试方法：直接调用未初始化 bean 的 Run/WaitStarted/Shutdown，断言稳定状态错误；Close 作为回滚入口保持幂等成功。
func TestManagedServerRejectsLifecycleBeforeInit(t *testing.T) {
	bean := &managedServer{}
	if err := bean.Run(context.Background()); !errcode.IsCode[ErrorCode](err, ErrCodeServerStateInvalid) {
		t.Fatalf("expected run state error, got=%v", err)
	}
	if err := bean.WaitStarted(context.Background()); !errcode.IsCode[ErrorCode](err, ErrCodeServerStateInvalid) {
		t.Fatalf("expected wait-ready state error, got=%v", err)
	}
	if err := bean.Shutdown(context.Background()); !errcode.IsCode[ErrorCode](err, ErrCodeServerStateInvalid) {
		t.Fatalf("expected shutdown state error, got=%v", err)
	}
	if err := bean.Close(context.Background()); err != nil {
		t.Fatalf("expected close before init to be idempotent: %v", err)
	}
}
