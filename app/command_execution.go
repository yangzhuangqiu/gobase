package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// runRootCommand 执行 root 命令完整生命周期。
func (this *App) runRootCommand(cmd *cobra.Command, args []string) error {
	if printed, err := this.handlePrintVersion(cmd); err != nil {
		return stageError("print_version", err)
	} else if printed {
		return nil
	}
	if this.inputErr != nil {
		return stageError("validate_app_setup", this.inputErr)
	}
	if listed, err := this.handleListBootstrapKeys(cmd); err != nil {
		return stageError("parse_list_bootstrap_keys_flag", err)
	} else if listed {
		return nil
	}
	if this.runner == nil {
		// command-only 应用执行 root 时展示命令清单，不要求无意义的占位 Runner。
		if cmd.HasAvailableSubCommands() {
			return stageError("render_help", cmd.Help())
		}
		return errorForAppRunnerRequired(messagef("stage=validate_runner reason=runner is required"))
	}
	rootRunner := func(ctx context.Context, runtime Runtime, _ CommandContext) error {
		return this.runner(ctx, runtime)
	}
	return stageError("execute_command_root", this.executeCommand(cmd, args, this.rootFlagBinder, RuntimeModeEnabled, false, rootRunner))
}

// runSubCommand 执行子命令完整生命周期。
func (this *App) runSubCommand(sub Command, cmd *cobra.Command, args []string) error {
	if printed, err := this.handlePrintVersion(cmd); err != nil {
		return stageError("print_version", err)
	} else if printed {
		return nil
	}
	if this.inputErr != nil {
		return stageError("validate_app_setup", this.inputErr)
	}
	if listed, err := this.handleListBootstrapKeys(cmd); err != nil {
		return stageError("parse_list_bootstrap_keys_flag", err)
	} else if listed {
		return nil
	}
	if sub.Runner == nil {
		return errorForAppCommandRunnerRequired(messagef("stage=validate_command command=" + sub.Use + " reason=runner is required"))
	}
	return stageError("execute_command_sub", this.executeCommand(
		cmd,
		args,
		sub.FlagBinder,
		sub.RuntimeMode,
		sub.DisableShutdownHooks,
		sub.Runner,
	))
}

// handlePrintVersion 在显式设置 --version 时输出版本并短路命令生命周期。
// 阶段角色：root/subcommand 共用的最早终止条件，禁止进入 Runtime、hooks 和 Runner。
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

// executeCommand 执行参数规则、Runtime、hooks、业务与关闭的统一生命周期。
func (this *App) executeCommand(
	cmd *cobra.Command,
	args []string,
	commandFlagBinder FlagBinder,
	runtimeMode RuntimeMode,
	disableShutdownHooks bool,
	runner CommandRunner,
) (err error) {
	ctx := cmd.Context()

	// 阶段一：先完成纯命令参数补全与校验，失败时不创建运行时资源。
	if err = this.applyFlagRules(this.rootFlagBinder); err != nil {
		return stageError("apply_root_flags", err)
	}
	if commandFlagBinder != this.rootFlagBinder {
		if err = this.applyFlagRules(commandFlagBinder); err != nil {
			return stageError("apply_command_flags", err)
		}
	}
	commandContext := buildCommandContext(cmd, args)

	// 阶段二：Disabled 不创建 Runtime/执行 Before Hooks，但仍进入统一 Runner 与 Shutdown 协调。
	if runtimeMode == RuntimeModeDisabled {
		return this.executeManagedRunner(ctx, nil, commandContext, runner, disableShutdownHooks)
	}
	if runtimeMode != RuntimeModeEnabled {
		return errorForAppInternal(messagef("op=app.command result=failed stage=resolve_runtime_mode reason=unsupported_mode mode=%d", runtimeMode))
	}
	input, inputErr := buildRuntimeInput(cmd)
	if inputErr != nil {
		return stageError("build_runtime_input", inputErr)
	}
	runtime, buildErr := this.runtimeApplication.BuildWithInput(ctx, input)
	if buildErr != nil {
		return stageError("build_runtime", buildErr)
	}
	defer func() {
		// Runtime Close 使用独立 deadline，不复用已取消的 run/shutdown context。
		closeCtx, cancelClose := context.WithTimeout(context.WithoutCancel(ctx), this.shutdown.RuntimeCloseTimeout)
		defer cancelClose()
		closeErr := stageError("close_runtime", runtime.Close(closeCtx))
		err = errors.Join(err, closeErr)
	}()
	if beforeErr := this.runBeforeHooks(ctx, runtime, commandContext); beforeErr != nil {
		return stageError("before_hooks", beforeErr)
	}
	return this.executeManagedRunner(ctx, runtime, commandContext, runner, disableShutdownHooks)
}
