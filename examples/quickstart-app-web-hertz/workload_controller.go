package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// workload 是应用监督器所需的最小长期工作负载合同。
type workload interface {
	// Run 启动并阻塞到工作负载退出。
	Run(context.Context) error
	// Shutdown 停止工作负载并等待资源收敛。
	Shutdown(context.Context) error
}

// namedWorkload 为运行与关闭错误提供稳定低基数名称。
type namedWorkload struct {
	// name 是日志和错误中的应用内稳定名称。
	name string
	// workload 是业务或诊断 Server 的生命周期实现。
	workload workload
}

// workloadControllerState 表示统一监督器的单运行周期阶段。
type workloadControllerState uint8

const (
	// workloadControllerStateNew 表示尚未启动且允许 Run 或终结性 Shutdown。
	workloadControllerStateNew workloadControllerState = iota
	// workloadControllerStateRunning 表示全部成员已启动监督 goroutine。
	workloadControllerStateRunning
	// workloadControllerStateStopping 表示任一成员退出或显式关闭已触发整体收敛。
	workloadControllerStateStopping
	// workloadControllerStateStopped 表示关闭事务及成员 Run 已完全返回。
	workloadControllerStateStopped
)

// workloadController 同时监督业务 Web 和可选诊断 Server，任一退出都会取消其他成员。
type workloadController struct {
	// workloads 是按声明顺序保存的工作负载，顺序也用于确定错误聚合顺序。
	workloads []namedWorkload
	// mu 保护状态、运行结果和关闭事务发布。
	mu sync.Mutex
	// state 是当前监督周期阶段。
	state workloadControllerState
	// cancel 取消全部成员共享的运行 context。
	cancel context.CancelFunc
	// runStarted 区分真正启动过成员和 Shutdown-before-Run 的终结状态。
	runStarted bool
	// runErrors 保存每个成员 Run 的最终结果。
	runErrors []error
	// primaryIndex 是使 Runner 返回的首个成员；Shutdown 不重复包装该错误。
	primaryIndex int
	// allDone 在全部成员 Run 返回后关闭。
	allDone chan struct{}
	// shutdownStarted 保证只启动一个统一关闭事务。
	shutdownStarted bool
	// shutdownDone 在统一关闭事务完成后关闭。
	shutdownDone chan struct{}
	// shutdownErr 保存首次统一关闭结果。
	shutdownErr error
}

// newWorkloadController 校验并冻结工作负载声明，不启动 goroutine。
func newWorkloadController(workloads ...namedWorkload) (*workloadController, error) {
	if len(workloads) == 0 {
		return nil, fmt.Errorf("op=quickstart.workloads result=failed stage=construct reason=workload_required")
	}
	// 第一阶段：校验稳定名称和非 nil 生命周期对象，拒绝不可定位的成员错误。
	seen := make(map[string]struct{}, len(workloads))
	resolved := make([]namedWorkload, len(workloads))
	for index, item := range workloads {
		if item.name == "" || item.workload == nil {
			return nil, fmt.Errorf("op=quickstart.workloads result=failed stage=construct reason=name_and_workload_required index=%d", index)
		}
		if _, exists := seen[item.name]; exists {
			return nil, fmt.Errorf("op=quickstart.workloads result=failed stage=construct reason=name_duplicate name=%s", item.name)
		}
		seen[item.name] = struct{}{}
		resolved[index] = item
	}
	// 第二阶段：只冻结声明并创建同步原语，不在构造期启动成员或 goroutine。
	return &workloadController{
		workloads:    resolved,
		state:        workloadControllerStateNew,
		primaryIndex: -1,
		allDone:      make(chan struct{}),
		shutdownDone: make(chan struct{}),
	}, nil
}

