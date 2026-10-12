package app

import (
	"fmt"
	"strings"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// ErrorCode 是 app 模块统一错误码类型（字符串）。
type ErrorCode = errcode.StringCode

const (
	// ErrCodeAppInternal 表示 app 内部不可归入稳定业务分支的错误。
	ErrCodeAppInternal ErrorCode = "app.internal"
	// ErrCodeAppRunnerRequired 表示 root 命令缺少必填 Runner。
	ErrCodeAppRunnerRequired ErrorCode = "app.lifecycle.runner_required"
	// ErrCodeAppCommandRunnerRequired 表示叶子子命令缺少必填 Runner。
	ErrCodeAppCommandRunnerRequired        ErrorCode = "app.lifecycle.command_runner_required"
	ErrCodeAppCLIInvalidExpression         ErrorCode = "app.cli.invalid_expression"
	ErrCodeAppCLIEmptyKey                  ErrorCode = "app.cli.empty_key"
	ErrCodeAppCLIInvalidBootstrapKey       ErrorCode = "app.cli.invalid_bootstrap_key"
	ErrCodeAppCLIEmptyConfigFile           ErrorCode = "app.cli.empty_config_file"
	ErrCodeAppCLIEmptyBootstrapEnvPrefix   ErrorCode = "app.cli.empty_bootstrap_env_prefix"
	ErrCodeAppCLIFlagReadFailed            ErrorCode = "app.cli.flag_read_failed"
	ErrCodeAppCLIFlagLookupFailed          ErrorCode = "app.cli.flag_lookup_failed"
	ErrCodeAppCLIFlagTypeUnsupported       ErrorCode = "app.cli.flag_type_unsupported"
	ErrCodeAppCLIConflict                  ErrorCode = "app.cli.conflict"
	ErrCodeAppListBootstrapKeysInvalidMode ErrorCode = "app.cli.list_bootstrap_keys_invalid_mode"
)

// stageError 给生命周期错误补充稳定 stage 标签，便于排障。
func stageError(stage string, err error) error {
	if err == nil {
		return nil
	}
	return errorf("op=app.command result=failed stage=%s reason=%w", stage, err)
}

func errorForAppInternal(msg string, cause ...error) error {
	return buildError(ErrCodeAppInternal, msg, cause...)
}

func errorForAppRunnerRequired(msg string, cause ...error) error {
	return buildError(ErrCodeAppRunnerRequired, msg, cause...)
}

func errorForAppCommandRunnerRequired(msg string, cause ...error) error {
	return buildError(ErrCodeAppCommandRunnerRequired, msg, cause...)
}

func errorForAppCLIInvalidExpression(msg string, cause ...error) error {
	return buildError(ErrCodeAppCLIInvalidExpression, msg, cause...)
}

func errorForAppCLIEmptyKey(msg string, cause ...error) error {
	return buildError(ErrCodeAppCLIEmptyKey, msg, cause...)
}

func errorForAppCLIInvalidBootstrapKey(msg string, cause ...error) error {
	return buildError(ErrCodeAppCLIInvalidBootstrapKey, msg, cause...)
}

func errorForAppCLIEmptyConfigFile(msg string, cause ...error) error {
	return buildError(ErrCodeAppCLIEmptyConfigFile, msg, cause...)
}

func errorForAppCLIEmptyBootstrapEnvPrefix(msg string, cause ...error) error {
	return buildError(ErrCodeAppCLIEmptyBootstrapEnvPrefix, msg, cause...)
}

func errorForAppCLIFlagReadFailed(msg string, cause ...error) error {
	return buildError(ErrCodeAppCLIFlagReadFailed, msg, cause...)
}

func errorForAppCLIFlagLookupFailed(msg string, cause ...error) error {
	return buildError(ErrCodeAppCLIFlagLookupFailed, msg, cause...)
}

func errorForAppCLIFlagTypeUnsupported(msg string, cause ...error) error {
	return buildError(ErrCodeAppCLIFlagTypeUnsupported, msg, cause...)
}

func errorForAppCLIConflict(msg string, cause ...error) error {
	return buildError(ErrCodeAppCLIConflict, msg, cause...)
}

func errorForAppListBootstrapKeysInvalidMode(msg string, cause ...error) error {
	return buildError(ErrCodeAppListBootstrapKeysInvalidMode, msg, cause...)
}

// errorf 统一构造 app 模块阶段错误，并尽量继承底层错误码。
func errorf(format string, args ...any) error {
	msg := messagef(format, args...)
	cause := firstErrorInValues(args...)
	if cause == nil {
		return errorForAppInternal(msg)
	}
	if code, ok := errcode.CodeOf[ErrorCode](cause); ok {
		return buildError(code, msg, cause)
	}
	return errorForAppInternal(msg, cause)
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
