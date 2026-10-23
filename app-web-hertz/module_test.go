package appwebhertz

import (
	"context"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/network/standard"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/errcode"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
	baseweb "github.com/yangzhuangqiu/gobase/web"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// serverDependentController 用于验证 Controller 反向依赖 web.Server 会形成真实依赖环。
type serverDependentController struct {
	// Server 是当前 Controller 试图反向注入的应用 Web Server。
	Server baseweb.Server
}

// recordingRequestObserver 保存通用 Metrics 类 Observer 收到的同一完成态快照。
type recordingRequestObserver struct {
	// observations 接收完成态值副本。
	observations chan webhertz.HTTPRequestObservation
}

// Observe 把完成态值复制到测试 channel。
func (this *recordingRequestObserver) Observe(_ context.Context, observation webhertz.HTTPRequestObservation) {
	this.observations <- observation
}

// GroupPath 让循环依赖测试 Controller 使用根路径。
func (this *serverDependentController) GroupPath() string {
	return ""
}

// RegisterRoutes 提供最小手动路由能力，使测试只聚焦 IOC 依赖环。
func (this *serverDependentController) RegisterRoutes(_ *route.RouterGroup) error {
	return nil
}

// TestModuleAutomaticallyAssemblesControllerFilterAndServer 验证 IOC Build 会自动聚合组件并发布唯一 web.Server。
// 测试方法：注册 Manager、两个 Controller、两个 Filter 和 Module，Build 后断言路由注册顺序、切片注入顺序及 web.Server 可解析。
func TestModuleAutomaticallyAssemblesControllerFilterAndServer(t *testing.T) {
	// 第一阶段：按可断言顺序注册 borrowed Manager 和应用组件。
	manager := newTestManager(t, map[string]any{
		"web": map[string]any{"hertz": map[string]any{"address": "127.0.0.1:0"}},
	})
	defer func() { _ = manager.Close() }()

	container := ioc.New()
	registerBorrowedManager(t, container, manager)
	order := make([]string, 0, 2)
	first := controllerFunc(func(_ *route.RouterGroup) error {
		order = append(order, "first")
		return nil
	})
	second := controllerFunc(func(_ *route.RouterGroup) error {
		order = append(order, "second")
		return nil
	})
	firstFilter := &filterStub{order: 20}
	secondFilter := &filterStub{order: 10}
	if err := container.Register(first, ioc.WithName("FirstController"), ioc.WithDeclare[Controller]()); err != nil {
		t.Fatalf("register first controller: %v", err)
	}
	if err := container.Register(second, ioc.WithName("SecondController"), ioc.WithDeclare[Controller]()); err != nil {
		t.Fatalf("register second controller: %v", err)
	}
	if err := container.Register(firstFilter, ioc.WithName("FirstFilter"), ioc.WithDeclare[Filter]()); err != nil {
		t.Fatalf("register first filter: %v", err)
	}
	if err := container.Register(secondFilter, ioc.WithName("SecondFilter"), ioc.WithDeclare[Filter]()); err != nil {
		t.Fatalf("register second filter: %v", err)
	}
	bundle := &instrumentationBundleStub{}
	if err := container.Register(bundle, ioc.WithName("InstrumentationBundle"), ioc.WithDeclare[InstrumentationBundle]()); err != nil {
		t.Fatalf("register instrumentation bundle: %v", err)
	}
	// 第二阶段：注册 Module 并让 IOC 完成自动注入和 Server Init。
	if err := NewModule().Register(container); err != nil {
		t.Fatalf("register module: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build container: %v", err)
	}

	// 第三阶段：验证接口发布、切片顺序和 borrowed 关闭边界。
	webServer, err := ioc.ResolveAs[baseweb.Server](container)
	if err != nil || webServer == nil {
		t.Fatalf("resolve web server: server=%v err=%v", webServer, err)
	}
	bean, err := ioc.ResolveAs[*managedServer](container)
	if err != nil {
		t.Fatalf("resolve managed server: %v", err)
	}
	if !reflect.DeepEqual(order, []string{"first", "second"}) {
		t.Fatalf("unexpected controller registration order: %v", order)
	}
	if len(bean.Controllers) != 2 ||
		reflect.ValueOf(bean.Controllers[0]).Pointer() != reflect.ValueOf(first).Pointer() ||
		reflect.ValueOf(bean.Controllers[1]).Pointer() != reflect.ValueOf(second).Pointer() {
		t.Fatalf("unexpected injected controllers: %v", bean.Controllers)
	}
	if !reflect.DeepEqual(bean.Filters, []Filter{firstFilter, secondFilter}) {
		t.Fatalf("unexpected injected filters: %v", bean.Filters)
	}
	if len(bean.InstrumentationBundles) != 1 || bean.InstrumentationBundles[0] != bundle {
		t.Fatalf("unexpected injected instrumentation bundles: %v", bean.InstrumentationBundles)
	}
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close container: %v", err)
	}
	if manager.Current() == nil {
		t.Fatal("expected borrowed manager to remain available after container close")
	}
}

