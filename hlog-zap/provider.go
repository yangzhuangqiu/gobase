package hlogzap

import (
	"context"
	"io"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
	"go.uber.org/zap"
)

const (
	// providerLoggerName 是共享 Provider 中 Hertz 事件使用的稳定 logger 维度。
	providerLoggerName = "hertz"
)

// providerLogger 把 Hertz hlog 调用适配到 log-zap Provider 的动态 Core。
//
// 语义:
// 1. 与业务 Logger 共享 Provider 当前 level、encoder、writer 和关闭状态；
// 2. 不拥有 Provider，不执行 Sync/Close，也不允许 SetOutput 替换共享 writer；
// 3. Hertz Notice 映射 Warn，Trace 映射 Debug，Fatal 保留 Zap 终止语义。
type providerLogger struct {
	// provider 是动态 level 的更新入口和全部底层资源的唯一所有者。
	provider *logzap.Provider
	// logger 是绑定 Provider 动态 Core、Hertz 名称和固定 caller 边界的 Zap Logger。
	logger *zap.SugaredLogger
}

var _ hlog.FullLogger = (*providerLogger)(nil)

// NewFromProvider 从既有 log-zap Provider 创建借用型 Hertz FullLogger。
//
// 参数:
// - provider: 必填；其生命周期必须覆盖返回 Logger 及使用该 Logger 的 Hertz Host。
//
// 返回:
// - hlog.FullLogger: 与业务日志共享 Provider 底层的借用适配器；
// - error: provider 为 nil 时返回稳定声明错误，不修改 Hertz 全局 Logger。
func NewFromProvider(provider *logzap.Provider) (hlog.FullLogger, error) {
	if provider == nil {
		return nil, errorForProviderInvalid("op=hlog_zap.new_from_provider result=failed stage=validate reason=provider_required")
	}
	logger := zap.New(
		provider.Core(),
		zap.AddCaller(),
		zap.AddCallerSkip(hlogCallerSkip),
	).With(zap.String("logger", providerLoggerName))
	return &providerLogger{provider: provider, logger: logger.Sugar()}, nil
}

// Trace 以 Debug 兼容级别记录非格式化 Hertz 事件。
func (this *providerLogger) Trace(values ...interface{}) {
	this.log(hlog.LevelTrace, values...)
}

// Debug 记录非格式化 Hertz 调试事件。
func (this *providerLogger) Debug(values ...interface{}) {
	this.log(hlog.LevelDebug, values...)
}

// Info 记录非格式化 Hertz 正常事件。
func (this *providerLogger) Info(values ...interface{}) {
	this.log(hlog.LevelInfo, values...)
}

// Notice 以 Warn 兼容级别记录非格式化 Hertz 通知事件。
func (this *providerLogger) Notice(values ...interface{}) {
	this.log(hlog.LevelNotice, values...)
}

// Warn 记录非格式化 Hertz 警告事件。
func (this *providerLogger) Warn(values ...interface{}) {
	this.log(hlog.LevelWarn, values...)
}

// Error 记录非格式化 Hertz 错误事件。
func (this *providerLogger) Error(values ...interface{}) {
	this.log(hlog.LevelError, values...)
}

// Fatal 记录非格式化 Hertz 致命事件并保留 Zap Fatal 终止语义。
func (this *providerLogger) Fatal(values ...interface{}) {
	this.log(hlog.LevelFatal, values...)
}

// Tracef 以 Debug 兼容级别记录格式化 Hertz 事件。
func (this *providerLogger) Tracef(format string, values ...interface{}) {
	this.logf(hlog.LevelTrace, format, values...)
}

// Debugf 记录格式化 Hertz 调试事件。
func (this *providerLogger) Debugf(format string, values ...interface{}) {
	this.logf(hlog.LevelDebug, format, values...)
}

// Infof 记录格式化 Hertz 正常事件。
func (this *providerLogger) Infof(format string, values ...interface{}) {
	this.logf(hlog.LevelInfo, format, values...)
}

// Noticef 以 Warn 兼容级别记录格式化 Hertz 通知事件。
func (this *providerLogger) Noticef(format string, values ...interface{}) {
	this.logf(hlog.LevelNotice, format, values...)
}

// Warnf 记录格式化 Hertz 警告事件。
func (this *providerLogger) Warnf(format string, values ...interface{}) {
	this.logf(hlog.LevelWarn, format, values...)
}

// Errorf 记录格式化 Hertz 错误事件。
func (this *providerLogger) Errorf(format string, values ...interface{}) {
	this.logf(hlog.LevelError, format, values...)
}

// Fatalf 记录格式化 Hertz 致命事件并保留 Zap Fatal 终止语义。
func (this *providerLogger) Fatalf(format string, values ...interface{}) {
	this.logf(hlog.LevelFatal, format, values...)
}

