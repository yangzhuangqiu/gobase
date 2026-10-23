package webhertzobserve

import (
	"fmt"
	"strings"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// ErrorCode 是 Web Hertz 观测适配对外稳定的字符串错误码类型。
type ErrorCode = errcode.StringCode

const (
	// ErrCodeDeclarationInvalid 表示 Option、Registry、Provider/Propagator 或 Observer 声明非法。
	ErrCodeDeclarationInvalid ErrorCode = "app_observe_webhertz.declaration_invalid"
	// ErrCodeRegistrationFailed 表示 Prometheus Collector 注册或注销事务失败。
	ErrCodeRegistrationFailed ErrorCode = "app_observe_webhertz.registration_failed"
	// ErrCodeModuleRegisterFailed 表示 IOC Module 无法发布适配 bean。
	ErrCodeModuleRegisterFailed ErrorCode = "app_observe_webhertz.module_register_failed"
)

// buildError 构造带稳定 code、阶段语义和可选 cause 的错误。
func buildError(code ErrorCode, message string, cause ...error) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "code=" + string(code)
	}
	for _, item := range cause {
		if item != nil {
			return errcode.Wrapf[ErrorCode](item, code, "%s", message)
		}
	}
	return errcode.New[ErrorCode](code, message)
}

// messagef 统一格式化适配器错误文本并阻止误用 %w。
func messagef(format string, arguments ...any) string {
	format = strings.TrimSpace(strings.ReplaceAll(format, "%w", "%v"))
	if len(arguments) == 0 {
		return format
	}
	return strings.TrimSpace(fmt.Sprintf(format, arguments...))
}
