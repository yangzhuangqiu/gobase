package applogzap

import (
	"fmt"
	"strings"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// ErrorCode 是 app-log-zap 对外稳定的字符串错误码类型。
type ErrorCode = errcode.StringCode

const (
	// ErrCodeBootstrapBuildFailed 表示 Bootstrap 无法交付可用日志 Provider。
	ErrCodeBootstrapBuildFailed ErrorCode = "app_log_zap.bootstrap.build_failed"
	// ErrCodeProviderSubscribeFailed 表示适配器无法取得 ConfigManager subscription。
	ErrCodeProviderSubscribeFailed ErrorCode = "app_log_zap.provider.subscribe_failed"
	// ErrCodeProviderCoreBuildFailed 表示适配器已取得配置、但无法构建 Zap 核心。
	ErrCodeProviderCoreBuildFailed ErrorCode = "app_log_zap.provider.core_build_failed"
	// ErrCodeProviderRuntimeConfigApplyFailed 表示动态配置无法安全应用到既有 Zap 核心。
	ErrCodeProviderRuntimeConfigApplyFailed ErrorCode = "app_log_zap.provider.runtime_config_apply_failed"
	// ErrCodeProviderCloseFailed 表示组合 Provider 无法完整刷新或释放 Zap 核心资源。
	ErrCodeProviderCloseFailed ErrorCode = "app_log_zap.provider.close_failed"
)

// errorForBootstrapBuildFailed 构造 Bootstrap 交付失败错误。
// 阶段角色：app-runtime Logging.Build 契约的统一失败出口。
func errorForBootstrapBuildFailed(format string, args ...any) error {
	return errorf(ErrCodeBootstrapBuildFailed, format, args...)
}

// errorForProviderSubscribeFailed 构造配置订阅获取失败错误。
// 阶段角色：组合 Provider 第一资源获取阶段的失败出口。
func errorForProviderSubscribeFailed(format string, args ...any) error {
	return errorf(ErrCodeProviderSubscribeFailed, format, args...)
}

// errorForProviderCoreBuildFailed 构造 Zap 核心创建失败错误。
// 阶段角色：组合 Provider 第二资源获取及订阅补偿阶段的失败出口。
func errorForProviderCoreBuildFailed(format string, args ...any) error {
	return errorf(ErrCodeProviderCoreBuildFailed, format, args...)
}

// errorForProviderRuntimeConfigApplyFailed 构造动态配置应用失败错误。
// 阶段角色：ConfigManager typed update 到 Zap RuntimeConfig 的通知边界。
func errorForProviderRuntimeConfigApplyFailed(format string, args ...any) error {
	return errorf(ErrCodeProviderRuntimeConfigApplyFailed, format, args...)
}

// errorForProviderCloseFailed 构造组合 Provider 关闭失败错误。
// 阶段角色：日志资源所有权归还阶段的稳定错误出口。
func errorForProviderCloseFailed(format string, args ...any) error {
	return errorf(ErrCodeProviderCloseFailed, format, args...)
}

// errorf 使用指定模块错误码包装格式化参数中的首个 cause。
// 阶段角色：根包所有错误进入 errcode 体系的唯一构造边界。
func errorf(code ErrorCode, format string, args ...any) error {
	message := messagef(format, args...)
	if message == "" {
		message = "code=" + string(code)
	}
	cause := firstErrorInValues(args...)
	if cause == nil {
		return errcode.New[ErrorCode](code, message)
	}
	return errcode.Wrapf[ErrorCode](cause, code, "%s", message)
}

// messagef 归一化单行错误消息，避免 %w 与 errcode 重复承担包装语义。
// 阶段角色：错误消息进入稳定错误载体前的格式化阶段。
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

// firstErrorInValues 返回格式化参数中的首个非 nil error。
// 阶段角色：在不改变消息参数顺序的前提下保留底层 cause 链。
func firstErrorInValues(values ...any) error {
	for _, current := range values {
		typed, ok := current.(error)
		if ok && typed != nil {
			return typed
		}
	}
	return nil
}
