package hlogzap

import (
	"fmt"
	"strings"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// ErrorCode 是 hlog-zap 模块统一错误码类型（字符串）。
type ErrorCode = errcode.StringCode

const (
	ErrCodeLogInternal             ErrorCode = "log.runtime.internal"
	ErrCodeLogModeInvalid          ErrorCode = "log.mode.invalid"
	ErrCodeLogFormatInvalid        ErrorCode = "log.format.invalid"
	ErrCodeLogFileFilenameRequired ErrorCode = "log.file.filename_required"
	ErrCodeLogLevelInvalid         ErrorCode = "log.level.invalid"
	// ErrCodeProviderInvalid 表示共享 log-zap Provider 声明缺失。
	ErrCodeProviderInvalid ErrorCode = "hlog_zap.provider.invalid"
)

// errorForProviderInvalid 构造共享 Provider 声明错误。
func errorForProviderInvalid(msg string, cause ...error) error {
	return buildError(ErrCodeProviderInvalid, msg, cause...)
}

// errorForLogModeInvalid 构造 mode 非法错误。
func errorForLogModeInvalid(msg string, cause ...error) error {
	return buildError(ErrCodeLogModeInvalid, msg, cause...)
}

// errorForLogFormatInvalid 构造 format 非法错误。
func errorForLogFormatInvalid(msg string, cause ...error) error {
	return buildError(ErrCodeLogFormatInvalid, msg, cause...)
}

// errorForLogFileFilenameRequired 构造 file 模式缺少文件名错误。
func errorForLogFileFilenameRequired(msg string, cause ...error) error {
	return buildError(ErrCodeLogFileFilenameRequired, msg, cause...)
}

// errorForLogInternal 构造运行期内部错误。
func errorForLogInternal(msg string, cause ...error) error {
	return buildError(ErrCodeLogInternal, msg, cause...)
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
