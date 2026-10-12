package app

import "errors"

// FlagBinder 定义业务 Flag 声明与字段绑定契约。
//
// 典型调用场景：
// 1. 应用装配层通过 WithFlagBinder 或 Command.FlagBinder 注入 root/子命令的 FlagBinder；
// 2. 构建命令树时由 app 调用 Bind 注册业务 Flag；
// 3. 解析完成后由 app 执行可选扩展：Complete（如实现）、ValidateFlags（如实现）。
type FlagBinder interface {
	// Bind 向当前命令的 Flag 集合注册业务参数并绑定普通 Flag 的目标字段。
	//
	// 参数：
	// - flags: 当前命令对应的 Flag 声明能力，调用方应只注册本命令语义相关参数。
	//   语义约束：影响配置合并的参数使用 Config*；普通命令参数使用无前缀方法。
	Bind(flags CommandFlags)
}

// applyFlagRules 对一个 FlagBinder 执行可选 Complete 与 ValidateFlags 规则。
// 阶段角色：Flag 解析后、Runtime Build 前完成参数归一化与 fail-fast 校验。
func (this *App) applyFlagRules(binder FlagBinder) error {
	if binder == nil {
		return nil
	}
	var errs []error
	if completer, ok := binder.(FlagCompleter); ok {
		if err := completer.Complete(); err != nil {
			errs = append(errs, errorf("stage=complete_flags: %w", err))
		}
	}
	if validator, ok := binder.(FlagValidator); ok {
		for _, err := range validator.ValidateFlags() {
			if err != nil {
				errs = append(errs, errorf("stage=validate_flags: %w", err))
			}
		}
	}
	return errors.Join(errs...)
}
