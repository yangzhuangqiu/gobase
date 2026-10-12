package app

// FlagCompleter 定义业务 Flag 补全的可选扩展契约。
//
// app 会在 ValidateFlags（如实现）前调用 Complete，用于派生字段补全、字符串规范化、切片去重排序等。
type FlagCompleter interface {
	// Complete 在 Flag 校验前执行，用于默认值填充与归一化。
	//
	// 返回：
	// - error: 补全过程中的语义错误；非 nil 时不构建 Runtime。
	Complete() error
}
