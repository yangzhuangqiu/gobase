package webhertz

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/cloudwego/hertz/pkg/network/standard"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/yangzhuangqiu/gobase/errcode"
)

const (
	// lifecycleTestTimeout 是本地监听和并发结果等待的最大测试预算。
	lifecycleTestTimeout = 3 * time.Second
)

// panicTransporter 模拟第三方 transport 在阻塞运行入口发生 panic。
type panicTransporter struct {
	// cause 是 ListenAndServe 应抛出的原始 panic 值。
	cause any
}

// shutdownFailureTransporter 模拟运行正常但优雅关闭失败的底层 transport。
type shutdownFailureTransporter struct {
	// stop 在 Shutdown 进入后解除 ListenAndServe 阻塞。
	stop chan struct{}
	// stopOnce 保证并发关闭测试不会重复关闭 stop channel。
	stopOnce sync.Once
	// cause 是 Shutdown 应返回并由两个公共入口共同保留的错误。
	cause error
}

// Close 模拟无需释放资源的强制关闭阶段。
func (this *panicTransporter) Close() error {
	return nil
}

// Shutdown 模拟无需等待资源的优雅关闭阶段。
func (this *panicTransporter) Shutdown(_ context.Context) error {
	return nil
}

// ListenAndServe 在运行阶段抛出预设 panic，验证适配层错误边界。
func (this *panicTransporter) ListenAndServe(_ network.OnData) error {
	panic(this.cause)
}

// Close 强制解除运行阻塞，供 Hertz 失败回滚路径使用。
func (this *shutdownFailureTransporter) Close() error {
	this.stopOnce.Do(func() { close(this.stop) })
	return nil
}

// Shutdown 解除运行阻塞并返回预设底层失败。
func (this *shutdownFailureTransporter) Shutdown(_ context.Context) error {
	this.stopOnce.Do(func() { close(this.stop) })
	return this.cause
}

// ListenAndServe 模拟已经运行并等待停止事务的 transport。
func (this *shutdownFailureTransporter) ListenAndServe(_ network.OnData) error {
	<-this.stop
	return nil
}

// TestLifecycleRejectsNilContexts 验证 nil context 不改变 Server 运行周期。
// 测试方法：依次调用 Run/WaitStarted/Shutdown(nil)，断言各自错误码；再用有效 Shutdown 证明仍在 new 状态。
func TestLifecycleRejectsNilContexts(t *testing.T) {
	instance, err := New(Config{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := instance.Run(nil); !errcode.IsCode[ErrorCode](err, ErrCodeContextInvalid) {
		t.Fatalf("expected run context error, got=%v", err)
	}
	if err := instance.Shutdown(nil); !errcode.IsCode[ErrorCode](err, ErrCodeContextInvalid) {
		t.Fatalf("expected shutdown context error, got=%v", err)
	}
	if err := instance.WaitStarted(nil); !errcode.IsCode[ErrorCode](err, ErrCodeContextInvalid) {
		t.Fatalf("expected startup context error, got=%v", err)
	}
	if err := instance.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown new server: %v", err)
	}
}

// TestWaitStartedBroadcastsStableStartupSuccess 验证多个等待者共享同一个首次启动成功结果。
// 测试方法：在 Run 前启动两个 WaitStarted 调用，真实 listener 可用后断言两者均成功；停止后再次等待仍返回缓存成功。
func TestWaitStartedBroadcastsStableStartupSuccess(t *testing.T) {
	instance := newStandardTestServer(t)
	startupResults := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func() { startupResults <- instance.WaitStarted(context.Background()) }()
	}
	runResult := make(chan error, 1)
	go func() { runResult <- instance.Run(context.Background()) }()
	for index := 0; index < 2; index++ {
		if err := waitForError(t, startupResults); err != nil {
			t.Fatalf("startup waiter %d: %v", index, err)
		}
	}
	if err := instance.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown started server: %v", err)
	}
	if err := waitForError(t, runResult); err != nil {
		t.Fatalf("run started server: %v", err)
	}
	if err := instance.WaitStarted(context.Background()); err != nil {
		t.Fatalf("expected cached startup success after stop: %v", err)
	}
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := instance.WaitStarted(canceledCtx); err != nil {
		t.Fatalf("expected cached startup success to outrank canceled caller: %v", err)
	}
}

