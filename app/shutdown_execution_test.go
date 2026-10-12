package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testProcessSignalSource 用可控 context 模拟 App.Run 的首次进程信号。
type testProcessSignalSource struct {
	// Context 在测试触发“信号”时取消。
	Context context.Context
	// StopCount 记录恢复默认信号行为的实际次数。
	StopCount atomic.Int32
}

// Notify 返回测试控制的 signal context，并提供可重复调用但可观测的底层 stop。
func (this *testProcessSignalSource) Notify(context.Context, ...os.Signal) (context.Context, func()) {
	return this.Context, func() { this.StopCount.Add(1) }
}

// TestUseCaseHostCancellationRunsShutdownBeforeRunnerConverges 验证宿主取消会并行启动 Shutdown Hook 与阻塞 Runner 收敛。
//
// 测试方法：
// 1. Runner 阻塞等待只能由 Shutdown Hook 释放的通道；
// 2. 取消 ExecuteContext 的宿主 context；
// 3. 断言 Hook 被调用、Runner 随后退出，且返回错误保留宿主 cause。
func TestUseCaseHostCancellationRunsShutdownBeforeRunnerConverges(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	hookCalled := make(chan struct{})
	cause := errors.New("host stopped")
	application := New(
		WithRunner(func(context.Context, Runtime) error {
			close(started)
			<-release
			return context.Canceled
		}),
		WithShutdown(WithShutdownHook("server", func(context.Context, Runtime, CommandContext) error {
			close(hookCalled)
			close(release)
			return nil
		})),
	)
	result := make(chan error, 1)
	go func() { result <- application.ExecuteContext(ctx, nil) }()
	<-started
	cancel(cause)

	select {
	case <-hookCalled:
	case <-time.After(time.Second):
		t.Fatal("shutdown hook was not started after host cancellation")
	}
	select {
	case err := <-result:
		if !errors.Is(err, cause) {
			t.Fatalf("expected host cancellation cause, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runner and shutdown hook did not converge")
	}
}

// TestUseCaseCommandCanDisableInheritedShutdownHooks 验证子命令可只关闭应用 Shutdown Group，而不影响 Runner。
//
// 测试方法：
// 1. 应用注册一个会记录调用的 Shutdown Hook；
// 2. 执行 RuntimeModeDisabled 且 DisableShutdownHooks=true 的子命令；
// 3. 断言 Runner 执行而 Hook 未执行。
func TestUseCaseCommandCanDisableInheritedShutdownHooks(t *testing.T) {
	hookCalled := false
	runnerCalled := false
	application := New(
		WithShutdown(WithShutdownHook("server", func(context.Context, Runtime, CommandContext) error {
			hookCalled = true
			return nil
		})),
		WithCommands(Command{
			Use:                  "doctor",
			RuntimeMode:          RuntimeModeDisabled,
			DisableShutdownHooks: true,
			Runner: func(context.Context, Runtime, CommandContext) error {
				runnerCalled = true
				return nil
			},
		}),
	)
	if err := application.ExecuteContext(context.Background(), []string{"doctor"}); err != nil {
		t.Fatalf("execute disabled hook command failed: %v", err)
	}
	if !runnerCalled || hookCalled {
		t.Fatalf("unexpected lifecycle result: runner=%t hook=%t", runnerCalled, hookCalled)
	}
}

// TestUseCaseRunTreatsSignalOnlyShutdownAsSuccess 验证进程信号只触发正常关闭，并立即恢复默认信号行为。
//
// 测试方法：
// 1. 用可控 signal context 替换标准库信号源并启动 App.Run；
// 2. Runner 启动后取消 signal context，Runner 协作返回 context.Canceled；
// 3. 断言 Run 返回 nil、Shutdown Hook 执行且底层 stop 只调用一次。
func TestUseCaseRunTreatsSignalOnlyShutdownAsSuccess(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	os.Args = []string{"demo"}

	signalCtx, signalCancel := context.WithCancel(context.Background())
	signalSource := &testProcessSignalSource{Context: signalCtx}
	started := make(chan struct{})
	hookCalled := false
	application := New(
		WithRunner(func(ctx context.Context, _ Runtime) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}),
		WithShutdown(WithShutdownHook("server", func(context.Context, Runtime, CommandContext) error {
			hookCalled = true
			return nil
		})),
	)
	application.signalSource = signalSource
	go func() {
		<-started
		signalCancel()
	}()

	if err := application.Run(); err != nil {
		t.Fatalf("signal-only shutdown should succeed, got: %v", err)
	}
	if !hookCalled || signalSource.StopCount.Load() != 1 {
		t.Fatalf("unexpected signal shutdown result: hook=%t stop_count=%d", hookCalled, signalSource.StopCount.Load())
	}
}

