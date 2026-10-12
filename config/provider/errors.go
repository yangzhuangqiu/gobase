package provider

import (
	"fmt"
	"strings"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// ErrorCode 是 provider 子包统一错误码类型（字符串）。
type ErrorCode = errcode.StringCode

const (
	ErrCodeProviderInternal                   ErrorCode = "cfg.provider.internal"
	ErrCodeProviderBootstrapSchemaTypeInvalid ErrorCode = "cfg.provider.bootstrap_schema_type_invalid"
)

func errorForProviderInternal(msg string, cause ...error) error {
	return buildError(ErrCodeProviderInternal, msg, cause...)
}

func errorForProviderBootstrapSchemaTypeInvalid(msg string, cause ...error) error {
	return buildError(ErrCodeProviderBootstrapSchemaTypeInvalid, msg, cause...)
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
