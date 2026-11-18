package app

import (
	"os"
	"time"
)

const (
	// defaultShutdownGracefulTimeout 是主动工作负载 graceful 阶段的默认截止时间。
	defaultShutdownGracefulTimeout = 15 * time.Second
	// defaultShutdownRuntimeCloseTimeout 是向 Runtime Close 传播的默认协作式截止时间。
	defaultShutdownRuntimeCloseTimeout = 10 * time.Second
)

// namedShutdownHook 保存声明名称和对应 Hook，用于稳定错误与观测上下文。
type namedShutdownHook struct {
	// Name 是应用内唯一的低基数组件名称。
	Name string
	// Hook 是停止单个主动工作负载或工作负载组的函数。
	Hook ShutdownHook
}

// shutdownConfiguration 保存归一化后的应用级 Shutdown 声明。
type shutdownConfiguration struct {
	// Signals 是 Run/Main 监听的进程退出信号集合。
	Signals []os.Signal
	// WorkloadGracefulTimeout 是所有 Workload 与高级 ShutdownHook 共享的 graceful 截止时间。
	WorkloadGracefulTimeout time.Duration
	// RuntimeCloseTimeout 是 Runtime Close 使用的独立协作式截止时间。
	RuntimeCloseTimeout time.Duration
	// SignalsSet 标记调用方是否显式声明过信号集合。
	SignalsSet bool
	// WorkloadGracefulTimeoutSet 标记调用方是否显式声明过 workload graceful timeout。
	WorkloadGracefulTimeoutSet bool
	// RuntimeCloseTimeoutSet 标记调用方是否显式声明过 Runtime Close timeout。
	RuntimeCloseTimeoutSet bool
}

// shutdownBudgetDeclaration 保存命令节点按字段声明的关闭预算。
type shutdownBudgetDeclaration struct {
	// WorkloadGracefulTimeout 是主动 Workload 与高级 Hook 的 graceful 预算。
	WorkloadGracefulTimeout time.Duration
	// RuntimeCloseTimeout 是 Runtime/Resource Close 的独立预算。
	RuntimeCloseTimeout time.Duration
	// WorkloadGracefulTimeoutSet 表示当前节点显式覆盖了该字段。
	WorkloadGracefulTimeoutSet bool
	// RuntimeCloseTimeoutSet 表示当前节点显式覆盖了该字段。
	RuntimeCloseTimeoutSet bool
}

// newDefaultShutdownConfiguration 创建每个 App 独占的默认 Shutdown 声明。
func newDefaultShutdownConfiguration() shutdownConfiguration {
	return shutdownConfiguration{
		Signals:                 defaultShutdownSignals(),
		WorkloadGracefulTimeout: defaultShutdownGracefulTimeout,
		RuntimeCloseTimeout:     defaultShutdownRuntimeCloseTimeout,
	}
}