// CtxTracef 接收 hlog context 并记录 Trace 兼容事件；共享 Core 不隐式提取 context 字段。
func (this *providerLogger) CtxTracef(_ context.Context, format string, values ...interface{}) {
	this.logf(hlog.LevelTrace, format, values...)
}

// CtxDebugf 接收 hlog context 并记录调试事件；共享 Core 不隐式提取 context 字段。
func (this *providerLogger) CtxDebugf(_ context.Context, format string, values ...interface{}) {
	this.logf(hlog.LevelDebug, format, values...)
}

// CtxInfof 接收 hlog context 并记录正常事件；共享 Core 不隐式提取 context 字段。
func (this *providerLogger) CtxInfof(_ context.Context, format string, values ...interface{}) {
	this.logf(hlog.LevelInfo, format, values...)
}

// CtxNoticef 接收 hlog context 并记录 Notice 兼容事件；共享 Core 不隐式提取 context 字段。
func (this *providerLogger) CtxNoticef(_ context.Context, format string, values ...interface{}) {
	this.logf(hlog.LevelNotice, format, values...)
}

// CtxWarnf 接收 hlog context 并记录警告事件；共享 Core 不隐式提取 context 字段。
func (this *providerLogger) CtxWarnf(_ context.Context, format string, values ...interface{}) {
	this.logf(hlog.LevelWarn, format, values...)
}

// CtxErrorf 接收 hlog context 并记录错误事件；共享 Core 不隐式提取 context 字段。
func (this *providerLogger) CtxErrorf(_ context.Context, format string, values ...interface{}) {
	this.logf(hlog.LevelError, format, values...)
}

// CtxFatalf 接收 hlog context 并记录致命事件；共享 Core 不隐式提取字段且保留 Zap Fatal 语义。
func (this *providerLogger) CtxFatalf(_ context.Context, format string, values ...interface{}) {
	this.logf(hlog.LevelFatal, format, values...)
}

// SetLevel 把 Hertz 全局级别更新映射为共享 Provider 的动态级别更新。
func (this *providerLogger) SetLevel(level hlog.Level) {
	_ = this.provider.ApplyRuntimeConfig(logzap.RuntimeConfig{Level: providerLevel(level)})
}

// SetOutput 保持 Provider 输出资源所有权，不允许 Hertz 绕过 Provider 替换 writer。
func (this *providerLogger) SetOutput(_ io.Writer) {}

// log 按 Hertz level 分派非格式化 Zap 调用。
// 阶段角色：统一 Trace/Notice 兼容映射并保持 Fatal 行为。
func (this *providerLogger) log(level hlog.Level, values ...interface{}) {
	// Hertz 的七档级别在此归一化到 Provider 支持的四档 Zap 语义。
	switch level {
	case hlog.LevelTrace, hlog.LevelDebug:
		this.logger.Debug(values...)
	case hlog.LevelInfo:
		this.logger.Info(values...)
	case hlog.LevelNotice, hlog.LevelWarn:
		this.logger.Warn(values...)
	case hlog.LevelError:
		this.logger.Error(values...)
	case hlog.LevelFatal:
		this.logger.Fatal(values...)
	default:
		this.logger.Warn(values...)
	}
}

// logf 按 Hertz level 分派格式化 Zap 调用。
// 阶段角色：Ctx 与非 Ctx 格式化入口共享唯一消息和级别映射。
func (this *providerLogger) logf(level hlog.Level, format string, values ...interface{}) {
	// Ctx 与非 Ctx 入口到达此处后共享相同的最终级别分派。
	switch level {
	case hlog.LevelTrace, hlog.LevelDebug:
		this.logger.Debugf(format, values...)
	case hlog.LevelInfo:
		this.logger.Infof(format, values...)
	case hlog.LevelNotice, hlog.LevelWarn:
		this.logger.Warnf(format, values...)
	case hlog.LevelError:
		this.logger.Errorf(format, values...)
	case hlog.LevelFatal:
		this.logger.Fatalf(format, values...)
	default:
		this.logger.Warnf(format, values...)
	}
}

// providerLevel 把 Hertz level 映射为 log-zap 支持的动态级别文本。
// 阶段角色：根日志不支持独立 Trace/Notice/Fatal 阈值，分别归一化为 Debug/Warn/Error。
func providerLevel(level hlog.Level) string {
	switch level {
	case hlog.LevelTrace, hlog.LevelDebug:
		return "debug"
	case hlog.LevelInfo:
		return "info"
	case hlog.LevelNotice, hlog.LevelWarn:
		return "warn"
	case hlog.LevelError, hlog.LevelFatal:
		return "error"
	default:
		return "warn"
	}
}
