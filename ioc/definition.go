package ioc

// Definition 将 Bean 与模块默认注册策略绑定为不可拆分的装配结果。
//
// 典型调用场景：模块在 ioc.gen.go 中返回 Definition，Composition Root 只负责选择模块并注册，
// 不重复了解 Bean 的声明类型、名称、注入开关和生命周期所有权。
type Definition struct {
	// Bean 是待注册实例；必填，必须满足 Register 的非 nil 约束。
	Bean any
	// Options 是模块拥有的默认注册策略；由 Define 复制，注册时按原顺序应用。
	Options []RegisterOption
}

// Define 构造模块 Bean Definition，并复制注册选项以隔离调用方后续修改。
//
// 参数：
// - bean：待注册实例，合法性在 RegisterDefinition 阶段统一校验；
// - options：名称、声明类型、注入与生命周期所有权选项。
//
// 返回：
// - Definition：可跨模块边界传递的原子装配结果。
func Define(bean any, options ...RegisterOption) Definition {
	return Definition{
		Bean:    bean,
		Options: append([]RegisterOption(nil), options...),
	}
}
