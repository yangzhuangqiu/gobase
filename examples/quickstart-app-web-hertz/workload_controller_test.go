package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeWorkload 为监督器测试提供可编排的 Run/Shutdown 行为与调用次数。
type fakeWorkload struct {
	// run 是单次运行行为。
	run func(context.Context) error
	// shutdown 是单次停止行为。
	shutdown func(context.Context) error
	// shutdownCalls 记录统一事务是否只调用一次成员 Shutdown。
	shutdownCalls atomic.Int32
}

// Run 执行测试声明的运行行为。
func (this *fakeWorkload) Run(ctx context.Context) error {
	return this.run(ctx)
}

// Shutdown 记录调用并执行测试声明的停止行为。
func (this *fakeWorkload) Shutdown(ctx context.Context) error {
	this.shutdownCalls.Add(1)
	return this.shutdown(ctx)
}

// TestWorkloadControllerCancelsSiblingsAndJoinsShutdownErrors 验证任一失败会取消兄弟并保留关闭及次级运行错误。
// 测试方法：让诊断成员立即失败、业务成员在取消后返回另一错误，再执行 Shutdown 并用 errors.Is 检查完整错误链。
func TestWorkloadControllerCancelsSiblingsAndJoinsShutdownErrors(t *testing.T) {
	// 第一阶段：构造一个立即失败成员和一个收到取消后失败的兄弟成员。
	primaryErr := errors.New("diagnostics listen failed")
	secondaryErr := errors.New("business stop failed")
	shutdownErr := errors.New("diagnostics shutdown failed")
	business := &fakeWorkload{
		run: func(ctx context.Context) error {
			<-ctx.Done()
			return secondaryErr
		},
		shutdown: func(context.Context) error { return nil },
	}
	diagnostics := &fakeWorkload{
		run:      func(context.Context) error { return primaryErr },
		shutdown: func(context.Context) error { return shutdownErr },
	}
	controller, err := newWorkloadController(
		namedWorkload{name: "business", workload: business},
		namedWorkload{name: "diagnostics", workload: diagnostics},
	)
	if err != nil {
		t.Fatalf("new controller: %v", err)
	}

	// 第二阶段：Runner 保留首发错误；Hook 结果保留次级 Run 和 Shutdown 错误。
	runErr := controller.Run(context.Background())
	if !errors.Is(runErr, primaryErr) || !strings.Contains(runErr.Error(), "name=diagnostics") {
		t.Fatalf("unexpected primary run error: %v", runErr)
	}
	joined := controller.Shutdown(context.Background())
	if !errors.Is(joined, secondaryErr) || !errors.Is(joined, shutdownErr) {
		t.Fatalf("expected joined secondary/shutdown errors, got=%v", joined)
	}
}

// TestWorkloadControllerParentCancellationIsNormal 验证宿主取消不是成员异常退出。
// 测试方法：两个成员等待共享 context，取消父 context 后断言 Run 和统一 Shutdown 均成功。
func TestWorkloadControllerParentCancellationIsNormal(t *testing.T) {
	newWaitingWorkload := func() *fakeWorkload {
		return &fakeWorkload{
			run: func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			},
			shutdown: func(context.Context) error { return nil },
		}
	}
	controller, err := newWorkloadController(
		namedWorkload{name: "business", workload: newWaitingWorkload()},
		namedWorkload{name: "diagnostics", workload: newWaitingWorkload()},
	)
	if err != nil {
		t.Fatalf("new controller: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := controller.Run(ctx); err != nil {
		t.Fatalf("run canceled controller: %v", err)
	}
	if err := controller.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown canceled controller: %v", err)
	}
}

// TestWorkloadControllerConcurrentShutdownSharesTransaction 验证并发关闭只执行一次成员停止事务。
// 测试方法：启动等待取消的成员，并发八个 Shutdown 调用，断言结果成功、成员各关闭一次且 Run 已返回。
func TestWorkloadControllerConcurrentShutdownSharesTransaction(t *testing.T) {
	// 第一阶段：启动两个只响应共享取消的成员。
	first := &fakeWorkload{run: waitForWorkloadCancellation, shutdown: successfulWorkloadShutdown}
	second := &fakeWorkload{run: waitForWorkloadCancellation, shutdown: successfulWorkloadShutdown}
	controller, err := newWorkloadController(
		namedWorkload{name: "first", workload: first},
		namedWorkload{name: "second", workload: second},
	)
	if err != nil {
		t.Fatalf("new controller: %v", err)
	}
	runResult := make(chan error, 1)
	go func() { runResult <- controller.Run(context.Background()) }()

	// 第二阶段：并发关闭共享单次事务，不因竞争重复调用成员 Shutdown。
	const callers = 8
	results := make(chan error, callers)
	for index := 0; index < callers; index++ {
		go func() { results <- controller.Shutdown(context.Background()) }()
	}
	for index := 0; index < callers; index++ {
		if err := <-results; err != nil {
			t.Fatalf("shutdown caller %d: %v", index, err)
		}
	}
	_ = <-runResult
	if first.shutdownCalls.Load() != 1 || second.shutdownCalls.Load() != 1 {
		t.Fatalf("expected one member shutdown: first=%d second=%d", first.shutdownCalls.Load(), second.shutdownCalls.Load())
	}
}

// waitForWorkloadCancellation 模拟正常响应监督 context 的长期工作负载。
func waitForWorkloadCancellation(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// successfulWorkloadShutdown 模拟无需额外资源回收的成功关闭。
func successfulWorkloadShutdown(context.Context) error {
	return nil
}
