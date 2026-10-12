package appruntime

import (
	"errors"
	"reflect"
	"strings"

	"github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/config/provider"
	"github.com/yangzhuangqiu/gobase/ioc"
)

// Application 是可重复使用的运行时构建蓝图。
//
// 语义：
// 1. Application 只保存构建声明，不持有单次运行资源；
// 2. 每次 Build 创建独立 Runtime；
// 3. 创建完成后调用方不应并发修改其内部选项。
type Application struct {
	// info 是注入运行时的应用元信息。
	info ApplicationInfo
	// configOptions 是每次 Build 重放的稳定 Config 声明。
	configOptions []config.Option
	// configBindings 是 app-runtime 拥有的 typed IOC 配置声明。
	configBindings []configBinding
	// logging 在 config 后创建本次日志 Provider。
	logging Logging
	// beans 是无需模块封装的简单 bean 声明。
	beans []any
	// modules 是显式可复用 bean 模块清单。
	modules []Module
	// moduleNames 用于模块名重复检测。
	moduleNames map[string]struct{}
	// iocOptions 透传到 ioc.New，仅面向底层策略调整。
	iocOptions []ioc.Option
	// inputErr 聚合构建蓝图阶段的声明错误。
	inputErr error
}

// New 创建运行时构建蓝图。
func New(options ...Option) *Application {
	result := &Application{
		info:        ApplicationInfo{Name: "app"},
		moduleNames: make(map[string]struct{}),
	}
	for _, option := range options {
		if option != nil {
			option(result)
		}
	}
	return result
}

// Info 返回应用元信息副本。
func (this *Application) Info() ApplicationInfo {
	if this == nil {
		return ApplicationInfo{}
	}
	return this.info
}

// BootstrapSchemas 返回 provider bootstrap schema 的只读副本。
func (this *Application) BootstrapSchemas() map[string][]provider.BootstrapKey {
	if this == nil {
		return map[string][]provider.BootstrapKey{}
	}
	return config.BootstrapSchemas(this.configOptions...)
}

// addModule 校验并记录模块声明。
// 阶段角色：Option 应用期的可复用 bean 模块注册收口。
func (this *Application) addModule(module Module) {
	// 阶段一：nil 声明不贡献能力，保持组合 Option 的可选语义。
	if module == nil {
		return
	}
	if isNilLike(module) {
		this.recordInputError(errorForModuleInvalid(messagef("op=app_runtime.module result=failed stage=declare reason=module_typed_nil")))
		return
	}
	name := strings.TrimSpace(module.Name())
	// 阶段二：在获取 config/logging 资源前收口稳定模块身份。
	if name == "" {
		this.recordInputError(errorForModuleInvalid(messagef("op=app_runtime.module result=failed stage=declare reason=name_required")))
		return
	}
	if _, exists := this.moduleNames[name]; exists {
		this.recordInputError(errorForModuleInvalid(messagef("op=app_runtime.module result=failed stage=declare reason=duplicate_name name=%s", name)))
		return
	}
	this.moduleNames[name] = struct{}{}
	// 阶段三：保存原始声明顺序，供 IOC Build 前稳定重放。
	this.modules = append(this.modules, module)

}

// addBean 校验并记录简单 bean 声明。
// 阶段角色：Option 应用期在获取 Runtime 资源前拒绝 nil bean。
func (this *Application) addBean(bean any) {
	if isNilLike(bean) {
		this.recordInputError(errorForBeanInvalid(messagef("op=app_runtime.bean result=failed stage=declare reason=bean_required")))
		return
	}
	this.beans = append(this.beans, bean)
}

// setLogging 校验并保存日志实现声明。
// 阶段角色：Option 应用期在 config Build 前拒绝 typed nil Logging。
func (this *Application) setLogging(logging Logging) {
	if logging == nil {
		return
	}
	if isNilLike(logging) {
		this.recordInputError(errorForLoggingInvalid(messagef("op=app_runtime.logging result=failed stage=declare reason=logging_typed_nil")))
		return
	}
	this.logging = logging
}

// isNilLike 识别 interface 中携带的 nil 或 typed nil 声明。
// 阶段角色：Application Option 声明期的统一 nil 边界。
func isNilLike(value any) bool {
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() {
		return true
	}
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

// recordInputError 聚合蓝图声明错误，延迟到 Build 统一 fail-fast。
func (this *Application) recordInputError(err error) {
	this.inputErr = errors.Join(this.inputErr, err)
}
