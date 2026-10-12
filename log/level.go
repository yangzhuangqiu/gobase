package log

import "log/slog"

// Level 表示跨 provider 稳定的日志严重程度。
// 数值与 slog.Level 对齐，便于根门面无损映射到标准 Handler。
type Level int8

const (
	// LevelDebug 用于开发阶段与问题诊断事件。
	LevelDebug Level = -4
	// LevelInfo 用于正常业务状态转换事件，也是推荐默认阈值。
	LevelInfo Level = 0
	// LevelWarn 用于降级、重试或可恢复异常事件。
	LevelWarn Level = 4
	// LevelError 用于操作失败事件；根契约不定义终止进程的 Fatal。
	LevelError Level = 8
)

// slogLevel 把公共 Level 映射到标准事件级别。
// 阶段角色：Logger 在 Enabled 与构造 Record 前统一完成级别转换。
func (this Level) slogLevel() slog.Level {
	return slog.Level(this)
}