// TestWaitStartedCallerTimeoutDoesNotPoisonStartup 验证单个启动等待预算不会改变共享启动结果。
// 测试方法：在 Run 前用已超时 context 等待并断言 wait-interrupted，随后正常 Run 并再次等待启动成功。
func TestWaitStartedCallerTimeoutDoesNotPoisonStartup(t *testing.T) {
	instance := newStandardTestServer(t)
	waitCtx, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	err := instance.WaitStarted(waitCtx)
	if !errcode.IsCode[ErrorCode](err, ErrCodeStartupFailed) ||
		!errcode.IsCode[ErrorCode](err, ErrCodeStartupWaitInterrupted) ||
		!errors.Is(err, ErrStartupWaitInterrupted) || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected isolated startup wait interruption: %v", err)
	}
	runResult := make(chan error, 1)
	go func() { runResult <- instance.Run(context.Background()) }()
	if err = instance.WaitStarted(context.Background()); err != nil {
		t.Fatalf("shared startup result was poisoned: %v", err)
	}
	if err = instance.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown server: %v", err)
	}
	if err = waitForError(t, runResult); err != nil {
		t.Fatalf("run result: %v", err)
	}
}

// TestWaitStartedPreservesStartupFailure 验证真实监听失败会发布可缓存、可分支的 startup result。
// 测试方法：占用目标端口后运行 Server，断言 WaitStarted 命中 startup/run 双层错误码，且 Run 保留运行失败。
func TestWaitStartedPreservesStartupFailure(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve startup failure address: %v", err)
	}
	defer occupied.Close()
	instance, err := New(
		Config{Address: occupied.Addr().String()},
		WithHertzOptions(server.WithTransport(standard.NewTransporter)),
	)
	if err != nil {
		t.Fatalf("new startup failure server: %v", err)
	}
	runResult := make(chan error, 1)
	go func() { runResult <- instance.Run(context.Background()) }()
	startupErr := instance.WaitStarted(context.Background())
	if !errcode.IsCode[ErrorCode](startupErr, ErrCodeStartupFailed) || !errcode.IsCode[ErrorCode](startupErr, ErrCodeRunFailed) {
		t.Fatalf("expected layered startup failure: %v", startupErr)
	}
	if cachedErr := instance.WaitStarted(context.Background()); cachedErr != startupErr {
		t.Fatalf("expected stable cached startup result: first=%v cached=%v", startupErr, cachedErr)
	}
	if runErr := waitForError(t, runResult); !errcode.IsCode[ErrorCode](runErr, ErrCodeRunFailed) {
		t.Fatalf("expected run failure: %v", runErr)
	}
}

