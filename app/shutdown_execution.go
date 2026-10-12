package app

import (
	"context"
	"errors"
	"sync"
)

// errRunnerCompleted 标记 Shutdown 由 Runner 结果触发，不作为外部错误返回。
var errRunnerCompleted = errors.New("runner completed")

// commandRunnerResult 保存 Runner 的正常返回或 panic 结果。
// 阶段角色：Runner 在独立 goroutine 执行时，把业务结果带回生命周期协调器。
type commandRunnerResult struct {
	// Err 是 Runner 正常返回的业务错误。
	Err error
	// Panic 是 Runner panic 的原始 payload。
	Panic any
	// Panicked 区分 nil payload panic 与没有 panic。
	Panicked bool
}

// executeManagedRunner 统一协调 Runner、退出触发源和应用 Shutdown Group。
// 阶段角色：BeforeRunHook 成功后启动业务，等待 Runner 与 Shutdown Hooks 完全收敛。
func (this *App) executeManagedRunner(
	parent context.Context,
	runtime Runtime,
	command CommandContext,
	runner CommandRunner,
	disableShutdownHooks bool,
) error {
	markExecutionCoordinatorStarted(parent)
	runCtx, cancelRun := context.WithCancelCause(parent)
	defer cancelRun(errRunnerCompleted)

	shutdownDone := make(chan error, 1)
	var shutdownOnce sync.Once
	var triggerCause error
	startShutdown := func(cause error) {
		if cause == nil {
			cause = context.Canceled
		}
		shutdownOnce.Do(func() {
			triggerCause = cause
			// Shutdown 一旦开始便恢复默认信号行为；第二次信号由操作系统直接处理。
			restoreExecutionSignals(parent)
			cancelRun(cause)
			go func() {
				if disableShutdownHooks {
					shutdownDone <- nil
					return
				}
				shutdownDone <- this.runShutdownHooks(parent, runtime, command)
			}()
		})
	}

	// 宿主/信号取消必须能在阻塞型 Runner 返回前启动 Hooks。
	stopTriggerWatch := make(chan struct{})
	go func() {
		select {
		case <-parent.Done():
			startShutdown(context.Cause(parent))
		case <-stopTriggerWatch:
		}
	}()

	// Runner 保持在命令调用 goroutine；已到达的 trigger 会阻止它启动。
	result := commandRunnerResult{}
	if parent.Err() == nil {
		result = executeCommandRunner(runCtx, runtime, command, runner)
	}
	if parent.Err() != nil {
		startShutdown(context.Cause(parent))
	} else {
		startShutdown(errRunnerCompleted)
	}
	close(stopTriggerWatch)

	// Runner 与全部 Hooks 都已收敛，调用方随后才可进入 Runtime Close。
	shutdownErr := <-shutdownDone

	var triggerErr error
	if triggerCause != errRunnerCompleted && executionControlFrom(parent) == nil {
		triggerErr = stageError("shutdown_trigger", triggerCause)
	}
	runnerErr := result.Err
	if triggerCause != errRunnerCompleted && isCancellationOnly(runnerErr, triggerCause) {
		runnerErr = nil
	}
	err := errors.Join(triggerErr, stageError("run_business", runnerErr), shutdownErr)
	if result.Panicked {
		panic(result.Panic)
	}
	return err
}

// executeCommandRunner 在命令调用 goroutine 执行 Runner，并暂存 panic 供 Shutdown 完成后恢复。
func executeCommandRunner(
	ctx context.Context,
	runtime Runtime,
	command CommandContext,
	runner CommandRunner,
) (result commandRunnerResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result.Panic = recovered
			result.Panicked = true
		}
	}()
	result.Err = runner(ctx, runtime, command)
	return result
}

// runShutdownHooks 并发执行应用级主动工作负载 Hooks，并按声明顺序聚合错误。
// 阶段角色：run context 取消后、Runtime Close 前完成 graceful→force→wait。
func (this *App) runShutdownHooks(parent context.Context, runtime Runtime, command CommandContext) error {
	if len(this.shutdown.Hooks) == 0 {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), this.shutdown.GracefulTimeout)
	defer cancel()

	results := make([]error, len(this.shutdown.Hooks))
	done := make(chan int, len(this.shutdown.Hooks))
	for index, item := range this.shutdown.Hooks {
		go func(index int, item namedShutdownHook) {
			results[index] = executeShutdownHook(shutdownCtx, runtime, command, item)
			done <- index
		}(index, item)
	}
	for range this.shutdown.Hooks {
		<-done
	}
	return errors.Join(results...)
}

// executeShutdownHook 为单个 Hook 增加稳定名称和 panic 隔离，避免一个组件跳过其他关闭分支。
func executeShutdownHook(ctx context.Context, runtime Runtime, command CommandContext, item namedShutdownHook) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.Join(err, errorForAppInternal(
				messagef("op=app.command result=failed stage=shutdown_hook reason=hook_panic name=%s payload=%v", item.Name, recovered),
			))
		}
	}()
	if hookErr := item.Hook(ctx, runtime, command); hookErr != nil {
		return errorf("op=app.command result=failed stage=shutdown_hook reason=hook_failed name=%s cause=%w", item.Name, hookErr)
	}
	return nil
}

// isCancellationOnly 判断错误链是否只由指定取消原因或标准 context 取消错误组成。
// 阶段角色：信号/宿主取消后忽略 Runner 的协作式退出错误，同时保留 joined 业务错误。
func isCancellationOnly(err error, cause error) bool {
	if err == nil {
		return true
	}
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		children := multi.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !isCancellationOnly(child, cause) {
				return false
			}
		}
		return true
	}
	if single := errors.Unwrap(err); single != nil {
		return isCancellationOnly(single, cause)
	}
	if cause != nil && errors.Is(err, cause) {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	return errors.Is(cause, context.DeadlineExceeded) && errors.Is(err, context.DeadlineExceeded)
}
