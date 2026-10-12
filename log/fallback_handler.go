package log

import (
	"log/slog"
	"os"
)

// newFallbackHandler 创建 provider 缺失时的标准 text Handler。
// 语义：默认 INFO、写入 stderr、保留 source，确保开箱可见且不污染业务 stdout。
func newFallbackHandler() slog.Handler {
	return slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	})
}
