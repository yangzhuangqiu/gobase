package app

import (
	"io"
	"reflect"
	"strings"

	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
)

// appOptionFunc 适配只作用于 App 的构建函数。
type appOptionFunc func(app *App)

// applyApp 执行 App 级构建函数。
func (this appOptionFunc) applyApp(app *App) { this(app) }

// commandOptionFunc 适配只作用于子命令的构建函数。
type commandOptionFunc func(command *commandConfiguration)

// applyCommand 执行子命令级构建函数。
func (this commandOptionFunc) applyCommand(command *commandConfiguration) { this(command) }

// nodeOptionFunc 适配 root 与子命令共享的节点构建函数。
type nodeOptionFunc func(command *commandConfiguration)

// applyApp 把共享节点语义作用到 root 声明。
func (this nodeOptionFunc) applyApp(app *App) { this(&app.rootCommand) }

// applyCommand 把共享节点语义作用到子命令声明。
func (this nodeOptionFunc) applyCommand(command *commandConfiguration) { this(command) }

// WithName 设置 root 命令名（例如 admin）。
func WithName(name string) Option {
	return appOptionFunc(func(app *App) { app.rootCommand.Use = name })
}

// WithDescription 设置当前命令节点 help 输出中的简短描述。
func WithDescription(description string) NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) { command.Short = description })
}

// WithLongDescription 设置当前命令节点的详细描述。
func WithLongDescription(description string) NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) { command.Long = description })
}

// WithExample 设置当前命令节点的示例文本。
func WithExample(example string) NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) { command.Example = example })
}

// WithAliases 设置当前命令节点的别名集合。
func WithAliases(aliases ...string) NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) {
		command.Aliases = append([]string{}, aliases...)
	})
}

// WithArgsValidator 设置当前命令节点的位置参数校验器。
func WithArgsValidator(validator ArgumentValidator) NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) { command.ValidateArgs = validator })
}

// WithVersion 设置 --version 输出文本。
func WithVersion(version string) Option {
	return appOptionFunc(func(app *App) { app.version = version })
}

// WithOutput 设置 help/version/schema 等命令输出目标。
func WithOutput(output io.Writer) Option {
	return appOptionFunc(func(app *App) { app.output = output })
}

// WithErrorOutput 设置命令错误输出目标。
func WithErrorOutput(output io.Writer) Option {
	return appOptionFunc(func(app *App) { app.errorOutput = output })
}

// WithRuntime 设置命令执行使用的 app-runtime 构建蓝图。
func WithRuntime(runtime *appruntime.Application) Option {
	return appOptionFunc(func(app *App) {
		if runtime == nil {
			app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_runtime reason=runtime_is_nil")))
			return
		}
		app.runtimeApplication = runtime
	})
}

// WithRuntimeOptions 设置默认 app-runtime 蓝图选项。
// 说明：该入口与 WithRuntime 互斥；需要完全控制蓝图时优先使用 WithRuntime。
func WithRuntimeOptions(options ...appruntime.Option) Option {
	return appOptionFunc(func(app *App) { app.runtimeOptions = append(app.runtimeOptions, options...) })
}

// WithRuntimeMode 设置当前节点的 Runtime 构建语义。
// 子命令未声明时自动继承；调用方只需在 Enabled 与 Disabled 之间选择。
func WithRuntimeMode(mode RuntimeMode) NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) {
		if mode != RuntimeModeEnabled && mode != RuntimeModeDisabled {
			command.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_runtime_mode reason=unsupported_mode mode=%d", mode)))
			return
		}
		command.RuntimeMode = mode
	})
}

// WithWorkloads 声明当前节点被选中时由 app 统一监督的主动工作负载。
func WithWorkloads(workloads ...Workload) NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) { command.addWorkloads(workloads...) })
}

// addWorkloads 校验并追加当前节点局部 Workload。
// 阶段角色：Option 构建期拒绝 nil、空名与节点内重名。
func (this *commandConfiguration) addWorkloads(workloads ...Workload) {
	if len(workloads) == 0 {
		this.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_workloads reason=workloads_empty")))
		return
	}
	for index, workload := range workloads {
		if isNilWorkload(workload) {
			this.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_workloads reason=workload_is_nil index=%d", index)))
			continue
		}
		name := strings.TrimSpace(workload.Name())
		if name == "" {
			this.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_workloads reason=name_empty index=%d", index)))
			continue
		}
		if _, exists := this.WorkloadNames[name]; exists {
			this.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_workloads reason=name_duplicate name=%s", name)))
			continue
		}
		this.WorkloadNames[name] = struct{}{}
		this.Workloads = append(this.Workloads, workload)
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

// WithFlagBinder 设置当前节点局部业务 Flag 绑定器。
func WithFlagBinder(binder FlagBinder) NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) { command.FlagBinder = binder })
}

// WithCommands 递归注册当前节点的直接子命令。
func WithCommands(commands ...Command) NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) { command.addCommands(commands...) })
}

// WithBeforeRunHook 注册当前节点 Runtime Build 后、Workload 前的准备 Hook。
func WithBeforeRunHook(hook BeforeRunHook) NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) {
		if hook != nil {
			command.BeforeHooks = append(command.BeforeHooks, hook)
		}
	})
}