// Run 并发启动全部成员；任一成员异常或提前退出都会取消其余成员并返回给 app Runner。
func (this *workloadController) Run(ctx context.Context) error {
	if this == nil || ctx == nil {
		return fmt.Errorf("op=quickstart.workloads result=failed stage=run_precheck reason=controller_and_context_required")
	}
	this.mu.Lock()
	if this.state != workloadControllerStateNew {
		this.mu.Unlock()
		return fmt.Errorf("op=quickstart.workloads result=failed stage=run_precheck reason=run_not_allowed")
	}
	if ctx.Err() != nil {
		this.state = workloadControllerStateStopped
		close(this.allDone)
		this.mu.Unlock()
		return nil
	}
	workCtx, cancel := context.WithCancel(ctx)
	this.cancel = cancel
	this.runStarted = true
	this.runErrors = make([]error, len(this.workloads))
	this.state = workloadControllerStateRunning
	this.mu.Unlock()

	// 每个成员只写自己的结果槽；buffered channel 保证 Runner 返回后其余成员仍能完成发布。
	results := make(chan int, len(this.workloads))
	var wait sync.WaitGroup
	wait.Add(len(this.workloads))
	for index, item := range this.workloads {
		go func(index int, item namedWorkload) {
			defer wait.Done()
			err := item.workload.Run(workCtx)
			this.mu.Lock()
			this.runErrors[index] = err
			this.mu.Unlock()
			results <- index
		}(index, item)
	}
	go func() {
		wait.Wait()
		close(this.allDone)
	}()

	// 首个退出结果决定 Runner 语义；取消兄弟成员后由 Shutdown Hook 完成最终等待和错误聚合。
	primaryIndex := <-results
	this.mu.Lock()
	this.primaryIndex = primaryIndex
	primaryErr := this.runErrors[primaryIndex]
	this.state = workloadControllerStateStopping
	this.mu.Unlock()
	cancel()
	if ctx.Err() != nil {
		return nil
	}
	if primaryErr == nil {
		return fmt.Errorf("op=quickstart.workloads result=failed stage=supervise reason=workload_exited_unexpectedly name=%s", this.workloads[primaryIndex].name)
	}
	return fmt.Errorf("op=quickstart.workloads result=failed stage=supervise reason=workload_failed name=%s: %w", this.workloads[primaryIndex].name, primaryErr)
}

// Shutdown 启动一次统一关闭事务，并在 context 到期触发成员 Force 后仍等待全部 Run 收敛。
func (this *workloadController) Shutdown(ctx context.Context) error {
	if this == nil {
		return nil
	}
	if ctx == nil {
		return fmt.Errorf("op=quickstart.workloads result=failed stage=shutdown_precheck reason=context_required")
	}
	this.mu.Lock()
	if !this.shutdownStarted {
		this.shutdownStarted = true
		if this.state == workloadControllerStateNew {
			// 终结性 Shutdown 必须在同一临界区阻止随后 Run 穿越并启动成员。
			this.state = workloadControllerStateStopping
		}
		if this.cancel != nil {
			this.cancel()
		}
		go this.driveShutdown(ctx)
	}
	done := this.shutdownDone
	this.mu.Unlock()
	// App Hook 的强合同要求返回后不再访问 Runtime，因此 deadline 只结束 graceful，不能跳过最终等待。
	<-done
	this.mu.Lock()
	err := this.shutdownErr
	this.mu.Unlock()
	return err
}

// driveShutdown 并发停止全部成员、等待 Run 收敛，并按声明顺序聚合非首发运行错误。
func (this *workloadController) driveShutdown(ctx context.Context) {
	shutdownErrors := make([]error, len(this.workloads))
	var wait sync.WaitGroup
	wait.Add(len(this.workloads))
	for index, item := range this.workloads {
		go func(index int, item namedWorkload) {
			defer wait.Done()
			if err := item.workload.Shutdown(ctx); err != nil {
				shutdownErrors[index] = fmt.Errorf("op=quickstart.workloads result=failed stage=shutdown_member reason=shutdown_failed name=%s: %w", item.name, err)
			}
		}(index, item)
	}
	wait.Wait()

	this.mu.Lock()
	started := this.runStarted
	this.mu.Unlock()
	if started {
		// 成员实现负责 graceful→force；Controller 必须等到其 Run 真正返回后才能让 Runtime Close。
		<-this.allDone
	}

	this.mu.Lock()
	secondaryRunErrors := make([]error, len(this.runErrors))
	for index, err := range this.runErrors {
		if index == this.primaryIndex || err == nil || errors.Is(err, context.Canceled) {
			continue
		}
		secondaryRunErrors[index] = fmt.Errorf("op=quickstart.workloads result=failed stage=wait_member reason=run_failed name=%s: %w", this.workloads[index].name, err)
	}
	this.shutdownErr = errors.Join(append(shutdownErrors, secondaryRunErrors...)...)
	this.state = workloadControllerStateStopped
	close(this.shutdownDone)
	this.mu.Unlock()
}
