package config

import (
	"fmt"
	"strings"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// ErrorCode 是 config 模块统一错误码类型（字符串）。
type ErrorCode = errcode.StringCode

const (
	ErrCodeConfigInternal                      ErrorCode = "cfg.runtime.internal"
	ErrCodeConfigManagerClosed                 ErrorCode = "cfg.manager.closed"
	ErrCodeConfigRebuildNotSet                 ErrorCode = "cfg.manager.rebuild_not_set"
	ErrCodeConfigResolveFileFailed             ErrorCode = "cfg.file.resolve_failed"
	ErrCodeConfigLoadConfigFileFailed          ErrorCode = "cfg.file.load_failed"
	ErrCodeConfigWatchFailed                   ErrorCode = "cfg.watch.failed"
	ErrCodeConfigWatchRefreshFailed            ErrorCode = "cfg.watch.refresh_failed"
	ErrCodeConfigSnapshotBuildDefaultFailed    ErrorCode = "cfg.snapshot.build_default_failed"
	ErrCodeConfigSnapshotRemoteLoadFailed      ErrorCode = "cfg.snapshot.remote_load_failed"
	ErrCodeConfigSnapshotRemoteParseFailed     ErrorCode = "cfg.snapshot.remote_parse_failed"
	ErrCodeConfigManagerSnapshotValidateFailed ErrorCode = "cfg.manager.snapshot_validate_failed"
	ErrCodeConfigMapConvertFailed              ErrorCode = "cfg.map.convert_failed"
	ErrCodeConfigDecodeFailed                  ErrorCode = "cfg.decode.failed"
	// ErrCodeConfigDecodeScopeNotFound 表示调用方请求的非空 scope 在当前 Snapshot 中不存在。
	ErrCodeConfigDecodeScopeNotFound                ErrorCode = "cfg.decode.scope_not_found"
	ErrCodeConfigManagerRefreshRebuildFailed        ErrorCode = "cfg.manager.refresh_rebuild_failed"
	ErrCodeConfigManagerRefreshNotifyPrecheckFailed ErrorCode = "cfg.manager.refresh_notify_precheck_failed"
	ErrCodeConfigManagerRefreshSubscriberFailed     ErrorCode = "cfg.manager.refresh_subscriber_failed"
	ErrCodeConfigManagerSubscribeFailed             ErrorCode = "cfg.manager.subscribe_failed"
	ErrCodeConfigObserveFailed                      ErrorCode = "cfg.observe.failed"
	ErrCodeConfigManagerCloseWatcherFailed          ErrorCode = "cfg.manager.close_watcher_failed"
	// ErrCodeConfigManagerCloseResourceFailed 表示 Manager 释放 Provider Session 时失败。
	ErrCodeConfigManagerCloseResourceFailed ErrorCode = "cfg.manager.close_resource_failed"
	ErrCodeConfigConfigFileInvalid          ErrorCode = "cfg.file.invalid"
	ErrCodeConfigConfigFileDisabledConflict ErrorCode = "cfg.file.disabled_conflict"
	ErrCodeConfigBootstrapBlockInvalid      ErrorCode = "cfg.bootstrap.block_invalid"
	ErrCodeConfigSubscriberInvalid          ErrorCode = "cfg.subscriber.invalid"
	ErrCodeConfigSubscriberPanic            ErrorCode = "cfg.subscriber.panic"
	ErrCodeConfigDecodeTargetNil            ErrorCode = "cfg.decode.target_nil"
	ErrCodeConfigOutInvalid                 ErrorCode = "cfg.out.invalid"
	ErrCodeConfigOutPointerRequired         ErrorCode = "cfg.out.pointer_required"
	ErrCodeConfigDecodedPointerRequired     ErrorCode = "cfg.decode.decoded_pointer_required"
	ErrCodeConfigDecodedTypeMismatch        ErrorCode = "cfg.decode.decoded_type_mismatch"
	ErrCodeBootstrapUnknownKey              ErrorCode = "cfg.bootstrap.unknown_key"
	ErrCodeBootstrapInvalidType             ErrorCode = "cfg.bootstrap.invalid_type"
	ErrCodeBootstrapProviderInvalid         ErrorCode = "cfg.bootstrap.provider_invalid"
	// ErrCodeProviderNameRequired 表示 Provider 声明缺少稳定名称，发生在 Build 获取资源前。
	ErrCodeProviderNameRequired ErrorCode = "cfg.provider.name_required"
	// ErrCodeProviderDuplicate 表示同一 Builder 重复声明相同 Provider 名称。
	ErrCodeProviderDuplicate ErrorCode = "cfg.provider.duplicate"
	// ErrCodeProviderSessionInvalid 表示 Provider 返回的 Session 或 Source 拓扑不满足内核运行契约。
	ErrCodeProviderSessionInvalid ErrorCode = "cfg.provider.session_invalid"
)

func errorForConfigInternal(msg string, cause ...error) error {
	return buildError(ErrCodeConfigInternal, msg, cause...)
}

func errorForConfigManagerClosed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigManagerClosed, msg, cause...)
}

