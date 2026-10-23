package apphlogzap

import (
	"fmt"
	"strings"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// ErrorCode 是 hlog-zap/app 对外稳定的字符串错误码类型。
type ErrorCode = errcode.StringCode

const (
	// ErrCodeBuildFailed 表示 Zap Provider 或进程级 hlog 租约无法完整交付。
	ErrCodeBuildFailed ErrorCode = "hlog_zap.app.build_failed"
	// ErrCodeCloseFailed 表示 hlog 恢复或底层日志 Provider 关闭不完整。
	ErrCodeCloseFailed ErrorCode = "hlog_zap.app.close_failed"
)

// errorForBuildFailed 构造应用日志与 hlog 原子装配失败错误。
// 阶段角色：确保调用方只观察到完整 Provider，半成品在返回前回滚。
func errorForBuildFailed(format string, args ...any) error {
	return errorf(ErrCodeBuildFailed, format, args...)
}

// errorForCloseFailed 构造 hlog 恢复与 Provider 关闭聚合错误。
// 阶段角色：Runtime.Close 可以按模块错误码识别日志收口失败。
func errorForCloseFailed(format string, args ...any) error {
	return errorf(ErrCodeCloseFailed, format, args...)
}

// errorf 以稳定错误码包装参数中的首个 cause。
// 阶段角色：本模块错误语义进入 errcode 的唯一边界。
func errorf(code ErrorCode, format string, args ...any) error {
	message := strings.TrimSpace(fmt.Sprintf(strings.ReplaceAll(format, "%w", "%v"), args...))
	for _, value := range args {
		if cause, ok := value.(error); ok && cause != nil {
			return errcode.Wrapf[ErrorCode](cause, code, "%s", message)
		}
	}
	return errcode.New[ErrorCode](code, message)
}
