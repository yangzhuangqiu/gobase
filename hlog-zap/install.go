package hlogzap

import (
	"os"
	"strings"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	hertzzap "github.com/hertz-contrib/logger/zap"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	// hlogCallerSkip 是 hlog/Zap 适配包装层到业务调用点的固定跳层数。
	//
	// 语义：
	// 1. hlog 适配边界自行锁定 caller，不再把 skip 作为业务配置；
	// 2. 该值由 caller 测试约束，避免框架内部文件污染调用位置。
	hlogCallerSkip = 3
)

// Install 是 Hertz 场景下替换 hlog 核心的唯一推荐入口。
//
// 典型调用场景:
// 1. 业务装配层在服务启动早期读取 `log.zap` 配置后调用一次；
// 2. 替换后，Hertz 框架内部日志与业务 logger 可共享同一日志语义。
//
// 返回:
// - *hertzzap.Logger: hertz-contrib logger 实例，供调用方按需执行 Sync 或高级定制。
func Install(config Config) (*hertzzap.Logger, error) {
	logger, err := NewLogger(config)
	if err != nil {
		return nil, err
	}
	hlog.SetLogger(logger)
	return logger, nil
}

// NewLogger 基于统一 `log.zap` 配置构建 hlog 可用的 zap logger。
//
// 语义:
// 1. 入参直接复用 log-zap 配置对象；
// 2. 仅构建 logger，不修改全局 hlog 状态；
// 3. 默认值、合并与校验统一复用 log-zap ResolveConfig。
func NewLogger(config Config) (*hertzzap.Logger, error) {
	resolved, err := logzap.ResolveConfig(logzap.Config(config))
	if err != nil {
		return nil, err
	}
	level, err := parseZapLevel(resolved.Level)
	if err != nil {
		return nil, err
	}
	return buildLogger(resolved, level), nil
}

// buildLogger 构建 hertz-contrib zap logger。
//
// 流程:
// 1. 基于已解析配置构建 encoder/writeSyncer/level；
// 2. 应用 hlog 适配边界固定 caller；
// 3. 返回不修改全局状态的 hertzzap Logger。
func buildLogger(config logzap.Config, level zapcore.Level) *hertzzap.Logger {
	options := make([]hertzzap.Option, 0, 6)
	options = append(options, hertzzap.WithCoreEnc(buildEncoder(config.Format)))
	options = append(options, hertzzap.WithCoreWs(buildWriteSyncer(config)))
	options = append(options, hertzzap.WithCoreLevel(zap.NewAtomicLevelAt(level)))
	if !config.DisableCaller {
		zapOptions := []zap.Option{zap.AddCaller(), zap.AddCallerSkip(hlogCallerSkip)}
		options = append(options, hertzzap.WithZapOptions(zapOptions...))
	}
	return hertzzap.NewLogger(options...)
}

// buildWriteSyncer 构建日志输出目标（stdout 或 lumberjack 文件轮转）。
func buildWriteSyncer(config logzap.Config) zapcore.WriteSyncer {
	if config.Mode == ModeConsole {
		return zapcore.AddSync(os.Stdout)
	}
	rotator := &lumberjack.Logger{
		Filename:   config.Rotate.Filename,
		MaxSize:    config.Rotate.MaxSize,
		MaxBackups: config.Rotate.MaxBackups,
		MaxAge:     config.Rotate.MaxAge,
		Compress:   config.Rotate.Compress,
		LocalTime:  config.Rotate.LocalTime,
	}
	return zapcore.AddSync(rotator)
}

// buildEncoder 构建 json/text 编码器。
func buildEncoder(format string) zapcore.Encoder {
	config := zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		TimeKey:        "ts",
		NameKey:        "name",
		CallerKey:      "caller",
		FunctionKey:    "func",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	if format == FormatText {
		return zapcore.NewConsoleEncoder(config)
	}
	return zapcore.NewJSONEncoder(config)
}

// parseZapLevel 解析级别并映射到 zapcore.Level。
func parseZapLevel(raw string) (zapcore.Level, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", "info", "notice":
		return zapcore.InfoLevel, nil
	case "trace", "debug":
		// trace 仅作为 Hertz 既有配置的 Debug 兼容别名，不扩展 Zap 级别模型。
		return zapcore.DebugLevel, nil
	case "warn", "warning":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	default:
		return zapcore.InfoLevel, errorForLogLevelInvalid(messagef("invalid level=%s", raw))
	}
}
