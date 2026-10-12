package webhertz

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// ErrorCode 是 web-hertz 模块稳定错误码类型。
type ErrorCode = errcode.StringCode

var (
	// ErrShutdownWaitInterrupted 标记 Shutdown 仅因当前调用者 context 结束而提前返回。
	// 典型场景：调用方使用 errors.Is 分支，避免把 caller timeout 误判为底层共享停止结果。
	ErrShutdownWaitInterrupted = errors.New("web-hertz shutdown wait interrupted")
)

const (
	// ErrCodeConfigInvalid 表示 Hertz typed Config 存在非法字段。
	ErrCodeConfigInvalid ErrorCode = "web_hertz.config.invalid"
	// ErrCodeDeclarationInvalid 表示构造 Option、Middleware 或 RouteFunc 声明非法。
	ErrCodeDeclarationInvalid ErrorCode = "web_hertz.declaration.invalid"
	// ErrCodeRegistryUnsupported 表示原生 Option 启用了当前生命周期不支持的 Registry。
	ErrCodeRegistryUnsupported ErrorCode = "web_hertz.registry.unsupported"
	// ErrCodeRouteRegistrationFailed 表示 RouteFunc 显式返回注册失败。
	ErrCodeRouteRegistrationFailed ErrorCode = "web_hertz.route.register_failed"
	// ErrCodeContextInvalid 表示 Run/Shutdown 收到 nil context。
	ErrCodeContextInvalid ErrorCode = "web_hertz.context.invalid"
	// ErrCodeStateInvalid 表示 Server 收到并发或重复 Run 等非法状态转换。
	ErrCodeStateInvalid ErrorCode = "web_hertz.state.invalid"
	// ErrCodeRunFailed 表示 Hertz 初始化、监听或运行阶段失败。
	ErrCodeRunFailed ErrorCode = "web_hertz.run.failed"
	// ErrCodeShutdownFailed 表示停止事务或当前调用者等待阶段失败。
	ErrCodeShutdownFailed ErrorCode = "web_hertz.shutdown.failed"
)

// errorForConfigInvalid 构造配置校验错误。
func errorForConfigInvalid(message string, cause ...error) error {
	return buildError(ErrCodeConfigInvalid, message, cause...)
}

// errorForDeclarationInvalid 构造声明校验错误。
func errorForDeclarationInvalid(message string, cause ...error) error {
	return buildError(ErrCodeDeclarationInvalid, message, cause...)
}

// errorForRegistryUnsupported 构造 Registry 能力拒绝错误。
func errorForRegistryUnsupported(message string, cause ...error) error {
	return buildError(ErrCodeRegistryUnsupported, message, cause...)
}

// errorForRouteRegistrationFailed 构造路由注册失败错误。
func errorForRouteRegistrationFailed(message string, cause ...error) error {
	return buildError(ErrCodeRouteRegistrationFailed, message, cause...)
}

// errorForContextInvalid 构造 context 输入错误。
func errorForContextInvalid(message string, cause ...error) error {
	return buildError(ErrCodeContextInvalid, message, cause...)
}

// errorForStateInvalid 构造生命周期状态错误。
func errorForStateInvalid(message string, cause ...error) error {
	return buildError(ErrCodeStateInvalid, message, cause...)
}

// errorForRunFailed 构造 Hertz 运行失败错误。
func errorForRunFailed(message string, cause ...error) error {
	return buildError(ErrCodeRunFailed, message, cause...)
}

// errorForShutdownFailed 构造停止或等待失败错误。
func errorForShutdownFailed(message string, cause ...error) error {
	return buildError(ErrCodeShutdownFailed, message, cause...)
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

// messagef 统一格式化错误消息并禁止产生双重 %w 包装。
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
