package appruntime

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

const (
	// runtimeBeanName 是 Runtime 基础 bean 的稳定名称。
	runtimeBeanName = "Runtime"
	// applicationInfoBeanName 是应用元信息基础 bean 的稳定名称。
	applicationInfoBeanName = "ApplicationInfo"
	// configManagerBeanName 是配置管理器基础 bean 的稳定名称。
	configManagerBeanName = "ConfigManager"
	// loggerBeanName 是默认日志基础 bean 的稳定名称。
	loggerBeanName = "Logger"
	// containerLoggerName 区分 IOC 自身日志来源。
	containerLoggerName = "app.runtime.ioc"
)

// Runtime 表示一次 Application.Build 成功创建的运行时实例。
type Runtime struct {
	// info 是本次运行使用的应用元信息。
	info ApplicationInfo
	// manager 由 Runtime 创建，并在依赖它的 IOC/log 资源之后关闭。
	manager config.Manager
	// logProvider 由日志 bootstrap 创建，并在 IOC 之后、config 之前关闭。
	logProvider baselog.Provider
	// logFactory 为业务和 IOC 提供绑定 provider 的 logger。
	logFactory baselog.LoggerFactory
	// container 托管业务 bean 生命周期。
	container ioc.Container

	closeOnce sync.Once
	// closeDone 在首次 Close 完成后关闭，建立并发读取 closeErr 的 happens-before。
	closeDone chan struct{}
	// closeErr 保存首次关闭的聚合结果。
	closeErr error
}

// Info 返回应用元信息快照。
func (this *Runtime) Info() ApplicationInfo {
	if this == nil {
		return ApplicationInfo{}
	}
	return this.info
}

// ConfigManager 返回本次运行配置管理器。
func (this *Runtime) ConfigManager() config.Manager {
	if this == nil {
		return nil
	}
	return this.manager
}

// Logger 返回绑定本次运行 provider 的显式命名 logger。
func (this *Runtime) Logger(name string) baselog.Logger {
	if this == nil || this.logFactory == nil {
		return baselog.NewLoggerFactory(nil).Get(name)
	}
	return this.logFactory.Get(name)
}

// Container 返回本次运行 IOC 容器。
func (this *Runtime) Container() ioc.Container {
	if this == nil {
		return nil
	}
	return this.container
}

// Build 使用稳定 Application 声明创建 config、log 与 IOC。
//
// 典型调用场景：普通后台任务、测试或 app 默认路径没有单次动态覆盖时调用。
func (this *Application) Build(ctx context.Context) (*Runtime, error) {
	return this.build(ctx, BuildInput{})
}

// BuildWithInput 使用稳定 Application 声明和单次外部输入创建 Runtime。
//
// 典型调用场景：app CLI 把 flags 转换为 config.Input，或嵌入式宿主按运行实例覆盖参数。
//
// 参数：
// - ctx：应用运行生命周期 context，config watcher 从它派生；
// - input：只影响本次 Build，不修改可复用 Application。
func (this *Application) BuildWithInput(ctx context.Context, input BuildInput) (*Runtime, error) {
	return this.build(ctx, input)
}

