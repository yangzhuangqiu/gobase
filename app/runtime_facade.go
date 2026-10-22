package app

import (
	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
	"github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

// Runtime 是 Runner 和 hooks 可见的只读运行时能力集合。
//
// 典型调用场景：命令执行期间读取配置、获取 logger 或在 composition root 解析 bean。
// 该接口不暴露 Close；Runtime 生命周期始终由 app 命令事务拥有。
type Runtime interface {
	// Info 返回稳定应用元信息。
	Info() appruntime.ApplicationInfo
	// ConfigManager 返回本次运行配置管理器。
	ConfigManager() config.Manager
	// Logger 返回按稳定组件名称绑定的 logger。
	Logger(name string) baselog.Logger
	// Container 返回已完成 Build 的 IOC Container。
	Container() ioc.Container
}

// Module 是 app-runtime 的显式业务 bean 模块契约。
type Module = appruntime.Module

// Logging 是 app-runtime 的日志 Provider 构建契约。
type Logging = appruntime.Logging

// WithConfig 聚合 Runtime 使用的稳定 Config 声明。
// 配置语义由 config.Defaults/File/Provider/Validate 等 Option 唯一定义。
func WithConfig(options ...config.Option) Option {
	return WithRuntimeOptions(appruntime.WithConfig(options...))
}

// WithConfigBinding 声明由 app-runtime 创建并注入 IOC 的 typed 配置视图。
// validator 在 Snapshot 发布前执行；业务 bean 依赖 *appruntime.ConfigValue[T]。
func WithConfigBinding[T any](scope string, validators ...func(*T) []error) Option {
	return WithRuntimeOptions(appruntime.WithConfigBinding[T](scope, validators...))
}

// WithLogging 选择 Runtime 使用的日志实现。
func WithLogging(logging Logging) Option {
	return WithRuntimeOptions(appruntime.WithLogging(logging))
}

// WithBeanDefinitions 注册携带接口、名称、注入与生命周期策略的完整 IOC bean 声明。
// 典型调用场景：使用基础设施模块提供的 Definition 工厂声明 Controller、Filter 等语义角色。
func WithBeanDefinitions(definitions ...ioc.Definition) Option {
	return WithRuntimeOptions(appruntime.WithBeanDefinitions(definitions...))
}

// WithModules 注册可复用业务 bean 模块。
func WithModules(modules ...Module) Option {
	return WithRuntimeOptions(appruntime.WithModules(modules...))
}

// WithIOCOptions 设置 Runtime 创建 IOC Container 时应用的高级策略。
func WithIOCOptions(options ...ioc.Option) Option {
	return WithRuntimeOptions(appruntime.WithIOCOptions(options...))
}
