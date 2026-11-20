package app

import (
	"context"
	"errors"
)

// BeforeRunHook 定义 Runtime 决策完成后、Workload.Run 启动前的准备契约。
//
// 典型调用场景：调用方需要基于当前命令记录启动审计、执行权限校验，或完成不产生长期工作负载的可回滚准备。
//
// 语义：
// 1. WithGlobalBeforeRunHook 注册的 Hook 对当前 App 的每次实际命令执行生效；
// 2. WithBeforeRunHook 注册的节点 Hook 只对选中节点生效；
// 3. WithoutRuntime 命令仍执行 Hook，但 runtime 参数为 nil；
// 4. Hook 不能启动 Server、Consumer、Scheduler 等长期主动工作负载；
// 5. 返回错误前必须撤销自身创建的临时副作用；
// 6. 任一 Hook 阶段失败后不启动 Workload 和 Shutdown Hooks，但已构建 Runtime 仍会关闭。
type BeforeRunHook func(ctx context.Context, runtime Runtime, command CommandContext) error

// runBeforeRunHooks 按声明顺序执行同一作用域的运行前 Hooks，并聚合错误。
// 阶段角色：Runtime 决策完成后、Workload.Run 启动前完成不产生长期工作负载的准备。
func runBeforeRunHooks(
	ctx context.Context,
	runtime Runtime,
	command CommandContext,
	scope string,
	hooks []BeforeRunHook,
) error {
	commandName := ""
	if command != nil {
		commandName = command.Name()
	}
	// 同一作用域的 Hook 没有依赖顺序短路语义；完整执行可返回全部声明错误。
	var errs []error
	for index, hook := range hooks {
		if hook == nil {
			continue
		}
		if err := hook(ctx, runtime, command); err != nil {
			errs = append(errs, errorf(
				"op=app.before_run_hook result=failed scope=%s index=%d command=%s reason=%w",
				scope,
				index,
				commandName,
				err,
			))
		}
	}
	return errors.Join(errs...)
}
