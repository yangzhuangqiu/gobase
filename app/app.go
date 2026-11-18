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
	// rootCommand 保存进程 root 节点声明，与子命令使用相同语义模型。
	rootCommand commandConfiguration
	// version 是 --version 输出。
	version string
	// runtimeApplication 是 config/log/ioc 运行时构建蓝图。
	runtimeApplication *appruntime.Application
	// runtimeOptions 用于未显式提供 runtimeApplication 时构建默认蓝图。
	runtimeOptions []appruntime.Option
	// shutdown 保存进程级信号与默认时间预算声明。
	shutdown shutdownConfiguration
	// signalSource 是 Run 使用的进程信号适配器；测试可替换，ExecuteContext 不使用。
	signalSource processSignalSource
	// executionBinding 为缓存命令树换入当前一次执行的 session。
	executionBinding *commandExecutionBinding
	// commandTree 是只构建一次的私有 Cobra 命令树，不通过公共 API 暴露。
	commandTree *cobra.Command
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
		rootCommand:      newCommandConfiguration("app", runtimeRequirementRequired),
		version:          "dev",
		shutdown:         newDefaultShutdownConfiguration(),
		signalSource:     osProcessSignalSource{},
		executionBinding: &commandExecutionBinding{},
	}
	result.rootCommand.Short = "Application built by App"
	for _, option := range options {
		if option != nil {
			option.applyApp(result)
		}
	}
	result.inputErr = errors.Join(result.inputErr, collectCommandInputErrors(result.rootCommand))
	if result.runtimeApplication == nil {
		runtimeOptions := []appruntime.Option{appruntime.WithName(result.rootCommand.Use)}
		runtimeOptions = append(runtimeOptions, result.runtimeOptions...)
		result.runtimeApplication = appruntime.New(runtimeOptions...)
	} else if len(result.runtimeOptions) > 0 {
		result.recordInputError(errorForAppInternal(messagef("op=app.new result=failed stage=resolve_runtime reason=runtime_and_runtime_options_conflict")))
	}
	return result
}

// Run 使用进程参数和 Background context 执行命令行应用。
func (this *App) Run() error {
	if this == nil {
		return errorForAppInternal(messagef("op=app.run result=failed stage=precheck reason=app_is_nil"))
	}
	// 阶段一：订阅实际信号，并为本次命令事务建立显式控制状态和类型化取消入口。
	events, stop := this.signalSource.Notify(this.shutdown.Signals...)
	session := newExecutionSession(stop)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.Canceled)
	defer session.RestoreSignals()
	go watchProcessSignals(events, session, cancel)

	// 阶段二：命令、Runtime、Workload 与 Shutdown 共享本次 session，但业务只接收标准 context。
	err := this.executeContext(ctx, os.Args[1:], session)
	// 阶段三：进程信号本身是正常退出原因；其他阶段失败时补充实际 signal 诊断上下文。
	signalCause := session.SignalCause()
	// Runtime Build/Before 阶段尚未进入协调器时，纯信号取消仍属于正常进程退出。
	if signalCause != nil &&
		!session.CoordinatorStarted() && isCancellationOnly(err, signalCause) {
		return nil
	}
	if signalCause != nil && err != nil {
		return errorf("op=app.run result=failed reason=shutdown_after_process_signal source=process_signal signal=%s cause=%w", signalCause.Signal, err)
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
	return this.executeContext(ctx, args, nil)
}

// executeContext 执行一次不额外安装进程信号的命令事务。
// 阶段角色：Run 与 ExecuteContext 的统一 Cobra 适配入口。
func (this *App) executeContext(ctx context.Context, args []string, session *executionSession) error {
	command := this.command()
	restoreBinding := this.executionBinding.bindSession(session)
	defer restoreBinding()
	command.SetArgs(append([]string{}, args...))
	return command.ExecuteContext(ctx)
}

// command 返回只构建一次的私有 Cobra root，并让后续执行复用 Flag 声明。
// 阶段角色：首次执行冻结命令树；每次调用只通过 executionBinding 替换 session。
func (this *App) command() *cobra.Command {
	if this.executionBinding == nil {
		this.executionBinding = &commandExecutionBinding{}
	}
	if this.commandTree == nil {
		this.commandTree = this.buildCommand(this.executionBinding)
	}
	return this.commandTree
}

// recordInputError 记录构建期输入错误，延迟到执行阶段统一 fail-fast。
func (this *App) recordInputError(err error) {
	this.inputErr = errors.Join(this.inputErr, err)
}

// collectCommandInputErrors 递归收集命令树声明错误。
// 阶段角色：命令树构建完成后，在任何 Runtime/Workload 启动前统一 fail-fast。
func collectCommandInputErrors(command commandConfiguration) error {
	result := command.InputErr
	for _, child := range command.Commands {
		result = errors.Join(result, collectCommandInputErrors(child.configuration))
	}
	return result
}
