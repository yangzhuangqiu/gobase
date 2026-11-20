package app

import (
	"context"
	"errors"
	"strings"

	"github.com/yangzhuangqiu/gobase/ioc"
)

// ArgumentValidator 定义命令位置参数校验契约，不泄漏底层 CLI 类型。
type ArgumentValidator func(args []string) error

// RunFunc 定义一次性 Workload 的业务入口。
// 典型调用场景：迁移、生成、校验等任务通过 Runner 适配为 workload-only Module。
type RunFunc func(ctx context.Context, runtime Runtime, command CommandContext) error

// runtimeRequirement 表示命令节点对完整应用 Runtime 的内部声明状态。
type runtimeRequirement uint8

const (
	// runtimeRequirementUnspecified 表示节点未声明 Runtime 需求，归并时继承最近祖先。
	runtimeRequirementUnspecified runtimeRequirement = iota
	// runtimeRequirementRequired 表示构建 config/log/ioc 完整运行时；root 默认使用该状态。
	runtimeRequirementRequired
	// runtimeRequirementDisabled 表示不构建 Runtime；BeforeRunHook、Workload 与 ShutdownHook 接收 nil Runtime。
	runtimeRequirementDisabled
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
	// RuntimeRequirement 是当前节点的内部声明；unspecified 表示沿命令路径继承。
	RuntimeRequirement runtimeRequirement
	// FlagBinder 只声明、完成并校验当前节点自己的业务 flags，不参与继承。
	FlagBinder FlagBinder
	// BeforeRunHooks 是仅在当前节点被选中时执行的局部业务运行前 Hook，不参与继承。
	BeforeRunHooks []BeforeRunHook
	// BeanDefinitions 是当前节点被选中时注册的完整 IOC Bean 声明，不参与继承。
	BeanDefinitions []ioc.Definition
	// Modules 是当前节点被选中时参与 Build 的局部模块，不参与继承。
	Modules []Module
	// ModuleNames 用于在当前节点声明阶段拒绝重复名称。
	ModuleNames map[string]struct{}
	// ShutdownHooks 是当前节点声明的高级关闭逃生动作。
	ShutdownHooks []namedShutdownHook
	// ShutdownHookNames 用于在当前节点声明阶段拒绝重复名称。
	ShutdownHookNames map[string]struct{}
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
	configuration := newCommandConfiguration(strings.TrimSpace(use), runtimeRequirementUnspecified)
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
func newCommandConfiguration(use string, requirement runtimeRequirement) commandConfiguration {
	return commandConfiguration{
		Use:                use,
		RuntimeRequirement: requirement,
		ModuleNames:        make(map[string]struct{}),
		ShutdownHookNames:  make(map[string]struct{}),
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
