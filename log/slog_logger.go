package log

import (
	"context"
	"log/slog"
	"runtime"
	"time"
)

const (
	// loggerCallerSkip 跳过 runtime.Callers、emit 和公共 Logger 方法三层，定位业务调用点。
	// 该值是根门面实现细节，由 caller 契约测试锁定，不允许 provider 配置覆盖。
	loggerCallerSkip = 3
)

// eventLogger 是新 Logger 契约的私有 slog 实现。
// 阶段角色：在公共接口切换前并行验证事件、caller 与字段语义。
type eventLogger struct {
	// inner 持有已绑定命名与固定字段的标准 slog Logger。
	inner *slog.Logger
}

// newEventLogger 使用指定 Handler 创建根事件 Logger。
// 前置条件：handler 已由 factory 完成 nil 降级。
func newEventLogger(handler slog.Handler) *eventLogger {
	return &eventLogger{inner: slog.New(handler)}
}

// Enabled 判断指定级别当前是否会被 Handler 处理。
func (this *eventLogger) Enabled(ctx context.Context, level Level) bool {
	return this.inner.Enabled(normalizeLogContext(ctx), level.slogLevel())
}

// Log 按显式级别记录结构化事件。
func (this *eventLogger) Log(ctx context.Context, level Level, msg string, fields ...Field) {
	this.emit(ctx, level, msg, fields)
}

// Debug 记录开发与问题诊断事件。
func (this *eventLogger) Debug(ctx context.Context, msg string, fields ...Field) {
	this.emit(ctx, LevelDebug, msg, fields)
}

// Info 记录正常业务状态转换事件。
func (this *eventLogger) Info(ctx context.Context, msg string, fields ...Field) {
	this.emit(ctx, LevelInfo, msg, fields)
}

// Warn 记录降级、重试或可恢复异常事件。
func (this *eventLogger) Warn(ctx context.Context, msg string, fields ...Field) {
	this.emit(ctx, LevelWarn, msg, fields)
}

// Error 记录操作失败事件，但不终止进程。
func (this *eventLogger) Error(ctx context.Context, msg string, fields ...Field) {
	this.emit(ctx, LevelError, msg, fields)
}

// With 派生携带固定字段的事件 Logger。
// 字段结构会立即转换并由 Handler.WithAttrs 持有；Field.Value 中的引用值不做深复制。
func (this *eventLogger) With(fields ...Field) Logger {
	attrs := fieldsToSlogAttrs(fields)
	if len(attrs) == 0 {
		return this
	}
	return newEventLogger(this.inner.Handler().WithAttrs(attrs))
}

// emit 构造标准 Record 并交给 Handler。
//
// 流程：
// 1. 先检查 Enabled，禁用级别不转换字段；
// 2. 捕获业务调用点并构造 Record；
// 3. 转换字段后同步交给 Handler。
func (this *eventLogger) emit(ctx context.Context, level Level, msg string, fields []Field) {
	ctx = normalizeLogContext(ctx)
	slogLevel := level.slogLevel()
	if !this.inner.Enabled(ctx, slogLevel) {
		return
	}

	// 事件已启用后再承担 caller 与字段转换成本。
	var callers [1]uintptr
	runtime.Callers(loggerCallerSkip, callers[:])
	record := slog.NewRecord(time.Now(), slogLevel, msg, callers[0])
	record.AddAttrs(fieldsToSlogAttrs(fields)...)
	_ = this.inner.Handler().Handle(ctx, record)
}

// normalizeLogContext 统一 nil context 的日志语义。
// nil 仅表示本条事件没有调用链字段，不创建或替换业务生命周期 context。
func normalizeLogContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