// TestRunWithCanceledContextConsumesCycleWithoutListening 验证启动前已取消 context 不建立监听。
// 测试方法：关闭 Run context 后调用 Run，断言正常返回；第二次 Run 命中单周期状态错误。
func TestRunWithCanceledContextConsumesCycleWithoutListening(t *testing.T) {
	instance, err := New(Config{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := instance.Run(ctx); err != nil {
		t.Fatalf("run canceled server: %v", err)
	}
	if err := instance.Run(context.Background()); !errcode.IsCode[ErrorCode](err, ErrCodeStateInvalid) {
		t.Fatalf("expected consumed run cycle, got=%v", err)
	}
	if err := instance.WaitStarted(context.Background()); !errcode.IsCode[ErrorCode](err, ErrCodeStartupFailed) || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled startup result, got=%v", err)
	}
}

// TestShutdownBeforeRunConsumesCycle 验证 new 状态 Shutdown 是终结性幂等操作。
// 测试方法：先连续 Shutdown 两次，再调用 Run，断言停止成功且运行周期不可重启。
func TestShutdownBeforeRunConsumesCycle(t *testing.T) {
	instance, err := New(Config{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := instance.Shutdown(context.Background()); err != nil {
		t.Fatalf("first shutdown: %v", err)
	}
	if err := instance.Shutdown(context.Background()); err != nil {
		t.Fatalf("second shutdown: %v", err)
	}
	if err := instance.Run(context.Background()); !errcode.IsCode[ErrorCode](err, ErrCodeStateInvalid) {
		t.Fatalf("expected state error after shutdown, got=%v", err)
	}
	if err := instance.WaitStarted(context.Background()); !errcode.IsCode[ErrorCode](err, ErrCodeStartupFailed) {
		t.Fatalf("expected shutdown-before-ready result, got=%v", err)
	}
}

// TestRunCancellationGracefullyStopsServer 验证 Run context 取消触发共享优雅停止并返回 nil。
// 测试方法：使用标准 transport 启动真实监听，确认 running 后取消 context，等待 Run 正常收敛。
func TestRunCancellationGracefullyStopsServer(t *testing.T) {
	instance := newStandardTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() { runResult <- instance.Run(ctx) }()
	waitForRunning(t, instance)
	cancel()
	if err := waitForError(t, runResult); err != nil {
		t.Fatalf("run after cancellation: %v", err)
	}
}

// TestConcurrentRunIsRejected 验证只有首次 Run 可以进入底层 Hertz。
// 测试方法：首个 Run 到达 running 后并发调用第二次 Run，断言状态错误，再正常关闭首个周期。
func TestConcurrentRunIsRejected(t *testing.T) {
	instance := newStandardTestServer(t)
	runResult := make(chan error, 1)
	go func() { runResult <- instance.Run(context.Background()) }()
	waitForRunning(t, instance)
	if err := instance.Run(context.Background()); !errcode.IsCode[ErrorCode](err, ErrCodeStateInvalid) {
		t.Fatalf("expected concurrent run error, got=%v", err)
	}
	if err := instance.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown server: %v", err)
	}
	if err := waitForError(t, runResult); err != nil {
		t.Fatalf("first run result: %v", err)
	}
}

// TestConcurrentShutdownSharesOneResult 验证运行中的并发 Shutdown 共享单次底层停止结果。
// 测试方法：启动真实监听后并发八次 Shutdown，断言全部成功且 Run 最终正常返回。
func TestConcurrentShutdownSharesOneResult(t *testing.T) {
	instance := newStandardTestServer(t)
	runResult := make(chan error, 1)
	go func() { runResult <- instance.Run(context.Background()) }()
	waitForRunning(t, instance)

	const callerCount = 8
	results := make(chan error, callerCount)
	for index := 0; index < callerCount; index++ {
		go func() { results <- instance.Shutdown(context.Background()) }()
	}
	for index := 0; index < callerCount; index++ {
		if err := waitForError(t, results); err != nil {
			t.Fatalf("shutdown caller %d: %v", index, err)
		}
	}
	if err := waitForError(t, runResult); err != nil {
		t.Fatalf("run result: %v", err)
	}
}

// TestShutdownCallerDeadlineDoesNotPoisonSharedStop 验证单个短 deadline 只限制当前调用者等待。
// 测试方法：用阻塞 Handler 保持活动连接，首个 Shutdown 超时后释放请求，断言后续调用与 Run 仍成功。
func TestShutdownCallerDeadlineDoesNotPoisonSharedStop(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	instance := newStandardTestServer(t, func(router *route.RouterGroup) error {
		router.GET("/block", func(_ context.Context, ctx *app.RequestContext) {
			close(requestStarted)
			<-releaseRequest
			ctx.String(consts.StatusOK, "ok")
		})
		return nil
	})
	runResult := make(chan error, 1)
	go func() { runResult <- instance.Run(context.Background()) }()
	waitForRunning(t, instance)

	requestResult := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + instance.address + "/block")
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			err = response.Body.Close()
		}
		requestResult <- err
	}()
	waitForSignal(t, requestStarted, "request start")

	shortCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := instance.Shutdown(shortCtx); !errcode.IsCode[ErrorCode](err, ErrCodeShutdownFailed) ||
		!errcode.IsCode[ErrorCode](err, ErrCodeShutdownWaitInterrupted) ||
		!errors.Is(err, ErrShutdownWaitInterrupted) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected caller deadline error, got=%v", err)
	}

	sharedResult := make(chan error, 1)
	go func() { sharedResult <- instance.Shutdown(context.Background()) }()
	close(releaseRequest)
	if err := waitForError(t, sharedResult); err != nil {
		t.Fatalf("shared shutdown result: %v", err)
	}
	if err := waitForError(t, runResult); err != nil {
		t.Fatalf("run result: %v", err)
	}
	if err := waitForError(t, requestResult); err != nil {
		t.Fatalf("request result: %v", err)
	}
}

