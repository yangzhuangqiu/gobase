package appwebhertz

import (
	"context"
	"strings"
	"time"

	baselog "github.com/yangzhuangqiu/gobase/log"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

const (
	// accessLoggerName 是根 LoggerFactory 下稳定、低基数的 HTTP Access Log 组件名。
	accessLoggerName = "web.hertz.request"
)

// AccessLogConfig 描述完成态请求日志的启用和应用侧过滤策略；默认关闭。
type AccessLogConfig struct {
	// Enabled 决定是否把完成态快照写入根 LoggerFactory；默认 false。
	Enabled bool `yaml:"enabled"`
	// SkipPaths 是不写访问日志的精确请求路径；不支持通配符且不得包含 query/fragment。
	SkipPaths []string `yaml:"skipPaths"`
	// SlowThreshold 是慢请求判定阈值；零值表示不区分慢请求，负值非法。
	SlowThreshold time.Duration `yaml:"slowThreshold"`
}

// resolveAccessLogConfig 校验慢请求阈值并把跳过路径冻结为去重的精确路径集合。
func resolveAccessLogConfig(input AccessLogConfig) (AccessLogConfig, error) {
	if input.SlowThreshold < 0 {
		return AccessLogConfig{}, errorForConfigInvalid("op=app_web_hertz.resolve_config result=failed stage=validate_access_log reason=slow_threshold_negative")
	}
	paths := make([]string, 0, len(input.SkipPaths))
	for _, raw := range input.SkipPaths {
		path := strings.TrimSpace(raw)
		if path == "" || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
			return AccessLogConfig{}, errorForConfigInvalid("op=app_web_hertz.resolve_config result=failed stage=validate_access_log reason=skip_path_invalid")
		}
		paths = append(paths, path)
	}
	input.SkipPaths = deduplicateStrings(paths)
	return input, nil
}

// accessLogObserver 把 Hertz 完成态快照映射为 app-web-hertz 自有的结构化访问日志。
type accessLogObserver struct {
	// logger 是从 Runtime 根 LoggerFactory 借用的稳定日志出口。
	logger baselog.Logger
	// skipPaths 保存请求期只读的精确路径集合。
	skipPaths map[string]struct{}
	// slowThreshold 为零时不标记慢请求，单位为纳秒。
	slowThreshold int64
}

// newAccessLogObserver 在 Server 构造期冻结 Access Log 策略并借用 Logger。
// 阶段角色：仅在 accessLog.enabled=true 时构造标准请求日志 Observer。
func newAccessLogObserver(logger baselog.Logger, config AccessLogConfig) (webhertz.RequestObserver, error) {
	if isNilInterface(logger) {
		return nil, errorForDeclarationInvalid("op=app_web_hertz.middleware result=failed stage=build_access_log reason=logger_required")
	}
	skipPaths := make(map[string]struct{}, len(config.SkipPaths))
	for _, path := range config.SkipPaths {
		skipPaths[path] = struct{}{}
	}
	return &accessLogObserver{
		logger:        logger,
		skipPaths:     skipPaths,
		slowThreshold: config.SlowThreshold.Nanoseconds(),
	}, nil
}

// Observe 过滤跳过路径，计算最终 outcome/slow 级别并记录安全字段集合。
func (this *accessLogObserver) Observe(ctx context.Context, observation webhertz.HTTPRequestObservation) {
	// 第一阶段：精确路径过滤只影响 Access Log，不阻止 Metrics 等其他完成态 Observer。
	if _, skipped := this.skipPaths[observation.Path]; skipped {
		return
	}
	// 第二阶段：先按 outcome 固定结果语义，再让慢成功请求和服务端失败选择更高日志级别。
	slow := this.slowThreshold > 0 && observation.Duration.Nanoseconds() >= this.slowThreshold
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

	// 第三阶段：字段固定且不采集 query、Header、Body、Cookie、Authorization、cause 或 Stack。
	this.logger.Log(ctx, level, "http_request",
		baselog.F("event", "http_request"), baselog.F("op", "web_hertz.request"), baselog.F("result", result), baselog.F("reason", reason),
		baselog.F("request_id", observation.RequestID), baselog.F("trace_id", observation.TraceID), baselog.F("method", observation.Method),
		baselog.F("route", observation.RoutePattern), baselog.F("path", observation.Path), baselog.F("status", observation.Status),
		baselog.F("duration_ms", observation.Duration.Milliseconds()), baselog.F("request_bytes", observation.RequestBytes),
		baselog.F("response_bytes", observation.ResponseBytes), baselog.F("outcome", observation.Outcome), baselog.F("slow", slow),
	)
}
