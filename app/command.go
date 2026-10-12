package app

import "context"

// Runner 定义 root 命令的极简业务入口。
//
// 典型调用场景：
// 1. 由 app.Run/app.Main 在 Runtime 构建完成后调用；
// 2. 业务通过 Runtime 获取配置、日志和 IOC 能力，无需感知 Cobra 或 Runtime 构建过程。
type Runner func(ctx context.Context, runtime Runtime) error

// ArgumentValidator 定义命令位置参数校验契约，不泄漏底层 CLI 类型。
type ArgumentValidator func(args []string) error

// CommandRunner 定义子命令业务入口。
type CommandRunner func(ctx context.Context, runtime Runtime, command CommandContext) error

// RuntimeMode 定义命令是否需要构建完整应用运行时。
type RuntimeMode uint8

const (
	// RuntimeModeEnabled 表示启用并构建 config/log/ioc 完整运行时；这是默认模式。
	RuntimeModeEnabled RuntimeMode = iota
	// RuntimeModeDisabled 表示命令不构建应用运行时，也不执行依赖 Runtime 的 BeforeRunHooks。
	// 应用级 Shutdown Hooks 默认仍执行，并收到 nil Runtime。
	RuntimeModeDisabled
)

// Command 描述一个接入应用生命周期的业务子命令。
type Command struct {
	// Use 是命令用法声明，例如 doctor [target]。
	Use string
	// Short 是 help 列表中的简短说明。
	Short string
	// Long 是命令详细说明，可为空。
	Long string
	// Example 是命令示例文本，可为空。
	Example string
	// Aliases 是命令别名集合。
	Aliases []string
	// ValidateArgs 校验位置参数；为空表示不额外校验。
	ValidateArgs ArgumentValidator
	// RuntimeMode 决定是否构建 app-runtime；默认 RuntimeModeEnabled。
	RuntimeMode RuntimeMode
	// FlagBinder 声明并绑定当前命令的业务 flags。
	FlagBinder FlagBinder
	// DisableShutdownHooks 表示当前命令不执行应用级 Shutdown Hooks；默认 false。
	// Runtime Build 成功时，该开关不影响 Runtime Close。
	DisableShutdownHooks bool
	// Runner 是命令业务入口；叶子命令必填。
	Runner CommandRunner
}
