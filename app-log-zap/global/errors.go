package global

import (
	"fmt"
	"strings"

	applogzap "github.com/yangzhuangqiu/gobase/app-log-zap"
	"github.com/yangzhuangqiu/gobase/errcode"
)

// ErrorCode 是 global 子包对外稳定的字符串错误码类型。
type ErrorCode = applogzap.ErrorCode

const (
	// ErrCodeGlobalManagerBuildFailed 表示全局入口无法创建其独占的 ConfigManager。
	ErrCodeGlobalManagerBuildFailed ErrorCode = "app_log_zap.global.manager_build_failed"
	// ErrCodeGlobalProviderBuildFailed 表示全局入口无法基于 ConfigManager 创建组合 Provider。
	ErrCodeGlobalProviderBuildFailed ErrorCode = "app_log_zap.global.provider_build_failed"
	// ErrCodeGlobalFallbackBuildFailed 表示配置路径降级后仍无法创建纯 Zap Provider。
	ErrCodeGlobalFallbackBuildFailed ErrorCode = "app_log_zap.global.fallback_build_failed"
	// ErrCodeGlobalCloseFailed 表示全局 Provider 或其 ConfigManager 未能完整关闭。
	ErrCodeGlobalCloseFailed ErrorCode = "app_log_zap.global.close_failed"
)

// errorForGlobalManagerBuildFailed 构造全局 ConfigManager 创建失败错误。
// 阶段角色：global bootstrap 第一资源获取阶段的稳定错误出口。
func errorForGlobalManagerBuildFailed(format string, args ...any) error {
	return errorf(ErrCodeGlobalManagerBuildFailed, format, args...)
}

// errorForGlobalProviderBuildFailed 构造全局组合 Provider 创建失败错误。
// 阶段角色：global bootstrap 第二资源获取及 Manager 回滚阶段的稳定错误出口。
func errorForGlobalProviderBuildFailed(format string, args ...any) error {
	return errorf(ErrCodeGlobalProviderBuildFailed, format, args...)
}

// errorForGlobalFallbackBuildFailed 构造全局降级 Provider 创建失败错误。
// 阶段角色：ConfigManager 路径失败后的最终日志出口准备阶段。
func errorForGlobalFallbackBuildFailed(format string, args ...any) error {
	return errorf(ErrCodeGlobalFallbackBuildFailed, format, args...)
}

// errorForGlobalCloseFailed 构造全局资源关闭失败错误。
// 阶段角色：Provider 与 ConfigManager 聚合关闭事务的稳定错误出口。
func errorForGlobalCloseFailed(format string, args ...any) error {
	return errorf(ErrCodeGlobalCloseFailed, format, args...)
}

// errorf 使用 global 错误码包装格式化参数中的首个 cause。
// 阶段角色：global 子包所有错误进入 errcode 体系的唯一构造边界。
func errorf(code ErrorCode, format string, args ...any) error {
	msg := messagef(format, args...)
	if msg == "" {
		msg = "code=" + string(code)
	}
	cause := firstErrorInValues(args...)
	if cause == nil {
		return errcode.New[ErrorCode](code, msg)
	}
	return errcode.Wrapf[ErrorCode](cause, code, "%s", msg)
}

// messagef 统一格式化错误消息，避免 %w 产生双重包装语义。
// 阶段角色：全局错误消息进入 errcode 前的单行归一化。
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

// firstErrorInValues 从格式化参数中提取第一个 error。
// 阶段角色：errorf 保留原始 cause 的参数扫描阶段。
func firstErrorInValues(values ...any) error {
	for _, current := range values {
		typed, ok := current.(error)
		if ok && typed != nil {
			return typed
		}
	}
	return nil
}
