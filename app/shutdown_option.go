package app

import (
	"os"
	"strings"
	"time"
)

// ShutdownOption 定义 `WithShutdown` 内应用级退出策略的声明契约。
//
// 典型调用场景：配置进程信号、graceful/Runtime Close 时间预算，或注册主动工作负载 Hook。
// ShutdownOption 只能通过本包的 `WithShutdown*` 构造函数创建。
type ShutdownOption interface {
	applyShutdown(app *App)
}

type shutdownOptionFunc func(app *App)

// applyShutdown 把单项 Shutdown 声明应用到 App 构建状态。
func (this shutdownOptionFunc) applyShutdown(app *App) {
	this(app)
}

// WithShutdown 聚合应用级退出策略；省略时仍使用默认信号和时间预算。
func WithShutdown(options ...ShutdownOption) Option {
	return func(app *App) {
		for index, option := range options {
			if option == nil {
				app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown reason=option_is_nil index=%d", index)))
				continue
			}
			option.applyShutdown(app)
		}
	}
}

// WithShutdownSignals 替换 Run/Main 的默认信号集合。
//
// 参数：
// - signals：至少包含一个非 nil os.Signal；重复声明属于 App 输入错误。
func WithShutdownSignals(signals ...os.Signal) ShutdownOption {
	return shutdownOptionFunc(func(app *App) {
		if app.shutdown.SignalsSet {
			app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_signals reason=duplicate_declaration")))
			return
		}
		app.shutdown.SignalsSet = true
		if len(signals) == 0 {
			app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_signals reason=signals_empty")))
			return
		}
		result := make([]os.Signal, 0, len(signals))
		for index, signal := range signals {
			if signal == nil {
				app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_signals reason=signal_is_nil index=%d", index)))
				continue
			}
			result = append(result, signal)
		}
		if len(result) > 0 {
			app.shutdown.Signals = result
		}
	})
}

// WithShutdownGracefulTimeout 设置 Shutdown Hooks 的共享 graceful 截止时间。
func WithShutdownGracefulTimeout(timeout time.Duration) ShutdownOption {
	return shutdownOptionFunc(func(app *App) {
		if app.shutdown.GracefulTimeoutSet {
			app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_graceful_timeout reason=duplicate_declaration")))
			return
		}
		app.shutdown.GracefulTimeoutSet = true
		if timeout <= 0 {
			app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_graceful_timeout reason=timeout_not_positive timeout=%s", timeout)))
			return
		}
		app.shutdown.GracefulTimeout = timeout
	})
}

// WithShutdownRuntimeCloseTimeout 设置 Runtime Close 的独立协作式截止时间。
func WithShutdownRuntimeCloseTimeout(timeout time.Duration) ShutdownOption {
	return shutdownOptionFunc(func(app *App) {
		if app.shutdown.RuntimeCloseTimeoutSet {
			app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_runtime_close_timeout reason=duplicate_declaration")))
			return
		}
		app.shutdown.RuntimeCloseTimeoutSet = true
		if timeout <= 0 {
			app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_runtime_close_timeout reason=timeout_not_positive timeout=%s", timeout)))
			return
		}
		app.shutdown.RuntimeCloseTimeout = timeout
	})
}

// WithShutdownHook 注册一个应用级主动工作负载 Hook。
//
// 参数：
// - name：应用内非空且唯一的稳定组件名称；
// - hook：满足 graceful→force→wait 返回强契约的关闭函数。
func WithShutdownHook(name string, hook ShutdownHook) ShutdownOption {
	return shutdownOptionFunc(func(app *App) {
		name = strings.TrimSpace(name)
		if name == "" {
			app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_hook reason=name_empty")))
			return
		}
		if hook == nil {
			app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_hook reason=hook_is_nil name=%s", name)))
			return
		}
		if _, exists := app.shutdown.HookNames[name]; exists {
			app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_hook reason=name_duplicate name=%s", name)))
			return
		}
		app.shutdown.HookNames[name] = struct{}{}
		app.shutdown.Hooks = append(app.shutdown.Hooks, namedShutdownHook{Name: name, Hook: hook})
	})
}
