package appwebhertz

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/network/standard"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	baseapp "github.com/yangzhuangqiu/gobase/app"
	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/ioc"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// appLifecycleController 用真实请求证明 app Workload 收敛先于 IOC Resource Close。
type appLifecycleController struct {
	// requestStarted 在阻塞 Handler 已经取得 IOC Resource 后关闭。
	requestStarted chan struct{}
	// releaseRequest 由测试释放，使在途请求完成。
	releaseRequest chan struct{}
	// handlerReturned 在 Handler 不再访问 Controller 后关闭。
	handlerReturned chan struct{}
	// workloadReturned 在真实 Hertz Workload.Run 完全收敛后关闭。
	workloadReturned chan struct{}
	// controllerClosed 在 IOC 逆序关闭 Controller Resource 后关闭。
	controllerClosed chan struct{}
	// requestOnce 保证并发或重试请求不会重复关闭 requestStarted。
	requestOnce sync.Once
	// handlerOnce 保证测试完成信号只发布一次。
	handlerOnce sync.Once
	// closeOnce 保证 IOC 重复关闭时只发布一次资源关闭信号。
	closeOnce sync.Once
}

// appLifecycleWorkload 只记录真实 Hertz Workload 的 Run 返回阶段，不改变任何生命周期行为。
type appLifecycleWorkload struct {
	// delegate 是生产 Module 返回的真实适配器。
	delegate baseapp.Workload
	// runReturned 在 delegate.Run 返回后关闭，供 IOC Resource 校验阶段顺序。
	runReturned chan struct{}
	// runOnce 保证异常重复调用不会重复关闭完成信号。
	runOnce sync.Once
}

// appLifecycleModule 为真实 Hertz Module 替换等价的可观测测试 Workload。
type appLifecycleModule struct {
	// delegate 保留生产 Module 的全部 IOC 注册行为。
	delegate Module
	// workload 仅增加 Run 返回信号，不改变生产生命周期语义。
	workload baseapp.Workload
}

// Name 保留生产 Module 的稳定身份。
func (this *appLifecycleModule) Name() string { return this.delegate.Name() }

// Register 委托生产 Module 声明真实 Hertz Server Bean。
func (this *appLifecycleModule) Register(registry ioc.Registry) error {
	return this.delegate.Register(registry)
}

// Workloads 返回带阶段观测的真实 Hertz 生命周期适配器。
func (this *appLifecycleModule) Workloads() []baseapp.Workload {
	return []baseapp.Workload{this.workload}
}

// Name 保留生产 Hertz Workload 的稳定诊断名称。
func (this *appLifecycleWorkload) Name() string { return this.delegate.Name() }

// Run 委托真实 Hertz Workload，并在其完整返回后发布测试阶段信号。
func (this *appLifecycleWorkload) Run(ctx context.Context, runtime baseapp.Runtime, command baseapp.CommandContext) error {
	defer this.runOnce.Do(func() { close(this.runReturned) })
	return this.delegate.Run(ctx, runtime, command)
}

// Shutdown 原样委托真实 Hertz Workload 的优雅停止请求。
func (this *appLifecycleWorkload) Shutdown(ctx context.Context, runtime baseapp.Runtime, command baseapp.CommandContext) error {
	return this.delegate.Shutdown(ctx, runtime, command)
}

// GroupPath 把真实关闭测试路由放在根逻辑分组。
func (this *appLifecycleController) GroupPath() string { return "/" }

// RegisterRoutes 注册一个由测试控制完成时机的真实阻塞请求。
func (this *appLifecycleController) RegisterRoutes(router *route.RouterGroup) error {
	router.GET("/block", func(_ context.Context, request *hertzapp.RequestContext) {
		this.requestOnce.Do(func() { close(this.requestStarted) })
		<-this.releaseRequest
		request.String(consts.StatusOK, "ok")
		this.handlerOnce.Do(func() { close(this.handlerReturned) })
	})
	return nil
}

// Close 验证 IOC 只有在 Handler 完全返回后才能释放 Controller Resource。
// 阶段角色：Runtime Close 逆序资源回收阶段。
func (this *appLifecycleController) Close(context.Context) error {
	select {
	case <-this.handlerReturned:
	default:
		return errors.New("op=app_web_hertz.test_close result=failed stage=controller_close reason=handler_still_running")
	}
	select {
	case <-this.workloadReturned:
		this.closeOnce.Do(func() { close(this.controllerClosed) })
		return nil
	default:
		return errors.New("op=app_web_hertz.test_close result=failed stage=controller_close reason=workload_still_running")
	}
}