// TestManagedServerLogsAssemblySummaryOnce 验证 IOC 聚合成功只发布一次高层 Info 摘要。
// 测试方法：注册基础依赖和空 Server Module，Build 后筛选 assembly Logger，断言固定 op/result、组件数量与能力开关。
func TestManagedServerLogsAssemblySummaryOnce(t *testing.T) {
	manager := newTestManager(t, map[string]any{
		"web": map[string]any{"hertz": map[string]any{
			"middleware": map[string]any{"recovery": map[string]any{"enabled": true}},
		}},
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

// TestModuleBuildsConfiguredMiddlewareFromSingleSnapshot 验证 IOC Init 从同一 Snapshot 装配标准 Middleware，并保持 typed policy 与生命周期边界。
// 测试方法：配置 Request ID/Recovery，注入固定 Generator 和预绑定 listener，访问 panic 路由后断言 500/ID，再检查 Manager 单次读取且无订阅。
func TestModuleBuildsConfiguredMiddlewareFromSingleSnapshot(t *testing.T) {
	// 第一阶段：准备单一 Snapshot、可控 listener 与会触发 panic 的 Controller 声明。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen configured module address: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	underlying := newTestManager(t, map[string]any{
		"web": map[string]any{"hertz": map[string]any{
			"exitWaitTime": time.Second,
			"middleware": map[string]any{
				"requestId": map[string]any{"enabled": true},
				"recovery":  map[string]any{"enabled": true, "response": "json"},
				"accessLog": map[string]any{"enabled": true, "slowThreshold": "1ns"},
			},
		}},
	})
	defer func() { _ = underlying.Close() }()
	manager := &countingManager{Manager: underlying}

	container := ioc.New()
	loggerFactory := registerBorrowedManager(t, container, manager)
	controller := controllerFunc(func(router *route.RouterGroup) error {
		router.GET("/panic", func(_ context.Context, _ *hertzapp.RequestContext) { panic("private panic") })
		return nil
	})
	if err := container.Register(controller, ioc.WithDeclare[Controller]()); err != nil {
		t.Fatalf("register configured controller: %v", err)
	}
	requestObserver := &recordingRequestObserver{observations: make(chan webhertz.HTTPRequestObservation, 1)}
	if err := container.Register(requestObserver, ioc.WithDeclare[webhertz.RequestObserver]()); err != nil {
		t.Fatalf("register request observer: %v", err)
	}
	// 第二阶段：通过 Module 注入 typed policy 并完成 IOC Init，配置内容只在此阶段读取。
	module := NewModule(
		WithRequestIDGenerator(func(_ context.Context, _ *hertzapp.RequestContext) (string, error) { return "ioc-request-id", nil }),
		WithServerOptions(webhertz.WithHertzOptions(
			server.WithListener(listener),
			server.WithTransport(standard.NewTransporter),
		)),
	)
	if err := module.Register(container); err != nil {
		t.Fatalf("register configured module: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build configured module: %v", err)
	}
	webServer, err := ioc.ResolveAs[baseweb.Server](container)
	if err != nil {
		t.Fatalf("resolve configured server: %v", err)
	}
	// 第三阶段：访问真实 panic 路由，验证 Request ID、Recovery 与响应合同共同生效。
	runResult := make(chan error, 1)
	go func() { runResult <- webServer.Run(context.Background()) }()

	response := requestHTTPResponseUntilReady(t, "http://"+listener.Addr().String()+"/panic")
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatalf("read configured response: %v", readErr)
	}
	if response.StatusCode != consts.StatusInternalServerError || response.Header.Get(defaultRequestIDHeader) != "ioc-request-id" || !strings.Contains(string(body), "internal_server_error") {
		t.Fatalf("unexpected configured response: status=%d request_id=%s body=%s", response.StatusCode, response.Header.Get(defaultRequestIDHeader), body)
	}
	// 第四阶段：内建日志与通用 Observer 必须共享最终 500、模板路由、Request ID 和 panic outcome。
	var observed webhertz.HTTPRequestObservation
	select {
	case observed = <-requestObserver.observations:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for general request observation")
	}
	loggerFactory.mu.Lock()
	var logged recordedLogEvent
	foundAccessLog := false
	allEvents := append([]recordedLogEvent{}, loggerFactory.events...)
	for _, event := range allEvents {
		if event.logger == accessLoggerName && event.message == "http_request" {
			logged = event
			foundAccessLog = true
			break
		}
	}
	loggerFactory.mu.Unlock()
	if !foundAccessLog {
		t.Fatalf("expected one access log event, got=%+v", allEvents)
	}
	if logged.level != baselog.LevelError || logged.fields["slow"] != true || logged.fields["request_id"] != "ioc-request-id" || logged.fields["route"] != "/panic" || logged.fields["status"] != consts.StatusInternalServerError || logged.fields["outcome"] != webhertz.RequestOutcomePanic {
		t.Fatalf("unexpected access log event: %+v", logged)
	}
	if observed.RequestID != logged.fields["request_id"] || observed.RoutePattern != logged.fields["route"] || observed.Status != logged.fields["status"] || observed.Outcome != logged.fields["outcome"] {
		t.Fatalf("expected shared completion snapshot semantics: log=%+v general=%+v", logged.fields, observed)
	}
	if manager.currentCalls != 1 || manager.subscribeCalls != 0 {
		t.Fatalf("unexpected Manager usage: current=%d subscribe=%d", manager.currentCalls, manager.subscribeCalls)
	}
	// 第五阶段：关闭容器并等待 Run 返回，确认新增装配不改变既有生命周期所有权。
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close configured container: %v", err)
	}
	if err := waitForLifecycleError(t, runResult); err != nil {
		t.Fatalf("configured run result: %v", err)
	}
}

