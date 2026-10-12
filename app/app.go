package app

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"

	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
)

// App 是不泄漏 Cobra 公共模型的命令行应用启动门面。
type App struct {
	// name 是 root 命令名。
	name string
	// description 是 root help 简述。
	description string
	// version 是 --version 输出。
	version string
	// runtimeApplication 是 config/log/ioc 运行时构建蓝图。
	runtimeApplication *appruntime.Application
	// runtimeOptions 用于未显式提供 runtimeApplication 时构建默认蓝图。
	runtimeOptions []appruntime.Option
	// runner 是 root 命令业务入口。
	runner Runner
	// rootFlagBinder 声明并绑定 root 业务 flags。
	rootFlagBinder FlagBinder
	// commands 是业务子命令描述集合。
	commands []Command
	// beforeHooks 在 Runtime 存活期间、Runner 前执行。
	beforeHooks []BeforeRunHook
	// shutdown 保存应用级信号、时间预算与主动工作负载 Hook 声明。
	shutdown shutdownConfiguration
	// signalSource 是 Run 使用的进程信号适配器；测试可替换，ExecuteContext 不使用。
	signalSource processSignalSource
	// root 是私有 Cobra 命令实现，不通过公共 API 暴露。
	root *cobra.Command
	// output 是 help/version/schema 等标准输出目标；为空时使用 Cobra 默认值。
	output io.Writer
	// errorOutput 是命令错误输出目标；为空时使用 Cobra 默认值。
	errorOutput io.Writer
	// inputErr 聚合 App 构建声明错误。
	inputErr error
}

// New 创建命令行应用，并在未显式注入时创建默认 app-runtime 蓝图。
func New(options ...Option) *App {
	result := &App{
		name:         "app",
		description:  "Application built by App",
		version:      "dev",
		shutdown:     newDefaultShutdownConfiguration(),
		signalSource: osProcessSignalSource{},
	}
	for _, option := range options {
		if option != nil {
			option(result)
		}
	}
	if result.runtimeApplication == nil {
		runtimeOptions := []appruntime.Option{appruntime.WithName(result.name)}
		runtimeOptions = append(runtimeOptions, result.runtimeOptions...)
		result.runtimeApplication = appruntime.New(runtimeOptions...)
	} else if len(result.runtimeOptions) > 0 {
		result.recordInputError(errorForAppInternal(messagef("op=app.new result=failed stage=resolve_runtime reason=runtime_and_runtime_options_conflict")))
	}
	return result
}

// AddCommand 在应用创建后追加一个业务子命令。
func (this *App) AddCommand(command Command) {
	this.commands = append(this.commands, command)
}

// Run 使用进程参数和 Background context 执行命令行应用。
func (this *App) Run() error {
	if this == nil {
		return errorForAppInternal(messagef("op=app.run result=failed stage=precheck reason=app_is_nil"))
	}
	signalCtx, stop := this.signalSource.Notify(context.Background(), this.shutdown.Signals...)
	control := newExecutionControl(stop)
	ctx := withExecutionControl(signalCtx, control)

	// 首个信号应立即恢复默认处理，不等待 Runtime Build 或业务阶段走到 Shutdown 协调器。
	go func() {
		<-signalCtx.Done()
		control.RestoreSignals()
	}()
	defer control.RestoreSignals()
	err := this.executeContext(ctx, os.Args[1:])
	// Runtime Build/Before 阶段尚未进入协调器时，纯信号取消仍属于正常进程退出。
	if signalCtx.Err() != nil && !executionCoordinatorStarted(ctx) && isCancellationOnly(err, context.Cause(signalCtx)) {
		return nil
	}
	return err
}

// ExecuteContext 使用显式上下文和参数执行命令行应用。
func (this *App) ExecuteContext(ctx context.Context, args []string) error {
	if this == nil {
		return errorForAppInternal(messagef("op=app.execute result=failed stage=precheck reason=app_is_nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return this.executeContext(ctx, args)
}

// executeContext 执行一次不额外安装进程信号的命令事务。
// 阶段角色：Run 与 ExecuteContext 的统一 Cobra 适配入口。
func (this *App) executeContext(ctx context.Context, args []string) error {
	command := this.command()
	command.SetArgs(append([]string{}, args...))
	return command.ExecuteContext(ctx)
}

// command 返回私有 Cobra root，并按需构建。
// 阶段角色：ExecuteContext 的底层 CLI 适配入口。
func (this *App) command() *cobra.Command {
	if this.root == nil {
		this.root = this.buildCommand()
	}
	return this.root
}

// recordInputError 记录构建期输入错误，延迟到执行阶段统一 fail-fast。
func (this *App) recordInputError(err error) {
	this.inputErr = errors.Join(this.inputErr, err)
}
