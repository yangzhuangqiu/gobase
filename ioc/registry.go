package ioc

// Registry 定义 IOC Build 前的最小 bean 声明契约。
//
// 典型调用场景：app-runtime Module 在装配阶段注册 bean，但不能提前 Resolve、Build 或 Close Container。
type Registry interface {
	// Register 注册一个 bean 及其声明类型。
	//
	// 参数：
	// - bean：必须为非 nil 实例；
	// - options：名称、声明类型、注入与生命周期所有权选项。
	//
	// 返回：
	// - error：声明非法、同一声明类型下名称冲突或注册阶段不允许时返回可分支错误。
	Register(bean any, options ...RegisterOption) error
	// RegisterDefinition 注册模块提供的 Bean 及其默认注册策略。
	//
	// 参数：
	// - definition：Bean 与 RegisterOption 的原子装配结果；空 Bean 沿用 Register 的非法 Bean 语义。
	//
	// 返回：
	// - error：完整复用 Register 的阶段、声明和名称冲突错误语义。
	RegisterDefinition(definition Definition) error
}
