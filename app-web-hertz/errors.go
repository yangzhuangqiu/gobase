package appwebhertz

import (
	"fmt"
	"strings"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// ErrorCode 是 app-web-hertz 对外稳定的字符串错误码类型。
type ErrorCode = errcode.StringCode

const (
	// ErrCodeConfigInvalid 表示 Snapshot 解码后的标准配置语义非法。
	ErrCodeConfigInvalid ErrorCode = "app_web_hertz.config_invalid"
	// ErrCodeConfigLoadFailed 表示配置快照、scope 解码或 provider 配置校验失败。
	ErrCodeConfigLoadFailed ErrorCode = "app_web_hertz.config_load_failed"
	// ErrCodeDeclarationInvalid 表示依赖、应用组件或 Option 声明非法。
	ErrCodeDeclarationInvalid ErrorCode = "app_web_hertz.declaration_invalid"
	// ErrCodeServerBuildFailed 表示 web-hertz Server 构建失败。
	ErrCodeServerBuildFailed ErrorCode = "app_web_hertz.server_build_failed"
	// ErrCodeModuleRegisterFailed 表示 IOC Module 无法注册托管 Server bean。
	ErrCodeModuleRegisterFailed ErrorCode = "app_web_hertz.module_register_failed"
	// ErrCodeServerStateInvalid 表示托管 Server 在 IOC Init 完成前被调用。
	ErrCodeServerStateInvalid ErrorCode = "app_web_hertz.server_state_invalid"
	// ErrCodeServerResolveFailed 表示 App Runner 无法从 Runtime IOC 解析唯一 web.Server。
	ErrCodeServerResolveFailed ErrorCode = "app_web_hertz.server_resolve_failed"
)

// errorForConfigInvalid 构造配置解析和组合校验错误。
func errorForConfigInvalid(message string, cause ...error) error {
	return buildError(ErrCodeConfigInvalid, message, cause...)
}

// errorForConfigLoadFailed 构造配置加载阶段错误。
func errorForConfigLoadFailed(message string, cause ...error) error {
	return buildError(ErrCodeConfigLoadFailed, message, cause...)
}

// errorForDeclarationInvalid 构造应用装配声明错误。
func errorForDeclarationInvalid(message string, cause ...error) error {
	return buildError(ErrCodeDeclarationInvalid, message, cause...)
}

// errorForServerBuildFailed 构造 web-hertz Server 构建阶段错误。
func errorForServerBuildFailed(message string, cause ...error) error {
	return buildError(ErrCodeServerBuildFailed, message, cause...)
}

// errorForModuleRegisterFailed 构造 IOC Module 注册阶段错误。
func errorForModuleRegisterFailed(message string, cause ...error) error {
	return buildError(ErrCodeModuleRegisterFailed, message, cause...)
}

// errorForServerStateInvalid 构造托管 Server 尚未初始化的状态错误。
func errorForServerStateInvalid(message string, cause ...error) error {
	return buildError(ErrCodeServerStateInvalid, message, cause...)
}

// errorForServerResolveFailed 构造 App Runner 的 Server 解析错误。
func errorForServerResolveFailed(message string, cause ...error) error {
	return buildError(ErrCodeServerResolveFailed, message, cause...)
}

// buildError 统一构造带稳定 code 和可选 cause 的阶段错误。
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

// messagef 统一格式化应用装配错误消息。
func messagef(format string, arguments ...any) string {
	format = strings.TrimSpace(strings.ReplaceAll(format, "%w", "%v"))
	if format == "" {
		return ""
	}
	if len(arguments) == 0 {
		return format
	}
	return strings.TrimSpace(fmt.Sprintf(format, arguments...))
}
