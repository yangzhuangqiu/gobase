package ioc

import (
	"fmt"
	"strings"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// ErrorCode 是 ioc 模块统一错误码类型。
type ErrorCode = errcode.StringCode

const (
	ErrCodeIOCInternal             ErrorCode = "ioc.internal"
	ErrCodeIOCContainerClosed      ErrorCode = "ioc.container.closed"
	ErrCodeIOCContainerNotBuilt    ErrorCode = "ioc.container.not_built"
	ErrCodeIOCRegisterInvalidBean  ErrorCode = "ioc.register.invalid_bean"
	ErrCodeIOCRegisterDuplicate    ErrorCode = "ioc.register.duplicate_name"
	ErrCodeIOCRegisterInvalidDecl  ErrorCode = "ioc.register.invalid_declare"
	ErrCodeIOCBuildDependencyMiss  ErrorCode = "ioc.build.dependency_not_found"
	ErrCodeIOCBuildDependencyMulti ErrorCode = "ioc.build.dependency_ambiguous"
	ErrCodeIOCBuildCircular        ErrorCode = "ioc.build.circular_dependency"
	ErrCodeIOCBuildInitFailed      ErrorCode = "ioc.build.init_failed"
	ErrCodeIOCResolveInvalidTarget ErrorCode = "ioc.resolve.invalid_target"
	ErrCodeIOCResolveNotFound      ErrorCode = "ioc.resolve.not_found"
	ErrCodeIOCResolveAmbiguous     ErrorCode = "ioc.resolve.ambiguous"
	ErrCodeIOCCloseFailed          ErrorCode = "ioc.close.failed"
)

// buildError 统一按错误码和可选 cause 构造错误。
func buildError(code ErrorCode, msg string, cause ...error) error {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "code=" + string(code)
	}
	for _, item := range cause {
		if item == nil {
			continue
		}
		return errcode.Wrapf[ErrorCode](item, code, "%s", msg)
	}
	return errcode.New[ErrorCode](code, msg)
}

// messagef 统一格式化错误消息，避免 %w 叠加 wrap。
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

// errorForIOCInternal 构造容器内部状态或不变量错误。
func errorForIOCInternal(msg string, cause ...error) error {
	return buildError(ErrCodeIOCInternal, msg, cause...)
}

// errorForIOCContainerClosed 构造容器已关闭语义错误。
func errorForIOCContainerClosed(msg string, cause ...error) error {
	return buildError(ErrCodeIOCContainerClosed, msg, cause...)
}

// errorForIOCContainerNotBuilt 构造容器未构建语义错误。
func errorForIOCContainerNotBuilt(msg string, cause ...error) error {
	return buildError(ErrCodeIOCContainerNotBuilt, msg, cause...)
}

// errorForIOCRegisterInvalidBean 构造注册 bean 非法错误。
func errorForIOCRegisterInvalidBean(msg string, cause ...error) error {
	return buildError(ErrCodeIOCRegisterInvalidBean, msg, cause...)
}

// errorForIOCRegisterDuplicate 构造注册名称冲突错误。
func errorForIOCRegisterDuplicate(msg string, cause ...error) error {
	return buildError(ErrCodeIOCRegisterDuplicate, msg, cause...)
}

// errorForIOCRegisterInvalidDecl 构造声明类型非法错误。
func errorForIOCRegisterInvalidDecl(msg string, cause ...error) error {
	return buildError(ErrCodeIOCRegisterInvalidDecl, msg, cause...)
}

// errorForIOCBuildDependencyMiss 构造依赖缺失错误。
func errorForIOCBuildDependencyMiss(msg string, cause ...error) error {
	return buildError(ErrCodeIOCBuildDependencyMiss, msg, cause...)
}

// errorForIOCBuildDependencyMulti 构造依赖歧义错误。
func errorForIOCBuildDependencyMulti(msg string, cause ...error) error {
	return buildError(ErrCodeIOCBuildDependencyMulti, msg, cause...)
}

// errorForIOCBuildCircular 构造循环依赖错误。
func errorForIOCBuildCircular(msg string, cause ...error) error {
	return buildError(ErrCodeIOCBuildCircular, msg, cause...)
}

// errorForIOCBuildInitFailed 构造初始化失败错误。
func errorForIOCBuildInitFailed(msg string, cause ...error) error {
	return buildError(ErrCodeIOCBuildInitFailed, msg, cause...)
}

// errorForIOCResolveInvalidTarget 构造解析目标非法错误。
func errorForIOCResolveInvalidTarget(msg string, cause ...error) error {
	return buildError(ErrCodeIOCResolveInvalidTarget, msg, cause...)
}

// errorForIOCResolveNotFound 构造解析目标不存在错误。
func errorForIOCResolveNotFound(msg string, cause ...error) error {
	return buildError(ErrCodeIOCResolveNotFound, msg, cause...)
}

// errorForIOCResolveAmbiguous 构造解析目标歧义错误。
func errorForIOCResolveAmbiguous(msg string, cause ...error) error {
	return buildError(ErrCodeIOCResolveAmbiguous, msg, cause...)
}

// errorForIOCCloseFailed 构造容器关闭失败错误。
func errorForIOCCloseFailed(msg string, cause ...error) error {
	return buildError(ErrCodeIOCCloseFailed, msg, cause...)
}