// TestModuleUsesCustomScopeAndPreservesConfigFailures 验证 Module scope 和配置错误在 IOC Init 阶段保持分层语义。
// 测试方法：先用 trim 后的自定义 scope 成功 Build，再用非法 provider 配置 Build，断言 app、IOC 和 web-hertz 错误码均可分支。
func TestModuleUsesCustomScopeAndPreservesConfigFailures(t *testing.T) {
	// 第一阶段：自定义 scope trim 后应完成正常 Build 和 Close。
	customManager := newTestManager(t, map[string]any{
		"services": map[string]any{"api": map[string]any{"address": "127.0.0.1:0"}},
	})
	customContainer := ioc.New()
	registerBorrowedManager(t, customContainer, customManager)
	if err := NewModule(WithScope(" services.api ")).Register(customContainer); err != nil {
		t.Fatalf("register custom module: %v", err)
	}
	if err := customContainer.Build(context.Background()); err != nil {
		t.Fatalf("build custom container: %v", err)
	}
	if err := customContainer.Close(context.Background()); err != nil {
		t.Fatalf("close custom container: %v", err)
	}
	if err := customManager.Close(); err != nil {
		t.Fatalf("close custom manager: %v", err)
	}

	// 第二阶段：默认 scope 中的非法 provider 字段应保留三层错误码。
	invalidManager := newTestManager(t, map[string]any{
		"web": map[string]any{"hertz": map[string]any{"maxRequestBodySize": -1}},
	})
	defer func() { _ = invalidManager.Close() }()
	invalidContainer := ioc.New()
	registerBorrowedManager(t, invalidContainer, invalidManager)
	if err := NewModule().Register(invalidContainer); err != nil {
		t.Fatalf("register invalid module: %v", err)
	}
	err := invalidContainer.Build(context.Background())
	if !errcode.IsCode[ioc.ErrorCode](err, ioc.ErrCodeIOCBuildInitFailed) ||
		!errcode.IsCode[ErrorCode](err, ErrCodeConfigLoadFailed) ||
		!errcode.IsCode[webhertz.ErrorCode](err, webhertz.ErrCodeConfigInvalid) {
		t.Fatalf("expected layered config build error, got=%v", err)
	}
}

