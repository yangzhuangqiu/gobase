package app

import "github.com/spf13/cobra"

// CommandContext 定义命令执行时对外暴露的最小上下文契约。
//
// 典型调用场景：
// 1. runner 和 hooks 识别当前命令；
// 2. 业务读取位置参数快照，但不依赖 Cobra。
type CommandContext interface {
	// Name 返回当前命令名（例如 doctor）。
	Name() string
	// Path 返回命令完整路径（例如 admin doctor）。
	Path() string
	// Args 返回位置参数快照（不含命令名与 flags）。
	Args() []string
}

// commandContextSnapshot 是 CommandContext 的内部实现。
type commandContextSnapshot struct {
	name string
	path string
	args []string
}

// Name 返回命令名。
func (this commandContextSnapshot) Name() string {
	return this.name
}

// Path 返回命令完整路径。
func (this commandContextSnapshot) Path() string {
	return this.path
}

// Args 返回参数快照副本，避免调用方修改内部状态。
func (this commandContextSnapshot) Args() []string {
	return append([]string{}, this.args...)
}

// buildCommandContext 从 cobra 命令构建稳定上下文快照。
func buildCommandContext(cmd *cobra.Command, args []string) CommandContext {
	commandName := ""
	commandPath := ""
	if cmd != nil {
		commandName = cmd.Name()
		commandPath = cmd.CommandPath()
	}
	copiedArgs := append([]string{}, args...)
	return commandContextSnapshot{
		name: commandName,
		path: commandPath,
		args: copiedArgs,
	}
}
