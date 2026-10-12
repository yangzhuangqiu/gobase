package zap

import (
	"io"
	"os"
	"strings"

	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// buildZapWriteSyncer 构建 zap 输出目标。
// 阶段角色：在 console/file 两种模式之间选择 stdout 或 lumberjack。
func buildZapWriteSyncer(config Config) (zapcore.WriteSyncer, io.Closer, error) {
	if config.Mode == ModeConsole {
		return zapcore.AddSync(os.Stdout), nil, nil
	}
	rotator := &lumberjack.Logger{
		Filename:   config.Rotate.Filename,
		MaxSize:    config.Rotate.MaxSize,
		MaxBackups: config.Rotate.MaxBackups,
		MaxAge:     config.Rotate.MaxAge,
		Compress:   config.Rotate.Compress,
		LocalTime:  config.Rotate.LocalTime,
	}
	closer, _ := interface{}(rotator).(io.Closer)
	return zapcore.AddSync(rotator), closer, nil
}

// buildZapEncoder 根据 format 构建 json/text 编码器。
func buildZapEncoder(format string) zapcore.Encoder {
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

// isIgnorableSyncError 判断 Sync 错误是否可忽略（主要针对 stdout/stderr 平台差异）。
func isIgnorableSyncError(err error) bool {
	if err == nil {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "invalid argument") || strings.Contains(message, "bad file descriptor")
}
