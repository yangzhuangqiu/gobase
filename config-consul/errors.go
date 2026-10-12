package consul

import (
	"fmt"
	"strings"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// ErrorCode 是 config-consul 模块统一错误码类型（字符串）。
type ErrorCode = errcode.StringCode

const (
	ErrCodeConsulInternal              ErrorCode = "cfg.consul.internal"
	ErrCodeConsulInvalidBootstrap      ErrorCode = "cfg.consul.bootstrap.invalid"
	ErrCodeConsulInvalidBinding        ErrorCode = "cfg.consul.binding.invalid"
	ErrCodeConsulInvalidSource         ErrorCode = "cfg.consul.source.invalid"
	ErrCodeConsulInvalidValueType      ErrorCode = "cfg.consul.value.invalid_type"
	ErrCodeConsulSDKInvalidInput       ErrorCode = "cfg.consul.sdk.invalid_input"
	ErrCodeConsulSDKClientCreateFailed ErrorCode = "cfg.consul.sdk.client_create_failed"
)

func errorForConsulInternal(msg string, cause ...error) error {
	return buildError(ErrCodeConsulInternal, msg, cause...)
}

func errorForConsulInvalidBootstrap(msg string, cause ...error) error {
	return buildError(ErrCodeConsulInvalidBootstrap, msg, cause...)
}

func errorForConsulInvalidBinding(msg string, cause ...error) error {
	return buildError(ErrCodeConsulInvalidBinding, msg, cause...)
}

func errorForConsulInvalidSource(msg string, cause ...error) error {
	return buildError(ErrCodeConsulInvalidSource, msg, cause...)
}

func errorForConsulInvalidValueType(msg string, cause ...error) error {
	return buildError(ErrCodeConsulInvalidValueType, msg, cause...)
}

func errorForConsulSDKInvalidInput(msg string, cause ...error) error {
	return buildError(ErrCodeConsulSDKInvalidInput, msg, cause...)
}

func errorForConsulSDKClientCreateFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConsulSDKClientCreateFailed, msg, cause...)
}

// errorf 统一构造 config-consul 阶段错误，并尽量继承底层错误码。
func errorf(format string, args ...any) error {
	msg := messagef(format, args...)
	cause := firstErrorInValues(args...)
	if cause == nil {
		return errorForConsulInternal(msg)
	}
	if code, ok := errcode.CodeOf[ErrorCode](cause); ok {
		return buildError(code, msg, cause)
	}
	return errorForConsulInternal(msg, cause)
}

// messagef 统一把 format/args 规范化为单条消息文本。
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

// buildError 统一应用两条约束：
// 1) msg 为空时自动补默认文案；
// 2) cause 只取第一个非 nil。
func buildError(code ErrorCode, msg string, cause ...error) error {
	finalMsg := strings.TrimSpace(msg)
	if finalMsg == "" {
		finalMsg = "code=" + string(code)
	}
	rootCause := firstNonNilCause(cause...)
	if rootCause == nil {
		return errcode.New[ErrorCode](code, finalMsg)
	}
	return errcode.Wrapf[ErrorCode](rootCause, code, "%s", finalMsg)
}

func firstNonNilCause(cause ...error) error {
	for _, current := range cause {
		if current != nil {
			return current
		}
	}
	return nil
}

func firstErrorInValues(values ...any) error {
	for _, current := range values {
		typed, ok := current.(error)
		if ok && typed != nil {
			return typed
		}
	}
	return nil
}