// build 执行 Runtime 原子构建事务。
// 阶段角色：Build 与 BuildWithInput 的统一资源获取、回滚和发布路径。
//
// 语义：
// 1. 正常失败返回可分支 error，并逆序回收已取得资源；
// 2. 扩展或业务 Init 意外 panic 时，先回收已取得资源，再包装为 build_failed 继续 panic；
// 3. 每次成功调用返回拥有独立资源的 Runtime。
func (this *Application) build(ctx context.Context, input BuildInput) (*Runtime, error) {
	if this == nil {
		return nil, errorForBuildFailed(messagef("op=app_runtime.build result=failed stage=precheck reason=application_is_nil"))
	}
	if this.inputErr != nil {
		return nil, this.inputErr
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// 阶段一：构建配置，Runtime 从此取得 manager 所有权。
	manager, err := this.buildConfigManager(ctx, input.Config)
	if err != nil {
		return nil, errorForBuildFailed(messagef("op=app_runtime.build result=failed stage=build_config reason=config_failed"), err)
	}
	var runtime *Runtime
	// panic 保护：扩展或业务 Init 的编程异常仍继续 panic，但必须先回收 Runtime 已取得的资源。
	defer func() {
		panicValue := recover()
		if panicValue == nil {
			return
		}
		var cleanupErr error
		if runtime != nil {
			cleanupErr = runtime.closeResourcesSafely(context.Background())
		} else {
			cleanupErr = closeConfigManagerSafely(manager)
		}
		panicErr := errorForBuildFailed(
			messagef("op=app_runtime.build result=failed stage=panic_cleanup reason=build_panic"),
			panicPayloadAsError(panicValue),
		)
		if cleanupErr != nil {
			panic(errors.Join(panicErr, cleanupErr))
		}
		panic(panicErr)
	}()

	// 阶段二：基于已发布 Snapshot 创建本次 Runtime 独占的 typed 配置视图。
	configValues, err := this.buildConfigValues(manager)
	if err != nil {
		closeErr := closeConfigManagerSafely(manager)
		return nil, errors.Join(errorForBuildFailed(messagef("op=app_runtime.build result=failed stage=build_config_values reason=config_binding_failed"), err), closeErr)
	}

	// 阶段三：基于配置初始化日志；失败时立即回收 manager。
	provider, err := this.buildLogging(ctx, manager)
	if err != nil {
		closeErr := closeConfigManagerSafely(manager)
		return nil, errors.Join(errorForBuildFailed(messagef("op=app_runtime.build result=failed stage=build_log reason=log_failed"), err), closeErr)
	}
	factory := baselog.NewLoggerFactory(nil)
	if provider != nil {
		factory = provider.Factory()
	}
	runtime = &Runtime{
		info:        this.info,
		manager:     manager,
		logProvider: provider,
		logFactory:  factory,
		closeDone:   make(chan struct{}),
	}

	// 阶段四：创建容器、注册 borrowed 基础 bean，再执行业务 bean 声明。
	containerOptions := []ioc.Option{ioc.WithLogger(factory.Get(containerLoggerName))}
	containerOptions = append(containerOptions, this.iocOptions...)
	runtime.container = ioc.New(containerOptions...)
	if err = this.registerBaseBeans(runtime); err == nil {
		err = this.registerConfigValues(runtime, configValues)
	}
	if err == nil {
		err = this.registerBeanDefinitions(runtime)
	}
	if err == nil {
		err = this.registerModules(runtime)
	}
	if err == nil {
		err = runtime.container.Build(ctx)
	}
	if err != nil {
		cleanupErr := runtime.closeResourcesSafely(context.Background())
		return nil, errors.Join(errorForBuildFailed(messagef("op=app_runtime.build result=failed stage=build_ioc reason=ioc_failed"), err), cleanupErr)
	}
	return runtime, nil
}

// panicPayloadAsError 把任意 panic payload 转换为不携带阶段错误码的底层 cause。
// 阶段角色：Build panic 清理边界的错误语义归一化。
func panicPayloadAsError(value any) error {
	if err, ok := value.(error); ok {
		return err
	}
	return errors.New(messagef("panic_payload=%v", value))
}

// Close 按 IOC→log→config 顺序关闭资源；并发调用等待并返回首次结果。
func (this *Runtime) Close(ctx context.Context) error {
	if this == nil {
		return nil
	}
	this.closeOnce.Do(func() {
		defer close(this.closeDone)
		if ctx == nil {
			ctx = context.Background()
		}
		this.closeErr = this.closeResourcesSafely(ctx)
	})
	<-this.closeDone
	return this.closeErr
}

// closeResources 执行运行时资源逆构建顺序回收。
// 阶段角色：正常 Close 与 Build 失败回滚的统一收口。
func (this *Runtime) closeResources(ctx context.Context) error {
	var errs []error
	if this.container != nil {
		if err := closeResource("close_ioc", "container_failed", func() error { return this.container.Close(ctx) }); err != nil {
			errs = append(errs, err)
		}
	}
	if this.logProvider != nil {
		if err := closeResource("close_log", "provider_failed", this.logProvider.Close); err != nil {
			errs = append(errs, err)
		}
	}
	if this.manager != nil {
		if err := closeResource("close_config", "manager_failed", this.manager.Close); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// closeResourcesSafely 收敛 Runtime 关闭链上的意外 panic，并保证 Close 等待者可获得稳定 error。
// 阶段角色：正常 Close 与 Build 失败回滚共用的 panic 隔离边界。
func (this *Runtime) closeResourcesSafely(ctx context.Context) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.Join(err, errorForCloseFailed(
				messagef("op=app_runtime.close result=failed stage=panic_boundary reason=close_panic"),
				panicPayloadAsError(recovered),
			))
		}
	}()
	return this.closeResources(ctx)
}

// buildConfigManager 根据蓝图与单次输入创建配置管理器。
// 阶段角色：Runtime Build 的第一资源获取阶段。
func (this *Application) buildConfigManager(ctx context.Context, input config.Input) (config.Manager, error) {
	prefix := strings.TrimSpace(input.EnvPrefix)
	if prefix == "" {
		prefix = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(this.info.Name), "-", "_"))
	}
	options := []config.Option{config.BootstrapEnvPrefix(prefix)}
	options = append(options, this.configOptions...)
	for _, binding := range this.configBindings {
		options = append(options, config.SnapshotValidators(binding.snapshotValidator()))
	}
	options = append(options, input.Options()...)
	builder := config.New(options...)
	manager, err := builder.Build(ctx)
	if err != nil {
		return nil, err
	}
	return manager, nil
}

