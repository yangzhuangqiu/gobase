package webhertz

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

// logDebug 记录 provider 调试事件；未注入实例 Logger 时兼容 Hertz 全局日志出口。
// 阶段角色：承接低频构造诊断，不改变构造成功或失败语义。
func logDebug(logger baselog.Logger, ctx context.Context, message string, fields ...baselog.Field) {
	if !isNilLogger(logger) {
		logger.Debug(ctx, message, fields...)
		return
	}
	hlog.SystemLogger().CtxDebugf(ctx, "%s", formatDiagnosticMessage(message, fields))
}

// logInfo 记录 provider 正常状态转换；未注入实例 Logger 时兼容 Hertz 全局日志出口。
// 阶段角色：生命周期状态发布必须先于对应完成信号。
func logInfo(logger baselog.Logger, ctx context.Context, message string, fields ...baselog.Field) {
	if !isNilLogger(logger) {
		logger.Info(ctx, message, fields...)
		return
	}
	hlog.SystemLogger().CtxInfof(ctx, "%s", formatDiagnosticMessage(message, fields))
}

// logError 记录 provider 失败或隔离分支；未注入实例 Logger 时兼容 Hertz 全局日志出口。
// 阶段角色：只报告运行期已发生的失败，不替代调用方对返回错误的处理。
func logError(logger baselog.Logger, ctx context.Context, message string, fields ...baselog.Field) {
	if !isNilLogger(logger) {
		logger.Error(ctx, message, fields...)
		return
	}
	hlog.SystemLogger().CtxErrorf(ctx, "%s", formatDiagnosticMessage(message, fields))
}

// formatDiagnosticMessage 把结构化字段降级为 hlog 可检索文本。
// 阶段角色：仅服务未注入 Logger 的 standalone 兼容路径。
func formatDiagnosticMessage(message string, fields []baselog.Field) string {
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(message))
	for _, field := range fields {
		if strings.TrimSpace(field.Key) == "" {
			continue
		}
		builder.WriteByte(' ')
		builder.WriteString(field.Key)
		builder.WriteByte('=')
		builder.WriteString(fmt.Sprint(field.Value))
	}
	return builder.String()
}

// isNilLogger 识别 nil interface 和携带可空具体类型的 typed nil。
// 阶段角色：统一保护 Option 声明与 standalone 降级分支。
func isNilLogger(logger baselog.Logger) bool {
	if logger == nil {
		return true
	}
	value := reflect.ValueOf(logger)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