// TestAppWorkloadGracefullyStopsRealHertzBeforeClosingIOCResources 验证 App、Workload、Hertz 与 IOC 的真实关闭顺序。
//
// 测试方法：使用预绑定 listener 启动真实 App 和阻塞请求，取消宿主 context 后断言 App 与 Controller
// 尚未关闭；释放请求后再断言 HTTP、Workload Run 和 IOC Resource 依次收敛，并保留宿主取消原因。
func TestAppWorkloadGracefullyStopsRealHertzBeforeClosingIOCResources(t *testing.T) {
	const exitWaitTime = 30 * time.Millisecond
	// 第一阶段：准备真实 listener 和同时充当 Controller/IOC Resource 的有状态组件。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen app lifecycle address: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	workloadReturned := make(chan struct{})
	controller := &appLifecycleController{
		requestStarted:   make(chan struct{}),
		releaseRequest:   make(chan struct{}),
		handlerReturned:  make(chan struct{}),
		workloadReturned: workloadReturned,
		controllerClosed: make(chan struct{}),
	}
	workload := &appLifecycleWorkload{delegate: newServerWorkload(), runReturned: workloadReturned}
	module := &appLifecycleModule{
		delegate: NewModule(WithServerOptions(webhertz.WithHertzOptions(
			server.WithListener(listener),
			server.WithTransport(standard.NewTransporter),
		))),
		workload: workload,
	}

	// 第二阶段：通过公开 App API 装配 Module 与 Workload，启动完整 Runtime 事务。
	application := baseapp.New(
		baseapp.WithName("app-web-hertz-lifecycle-test"),
		baseapp.WithConfig(
			baseconfig.File(baseconfig.FileDisabled()),
			baseconfig.Defaults(map[string]any{"web": map[string]any{"hertz": map[string]any{
				"exitWaitTime": exitWaitTime,
				"shutdown":     map[string]any{"handlerCompletion": "required"},
			}}}),
		),
		baseapp.WithBeanDefinitions(DefineController(controller)),
		baseapp.WithModules(module),
		baseapp.WithShutdownPolicy(baseapp.ShutdownPolicy{WorkloadGracefulTimeout: 2 * time.Second}),
	)
	hostCtx, cancelHost := context.WithCancelCause(context.Background())
	hostCause := errors.New("test host stopping")
	appResult := make(chan error, 1)
	go func() { appResult <- application.ExecuteContext(hostCtx, nil) }()

	// 第三阶段：发出真实请求并等待 Handler 进入，再触发 App 的统一关闭协调器。
	requestResult := make(chan error, 1)
	go func() {
		response, requestErr := http.Get("http://" + listener.Addr().String() + "/block")
		if requestErr == nil {
			_, requestErr = io.Copy(io.Discard, response.Body)
			requestErr = errors.Join(requestErr, response.Body.Close())
		}
		requestResult <- requestErr
	}()
	waitForAppLifecycleSignal(t, controller.requestStarted, "request start")
	cancelHost(hostCause)

	// 第四阶段：等待超过 Hertz transport budget；required 合同仍不得返回或提前关闭 IOC Resource。
	select {
	case err := <-appResult:
		t.Fatalf("app returned before request completed: %v", err)
	case <-time.After(3 * exitWaitTime):
	}
	select {
	case <-controller.controllerClosed:
		t.Fatal("controller resource closed before request completed")
	default:
	}

	// 第五阶段：请求完成后，Hertz Run、Workload 与 IOC Close 必须依次收敛。
	close(controller.releaseRequest)
	if err := waitForAppLifecycleError(t, requestResult, "request result"); err != nil {
		t.Fatalf("active request should finish during graceful shutdown: %v", err)
	}
	if err := waitForAppLifecycleError(t, appResult, "app result"); !errors.Is(err, hostCause) {
		t.Fatalf("expected host shutdown cause, got=%v", err)
	}
	waitForAppLifecycleSignal(t, controller.controllerClosed, "controller close")
}

// waitForAppLifecycleSignal 在固定测试预算内等待生命周期阶段信号。
func waitForAppLifecycleSignal(t *testing.T, signal <-chan struct{}, stage string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", stage)
	}
}

// waitForAppLifecycleError 在固定测试预算内读取异步生命周期结果。
func waitForAppLifecycleError(t *testing.T, result <-chan error, stage string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", stage)
		return nil
	}
}
