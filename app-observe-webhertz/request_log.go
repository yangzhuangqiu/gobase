package webhertzobserve

import (
	"context"
	"reflect"
	"strings"

	appwebhertz "github.com/yangzhuangqiu/gobase/app-web-hertz"
	baselog "github.com/yangzhuangqiu/gobase/log"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

const (
	// defaultRequestLoggerName 是根 LoggerFactory 下稳定的请求事件组件名。
	defaultRequestLoggerName = "web.hertz.request"
)

// RequestLogEnricher 定义在结构化请求事件中追加应用白名单字段的 typed 策略。
type RequestLogEnricher func(ctx context.Context, observation webhertz.HTTPRequestObservation) []baselog.Field

// RequestLogOption 定义 Request Log Observer 的纯构造选项。
type RequestLogOption func(options *requestLogOptions) error

// requestLogOptions 保存一次 Observer 构造的不可变策略。
type requestLogOptions struct {
	// loggerName 是 LoggerFactory 中的稳定组件名。
	loggerName string
	// enricher 在日志级别启用后生成额外白名单字段。
	enricher RequestLogEnricher
}

// requestLogObserver 把完成态快照映射为根日志结构化事件。
type requestLogObserver struct {
	// logger 是借用 Factory 得到的稳定事件出口。
	logger baselog.Logger
	// enricher 是可选应用字段策略。
	enricher RequestLogEnricher
}

var _ appwebhertz.RequestLogObserver = (*requestLogObserver)(nil)

// WithRequestLoggerName 设置请求日志组件名；空白值回退稳定默认值。
func WithRequestLoggerName(name string) RequestLogOption {
	return func(options *requestLogOptions) error {
		options.loggerName = strings.TrimSpace(name)
		return nil
	}
}

// WithRequestLogEnricher 设置应用白名单字段扩展；nil 在构造期 fail-fast。
func WithRequestLogEnricher(enricher RequestLogEnricher) RequestLogOption {
	return func(options *requestLogOptions) error {
		if enricher == nil {
			return buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.request_log result=failed stage=declare_enricher reason=enricher_required")
		}
		options.enricher = enricher
		return nil
	}
}

// NewRequestLogObserver 基于 borrowed LoggerFactory 创建无资源 Request Log 适配器。
//
// 参数:
// - factory: 根日志工厂，必填且所有权不转移；
// - options: Logger 名称和可选字段扩展策略。
//
// 返回:
// - RequestLogObserver: 并发安全且不修改全局 Logger；
// - error: 依赖或 Option 声明非法。
func NewRequestLogObserver(factory baselog.LoggerFactory, options ...RequestLogOption) (appwebhertz.RequestLogObserver, error) {
	if isNilValue(factory) {
		return nil, buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.request_log result=failed stage=precheck reason=logger_factory_required")
	}
	// 第一阶段：只执行一次 Option 并冻结纯值策略。
	resolved, err := resolveRequestLogOptions(options)
	if err != nil {
		return nil, err
	}
	return newRequestLogObserver(factory, resolved)
}

// newRequestLogObserver 从已冻结策略创建借用 Logger 的适配器，不重放 Option。
func newRequestLogObserver(factory baselog.LoggerFactory, resolved requestLogOptions) (appwebhertz.RequestLogObserver, error) {
	if isNilValue(factory) {
		return nil, buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.request_log result=failed stage=precheck reason=logger_factory_required")
	}
	return &requestLogObserver{logger: factory.Get(resolved.loggerName), enricher: resolved.enricher}, nil
}

// resolveRequestLogOptions 解析纯构造策略，供显式构造和 Module 注册前校验共用。
func resolveRequestLogOptions(options []RequestLogOption) (requestLogOptions, error) {
	resolved := requestLogOptions{loggerName: defaultRequestLoggerName}
	// 顺序应用纯构造 Option；nil 或策略错误不得产生 Logger。
	for index, option := range options {
		if option == nil {
			return requestLogOptions{}, buildError(ErrCodeDeclarationInvalid, messagef("op=app_observe_webhertz.request_log result=failed stage=resolve_options reason=option_required option_index=%d", index))
		}
		if err := option(&resolved); err != nil {
			return requestLogOptions{}, err
		}
	}
	if resolved.loggerName == "" {
		resolved.loggerName = defaultRequestLoggerName
	}
	return resolved, nil
}

// ObserveRequestLog 按最终 outcome 和 slow 标记选择级别，并记录稳定字段集合。
func (this *requestLogObserver) ObserveRequestLog(ctx context.Context, observation webhertz.HTTPRequestObservation, slow bool) {
	level := baselog.LevelInfo
	result := "success"
	reason := "completed"
	if observation.Outcome != webhertz.RequestOutcomeSuccess {
		result = "failed"
		reason = string(observation.Outcome)
	}
	if slow && observation.Outcome == webhertz.RequestOutcomeSuccess {
		level = baselog.LevelWarn
		reason = "slow"
	}
	if observation.Outcome == webhertz.RequestOutcomeServerError || observation.Outcome == webhertz.RequestOutcomePanic {
		level = baselog.LevelError
	}
	if !this.logger.Enabled(ctx, level) {
		return
	}

	// 基础字段固定且不包含 query、Header、Body、Cookie、Authorization、cause 或 Stack。
	fields := []baselog.Field{
		baselog.F("event", "http_request"), baselog.F("op", "web_hertz.request"), baselog.F("result", result), baselog.F("reason", reason),
		baselog.F("request_id", observation.RequestID), baselog.F("trace_id", observation.TraceID), baselog.F("method", observation.Method),
		baselog.F("route", observation.RoutePattern), baselog.F("path", observation.Path), baselog.F("status", observation.Status),
		baselog.F("duration_ms", observation.Duration.Milliseconds()), baselog.F("request_bytes", observation.RequestBytes),
		baselog.F("response_bytes", observation.ResponseBytes), baselog.F("outcome", observation.Outcome), baselog.F("slow", slow),
	}
	fields = append(fields, this.enrichedFields(ctx, observation)...)
	this.logger.Log(ctx, level, "http_request", fields...)
}

// enrichedFields 隔离 Enricher panic，并过滤空键和稳定保留字段，避免覆盖核心搜索合同。
func (this *requestLogObserver) enrichedFields(ctx context.Context, observation webhertz.HTTPRequestObservation) (result []baselog.Field) {
	if this.enricher == nil {
		return nil
	}
	defer func() {
		if recover() != nil {
			result = nil
			this.logger.Error(ctx, "request_log_enricher_failed",
				baselog.F("op", "app_observe_webhertz.request_log"), baselog.F("result", "failed"), baselog.F("reason", "enricher_panic"),
			)
		}
	}()
	// Enricher 只允许追加非保留白名单键，核心请求合同不能被覆盖。
	reserved := requestLogReservedFieldKeys
	for _, field := range this.enricher(ctx, observation) {
		key := strings.TrimSpace(field.Key)
		if key == "" {
			continue
		}
		if _, exists := reserved[key]; exists {
			continue
		}
		result = append(result, field)
	}
	return result
}

// requestLogReservedFieldKeys 定义完成态日志核心合同，Enricher 只能追加其外字段。
var requestLogReservedFieldKeys = map[string]struct{}{
	"event": {}, "op": {}, "result": {}, "reason": {}, "request_id": {}, "trace_id": {}, "method": {}, "route": {},
	"path": {}, "status": {}, "duration_ms": {}, "request_bytes": {}, "response_bytes": {}, "outcome": {}, "slow": {},
}

// isNilValue 统一识别依赖 interface 中的 typed nil。
func isNilValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