// TestWithScopeUsesDefaultAndLastDeclaration 验证 scope Option 保留默认回退与标量 Option 的最后声明优先级。
// 测试方法：分别解析无 Option、空白 scope 和两个连续 scope，断言默认值、trim 与最后声明结果。
func TestWithScopeUsesDefaultAndLastDeclaration(t *testing.T) {
	tests := map[string]struct {
		options  []Option
		expected string
	}{
		"default":   {expected: DefaultScope},
		"blank":     {options: []Option{WithScope("  ")}, expected: DefaultScope},
		"last wins": {options: []Option{WithScope("services.first"), WithScope(" services.second ")}, expected: "services.second"},
	}
	for name, item := range tests {
		t.Run(name, func(t *testing.T) {
			resolved, err := resolveOptions(item.options)
			if err != nil {
				t.Fatalf("resolve scope options: %v", err)
			}
			if resolved.scope != item.expected {
				t.Fatalf("unexpected scope: got=%q expected=%q", resolved.scope, item.expected)
			}
		})
	}
}

// TestModuleRequiresManagerAndRejectsControllerServerCycle 验证自动装配依赖图会拒绝缺失配置和反向 Server 依赖。
// 测试方法：分别省略 Manager、让 Controller 注入 web.Server，断言 IOC 在 Init 前返回依赖缺失或循环错误。
func TestModuleRequiresManagerAndRejectsControllerServerCycle(t *testing.T) {
	// 第一阶段：没有 Manager 时在依赖收集阶段失败，不进入 Server Init。
	missingManagerContainer := ioc.New()
	if err := NewModule().Register(missingManagerContainer); err != nil {
		t.Fatalf("register missing manager module: %v", err)
	}
	err := missingManagerContainer.Build(context.Background())
	if !errcode.IsCode[ioc.ErrorCode](err, ioc.ErrCodeIOCBuildDependencyMiss) {
		t.Fatalf("expected manager dependency error, got=%v", err)
	}

	// LoggerFactory 同样是 app-web-hertz 的基础依赖；裸 IOC 必须显式注册。
	managerOnly := newTestManager(t, map[string]any{"web": map[string]any{"hertz": map[string]any{}}})
	defer func() { _ = managerOnly.Close() }()
	missingLoggerContainer := ioc.New()
	if err := missingLoggerContainer.Register(managerOnly, ioc.WithDeclare[baseconfig.Manager](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register manager-only dependency: %v", err)
	}
	if err := NewModule().Register(missingLoggerContainer); err != nil {
		t.Fatalf("register missing logger module: %v", err)
	}
	if err := missingLoggerContainer.Build(context.Background()); !errcode.IsCode[ioc.ErrorCode](err, ioc.ErrCodeIOCBuildDependencyMiss) {
		t.Fatalf("expected logger factory dependency error, got=%v", err)
	}

	// 第三阶段：Controller 反向依赖 Server 时由拓扑检查拒绝真实循环。
	manager := newTestManager(t, map[string]any{"web": map[string]any{"hertz": map[string]any{}}})
	defer func() { _ = manager.Close() }()
	cycleContainer := ioc.New()
	registerBorrowedManager(t, cycleContainer, manager)
	controller := &serverDependentController{}
	if err := cycleContainer.Register(controller, ioc.WithDeclare[Controller]()); err != nil {
		t.Fatalf("register cycle controller: %v", err)
	}
	if err := NewModule().Register(cycleContainer); err != nil {
		t.Fatalf("register cycle module: %v", err)
	}
	err = cycleContainer.Build(context.Background())
	if !errcode.IsCode[ioc.ErrorCode](err, ioc.ErrCodeIOCBuildCircular) {
		t.Fatalf("expected controller/server cycle error, got=%v", err)
	}
}

// TestModuleRejectsInvalidDeclarationsBeforeRegistryMutation 验证 Module 在注册 Server bean 前拒绝非法声明。
// 测试方法：传入 nil Registry 和 nil Option，断言稳定声明错误；随后 Build 空容器证明 nil Option 未留下部分注册。
func TestModuleRejectsInvalidDeclarationsBeforeRegistryMutation(t *testing.T) {
	// 第一阶段：nil Registry/Option 必须在任何 bean 注册前失败。
	if err := NewModule().Register(nil); !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected nil registry declaration error, got=%v", err)
	}
	container := ioc.New()
	if err := NewModule(nil).Register(container); !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected nil option declaration error, got=%v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("expected registry to remain unchanged, got=%v", err)
	}
	if _, err := ioc.ResolveAs[baseweb.Server](container); !errcode.IsCode[ioc.ErrorCode](err, ioc.ErrCodeIOCResolveNotFound) {
		t.Fatalf("expected no partially registered server, got=%v", err)
	}
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close empty container: %v", err)
	}

	// 第二阶段：重复 Module 注册保留 app 与 IOC 两层错误身份。
	duplicateContainer := ioc.New()
	if err := NewModule().Register(duplicateContainer); err != nil {
		t.Fatalf("register first module: %v", err)
	}
	err := NewModule().Register(duplicateContainer)
	if !errcode.IsCode[ErrorCode](err, ErrCodeModuleRegisterFailed) ||
		!errcode.IsCode[ioc.ErrorCode](err, ioc.ErrCodeIOCRegisterDuplicate) {
		t.Fatalf("expected layered duplicate module error, got=%v", err)
	}
	if err := duplicateContainer.Close(context.Background()); err != nil {
		t.Fatalf("close duplicate container: %v", err)
	}
}

