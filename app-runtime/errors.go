package appruntime

import (
	"fmt"
	"strings"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// ErrorCode 是 app-runtime 的稳定错误码类型。
type ErrorCode = errcode.StringCode

const (
	// ErrCodeRuntimeBuildFailed 表示 config/log/ioc 构建阶段失败。
	ErrCodeRuntimeBuildFailed ErrorCode = "app_runtime.build.failed"
	// ErrCodeRuntimeCloseFailed 表示运行时资源关闭阶段失败。
	ErrCodeRuntimeCloseFailed ErrorCode = "app_runtime.close.failed"
	// ErrCodeModuleInvalid 表示 Module 声明无效、回调为空或名称重复。
	ErrCodeModuleInvalid ErrorCode = "app_runtime.module.invalid"
	// ErrCodeBeanInvalid 表示简单 bean 声明为 nil 或 typed nil。
	ErrCodeBeanInvalid ErrorCode = "app_runtime.bean.invalid"
	// ErrCodeLoggingInvalid 表示 Logging 声明携带 typed nil。
	ErrCodeLoggingInvalid ErrorCode = "app_runtime.logging.invalid"
)

// errorForBuildFailed 构造运行时构建错误。
func errorForBuildFailed(msg string, cause ...error) error {
	return buildError(ErrCodeRuntimeBuildFailed, msg, cause...)
}

// errorForCloseFailed 构造运行时关闭错误。
func errorForCloseFailed(msg string, cause ...error) error {
	return buildError(ErrCodeRuntimeCloseFailed, msg, cause...)
}

// errorForModuleInvalid 构造 Module 声明错误。
func errorForModuleInvalid(msg string, cause ...error) error {
	return buildError(ErrCodeModuleInvalid, msg, cause...)
}

// errorForBeanInvalid 构造简单 bean 声明错误。
func errorForBeanInvalid(msg string, cause ...error) error {
	return buildError(ErrCodeBeanInvalid, msg, cause...)
}

// errorForLoggingInvalid 构造 Logging 声明错误。
func errorForLoggingInvalid(msg string, cause ...error) error {
	return buildError(ErrCodeLoggingInvalid, msg, cause...)
}

// buildError 创建可分支错误并保留首个底层原因。
func buildError(code ErrorCode, msg string, cause ...error) error {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "code=" + string(code)
	}
	for _, item := range cause {
		if item != nil {
			return errcode.Wrapf[ErrorCode](item, code, "%s", msg)
		}
	}
	return errcode.New[ErrorCode](code, msg)
}

// messagef 构造单行语义消息，并把 %w 归一化为普通格式化占位符。
func messagef(format string, args ...any) string {
	format = strings.TrimSpace(strings.ReplaceAll(format, "%w", "%v"))
	if len(args) == 0 {
		return format
	}
	return strings.TrimSpace(fmt.Sprintf(format, args...))
}
