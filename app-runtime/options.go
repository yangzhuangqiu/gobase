package appruntime

import (
	"strings"

	"github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/ioc"
)

// Option 定义 Application 构建蓝图的可组合选项。
type Option func(application *Application)

// WithName 设置稳定应用名；默认值为 app。
// 该名称进入 ApplicationInfo，并作为 bootstrap 环境变量前缀的默认来源。
func WithName(name string) Option {
	return func(application *Application) {
		name = strings.TrimSpace(name)
		if name != "" {
			application.info.Name = name
		}
	}
}

// WithConfig 聚合本应用每次 Build 重放的稳定 Config 声明。
//
// 参数：
// - options：由 config 模块定义的 Defaults/File/Provider/Validate 等唯一事实源；
// - nil：被忽略；多次调用按 Application Option 顺序追加。
func WithConfig(options ...config.Option) Option {
	return func(application *Application) {
		for _, option := range options {
			if option != nil {
				application.configOptions = append(application.configOptions, option)
			}
		}
	}
}

// WithLogging 选择基于 ConfigManager 构建日志 Provider 的实现。
// 后声明覆盖此前实现；nil 表示保留已有声明。
func WithLogging(logging Logging) Option {
	return func(application *Application) {
		application.setLogging(logging)
	}
}

// WithBeanDefinitions 注册携带完整 IOC 策略的 managed bean 声明。
//
// 典型调用场景：Controller、Filter 等组件需要按语义接口聚合，调用方使用模块提供的 Definition 工厂声明角色。
//
// 参数：
// - definitions：按输入顺序保存；Bean 为空或 typed nil 时在 Build 获取资源前失败。
func WithBeanDefinitions(definitions ...ioc.Definition) Option {
	return func(application *Application) {
		for _, definition := range definitions {
			application.addBeanDefinition(definition)
		}
	}
}

// WithModules 注册可复用业务 Module；同名声明会在 Build 获取资源前失败。
func WithModules(modules ...Module) Option {
	return func(application *Application) {
		for _, module := range modules {
			application.addModule(module)
		}
	}
}

// WithIOCOptions 设置创建 IOC Container 时应用的高级策略选项。
// Runtime 注入的默认 IOC logger 先应用，调用方 Option 可显式覆盖。
func WithIOCOptions(options ...ioc.Option) Option {
	return func(application *Application) {
		application.iocOptions = append(application.iocOptions, options...)
	}
}
