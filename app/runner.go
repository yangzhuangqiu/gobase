package app

import (
	"context"

	"github.com/yangzhuangqiu/gobase/ioc"
)

const (
	// runnerModuleName 是一次性任务的固定模块与工作负载名称；单个命令节点只能声明一个该包装器。
	runnerModuleName = "runner"
)

// runnerModule 把自行完成的一次性函数适配为不声明 Bean、只贡献 Workload 的 Module。
type runnerModule struct {
	// run 是 Runtime Build 完成后执行的一次性业务函数；返回即表示任务真实完成。
	run RunFunc
}

var _ WorkloadModule = (*runnerModule)(nil)
var _ Workload = (*runnerModule)(nil)

// Runner 把一次性任务包装为名称固定为 runner 的 workload-only Module。
//
// 典型调用场景：迁移、生成、校验等会自行完成的命令通过 WithModules(Runner(fn))
// 接入统一生命周期，不需要注册 Bean 或实现有状态 Shutdown。
//
// 参数：
// - run：Runtime Build 完成后执行的业务函数；nil 返回 nil，由 WithModules 作为声明错误拒绝。
//
// 返回：
// - Module：不注册 Bean，只贡献一个同名 Workload；Run 返回即触发统一 Resource Close。
func Runner(run RunFunc) Module {
	if run == nil {
		return nil
	}
	return &runnerModule{run: run}
}

// Name 返回一次性任务的固定低基数 Module/Workload 诊断名称。
func (this *runnerModule) Name() string {
	return runnerModuleName
}

// Register 保持 workload-only 语义，不向 IOC 声明任何 Bean。
// 阶段角色：Runtime Build 的 Module 注册阶段无副作用通过。
func (this *runnerModule) Register(ioc.Registry) error { return nil }

// Workloads 返回由当前 Module 独占的一次性执行单元。
func (this *runnerModule) Workloads() []Workload { return []Workload{this} }

// Run 执行一次性业务函数；函数返回即是该 Workload 的真实收敛点。
func (this *runnerModule) Run(ctx context.Context, runtime Runtime, command CommandContext) error {
	return this.run(ctx, runtime, command)
}

// Shutdown 对已自行完成或响应 run context 的一次性任务执行无副作用关闭。
func (this *runnerModule) Shutdown(context.Context, Runtime, CommandContext) error {
	return nil
}
