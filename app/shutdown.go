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
	// GracefulTimeout 是所有 Shutdown Hooks 共享的 graceful 截止时间。
	GracefulTimeout time.Duration
	// RuntimeCloseTimeout 是 Runtime Close 使用的独立协作式截止时间。
	RuntimeCloseTimeout time.Duration
	// Hooks 是按声明顺序保存、执行时并发调用的主动工作负载 Hook。
	Hooks []namedShutdownHook
	// HookNames 用于在 App 声明阶段拒绝重复组件名称。
	HookNames map[string]struct{}
	// SignalsSet 标记调用方是否显式声明过信号集合。
	SignalsSet bool
	// GracefulTimeoutSet 标记调用方是否显式声明过 graceful timeout。
	GracefulTimeoutSet bool
	// RuntimeCloseTimeoutSet 标记调用方是否显式声明过 Runtime Close timeout。
	RuntimeCloseTimeoutSet bool
}

// newDefaultShutdownConfiguration 创建每个 App 独占的默认 Shutdown 声明。
func newDefaultShutdownConfiguration() shutdownConfiguration {
	return shutdownConfiguration{
		Signals:             defaultShutdownSignals(),
		GracefulTimeout:     defaultShutdownGracefulTimeout,
		RuntimeCloseTimeout: defaultShutdownRuntimeCloseTimeout,
		HookNames:           make(map[string]struct{}),
	}
}
