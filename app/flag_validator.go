package app

// FlagValidator 定义业务 Flag 语义校验的可选扩展契约。
//
// 语义：
// 1. app 在 Flag 解析和 Complete 后、Runtime Build 前按 type assertion 调用；
// 2. 仅校验命令参数及其组合约束；
// 3. 配置快照校验统一使用 app.WithConfig(config.Validate[T](...))。
type FlagValidator interface {
	// ValidateFlags 返回可聚合的业务 Flag 语义错误集合。
	//
	// 返回：
	// - []error: nil 元素会被忽略；存在非 nil 错误时不构建 Runtime。
	ValidateFlags() []error
}
