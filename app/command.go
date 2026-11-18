package app

import (
	"context"
	"errors"
	"strings"
)

// ArgumentValidator 定义命令位置参数校验契约，不泄漏底层 CLI 类型。
type ArgumentValidator func(args []string) error

// RunFunc 定义一次性 Workload 的业务入口。
// 典型调用场景：迁移、生成、校验等任务通过 Runner 适配为 Workload。
type RunFunc func(ctx context.Context, runtime Runtime, command CommandContext) error

// RuntimeMode 定义命令节点是否需要构建完整应用运行时。
type RuntimeMode uint8

const (
	// runtimeModeUnspecified 表示子命令未显式声明 RuntimeMode，归并时继承最近祖先。
	runtimeModeUnspecified RuntimeMode = iota
	// RuntimeModeEnabled 表示启用并构建 config/log/ioc 完整运行时；root 默认使用该模式。
	RuntimeModeEnabled
	// RuntimeModeDisabled 表示不构建应用运行时，也不执行依赖 Runtime 的 BeforeRunHooks。
	RuntimeModeDisabled
)

// Command 是通过 NewCommand 构建的不可变命令节点声明。
// 说明：命令节点只描述局部语义；继承和覆盖在选中命令时由 executionPlan 冻结。
type Command struct {
	// configuration 保存节点私有声明，避免调用方绕过 Option 归并规则直接修改字段。
	configuration commandConfiguration
}

// commandConfiguration 保存 root 或子命令节点的局部声明。
type commandConfiguration struct {
	// Use 是命令用法声明；root 默认 app，子命令必填。
	Use string
	// Short 是 help 列表中的简短说明。
	Short string
	// Long 是命令详细说明。
	Long string
	// Example 是命令示例文本。
	Example string
	// Aliases 是当前节点的命令别名集合。
	Aliases []string
	// ValidateArgs 校验当前节点的位置参数。
	ValidateArgs ArgumentValidator
	// RuntimeMode 是当前节点显式声明；内部 unspecified 零值表示沿命令路径继承。
	RuntimeMode RuntimeMode
	// FlagBinder 只声明、完成并校验当前节点自己的业务 flags，不参与继承。
	FlagBinder FlagBinder
	// BeforeHooks 是当前节点追加的业务前置切面，执行时按 root 到叶子归并。
	BeforeHooks []BeforeRunHook
	// Workloads 是当前节点被选中时独占执行的主动工作负载，不参与继承。
	Workloads []Workload
	// WorkloadNames 用于在当前节点声明阶段拒绝重复名称。
	WorkloadNames map[string]struct{}
	// ShutdownHooks 是当前节点声明的高级关闭逃生动作。
	ShutdownHooks []namedShutdownHook
	// ShutdownHookNames 用于在当前节点声明阶段拒绝重复名称。
	ShutdownHookNames map[string]struct{}
	// DisableInheritedShutdownHooks 表示归并到此节点时清空祖先 Hook，随后仍可追加本节点 Hook。
	DisableInheritedShutdownHooks bool
	// ShutdownBudget 是当前节点按字段覆盖的关闭预算。
	ShutdownBudget shutdownBudgetDeclaration
	// Commands 是当前节点的直接子命令，允许递归构建命令树。
	Commands []Command
	// InputErr 聚合当前节点 Option 声明错误，在执行前统一失败。
	InputErr error
}

// NewCommand 使用与 root 一致的 Option 风格构建一个子命令节点。
//
// 参数：
// - use：命令用法声明，例如 doctor [target]；必须非空。
// - options：只接受 CommandOption；NodeOption 可同时用于 root 和子命令。
func NewCommand(use string, options ...CommandOption) Command {
	configuration := newCommandConfiguration(strings.TrimSpace(use), runtimeModeUnspecified)
	if configuration.Use == "" {
		configuration.recordInputError(errorForAppInternal(messagef("op=app.command result=failed stage=new_command reason=use_empty")))
	}
	for _, option := range options {
		if option != nil {
			option.applyCommand(&configuration)
		}
	}
	return Command{configuration: configuration}
}

// newCommandConfiguration 创建节点独占的声明容器。
// 阶段角色：App root 与 NewCommand 的统一构建基线。
func newCommandConfiguration(use string, runtimeMode RuntimeMode) commandConfiguration {
	return commandConfiguration{
		Use:               use,
		RuntimeMode:       runtimeMode,
		WorkloadNames:     make(map[string]struct{}),
		ShutdownHookNames: make(map[string]struct{}),
	}
}

// recordInputError 聚合当前节点构建错误，避免 Option 顺序隐藏多个声明问题。
func (this *commandConfiguration) recordInputError(err error) {
	this.InputErr = errors.Join(this.InputErr, err)
}

// addCommands 向当前节点追加直接子命令。
// 阶段角色：WithCommands 的递归命令树装配边界。
func (this *commandConfiguration) addCommands(commands ...Command) {
	if len(commands) == 0 {
		this.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_commands reason=commands_empty")))
		return
	}
	for index, command := range commands {
		if strings.TrimSpace(command.configuration.Use) == "" {
			this.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_commands reason=command_use_empty index=%d", index)))
		}
		this.Commands = append(this.Commands, command)
	}
}
