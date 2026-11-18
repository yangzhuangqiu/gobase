package app

import (
	"os"
	"time"
)

// ShutdownPolicy 定义进程退出到资源释放之间的统一关闭预算。
//
// 语义：duration 零值允许 ConfigManager 启动快照补齐，仍缺失时使用内建默认值；
// 负 duration 属于声明错误；Signals 不读取 ConfigManager，非 nil 值替换平台默认信号。
type ShutdownPolicy struct {
	// Signals 是 Run 监听的进程退出信号；nil 使用平台默认集合，空非 nil 集合非法。
	Signals []os.Signal
	// WorkloadGracefulTimeout 是全部 Workload 与高级 ShutdownHook 共享的 graceful 预算。
	WorkloadGracefulTimeout time.Duration
	// RuntimeCloseTimeout 是 Workload 全部收敛后传给 Runtime/Resource Close 的独立协作式预算。
	RuntimeCloseTimeout time.Duration
}

// ShutdownBudget 定义单个命令节点可按字段覆盖的关闭时间预算。
// 语义：零值表示不覆盖，继续继承祖先节点或 App/配置/内建默认值；负值非法。
type ShutdownBudget struct {
	// WorkloadGracefulTimeout 是全部 Workload 与高级 ShutdownHook 共享的 graceful 预算。
	WorkloadGracefulTimeout time.Duration
	// RuntimeCloseTimeout 是 Workload 收敛后 Runtime/Resource Close 的独立预算。
	RuntimeCloseTimeout time.Duration
}

// DefaultShutdownPolicy 返回当前平台可独立修改的默认关闭策略快照。
func DefaultShutdownPolicy() ShutdownPolicy {
	return ShutdownPolicy{
		Signals:                 append([]os.Signal{}, defaultShutdownSignals()...),
		WorkloadGracefulTimeout: defaultShutdownGracefulTimeout,
		RuntimeCloseTimeout:     defaultShutdownRuntimeCloseTimeout,
	}
}

// WithShutdownPolicy 声明统一关闭策略；每个非零字段在同一 App 内只能显式声明一次。
//
// 参数：
// - policy：显式字段优先；duration 零值依次采用 ConfigManager 启动快照与内建默认值。
func WithShutdownPolicy(policy ShutdownPolicy) Option {
	return appOptionFunc(func(app *App) {
		// 第一阶段：信号必须在 Runtime Build 前冻结，因此只接受代码显式声明。
		if policy.Signals != nil {
			if app.shutdown.SignalsSet {
				app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_policy reason=signals_duplicate_declaration")))
			} else {
				app.shutdown.SignalsSet = true
				if len(policy.Signals) == 0 {
					app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_policy reason=signals_empty")))
				} else {
					signals := make([]os.Signal, 0, len(policy.Signals))
					for index, signal := range policy.Signals {
						if signal == nil {
							app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_policy reason=signal_is_nil index=%d", index)))
							continue
						}
						signals = append(signals, signal)
					}
					if len(signals) > 0 {
						app.shutdown.Signals = signals
					}
				}
			}
		}

		// 第二阶段：非零 duration 是显式声明；零值留给 ConfigManager 启动快照与内建默认值。
		if policy.WorkloadGracefulTimeout != 0 {
			if app.shutdown.WorkloadGracefulTimeoutSet {
				app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_policy reason=workload_graceful_timeout_duplicate_declaration")))
			} else {
				app.shutdown.WorkloadGracefulTimeoutSet = true
				if policy.WorkloadGracefulTimeout < 0 {
					app.recordInputError(errorForAppInternal(messagef(
						"op=app.option result=failed stage=with_shutdown_policy reason=workload_graceful_timeout_negative timeout=%s",
						policy.WorkloadGracefulTimeout,
					)))
				} else {
					app.shutdown.WorkloadGracefulTimeout = policy.WorkloadGracefulTimeout
				}
			}
		}
		if policy.RuntimeCloseTimeout != 0 {
			if app.shutdown.RuntimeCloseTimeoutSet {
				app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_policy reason=runtime_close_timeout_duplicate_declaration")))
			} else {
				app.shutdown.RuntimeCloseTimeoutSet = true
				if policy.RuntimeCloseTimeout < 0 {
					app.recordInputError(errorForAppInternal(messagef(
						"op=app.option result=failed stage=with_shutdown_policy reason=runtime_close_timeout_negative timeout=%s",
						policy.RuntimeCloseTimeout,
					)))
				} else {
					app.shutdown.RuntimeCloseTimeout = policy.RuntimeCloseTimeout
				}
			}
		}
	})
}

// WithShutdownBudget 按字段覆盖当前子命令路径的关闭预算。
// 说明：命令显式值优先于祖先节点、App ShutdownPolicy、ConfigManager 快照和内建默认值。
func WithShutdownBudget(budget ShutdownBudget) CommandOption {
	return commandOptionFunc(func(command *commandConfiguration) {
		if budget.WorkloadGracefulTimeout != 0 {
			if command.ShutdownBudget.WorkloadGracefulTimeoutSet {
				command.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_budget reason=workload_graceful_timeout_duplicate_declaration")))
			} else if budget.WorkloadGracefulTimeout < 0 {
				command.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_budget reason=workload_graceful_timeout_negative timeout=%s", budget.WorkloadGracefulTimeout)))
			} else {
				command.ShutdownBudget.WorkloadGracefulTimeout = budget.WorkloadGracefulTimeout
				command.ShutdownBudget.WorkloadGracefulTimeoutSet = true
			}
		}
		if budget.RuntimeCloseTimeout != 0 {
			if command.ShutdownBudget.RuntimeCloseTimeoutSet {
				command.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_budget reason=runtime_close_timeout_duplicate_declaration")))
			} else if budget.RuntimeCloseTimeout < 0 {
				command.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_budget reason=runtime_close_timeout_negative timeout=%s", budget.RuntimeCloseTimeout)))
			} else {
				command.ShutdownBudget.RuntimeCloseTimeout = budget.RuntimeCloseTimeout
				command.ShutdownBudget.RuntimeCloseTimeoutSet = true
			}
		}
	})
}
