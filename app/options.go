package app

import (
	"io"
	"reflect"
	"strings"

	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
)

// Option 定义 App 构建期可组合选项。
type Option func(app *App)

// WithName 设置 root 命令名（例如 admin）。
func WithName(name string) Option {
	return func(app *App) {
		app.name = name
	}
}

// WithDescription 设置 help 输出中的简短描述。
func WithDescription(description string) Option {
	return func(app *App) {
		app.description = description
	}
}

// WithVersion 设置 --version 输出文本。
func WithVersion(version string) Option {
	return func(app *App) {
		app.version = version
	}
}

// WithOutput 设置 help/version/schema 等命令输出目标。
func WithOutput(output io.Writer) Option {
	return func(app *App) {
		app.output = output
	}
}

// WithErrorOutput 设置命令错误输出目标。
func WithErrorOutput(output io.Writer) Option {
	return func(app *App) {
		app.errorOutput = output
	}
}

// WithRuntime 设置命令执行使用的 app-runtime 构建蓝图。
func WithRuntime(runtime *appruntime.Application) Option {
	return func(app *App) {
		if runtime == nil {
			app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_runtime reason=runtime_is_nil")))
			return
		}
		app.runtimeApplication = runtime
	}
}

// WithRuntimeOptions 设置默认 app-runtime 蓝图选项。
// 说明：该入口与 WithRuntime 互斥；需要完全控制蓝图时优先使用 WithRuntime。
func WithRuntimeOptions(options ...appruntime.Option) Option {
	return func(app *App) {
		app.runtimeOptions = append(app.runtimeOptions, options...)
	}
}

// WithWorkloads 声明由 root 命令统一监督的主动工作负载。
//
// 参数：
// - workloads：至少一个非 nil Workload；名称 Trim 后必须非空且在 App 内唯一。
//
// 说明：一次性 root 任务可用 Runner 包装为 Workload；子命令继续使用自身 CommandRunner。
func WithWorkloads(workloads ...Workload) Option {
	return func(app *App) {
		if len(workloads) == 0 {
			app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_workloads reason=workloads_empty")))
			return
		}
		for index, workload := range workloads {
			if isNilWorkload(workload) {
				app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_workloads reason=workload_is_nil index=%d", index)))
				continue
			}
			name := strings.TrimSpace(workload.Name())
			if name == "" {
				app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_workloads reason=name_empty index=%d", index)))
				continue
			}
			if _, exists := app.workloadNames[name]; exists {
				app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_workloads reason=name_duplicate name=%s", name)))
				continue
			}
			app.workloadNames[name] = struct{}{}
			app.workloads = append(app.workloads, workload)
		}
	}
}

// isNilWorkload 识别接口中的 typed nil，避免把声明错误推迟为执行期 panic。
func isNilWorkload(workload Workload) bool {
	if workload == nil {
		return true
	}
	value := reflect.ValueOf(workload)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// WithFlagBinder 设置 root 级业务 Flag 绑定器。
//
// 参数：
// - binder：声明 root flags 并绑定普通 Flag 目标字段；为空表示 root 不声明业务 Flag。
func WithFlagBinder(binder FlagBinder) Option {
	return func(app *App) {
		app.rootFlagBinder = binder
	}
}

// WithCommands 批量注册业务子命令。
func WithCommands(commands ...Command) Option {
	return func(app *App) {
		app.commands = append(app.commands, commands...)
	}
}

// WithBeforeRunHook 注册 Runtime Build 后、业务执行前 hook。
func WithBeforeRunHook(hook BeforeRunHook) Option {
	return func(app *App) {
		if hook != nil {
			app.beforeHooks = append(app.beforeHooks, hook)
		}
	}
}