// TestUseCaseRunPreservesShutdownHookCancellationError 验证进程信号不会吞掉 Hook 自己返回的 context 错误。
//
// 测试方法：模拟信号取消并让 Runner 协作退出，Shutdown Hook 返回 context.Canceled，断言 Run 仍返回 Hook 阶段错误。
func TestUseCaseRunPreservesShutdownHookCancellationError(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	os.Args = []string{"demo"}

	signalCtx, signalCancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	application := New(
		WithRunner(func(ctx context.Context, _ Runtime) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}),
		WithShutdown(WithShutdownHook("server", func(context.Context, Runtime, CommandContext) error {
			return context.Canceled
		})),
	)
	application.signalSource = &testProcessSignalSource{Context: signalCtx}
	go func() {
		<-started
		signalCancel()
	}()

	err := application.Run()
	if err == nil || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "stage=shutdown_hook") {
		t.Fatalf("expected shutdown hook cancellation error, got: %v", err)
	}
}

// TestUseCaseDuplicateShutdownDeclarationsFailFast 验证冲突的 Shutdown 配置不会采用声明顺序静默覆盖。
//
// 测试方法：重复声明 graceful timeout 与 Hook 名称，执行合法 Runner，并断言 setup 阶段失败。
func TestUseCaseDuplicateShutdownDeclarationsFailFast(t *testing.T) {
	noopHook := func(context.Context, Runtime, CommandContext) error { return nil }
	application := New(
		WithRunner(func(context.Context, Runtime) error { return nil }),
		WithShutdown(
			WithShutdownGracefulTimeout(time.Second),
			WithShutdownGracefulTimeout(2*time.Second),
			WithShutdownHook("server", noopHook),
			WithShutdownHook("server", noopHook),
		),
	)
	if err := application.ExecuteContext(context.Background(), nil); err == nil {
		t.Fatal("duplicate shutdown declarations must fail fast")
	}
}

// TestUseCaseShutdownHooksRunConcurrentlyAndAggregateErrors 验证 Hooks 没有隐式声明顺序且全部错误可分支。
//
// 测试方法：两个 Hook 分别发布已启动信号后等待另一个 Hook；若串行会超时，若并发则都返回预设错误并被聚合。
func TestUseCaseShutdownHooksRunConcurrentlyAndAggregateErrors(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	firstErr := errors.New("first shutdown failed")
	secondErr := errors.New("second shutdown failed")
	application := New(
		WithRunner(func(context.Context, Runtime) error { return nil }),
		WithShutdown(
			WithShutdownHook("first", func(context.Context, Runtime, CommandContext) error {
				close(firstStarted)
				<-secondStarted
				return firstErr
			}),
			WithShutdownHook("second", func(context.Context, Runtime, CommandContext) error {
				close(secondStarted)
				<-firstStarted
				return secondErr
			}),
		),
	)
	result := make(chan error, 1)
	go func() { result <- application.ExecuteContext(context.Background(), nil) }()

	select {
	case err := <-result:
		if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
			t.Fatalf("expected both shutdown errors, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown hooks did not run concurrently")
	}
}

// TestUseCaseCancellationDoesNotHideRunnerBusinessError 验证协作取消错误不会吞掉同链上的真实业务失败。
//
// 测试方法：宿主以自定义 cause 取消，Runner 返回 context.Canceled 与业务错误的 Join，断言两个业务来源均保留。
func TestUseCaseCancellationDoesNotHideRunnerBusinessError(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	started := make(chan struct{})
	hostCause := errors.New("host stopped")
	businessErr := errors.New("serve failed")
	application := New(WithRunner(func(ctx context.Context, _ Runtime) error {
		close(started)
		<-ctx.Done()
		return errors.Join(ctx.Err(), businessErr)
	}))
	result := make(chan error, 1)
	go func() { result <- application.ExecuteContext(ctx, nil) }()
	<-started
	cancel(hostCause)

	select {
	case err := <-result:
		if !errors.Is(err, hostCause) || !errors.Is(err, businessErr) {
			t.Fatalf("expected host and runner business errors, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled runner did not converge")
	}
}
