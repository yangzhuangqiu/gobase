package appruntime

import (
	"strings"

	"github.com/yangzhuangqiu/gobase/ioc"
)

// Module 定义可复用业务模块的 bean 声明契约。
//
// 典型调用场景：基础设施或业务包导出 Module，由 Application 在 IOC Build 前显式执行。
// Module 只声明 bean；资源获取与释放由 bean 的 ioc.Initializer/Closer 承担。
type Module interface {
	// Name 返回稳定模块名称，用于重复检测和错误归因。
	Name() string
	// Register 向最小 Registry 注册当前模块拥有的 bean 声明。
	//
	// 返回：
	// - error：任一声明失败时停止后续 Module，并触发 Runtime Build 回滚。
	Register(registry ioc.Registry) error
}

// ModuleFunc 是函数式 bean 模块声明入口。
type ModuleFunc func(registry ioc.Registry) error

type functionModule struct {
	// name 是模块稳定名称。
	name string
	// register 是纯 bean 声明函数，不应打开连接或启动 goroutine。
	register ModuleFunc
}

// Name 返回函数式模块名称。
func (this *functionModule) Name() string {
	return this.name
}

// Register 执行函数式 bean 声明。
func (this *functionModule) Register(registry ioc.Registry) error {
	if this.register == nil {
		return errorForModuleInvalid(messagef("op=app_runtime.module result=failed stage=register reason=register_is_nil name=%s", this.name))
	}
	return this.register(registry)
}

// NewModule 创建显式函数式业务模块。
//
// 参数：
// - name：稳定且非空的模块标识；
// - register：只执行 bean 注册的回调，不承担运行期生命周期。
func NewModule(name string, register ModuleFunc) Module {
	return &functionModule{name: strings.TrimSpace(name), register: register}
}