// TestManagedServerRejectsLifecycleBeforeInit 验证托管代理不会在 IOC Init 前发生 nil 委托 panic。
// 测试方法：直接调用未初始化 bean 的 Run/Shutdown，断言稳定状态错误；Close 作为回滚入口保持幂等成功。
func TestManagedServerRejectsLifecycleBeforeInit(t *testing.T) {
	bean := &managedServer{}
	if err := bean.Run(context.Background()); !errcode.IsCode[ErrorCode](err, ErrCodeServerStateInvalid) {
		t.Fatalf("expected run state error, got=%v", err)
	}
	if err := bean.Shutdown(context.Background()); !errcode.IsCode[ErrorCode](err, ErrCodeServerStateInvalid) {
		t.Fatalf("expected shutdown state error, got=%v", err)
	}
	if err := bean.Close(context.Background()); err != nil {
		t.Fatalf("expected close before init to be idempotent: %v", err)
	}
}

// registerBorrowedManager 把测试 Manager 以 app-runtime 相同的借用语义注册到 IOC。
// 阶段角色：Server bean 只读取 Manager，不取得其关闭所有权。
func registerBorrowedManager(t *testing.T, container ioc.Container, manager baseconfig.Manager) *recordingLoggerFactory {
	t.Helper()
	if err := container.Register(manager, ioc.WithName("ConfigManager"), ioc.WithDeclare[baseconfig.Manager](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register config manager: %v", err)
	}
	loggerFactory := &recordingLoggerFactory{}
	if err := container.Register(loggerFactory, ioc.WithName("LoggerFactory"), ioc.WithDeclare[baselog.LoggerFactory](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register logger factory: %v", err)
	}
	return loggerFactory
}

// newTestManager 构建禁用文件发现且只包含给定默认值的 Manager。
// 阶段角色：测试自行关闭返回对象，Server bean 只验证借用语义。
func newTestManager(t *testing.T, defaults map[string]any) baseconfig.Manager {
	t.Helper()
	manager, err := baseconfig.New(
		baseconfig.File(baseconfig.FileDisabled()),
		baseconfig.Defaults(defaults),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build config manager: %v", err)
	}
	return manager
}

// requestHTTPResponseUntilReady 在固定预算内重试连接并返回首个 HTTP 响应。
// 阶段角色：用于需要断言非 2xx Body/Header 的真实 Module 集成测试。
func requestHTTPResponseUntilReady(t *testing.T, target string) *http.Response {
	t.Helper()
	deadline := time.Now().Add(lifecycleTestTimeout)
	for {
		response, err := http.Get(target)
		if err == nil {
			return response
		}
		if time.Now().After(deadline) {
			t.Fatalf("configured request did not become ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