// buildLogging 在 config 可用后创建日志 provider。
// 阶段角色：Runtime Build 的第二资源获取阶段。
func (this *Application) buildLogging(ctx context.Context, manager config.Manager) (baselog.Provider, error) {
	if this.logging == nil {
		return nil, nil
	}
	return this.logging.Build(ctx, manager)
}

// registerBaseBeans 把运行时拥有的基础对象以 borrowed 语义注册到 IOC。
// 阶段角色：Beans/Modules 执行前的基础依赖准备。
func (this *Application) registerBaseBeans(runtime *Runtime) error {
	container := runtime.container
	if err := container.Register(runtime, ioc.WithName(runtimeBeanName), ioc.WithBorrowed()); err != nil {
		return errorForBuildFailed(messagef("op=app_runtime.build result=failed stage=register_base reason=runtime_failed"), err)
	}
	info := &runtime.info
	if err := container.Register(info, ioc.WithName(applicationInfoBeanName), ioc.WithBorrowed()); err != nil {
		return errorForBuildFailed(messagef("op=app_runtime.build result=failed stage=register_base reason=application_info_failed"), err)
	}
	if err := container.Register(runtime.manager, ioc.WithName(configManagerBeanName), ioc.WithDeclare[config.Manager](), ioc.WithBorrowed()); err != nil {
		return errorForBuildFailed(messagef("op=app_runtime.build result=failed stage=register_base reason=config_manager_failed"), err)
	}
	logger := runtime.Logger(loggerBeanName)
	if err := container.Register(logger, ioc.WithName(loggerBeanName), ioc.WithDeclare[baselog.Logger](), ioc.WithBorrowed()); err != nil {
		return errorForBuildFailed(messagef("op=app_runtime.build result=failed stage=register_base reason=logger_failed"), err)
	}
	if err := container.Register(runtime.logFactory, ioc.WithName("LoggerFactory"), ioc.WithDeclare[baselog.LoggerFactory](), ioc.WithBorrowed()); err != nil {
		return errorForBuildFailed(messagef("op=app_runtime.build result=failed stage=register_base reason=logger_factory_failed"), err)
	}
	return nil
}

// registerConfigValues 把 app-runtime 创建的 typed ConfigValue 注册为 borrowed bean。
// 阶段角色：业务 bean 声明前提供并发安全 typed config 依赖。
func (this *Application) registerConfigValues(runtime *Runtime, values []boundConfigValue) error {
	for _, item := range values {
		if err := runtime.container.Register(item.Value, ioc.WithName(item.Name), ioc.WithBorrowed()); err != nil {
			return errorForBuildFailed(messagef("op=app_runtime.build result=failed stage=register_config_value reason=register_failed name=%s scope=%s", item.Name, item.Scope), err)
		}
	}
	return nil
}

// registerBeanDefinitions 按声明顺序注册携带完整 IOC 策略的 bean。
// 阶段角色：基础 borrowed bean 之后、模块声明之前发布接口索引和生命周期所有权。
func (this *Application) registerBeanDefinitions(runtime *Runtime) error {
	for index, definition := range this.beanDefinitions {
		if err := runtime.container.RegisterDefinition(definition); err != nil {
			return errorForBuildFailed(messagef("op=app_runtime.build result=failed stage=register_bean_definition reason=register_failed definition_index=%d", index), err)
		}
	}
	return nil
}

// registerModules 按稳定输入顺序执行可复用 bean 模块。
// 阶段角色：IOC Build 前的模块化定义注册阶段。
func (this *Application) registerModules(runtime *Runtime) error {
	for _, module := range this.modules {
		if err := module.Register(runtime.container); err != nil {
			return errorForBuildFailed(messagef("op=app_runtime.build result=failed stage=register_module reason=register_failed name=%s", module.Name()), err)
		}
	}
	return nil
}

// closeConfigManagerSafely 关闭构建阶段已取得但尚未发布的 Manager。
// 阶段角色：日志构建失败与 panic 回滚时保持错误化 panic 语义。
func closeConfigManagerSafely(manager config.Manager) (err error) {
	if manager == nil {
		return nil
	}
	return closeResource("close_config", "manager_failed", manager.Close)
}

// closeResource 隔离单个资源关闭 panic，并把 error/panic 统一转换为 close.failed。
// 阶段角色：保证一个层级异常不会跳过后续资源收敛。
func closeResource(stage string, failureReason string, closeFn func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.Join(err, errorForCloseFailed(
				messagef("op=app_runtime.close result=failed stage=%s reason=close_panic", stage),
				panicPayloadAsError(recovered),
			))
		}
	}()
	if closeErr := closeFn(); closeErr != nil {
		return errorForCloseFailed(messagef("op=app_runtime.close result=failed stage=%s reason=%s", stage, failureReason), closeErr)
	}
	return nil
}
