package log

// Provider 定义日志提供者契约。
//
// 语义：
// 1. Provider 在构造成功后拥有一个非 nil、稳定的 LoggerFactory；
// 2. 业务只通过 LoggerFactory 获取 Logger，不感知底层日志实现；
// 3. Close 负责资源释放，生命周期由 provider 创建方管理；
// 4. Close 后既有 LoggerFactory/Logger 保持可调用，但不再输出事件。
type Provider interface {
	// Factory 返回 provider 构造期创建的稳定日志工厂。
	// 返回：
	// - LoggerFactory: 成功构造的 Provider 必须始终返回同一非 nil 实例。
	Factory() LoggerFactory
	// Close 释放 provider 资源（应幂等）。
	Close() error
}
