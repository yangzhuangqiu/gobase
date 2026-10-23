package appwebhertz

import (
	"github.com/cloudwego/hertz/pkg/app"
	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// InstrumentationBundle 定义必须原子安装的 Hertz 插桩合同。
//
// 典型调用场景:
// 1. Trace 适配器成对提供 Hertz 原生 Tracer Option 与 Server Middleware；
// 2. app-web-hertz 在 provider 构造前验证完整 Bundle，并把 Middleware 放入固定插桩阶段。
//
// 说明:
// - 合同只依赖 Hertz，不暴露 OTel、Prometheus 或具体 exporter 类型；
// - 实现必须返回可并发复用的不可变声明，不得在方法调用时获取外部资源。
type InstrumentationBundle interface {
	// HertzOptions 返回必须与 Middleware 同时安装的 Hertz 原生构造声明。
	// 返回 slice 的所有权属于调用方；实现不得依赖调用方保留或修改该 slice。
	HertzOptions() []hertzconfig.Option
	// Middleware 返回插桩阶段的全局 Middleware，按 slice 顺序执行。
	// 返回 slice 的所有权属于调用方；nil Handler 会使 Server 构造 fail-fast。
	Middleware() []app.HandlerFunc
}

// instrumentationPlan 保存全部 Bundle 校验后的原子 Hertz 构造片段。
type instrumentationPlan struct {
	// hertzOptions 先于调用方原生高级 Option 应用。
	hertzOptions []hertzconfig.Option
	// middleware 位于 advanced outer 与标准 Middleware 之间。
	middleware []app.HandlerFunc
	// metadataProviders 把 Trace ID 等完成态关联值桥接给 Request Observer。
	metadataProviders []webhertz.RequestObservationMetadataProvider
}

// prepareInstrumentationBundles 在 provider 构造前展开并验证全部原子插桩声明。
// 阶段角色：任何 Bundle 非法都会阻止全部 Hertz Option/Middleware 安装，避免半插桩 Server。
func prepareInstrumentationBundles(bundles []InstrumentationBundle) (instrumentationPlan, error) {
	plan := instrumentationPlan{}
	for bundleIndex, bundle := range bundles {
		if isNilInterface(bundle) {
			return instrumentationPlan{}, errorForDeclarationInvalid(messagef("op=app_web_hertz.build result=failed stage=validate_instrumentation reason=bundle_required bundle_index=%d", bundleIndex))
		}
		// 先完整校验当前 Bundle，再合并到计划；失败时不会发布当前 Bundle 的任何一半。
		nativeOptions := bundle.HertzOptions()
		for optionIndex, option := range nativeOptions {
			if option.F == nil {
				return instrumentationPlan{}, errorForDeclarationInvalid(messagef("op=app_web_hertz.build result=failed stage=validate_instrumentation reason=hertz_option_required bundle_index=%d option_index=%d", bundleIndex, optionIndex))
			}
		}
		middleware := bundle.Middleware()
		for handlerIndex, handler := range middleware {
			if handler == nil {
				return instrumentationPlan{}, errorForDeclarationInvalid(messagef("op=app_web_hertz.build result=failed stage=validate_instrumentation reason=middleware_required bundle_index=%d handler_index=%d", bundleIndex, handlerIndex))
			}
		}
		plan.hertzOptions = append(plan.hertzOptions, nativeOptions...)
		plan.middleware = append(plan.middleware, middleware...)
		if provider, ok := bundle.(webhertz.RequestObservationMetadataProvider); ok && !isNilInterface(provider) {
			plan.metadataProviders = append(plan.metadataProviders, provider)
		}
	}
	return plan, nil
}