func errorForConfigRebuildNotSet(msg string, cause ...error) error {
	return buildError(ErrCodeConfigRebuildNotSet, msg, cause...)
}

func errorForConfigResolveFileFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigResolveFileFailed, msg, cause...)
}

func errorForConfigLoadConfigFileFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigLoadConfigFileFailed, msg, cause...)
}

func errorForConfigWatchFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigWatchFailed, msg, cause...)
}

func errorForConfigWatchRefreshFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigWatchRefreshFailed, msg, cause...)
}

func errorForConfigSnapshotBuildDefaultFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigSnapshotBuildDefaultFailed, msg, cause...)
}

func errorForConfigSnapshotRemoteLoadFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigSnapshotRemoteLoadFailed, msg, cause...)
}

func errorForConfigSnapshotRemoteParseFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigSnapshotRemoteParseFailed, msg, cause...)
}

func errorForConfigManagerSnapshotValidateFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigManagerSnapshotValidateFailed, msg, cause...)
}

func errorForConfigMapConvertFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigMapConvertFailed, msg, cause...)
}

func errorForConfigDecodeFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigDecodeFailed, msg, cause...)
}

func errorForConfigDecodeScopeNotFound(msg string, cause ...error) error {
	return buildError(ErrCodeConfigDecodeScopeNotFound, msg, cause...)
}

func errorForConfigManagerRefreshRebuildFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigManagerRefreshRebuildFailed, msg, cause...)
}

func errorForConfigManagerRefreshNotifyPrecheckFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigManagerRefreshNotifyPrecheckFailed, msg, cause...)
}

func errorForConfigManagerRefreshSubscriberFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigManagerRefreshSubscriberFailed, msg, cause...)
}

func errorForConfigManagerSubscribeFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigManagerSubscribeFailed, msg, cause...)
}

func errorForConfigObserveFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigObserveFailed, msg, cause...)
}

func errorForConfigManagerCloseWatcherFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigManagerCloseWatcherFailed, msg, cause...)
}

func errorForConfigManagerCloseResourceFailed(msg string, cause ...error) error {
	return buildError(ErrCodeConfigManagerCloseResourceFailed, msg, cause...)
}

func errorForConfigConfigFileInvalid(msg string, cause ...error) error {
	return buildError(ErrCodeConfigConfigFileInvalid, msg, cause...)
}

func errorForConfigConfigFileDisabledConflict(msg string, cause ...error) error {
	return buildError(ErrCodeConfigConfigFileDisabledConflict, msg, cause...)
}

func errorForConfigBootstrapBlockInvalid(msg string, cause ...error) error {
	return buildError(ErrCodeConfigBootstrapBlockInvalid, msg, cause...)
}

func errorForConfigSubscriberInvalid(msg string, cause ...error) error {
	return buildError(ErrCodeConfigSubscriberInvalid, msg, cause...)
}

func errorForConfigSubscriberPanic(msg string, cause ...error) error {
	return buildError(ErrCodeConfigSubscriberPanic, msg, cause...)
}

func errorForConfigDecodeTargetNil(msg string, cause ...error) error {
	return buildError(ErrCodeConfigDecodeTargetNil, msg, cause...)
}

func errorForConfigOutInvalid(msg string, cause ...error) error {
	return buildError(ErrCodeConfigOutInvalid, msg, cause...)
}

func errorForConfigOutPointerRequired(msg string, cause ...error) error {
	return buildError(ErrCodeConfigOutPointerRequired, msg, cause...)
}

func errorForConfigDecodedPointerRequired(msg string, cause ...error) error {
	return buildError(ErrCodeConfigDecodedPointerRequired, msg, cause...)
}

func errorForConfigDecodedTypeMismatch(msg string, cause ...error) error {
	return buildError(ErrCodeConfigDecodedTypeMismatch, msg, cause...)
}

func errorForBootstrapUnknownKey(msg string, cause ...error) error {
	return buildError(ErrCodeBootstrapUnknownKey, msg, cause...)
}

func errorForBootstrapInvalidType(msg string, cause ...error) error {
	return buildError(ErrCodeBootstrapInvalidType, msg, cause...)
}

func errorForBootstrapProviderInvalid(msg string, cause ...error) error {
	return buildError(ErrCodeBootstrapProviderInvalid, msg, cause...)
}

func errorForProviderNameRequired(msg string, cause ...error) error {
	return buildError(ErrCodeProviderNameRequired, msg, cause...)
}

func errorForProviderDuplicate(msg string, cause ...error) error {
	return buildError(ErrCodeProviderDuplicate, msg, cause...)
}

func errorForProviderSessionInvalid(msg string, cause ...error) error {
	return buildError(ErrCodeProviderSessionInvalid, msg, cause...)
}

// errorf 统一构造 config 模块阶段错误，并尽量继承底层错误码。
func errorf(format string, args ...any) error {
	msg := messagef(format, args...)
	cause := firstErrorInValues(args...)
	if cause == nil {
		return errorForConfigInternal(msg)
	}
	if code, ok := errcode.CodeOf[ErrorCode](cause); ok {
		return buildError(code, msg, cause)
	}
	return errorForConfigInternal(msg, cause)
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
