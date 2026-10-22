package appwebhertz

import (
	"github.com/yangzhuangqiu/gobase/ioc"
	baseweb "github.com/yangzhuangqiu/gobase/web"
)

const (
	// moduleName 是 app-runtime 识别重复模块和归因失败的稳定名称。
	moduleName = "app-web-hertz"
	// serverBeanName 是 web.Server 在 IOC 类型范围内的稳定 bean 名称。
	serverBeanName = "WebHertzServer"
)

// Module 定义 app-web-hertz 在 IOC Build 前声明托管 Server bean 的合同。
//
// 典型调用场景:
// 1. 直接调用 Register 向普通 IOC Container 安装自动装配能力；
// 2. 作为 app-runtime Module 使用，由 Runtime 在业务 bean 注册后统一执行。
type Module interface {
	// Name 返回稳定模块名称，用于重复检测和错误归因。
	Name() string
	// Register 注册声明为 web.Server 的托管 bean，不读取配置或启动监听。
	//
	// 参数:
	// - registry: 必填；必须处于 IOC Build 前的注册阶段。
	//
	// 返回:
	// - nil: Server bean 声明成功；
	// - error: Option、Registry 或重复声明非法，保留底层 IOC cause。
	Register(registry ioc.Registry) error
}

type module struct {
	// scope 是 Server bean Init 阶段单次读取的独立配置路径。
	scope string
	// options 是每次 Register 重放的纯内存 Server 装配声明。
	options []Option
}

// NewModule 创建自动聚合应用组件并发布 web.Server 的 IOC Module。
//
// 参数:
// - scope: trim 后为空时使用 `web.hertz`；非空值作为 config dotted path；
// - options: 控制路径风格、标准 typed policy 和 web-hertz 原生扩展，不承载 IOC 应用组件。
//
// 返回:
// - Module: 可注册一次或在不同 Container 中重放的无资源声明对象。
func NewModule(scope string, options ...Option) Module {
	return &module{
		scope:   resolveScope(scope),
		options: append([]Option{}, options...),
	}
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
// 3. 同一实例声明为 web.Server，供 Build 后唯一解析和运行。
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
	bean := &managedServer{scope: this.scope, options: resolvedOptions}
	if err := registry.Register(bean, ioc.WithName(serverBeanName), ioc.WithDeclare[baseweb.Server]()); err != nil {
		return errorForModuleRegisterFailed(
			"op=app_web_hertz.module.register result=failed stage=register_server reason=ioc_register_failed",
			err,
		)
	}
	return nil
}