// TestRequiredHandlerCompletionWaitsBeyondHertzExitBudget 验证显式 required 合同不把 Hertz transport 返回误判为 Handler 完成。
// 测试方法：让真实 Handler 阻塞超过 ExitWaitTime，断言 Engine 已退出但 Shutdown/Run 仍等待；释放请求后再全部收敛。
func TestRequiredHandlerCompletionWaitsBeyondHertzExitBudget(t *testing.T) {
	const exitWaitTime = 30 * time.Millisecond
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	handlerReturned := make(chan struct{})
	instance := newStandardTestServerWithConfigAndOptions(
		t,
		Config{
			ExitWaitTime: exitWaitTime,
			Shutdown:     ShutdownConfig{HandlerCompletion: HandlerCompletionRequired},
		},
		[]Option{WithRoutes(func(router *route.RouterGroup) error {
			router.GET("/block", func(_ context.Context, ctx *app.RequestContext) {
				close(requestStarted)
				<-releaseRequest
				ctx.String(consts.StatusOK, "ok")
				close(handlerReturned)
			})
			return nil
		})},
	)
	if instance.requestGate == nil {
		t.Fatal("expected required contract to install request gate")
	}

	// 第一阶段：启动真实请求并进入阻塞 Handler，再触发共享关闭事务。
	runResult := make(chan error, 1)
	go func() { runResult <- instance.Run(context.Background()) }()
	waitForRunning(t, instance)
	requestResult := make(chan error, 1)
	go func() { requestResult <- getAndDiscard("http://" + instance.address + "/block") }()
	waitForSignal(t, requestStarted, "required request start")
	shutdownResult := make(chan error, 1)
	go func() { shutdownResult <- instance.Shutdown(context.Background()) }()

	// 第二阶段：等待超过 Hertz budget，证明 transport/Run 入口退出不再突破 Handler 屏障。
	waitForSignal(t, instance.engineDone, "required Hertz Run return")
	time.Sleep(3 * exitWaitTime)
	select {
	case <-handlerReturned:
		t.Fatal("blocked handler returned unexpectedly")
	default:
	}
	select {
	case err := <-shutdownResult:
		t.Fatalf("required shutdown returned before handler: %v", err)
	default:
	}
	select {
	case err := <-runResult:
		t.Fatalf("required run returned before handler: %v", err)
	default:
	}

	// 第三阶段：最后一个 Handler 返回后，request、Shutdown 和 Run 才允许依次收敛。
	close(releaseRequest)
	waitForSignal(t, handlerReturned, "required handler return")
	if err := waitForError(t, requestResult); err != nil {
		t.Fatalf("required request result: %v", err)
	}
	if err := waitForError(t, shutdownResult); err != nil {
		t.Fatalf("required shutdown result: %v", err)
	}
	if err := waitForError(t, runResult); err != nil {
		t.Fatalf("required run result: %v", err)
	}
}

