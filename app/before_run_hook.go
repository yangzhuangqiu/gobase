package app

import (
	"context"
	"errors"
)

// BeforeRunHook 定义 Runtime Build 成功后、Runner 启动前的准备契约。
//
// 典型调用场景：调用方需要基于 Runtime 和当前命令记录启动审计，或完成不产生长期工作负载的可回滚准备。
//
// 语义：
// 1. Hook 不能启动 Server、Consumer、Scheduler 等长期主动工作负载；
// 2. 返回错误前必须撤销自身创建的临时副作用；
// 3. 任一 Hook 失败后不启动 Runner 和 Shutdown Hooks，但已构建 Runtime 仍会关闭。
type BeforeRunHook func(ctx context.Context, runtime Runtime, command CommandContext) error

// runBeforeHooks 按声明顺序执行准备 Hooks，并聚合错误。
// 阶段角色：Runtime Build 后、Runner 启动前完成不产生长期工作负载的准备。
func (this *App) runBeforeHooks(ctx context.Context, runtime Runtime, command CommandContext) error {
	commandName := ""
	if command != nil {
		commandName = command.Name()
	}
	var errs []error
	for index, hook := range this.beforeHooks {
		if hook == nil {
			continue
		}
		if err := hook(ctx, runtime, command); err != nil {
			errs = append(errs, errorf("stage=before_hook index=%d command=%s: %w", index, commandName, err))
		}
	}
	return errors.Join(errs...)
}
