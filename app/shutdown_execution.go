package app

import (
	"context"
	"errors"
	"sync"
)

// errWorkloadsCompleted 标记 Shutdown 由 Workload 结果触发，不作为外部错误返回。
var errWorkloadsCompleted = errors.New("workloads completed")

// workloadCallResult 保存 Workload 调用的正常返回或 panic 结果。
type workloadCallResult struct {
	// Err 是 Workload 正常返回的业务错误。
	Err error
	// Panic 是 Workload panic 的原始 payload。
	Panic any
	// Panicked 区分 nil payload panic 与没有 panic。
	Panicked bool
}

// executeManagedWorkloads 统一协调 Workload、退出触发源和应用 Shutdown actions。
// 阶段角色：BeforeRunHook 成功后启动业务，等待 Workload 与高级 Hooks 完全收敛。
func (this *App) executeManagedWorkloads(
	session *executionSession,
	parent context.Context,
	runtime Runtime,
	command CommandContext,
	workloads *workloadSupervisor,
	shutdownHooks []namedShutdownHook,
	shutdownPolicy executionShutdownPolicy,
) error {
	session.MarkCoordinatorStarted()
	runCtx, cancelRun := context.WithCancelCause(parent)
	defer cancelRun(errWorkloadsCompleted)

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
			session.RestoreSignals()
			cancelRun(cause)
			go func() {
				shutdownDone <- runShutdownActions(parent, runtime, command, workloads, shutdownHooks, shutdownPolicy)
			}()
		})
	}

	// 宿主/信号取消必须能在阻塞型 Workload 返回前启动 Shutdown。
	stopTriggerWatch := make(chan struct{})
	go func() {
		select {
		case <-parent.Done():
			startShutdown(context.Cause(parent))
		case <-stopTriggerWatch:
		}
	}()

	// Workload 监督器保持在命令调用 goroutine；已到达的 trigger 会阻止它启动。
	result := workloadCallResult{}
	if parent.Err() == nil {
		result = executeWorkloadSupervisor(runCtx, runtime, command, workloads)
	}
	if parent.Err() != nil {
		startShutdown(context.Cause(parent))
	} else {
		startShutdown(errWorkloadsCompleted)
	}
	close(stopTriggerWatch)

	// Workload 与全部 Hooks 都已收敛，调用方随后才可进入 Runtime Close。
	shutdownErr := <-shutdownDone
	var triggerErr error
	if triggerCause != errWorkloadsCompleted && !session.IsSignalCause(triggerCause) {
		triggerErr = stageError("shutdown_trigger", triggerCause)
	}
	workloadErr := result.Err
	if triggerCause != errWorkloadsCompleted && isCancellationOnly(workloadErr, triggerCause) {
		workloadErr = nil
	}
	err := errors.Join(triggerErr, stageError("run_business", workloadErr), shutdownErr)
	if result.Panicked {
		panic(result.Panic)
	}
	return err
}

// executeWorkloadSupervisor 执行监督器，并暂存 panic 供 Shutdown 完成后恢复。
func executeWorkloadSupervisor(
	ctx context.Context,
	runtime Runtime,
	command CommandContext,
	workloads *workloadSupervisor,
) (result workloadCallResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result.Panic = recovered
			result.Panicked = true
		}
	}()
	result.Err = workloads.Run(ctx, runtime, command)
	return result
}

// runShutdownActions 并发执行语义化 Workload 收敛与高级 Shutdown Hooks。
// 阶段角色：run context 取消后、Runtime Close 前建立“主动工作已停止”的强屏障。
func runShutdownActions(
	parent context.Context,
	runtime Runtime,
	command CommandContext,
	workloads *workloadSupervisor,
	shutdownHooks []namedShutdownHook,
	shutdownPolicy executionShutdownPolicy,
) error {
	actionCount := len(shutdownHooks) + 1
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), shutdownPolicy.WorkloadGracefulTimeout)
	defer cancel()

	results := make([]error, actionCount)
	done := make(chan int, actionCount)
	go func() {
		results[0] = workloads.Shutdown(shutdownCtx, runtime, command)
		done <- 0
	}()
	for index, item := range shutdownHooks {
		go func(index int, item namedShutdownHook) {
			resultIndex := index + 1
			results[resultIndex] = executeShutdownHook(shutdownCtx, runtime, command, item)
			done <- resultIndex
		}(index, item)
	}
	for index := 0; index < actionCount; index++ {
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
// 阶段角色：信号/宿主取消后忽略 Workload 的协作式退出错误，同时保留 joined 业务错误。
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