// TestBestEffortHandlerCompletionReturnsAfterHertzExitBudget 验证 bestEffort 保留 Hertz 有界尽力关闭合同且不安装 gate。
// 测试方法：让真实 Handler 阻塞超过 ExitWaitTime，断言 Shutdown/Run 可先返回，再释放 Handler 完成测试清理。
func TestBestEffortHandlerCompletionReturnsAfterHertzExitBudget(t *testing.T) {
	const exitWaitTime = 30 * time.Millisecond
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	handlerReturned := make(chan struct{})
	instance := newStandardTestServerWithConfigAndOptions(
		t,
		Config{
			ExitWaitTime: exitWaitTime,
		},
		[]Option{WithRoutes(func(router *route.RouterGroup) error {
			router.GET("/block", func(_ context.Context, ctx *app.RequestContext) {
				close(requestStarted)
				<-releaseRequest
				ctx.String(consts.StatusOK, "ok")
				close(handlerReturned)
			})
			return nil
		})},
	)
	if instance.requestGate != nil {
		t.Fatal("expected bestEffort default to omit request gate")
	}

	// 第一阶段：保持 Handler 阻塞并触发有界 Hertz Shutdown。
	runResult := make(chan error, 1)
	go func() { runResult <- instance.Run(context.Background()) }()
	waitForRunning(t, instance)
	requestResult := make(chan error, 1)
	go func() { requestResult <- getAndDiscard("http://" + instance.address + "/block") }()
	waitForSignal(t, requestStarted, "best-effort request start")
	if err := instance.Shutdown(context.Background()); err != nil {
		t.Fatalf("best-effort shutdown result: %v", err)
	}
	if err := waitForError(t, runResult); err != nil {
		t.Fatalf("best-effort run result: %v", err)
	}
	select {
	case <-handlerReturned:
		t.Fatal("best-effort handler should still be blocked when Run returns")
	default:
	}

	// 第二阶段：显式释放仍在执行的 Handler，避免测试泄漏 goroutine 和连接。
	close(releaseRequest)
	waitForSignal(t, handlerReturned, "best-effort handler return")
	if err := waitForError(t, requestResult); err != nil {
		t.Fatalf("best-effort request result: %v", err)
	}
}

// TestRunReturnsCodedListenError 验证真实端口冲突保留 Hertz 监听错误 cause。
// 测试方法：占用一个本地地址后让标准 transport 再次绑定，断言 Run 返回稳定运行错误码。
func TestRunReturnsCodedListenError(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve address: %v", err)
	}
	defer occupied.Close()
	instance, err := New(
		Config{Address: occupied.Addr().String()},
		WithHertzOptions(server.WithTransport(standard.NewTransporter)),
	)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := instance.Run(context.Background()); !errcode.IsCode[ErrorCode](err, ErrCodeRunFailed) {
		t.Fatalf("expected coded listen error, got=%v", err)
	}
}

// TestRunConvertsTransportPanicToError 验证第三方 transport 运行期 panic 不突破 web.Server 返回合同。
// 测试方法：注入会 panic 的 transport，断言 Run 返回错误码并保留原始 sentinel。
func TestRunConvertsTransportPanicToError(t *testing.T) {
	root := errors.New("transport panic")
	instance, err := New(
		Config{},
		WithHertzOptions(server.WithTransport(func(_ *config.Options) network.Transporter {
			return &panicTransporter{cause: root}
		})),
	)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	err = instance.Run(context.Background())
	if !errcode.IsCode[ErrorCode](err, ErrCodeRunFailed) || !errors.Is(err, root) {
		t.Fatalf("expected coded panic error with cause, got=%v", err)
	}
}

// TestRunConvertsNonErrorTransportPanicToCodedError 验证非 error panic 同样不会突破 web.Server 返回合同。
// 测试方法：注入抛出字符串的 transport，断言 Run 返回稳定运行错误码和可定位的 panic 阶段。
func TestRunConvertsNonErrorTransportPanicToCodedError(t *testing.T) {
	instance, err := New(
		Config{},
		WithHertzOptions(server.WithTransport(func(_ *config.Options) network.Transporter {
			return &panicTransporter{cause: "transport panic"}
		})),
	)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	err = instance.Run(context.Background())
	if !errcode.IsCode[ErrorCode](err, ErrCodeRunFailed) || !strings.Contains(err.Error(), "stage=engine_run") {
		t.Fatalf("expected coded non-error panic, got=%v", err)
	}
}

