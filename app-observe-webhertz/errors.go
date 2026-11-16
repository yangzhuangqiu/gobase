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
	// ErrCodeMetricsServerConfigInvalid 表示 Metrics Server 监听配置不满足稳定约束。
	ErrCodeMetricsServerConfigInvalid ErrorCode = "app_observe_webhertz.metrics_server_config_invalid"
	// ErrCodeMetricsServerConfigLoadFailed 表示 ConfigManager 或配置 scope 读取失败。
	ErrCodeMetricsServerConfigLoadFailed ErrorCode = "app_observe_webhertz.metrics_server_config_load_failed"
	// ErrCodeMetricsServerListenFailed 表示 Runner 无法在业务 Server 启动前占用诊断端口。
	ErrCodeMetricsServerListenFailed ErrorCode = "app_observe_webhertz.metrics_server_listen_failed"
	// ErrCodeMetricsServerRunFailed 表示业务/Metrics 工作负载运行或关闭未正常收敛。
	ErrCodeMetricsServerRunFailed ErrorCode = "app_observe_webhertz.metrics_server_run_failed"
	// ErrCodeRunnerResolveFailed 表示 Runner 无法从 Runtime IOC 解析业务 Server 或 Metrics 计划。
	ErrCodeRunnerResolveFailed ErrorCode = "app_observe_webhertz.runner_resolve_failed"
)

// errorForDeclarationInvalid 构造 Option、依赖或能力组合声明错误。
func errorForDeclarationInvalid(message string, cause ...error) error {
	return buildError(ErrCodeDeclarationInvalid, message, cause...)
}

// errorForMetricsServerConfigInvalid 构造 Metrics Server 纯配置校验错误。
func errorForMetricsServerConfigInvalid(message string, cause ...error) error {
	return buildError(ErrCodeMetricsServerConfigInvalid, message, cause...)
}

// errorForMetricsServerConfigLoadFailed 构造配置快照和 scope 加载错误。
func errorForMetricsServerConfigLoadFailed(message string, cause ...error) error {
	return buildError(ErrCodeMetricsServerConfigLoadFailed, message, cause...)
}

// errorForMetricsServerListenFailed 构造 Metrics 端口预绑定错误。
func errorForMetricsServerListenFailed(message string, cause ...error) error {
	return buildError(ErrCodeMetricsServerListenFailed, message, cause...)
}

// errorForMetricsServerRunFailed 构造双工作负载运行和关闭错误。
func errorForMetricsServerRunFailed(message string, cause ...error) error {
	return buildError(ErrCodeMetricsServerRunFailed, message, cause...)
}

// errorForRunnerResolveFailed 构造 Runtime IOC 解析错误。
func errorForRunnerResolveFailed(message string, cause ...error) error {
	return buildError(ErrCodeRunnerResolveFailed, message, cause...)
}

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
