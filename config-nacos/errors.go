package nacos

import (
	"fmt"
	"strings"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// ErrorCode 是 config-nacos 模块统一错误码类型（字符串）。
type ErrorCode = errcode.StringCode

const (
	ErrCodeNacosInternal              ErrorCode = "cfg.nacos.internal"
	ErrCodeNacosInvalidBootstrap      ErrorCode = "cfg.nacos.bootstrap.invalid"
	ErrCodeNacosInvalidBinding        ErrorCode = "cfg.nacos.binding.invalid"
	ErrCodeNacosInvalidSource         ErrorCode = "cfg.nacos.source.invalid"
	ErrCodeNacosInvalidValueType      ErrorCode = "cfg.nacos.value.invalid_type"
	ErrCodeNacosSDKInvalidInput       ErrorCode = "cfg.nacos.sdk.invalid_input"
	ErrCodeNacosSDKClientCreateFailed ErrorCode = "cfg.nacos.sdk.client_create_failed"
)

func errorForNacosInternal(msg string, cause ...error) error {
	return buildError(ErrCodeNacosInternal, msg, cause...)
}

func errorForNacosInvalidBootstrap(msg string, cause ...error) error {
	return buildError(ErrCodeNacosInvalidBootstrap, msg, cause...)
}

func errorForNacosInvalidBinding(msg string, cause ...error) error {
	return buildError(ErrCodeNacosInvalidBinding, msg, cause...)
}

func errorForNacosInvalidSource(msg string, cause ...error) error {
	return buildError(ErrCodeNacosInvalidSource, msg, cause...)
}

func errorForNacosInvalidValueType(msg string, cause ...error) error {
	return buildError(ErrCodeNacosInvalidValueType, msg, cause...)
}

func errorForNacosSDKInvalidInput(msg string, cause ...error) error {
	return buildError(ErrCodeNacosSDKInvalidInput, msg, cause...)
}

func errorForNacosSDKClientCreateFailed(msg string, cause ...error) error {
	return buildError(ErrCodeNacosSDKClientCreateFailed, msg, cause...)
}

// errorf 统一构造 config-nacos 阶段错误，并尽量继承底层错误码。
func errorf(format string, args ...any) error {
	msg := messagef(format, args...)
	cause := firstErrorInValues(args...)
	if cause == nil {
		return errorForNacosInternal(msg)
	}
	if code, ok := errcode.CodeOf[ErrorCode](cause); ok {
		return buildError(code, msg, cause)
	}
	return errorForNacosInternal(msg, cause)
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
