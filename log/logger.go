package log

import "context"

// Logger 定义业务只依赖事件语义、不感知具体日志框架的稳定记录契约。
//
// 典型调用场景:
// 1. 业务构造期由 LoggerFactory 获取组件 Logger，运行期记录结构化事件；
// 2. 请求链路传入 context，由装配层 ContextExtractor 补充白名单字段；
// 3. 高成本字段计算前通过 Enabled 判断对应级别是否启用。
type Logger interface {
	// Enabled 判断指定级别当前是否会被处理。
	Enabled(ctx context.Context, level Level) bool
	// Log 按显式级别记录结构化事件，适合自定义级别与基础设施调用。
	Log(ctx context.Context, level Level, msg string, fields ...Field)
	// Debug 记录开发与问题诊断事件。
	Debug(ctx context.Context, msg string, fields ...Field)
	// Info 记录正常业务状态转换事件。
	Info(ctx context.Context, msg string, fields ...Field)
	// Warn 记录降级、重试或可恢复异常事件。
	Warn(ctx context.Context, msg string, fields ...Field)
	// Error 记录操作失败事件；不会终止进程。
	Error(ctx context.Context, msg string, fields ...Field)
	// With 派生携带固定字段的不可变 Logger。
	With(fields ...Field) Logger
}