// TestShutdownFailureIsSharedWithRun 验证底层停止失败不会被任一生命周期入口吞掉。
// 测试方法：注入关闭后返回 sentinel 的 transport，断言 Shutdown 与 Run 均返回停止错误码并保留 cause。
func TestShutdownFailureIsSharedWithRun(t *testing.T) {
	root := errors.New("shutdown failed")
	transporter := &shutdownFailureTransporter{stop: make(chan struct{}), cause: root}
	instance, err := New(
		Config{},
		WithHertzOptions(server.WithTransport(func(_ *config.Options) network.Transporter {
			return transporter
		})),
	)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	runResult := make(chan error, 1)
	go func() { runResult <- instance.Run(context.Background()) }()
	waitForRunning(t, instance)
	shutdownErr := instance.Shutdown(context.Background())
	if !errcode.IsCode[ErrorCode](shutdownErr, ErrCodeShutdownFailed) || !errors.Is(shutdownErr, root) {
		t.Fatalf("expected shutdown error with cause, got=%v", shutdownErr)
	}
	if errors.Is(shutdownErr, ErrShutdownWaitInterrupted) {
		t.Fatalf("stable shutdown failure must not carry caller wait marker: %v", shutdownErr)
	}
	runErr := waitForError(t, runResult)
	if !errcode.IsCode[ErrorCode](runErr, ErrCodeShutdownFailed) || !errors.Is(runErr, root) {
		t.Fatalf("expected run shutdown error with cause, got=%v", runErr)
	}
}

// TestHertzNotRunningErrorVersionLock 验证当前 Hertz 版本的未导出状态错误仍可被安全归一化。
// 测试方法：直接在未运行 Engine 上调用 Hertz Shutdown，以真实返回值约束适配层判断。
func TestHertzNotRunningErrorVersionLock(t *testing.T) {
	instance, err := New(Config{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := instance.engine.Shutdown(context.Background()); !isHertzNotRunningError(err) {
		t.Fatalf("unexpected Hertz not-running error: %v", err)
	}
}

// newStandardTestServer 构造使用预绑定本地 listener 的标准 transport Server。
func newStandardTestServer(t *testing.T, routes ...RouteFunc) *Server {
	t.Helper()
	options := make([]Option, 0, 1)
	if len(routes) > 0 {
		options = append(options, WithRoutes(routes...))
	}
	return newStandardTestServerWithOptions(t, options)
}

// newStandardTestServerWithOptions 为调用方扩展声明补齐确定性的本地 listener 与标准 transport。
func newStandardTestServerWithOptions(t *testing.T, options []Option) *Server {
	return newStandardTestServerWithConfigAndOptions(t, Config{ExitWaitTime: time.Second}, options)
}

// newStandardTestServerWithConfigAndOptions 使用指定配置补齐确定性的本地 listener 与标准 transport。
// 阶段角色：关闭合同集成测试入口，确保 typed Config 与真实 Hertz transport 同时生效。
func newStandardTestServerWithConfigAndOptions(t *testing.T, input Config, options []Option) *Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test address: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	native := WithHertzOptions(
		server.WithListener(listener),
		server.WithTransport(standard.NewTransporter),
	)
	allOptions := append([]Option{native}, options...)
	instance, err := New(input, allOptions...)
	if err != nil {
		t.Fatalf("new standard server: %v", err)
	}
	return instance
}

// getAndDiscard 发起真实 GET 并完整消费响应，返回请求或关闭失败。
// 阶段角色：生命周期集成测试客户端入口，确保 keep-alive 请求完成态可观测。
func getAndDiscard(url string) error {
	response, err := http.Get(url)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(io.Discard, response.Body)
	return errors.Join(copyErr, response.Body.Close())
}

// waitForRunning 等待 Hertz 发布真实 running 状态，超时即终止测试。
func waitForRunning(t *testing.T, instance *Server) {
	t.Helper()
	deadline := time.NewTimer(lifecycleTestTimeout)
	ticker := time.NewTicker(time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("server did not reach running state")
		case <-ticker.C:
			if instance.engine.IsRunning() {
				return
			}
		}
	}
}

// waitForError 等待异步 error 结果，避免失败路径永久阻塞测试进程。
func waitForError(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(lifecycleTestTimeout):
		t.Fatal("timed out waiting for error result")
		return nil
	}
}

// waitForSignal 等待测试阶段信号，超时消息携带阶段名称便于定位。
func waitForSignal(t *testing.T, signal <-chan struct{}, stage string) {
	t.Helper()
	select {
	case <-signal:
		return
	case <-time.After(lifecycleTestTimeout):
		t.Fatalf("timed out waiting for %s", stage)
	}
}
