package app

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// workloadRunState 保存单个 Workload 的稳定名称、运行完成点和结果。
type workloadRunState struct {
	// Name 是用于错误定位的声明期稳定名称。
	Name string
	// Workload 是本次执行借用的主动工作负载。
	Workload Workload
	// Done 在 Run 已完全返回后关闭。
	Done chan struct{}
	// Result 保存 Run 的 error 或 panic 结果；读取前必须等待 Done。
	Result commandRunnerResult
}

// workloadSupervisor 监督一次 root 执行中的全部 Workload。
// 阶段角色：把“任一返回触发全组关闭”和“全部 Run 收敛后才能释放 Resource”固化在 app 内部。
type workloadSupervisor struct {
	// workloads 是按声明顺序冻结的工作负载快照。
	workloads []Workload
	// stateMu 串行化 Run 建立与 Shutdown 取快照，封住启动/停止竞态。
	stateMu sync.Mutex
	// runStarted 标记本次监督器是否已经启动 Workload。
	runStarted bool
	// shutdownStarted 标记关闭动作是否已经接管本次监督器。
	shutdownStarted bool
	// states 保存本次实际启动的运行状态。
	states []*workloadRunState
	// runContext 保存 app 取消原因，用于忽略非触发 Workload 的纯协作式退出错误。
	runContext context.Context
	// triggerIndex 是首个完成并触发全组关闭的 Workload 下标。
	triggerIndex int
}

// newWorkloadSupervisor 创建单次命令执行独占的工作负载监督器。
func newWorkloadSupervisor(workloads []Workload) *workloadSupervisor {
	return &workloadSupervisor{
		workloads:    append([]Workload{}, workloads...),
		triggerIndex: -1,
	}
}

// Run 并发启动全部 Workload，并在任意一个 Run 返回时把结果交给 app 关闭协调器。
// 阶段角色：root CommandRunner 的语义实现；不在此方法中直接释放任何 Resource。
func (this *workloadSupervisor) Run(ctx context.Context, runtime Runtime, command CommandContext) error {
	this.stateMu.Lock()
	if this.shutdownStarted {
		this.stateMu.Unlock()
		return nil
	}
	this.runStarted = true
	this.runContext = ctx
	this.states = make([]*workloadRunState, len(this.workloads))
	firstDone := make(chan int, 1)
	var firstOnce sync.Once
	entered := make([]chan struct{}, len(this.workloads))

	// 第一阶段：持锁发布全部 Run 调用入口，使 Shutdown 只能观察完整启动快照。
	for index, workload := range this.workloads {
		state := &workloadRunState{
			Name:     strings.TrimSpace(workload.Name()),
			Workload: workload,
			Done:     make(chan struct{}),
		}
		this.states[index] = state
		entered[index] = make(chan struct{})
		go func(index int, state *workloadRunState, entered chan struct{}) {
			close(entered)
			state.Result = executeWorkloadRun(ctx, runtime, command, state)
			firstOnce.Do(func() {
				this.triggerIndex = index
				firstDone <- index
			})
			close(state.Done)
		}(index, state, entered[index])
	}
	for _, started := range entered {
		<-started
	}
	this.stateMu.Unlock()

	// 第二阶段：首个完成点只负责触发统一 Shutdown；其余结果在 Shutdown 中稳定聚合。
	trigger := <-firstDone
	result := this.states[trigger].Result
	if result.Panicked {
		panic(result.Panic)
	}
	return result.Err
}

// Shutdown 并发请求全部 Workload 停止，并等待每个 Shutdown 与 Run 都完全返回。
// 阶段角色：Runtime Close 前的强收敛屏障；context 只表达 graceful 预算，不允许超时后遗弃 Run。
func (this *workloadSupervisor) Shutdown(ctx context.Context, runtime Runtime, command CommandContext) error {
	this.stateMu.Lock()
	this.shutdownStarted = true
	if !this.runStarted {
		this.stateMu.Unlock()
		return nil
	}
	states := append([]*workloadRunState{}, this.states...)
	this.stateMu.Unlock()

	// 第一阶段：并发发出语义化停止请求，避免声明顺序把多个 graceful 窗口串行放大。
	shutdownResults := make([]error, len(states))
	shutdownDone := make(chan int, len(states))
	for index, state := range states {
		go func(index int, state *workloadRunState) {
			shutdownResults[index] = executeWorkloadShutdown(ctx, runtime, command, state)
			shutdownDone <- index
		}(index, state)
	}
	for range states {
		<-shutdownDone
	}

	// 第二阶段：即使 graceful 预算已到，也必须等待 Run 真正退出后才能释放被借用的 Resource。
	for _, state := range states {
		<-state.Done
	}
	runResults := make([]error, len(states))
	for index, state := range states {
		if index == this.triggerIndex {
			continue
		}
		runResults[index] = workloadRunError(state, context.Cause(this.runContext))
	}
	return errors.Join(errors.Join(shutdownResults...), errors.Join(runResults...))
}

// executeWorkloadRun 调用单个 Workload Run，并暂存 panic 供监督器在收敛后处理。
func executeWorkloadRun(
	ctx context.Context,
	runtime Runtime,
	command CommandContext,
	state *workloadRunState,
) (result commandRunnerResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result.Panic = recovered
			result.Panicked = true
		}
	}()
	result.Err = state.Workload.Run(ctx, runtime, command)
	return result
}

// executeWorkloadShutdown 为单个语义化关闭动作补充名称、阶段和 panic 隔离。
func executeWorkloadShutdown(
	ctx context.Context,
	runtime Runtime,
	command CommandContext,
	state *workloadRunState,
) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.Join(err, errorForAppInternal(messagef(
				"op=app.workload result=failed stage=shutdown reason=panic name=%s payload=%v", state.Name, recovered,
			)))
		}
	}()
	if shutdownErr := state.Workload.Shutdown(ctx, runtime, command); shutdownErr != nil {
		return errorf("op=app.workload result=failed stage=shutdown reason=workload_failed name=%s cause=%w", state.Name, shutdownErr)
	}
	return nil
}

// workloadRunError 忽略非触发 Workload 的纯协作式取消，并把真实失败转换为可搜索错误。
func workloadRunError(state *workloadRunState, shutdownCause error) error {
	if state.Result.Panicked {
		return errorForAppInternal(messagef(
			"op=app.workload result=failed stage=run reason=panic name=%s payload=%v", state.Name, state.Result.Panic,
		))
	}
	if isCancellationOnly(state.Result.Err, shutdownCause) {
		return nil
	}
	if state.Result.Err != nil {
		return errorf("op=app.workload result=failed stage=run reason=workload_failed name=%s cause=%w", state.Name, state.Result.Err)
	}
	return nil
}
