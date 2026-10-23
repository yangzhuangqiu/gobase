package hlogzap

import (
	"fmt"
	"strings"

	"github.com/yangzhuangqiu/gobase/errcode"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
)

// ErrorCode 是 hlog-zap 模块统一错误码类型（字符串）。
type ErrorCode = errcode.StringCode

const (
	// ErrCodeLogInternal 沿用 log-zap 的运行期内部错误码，便于调用方跨适配层统一分支。
	ErrCodeLogInternal ErrorCode = logzap.ErrCodeLogInternal
	// ErrCodeLogModeInvalid 沿用 log-zap 的输出模式校验错误码。
	ErrCodeLogModeInvalid ErrorCode = logzap.ErrCodeLogModeInvalid
	// ErrCodeLogFormatInvalid 沿用 log-zap 的编码格式校验错误码。
	ErrCodeLogFormatInvalid ErrorCode = logzap.ErrCodeLogFormatInvalid
	// ErrCodeLogFileFilenameRequired 沿用 log-zap 的文件路径必填错误码。
	ErrCodeLogFileFilenameRequired ErrorCode = logzap.ErrCodeLogFileFilenameRequired
	// ErrCodeLogLevelInvalid 沿用 log-zap 的日志级别校验错误码。
	ErrCodeLogLevelInvalid ErrorCode = logzap.ErrCodeLogLevelInvalid
	// ErrCodeProviderInvalid 表示共享 log-zap Provider 声明缺失。
	ErrCodeProviderInvalid ErrorCode = "hlog_zap.provider.invalid"
	// ErrCodeInstallConflict 表示进程中已有尚未恢复的 hlog-zap 租约。
	ErrCodeInstallConflict ErrorCode = "hlog_zap.install.conflict"
)

// errorForInstallConflict 构造进程级 hlog 租约冲突错误。
// 阶段角色：在修改全局 Logger 前拒绝第二个并行 owner。
func errorForInstallConflict(msg string, cause ...error) error {
	return buildError(ErrCodeInstallConflict, msg, cause...)
}

// errorForProviderInvalid 构造共享 Provider 声明错误。
func errorForProviderInvalid(msg string, cause ...error) error {
	return buildError(ErrCodeProviderInvalid, msg, cause...)
}

// errorForLogLevelInvalid 构造 level 非法错误。
func errorForLogLevelInvalid(msg string, cause ...error) error {
	return buildError(ErrCodeLogLevelInvalid, msg, cause...)
}

// buildError 统一按错误码和可选 cause 构造错误。
func buildError(code ErrorCode, msg string, cause ...error) error {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "code=" + string(code)
	}
	for _, item := range cause {
		if item == nil {
			continue
		}
		return errcode.Wrapf[ErrorCode](item, code, "%s", msg)
	}
	return errcode.New[ErrorCode](code, msg)
}

// messagef 统一格式化错误消息，禁止使用 %w 产生双重 wrap 语义。
func messagef(format string, args ...any) string {
	format = strings.TrimSpace(strings.ReplaceAll(format, "%w", "%v"))
	if format == "" {
		return ""
	}
	if len(args) == 0 {
		return format
	}
	return strings.TrimSpace(fmt.Sprintf(format, args...))
}
