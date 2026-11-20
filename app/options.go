package app

import (
	"io"
	"reflect"
	"strings"

	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
	"github.com/yangzhuangqiu/gobase/ioc"
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

// WithoutRuntime 声明当前命令及未覆盖子节点不需要构建应用 Runtime。
// 典型调用场景：纯本地诊断、离线转换或修复 Runtime 启动条件的命令。
func WithoutRuntime() NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) {
		command.RuntimeRequirement = runtimeRequirementDisabled
	})
}

// RequireRuntime 显式恢复当前命令及未覆盖子节点的完整 Runtime 构建语义。
// 典型调用场景：在 WithoutRuntime 子树中，仅为少数依赖 config/log/ioc 的命令重新启用 Runtime。
func RequireRuntime() NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) {
		command.RuntimeRequirement = runtimeRequirementRequired
	})
}

// WithBeanDefinitions 声明当前节点被选中时注册的私有 IOC Bean。
//
// 语义：
// - root 与子命令共享本入口；
// - Definition 不沿命令路径继承，只使用最终选中节点的声明；
// - 真正的进程级高级声明使用 WithRuntimeOptions(appruntime.WithBeanDefinitions(...))。
func WithBeanDefinitions(definitions ...ioc.Definition) NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) { command.addBeanDefinitions(definitions...) })
}

// addBeanDefinitions 校验、复制并追加当前节点局部 Definition。
// 阶段角色：Option 构建期拒绝空列表、nil 与 typed nil Bean，避免无效对象进入 Runtime Build。
func (this *commandConfiguration) addBeanDefinitions(definitions ...ioc.Definition) {
	// 阶段一：空声明通常表示 composition root 装配遗漏，不静默退化为无对象图。
	if len(definitions) == 0 {
		this.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_bean_definitions reason=definitions_empty")))
		return
	}
	// 阶段二：逐项校验 Bean，并复制模块拥有的注册策略，隔离调用方后续修改。
	for index, definition := range definitions {
		if isNilValue(definition.Bean) {
			this.recordInputError(errorForAppInternal(messagef(
				"op=app.option result=failed stage=with_bean_definitions reason=bean_is_nil index=%d",
				index,
			)))
			continue
		}
		definition.Options = append([]ioc.RegisterOption{}, definition.Options...)
		this.BeanDefinitions = append(this.BeanDefinitions, definition)
	}
}

// WithModules 声明当前节点被选中时参与 Runtime Build 的局部 Module。
//
// 语义：
// - root 与子命令共享本入口；
// - Module 不沿命令路径继承，只使用最终选中节点的声明；
// - 实现 WorkloadModule 的 Module 还会在 Runtime Build 前贡献主动 Workload。
func WithModules(modules ...Module) NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) { command.addModules(modules...) })
}

// addModules 校验并追加当前节点局部 Module。
// 阶段角色：Option 构建期拒绝 nil、空名与节点内重名；Workload 合同延迟到执行计划解析。
func (this *commandConfiguration) addModules(modules ...Module) {
	if len(modules) == 0 {
		this.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_modules reason=modules_empty")))
		return
	}
	for index, module := range modules {
		if isNilValue(module) {
			this.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_modules reason=module_is_nil index=%d", index)))
			continue
		}
		name := strings.TrimSpace(module.Name())
		if name == "" {
			this.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_modules reason=name_empty index=%d", index)))
			continue
		}
		if _, exists := this.ModuleNames[name]; exists {
			this.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_modules reason=name_duplicate name=%s", name)))
			continue
		}
		this.ModuleNames[name] = struct{}{}
		this.Modules = append(this.Modules, module)
	}
}

// isNilValue 识别 interface 中携带的 nil 或 typed nil，避免声明错误推迟为执行期 panic。
func isNilValue(target any) bool {
	if target == nil {
		return true
	}
	value := reflect.ValueOf(target)
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

// WithGlobalBeforeRunHook 注册当前 App 每次实际命令执行都调用的运行前 Hook。
// 说明：按声明顺序执行；help/version 等未进入实际命令执行管线的操作不会调用。
func WithGlobalBeforeRunHook(hook BeforeRunHook) Option {
	return appOptionFunc(func(app *App) {
		if hook != nil {
			app.globalBeforeRunHooks = append(app.globalBeforeRunHooks, hook)
		}
	})
}

// WithBeforeRunHook 注册仅在当前命令节点被选中时调用的局部运行前 Hook。
// 说明：节点 Hook 不继承；WithoutRuntime 命令仍调用 Hook，但传入 nil Runtime。
func WithBeforeRunHook(hook BeforeRunHook) NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) {
		if hook != nil {
			command.BeforeRunHooks = append(command.BeforeRunHooks, hook)
		}
	})
}
