package app

import "context"

const (
	// runnerWorkloadName 是一次性 root 任务的固定诊断名称；单个 App 只能声明一个该包装器。
	runnerWorkloadName = "runner"
)

// runnerWorkload 把自行完成的一次性函数适配为统一 Workload 合同。
type runnerWorkload struct {
	// run 是 Runtime Build 完成后执行的一次性业务函数；返回即表示任务真实完成。
	run func(ctx context.Context, runtime Runtime) error
}

// Runner 把一次性 root 任务包装为名称固定为 runner 的 Workload。
//
// 典型调用场景：迁移、生成、校验等会自行完成的 root 任务通过 WithWorkloads(Runner(fn))
// 接入统一生命周期，不需要实现无意义的 Shutdown 方法。
//
// 参数：
// - run：Runtime Build 完成后执行的业务函数；nil 会由 WithWorkloads 作为声明错误拒绝。
//
// 返回：
// - Workload：Run 返回即触发统一 Resource Close，Shutdown 为无副作用操作。
func Runner(run func(ctx context.Context, runtime Runtime) error) Workload {
	if run == nil {
		return nil
	}
	return &runnerWorkload{run: run}
}

// Name 返回一次性 root 任务的固定低基数诊断名称。
func (this *runnerWorkload) Name() string {
	return runnerWorkloadName
}

// Run 执行一次性业务函数；函数返回即是该 Workload 的真实收敛点。
func (this *runnerWorkload) Run(ctx context.Context, runtime Runtime, _ CommandContext) error {
	return this.run(ctx, runtime)
}

// Shutdown 对已自行完成或响应 run context 的一次性任务执行无副作用关闭。
func (this *runnerWorkload) Shutdown(context.Context, Runtime, CommandContext) error {
	return nil
}
