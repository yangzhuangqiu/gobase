package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yangzhuangqiu/gobase/ioc"
)

// runCommand 执行 root、一级子命令和任意深度子命令的统一生命周期。
func (this *App) runCommand(
	session *executionSession,
	plan executionPlan,
	cmd *cobra.Command,
	args []string,
) error {
	if printed, err := this.handlePrintVersion(cmd); err != nil {
		return stageError("print_version", err)
	} else if printed {
		return nil
	}
	if err := errors.Join(this.inputErr, plan.InputErr); err != nil {
		return stageError("validate_app_setup", err)
	}
	if listed, err := this.handleListBootstrapKeys(cmd); err != nil {
		return stageError("parse_list_bootstrap_keys_flag", err)
	} else if listed {
		return nil
	}
	workloads, workloadErr := resolveModuleWorkloads(plan.Modules)
	if workloadErr != nil {
		return stageError("resolve_module_workloads", workloadErr)
	}
	if len(workloads) == 0 {
		// 纯分组节点展示自己的下一层命令；真正叶子节点必须声明至少一个 Workload。
		if cmd.HasAvailableSubCommands() {
			return stageError("render_help", cmd.Help())
		}
		return errorForAppWorkloadRequired(messagef("stage=validate_workloads command=%s reason=workload is required", cmd.CommandPath()))
	}
	return stageError("execute_command", this.executeCommand(session, plan, workloads, cmd, args))
}

// handlePrintVersion 在显式设置 --version 时输出版本并短路命令生命周期。
// 阶段角色：所有命令共用的最早终止条件，禁止进入 Runtime、hooks 和 Workload。
func (this *App) handlePrintVersion(cmd *cobra.Command) (bool, error) {
	printVersion, err := cmd.Flags().GetBool(versionFlagName)
	if err != nil {
		return false, errorf("stage=parse_version_flag reason=%w", err)
	}
	if !printVersion {
		return false, nil
	}
	if _, err = fmt.Fprintln(cmd.OutOrStdout(), this.version); err != nil {
		return true, errorf("stage=write_version reason=%w", err)
	}
	return true, nil
}

// executeCommand 执行参数规则、Runtime、hooks、Workload 与关闭的统一生命周期。
func (this *App) executeCommand(
	session *executionSession,
	plan executionPlan,
	workloads []Workload,
	cmd *cobra.Command,
	args []string,
) (err error) {
	ctx := cmd.Context()

	// 阶段一：只有选中节点的 FlagBinder 参与本次补全和校验，祖先业务 Binder 不泄漏作用域。
	if err = this.applyFlagRules(plan.FlagBinder); err != nil {
		return stageError("apply_command_flags", err)
	}
	commandContext := buildCommandContext(cmd, args)
	shutdownPolicy := this.shutdown.executionPolicy(plan.ShutdownBudget)
	supervisor := newWorkloadSupervisor(workloads)

	// 阶段二：WithoutRuntime 不创建 Runtime，但全局与选中节点 Before Hook 仍以 nil Runtime 执行。
	if plan.RuntimeRequirement == runtimeRequirementDisabled {
		if beforeErr := this.runExecutionBeforeRunHooks(ctx, nil, commandContext, plan.BeforeRunHooks); beforeErr != nil {
			return beforeErr
		}
		return this.executeManagedWorkloads(session, ctx, nil, commandContext, supervisor, plan.ShutdownHooks, shutdownPolicy)
	}
	input, inputErr := buildRuntimeInput(cmd)
	if inputErr != nil {
		return stageError("build_runtime_input", inputErr)
	}
	input.BeanDefinitions = append([]ioc.Definition{}, plan.BeanDefinitions...)
	input.Modules = append([]Module{}, plan.Modules...)
	runtime, buildErr := this.runtimeApplication.BuildWithInput(ctx, input)
	if buildErr != nil {
		return stageError("build_runtime", buildErr)
	}
	defer func() {
		// Runtime Close 使用独立 deadline，不复用已取消的 run/shutdown context。
		closeCtx, cancelClose := context.WithTimeout(context.WithoutCancel(ctx), shutdownPolicy.RuntimeCloseTimeout)
		defer cancelClose()
		err = errors.Join(err, stageError("close_runtime", runtime.Close(closeCtx)))
	}()
	resolvedPolicy, policyErr := this.resolveExecutionShutdownPolicy(runtime, plan.ShutdownBudget)
	if policyErr != nil {
		return stageError("resolve_shutdown_policy", policyErr)
	}
	shutdownPolicy = resolvedPolicy
	if beforeErr := this.runExecutionBeforeRunHooks(ctx, runtime, commandContext, plan.BeforeRunHooks); beforeErr != nil {
		return beforeErr
	}
	return this.executeManagedWorkloads(session, ctx, runtime, commandContext, supervisor, plan.ShutdownHooks, shutdownPolicy)
}

// runExecutionBeforeRunHooks 先执行 App 全局运行前 Hook，再执行选中命令节点局部运行前 Hook。
// 阶段角色：Runtime 决策完成后、Workload 启动前落实作用域隔离；全局阶段失败时不进入局部准备。
func (this *App) runExecutionBeforeRunHooks(
	ctx context.Context,
	runtime Runtime,
	command CommandContext,
	commandHooks []BeforeRunHook,
) error {
	if err := runBeforeRunHooks(ctx, runtime, command, "global", this.globalBeforeRunHooks); err != nil {
		return stageError("global_before_run_hooks", err)
	}
	if err := runBeforeRunHooks(ctx, runtime, command, "command", commandHooks); err != nil {
		return stageError("before_run_hooks", err)
	}
	return nil
}
