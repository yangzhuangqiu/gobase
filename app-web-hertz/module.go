package appwebhertz

import (
	baseapp "github.com/yangzhuangqiu/gobase/app"
	"github.com/yangzhuangqiu/gobase/ioc"
	baseweb "github.com/yangzhuangqiu/gobase/web"
)

const (
	// moduleName 是 app-runtime 识别重复模块和归因失败的稳定名称。
	moduleName = "app-web-hertz"
	// serverBeanName 是 web.Server 在 IOC 类型范围内的稳定 bean 名称。
	serverBeanName = "WebHertzServer"
)

// Module 定义声明当前 Runtime 唯一托管 Server bean，并贡献其主动生命周期的两阶段合同。
//
// 典型调用场景:
// 1. 直接调用 Register 向普通 IOC Container 安装自动装配能力；
// 2. 作为 app Node Module 使用，由 Runtime 注册 Bean、由 app 监督其贡献的 Workload。
type Module interface {
	baseapp.WorkloadModule
}

type module struct {
	// options 是每次 Register 重放的纯内存 Server 装配声明。
	options []Option
}

// NewModule 创建自动聚合应用组件并发布当前 Runtime 唯一 web.Server 的 IOC Module。
//
// 参数:
// - options: 控制 config scope、路径风格、标准 typed policy 和 web-hertz 原生扩展，不承载 IOC 应用组件。
//
// 返回:
// - Module: 可跨 Container 重放，并为选中命令贡献唯一 Hertz Server Workload。
func NewModule(options ...Option) Module {
	return &module{options: append([]Option{}, options...)}
}

// Workloads 返回使用当前 Module 所声明 Server 的主动生命周期适配器。
// 阶段角色：app 在 Runtime Build 前冻结本次 Workload 集合，适配器本身不获取资源。
func (this *module) Workloads() []baseapp.Workload {
	return []baseapp.Workload{newServerWorkload()}
}

// Name 返回 app-runtime 用于重复检测的稳定模块名。
func (this *module) Name() string {
	return moduleName
}

// Register 校验纯声明后注册由 IOC 托管生命周期的 Server bean。
//
// 流程:
// 1. 在修改 Registry 前解析全部 Option，保证声明错误无部分注册；
// 2. 创建尚未读取配置、尚未持有 provider 的 Server bean；
// 3. 同一实例声明为 web.Server/StartupServer，供 Build 后唯一解析、等待启动结果和运行。
func (this *module) Register(registry ioc.Registry) error {
	// 第一阶段：全部纯声明在 Registry 修改前 fail-fast。
	if this == nil {
		return errorForDeclarationInvalid("op=app_web_hertz.module.register result=failed stage=precheck reason=module_required")
	}
	if isNilInterface(registry) {
		return errorForDeclarationInvalid("op=app_web_hertz.module.register result=failed stage=precheck reason=registry_required")
	}
	resolvedOptions, err := resolveOptions(this.options)
	if err != nil {
		return err
	}

	// 第二阶段：只发布依赖图节点，配置读取和 provider 构造延迟到 IOC Init。
	bean := &managedServer{configScope: resolvedOptions.configScope, options: resolvedOptions}
	if err := registry.Register(
		bean,
		ioc.WithName(serverBeanName),
		ioc.WithDeclare[baseweb.Server](),
		ioc.WithDeclare[baseweb.StartupServer](),
	); err != nil {
		return errorForModuleRegisterFailed(
			"op=app_web_hertz.module.register result=failed stage=register_server reason=ioc_register_failed",
			err,
		)
	}
	return nil
}
