package appwebhertz

import (
	"context"
	"time"

	baselog "github.com/yangzhuangqiu/gobase/log"
)

const (
	// assemblyLoggerName 是 app-web-hertz IOC 聚合完成事件的稳定 Logger 名称。
	assemblyLoggerName = "app.web.hertz.assembly"
	// middlewareLoggerName 是 app-web-hertz 内建请求能力诊断事件的稳定 Logger 名称。
	middlewareLoggerName = "app.web.hertz.middleware"
	// providerLoggerName 是交给 web-hertz 实例记录构造和生命周期事件的稳定 Logger 名称。
	providerLoggerName = "web.hertz"
)

// serverLoggers 保存一次 IOC Server 装配使用的命名 Logger 快照。
// 阶段角色：Factory 只在 Init 构造期访问，运行期组件直接借用具体 Logger。
type serverLoggers struct {
	// assembly 记录 IOC 聚合后的高层组装摘要。
	assembly baselog.Logger
	// middleware 记录 Recovery、Request ID 与 Gzip 等内建能力异常。
	middleware baselog.Logger
	// provider 交给 web-hertz 记录底层构造与生命周期事件。
	provider baselog.Logger
	// request 记录完成态 HTTP Access Log。
	request baselog.Logger
}

// resolveServerLoggers 从 IOC 根 Factory 一次性派生全部稳定命名 Logger。
// 阶段角色：managedServer 读取配置前完成基础依赖校验，避免请求期再访问 Factory。
func resolveServerLoggers(factory baselog.LoggerFactory) (serverLoggers, error) {
	if isNilInterface(factory) {
		return serverLoggers{}, errorForDeclarationInvalid("op=app_web_hertz.logging result=failed stage=resolve reason=logger_factory_required")
	}
	return serverLoggers{
		assembly:   factory.Get(assemblyLoggerName),
		middleware: factory.Get(middlewareLoggerName),
		provider:   factory.Get(providerLoggerName),
		request:    factory.Get(accessLoggerName),
	}, nil
}

// logAssemblySuccess 记录应用组件和标准能力已经冻结的单次高层摘要。
// 阶段角色：只在底层 Server 完整构造并发布后执行，失败路径由 IOC Build 边界统一记录。
func logAssemblySuccess(ctx context.Context, logger baselog.Logger, scope string, input Config, components serverComponents, startedAt time.Time) {
	globalFilters, groupedFilters, logicalGroups := filterAssemblyCounts(components.filters)
	// 单条事件同时覆盖 IOC 聚合规模、标准能力开关和构造耗时，不展开高基数声明明细。
	logger.Info(ctx, "Application Hertz server assembled",
		baselog.F("op", "app_web_hertz.assemble"), baselog.F("result", "ok"), baselog.F("stage", "complete"),
		baselog.F("scope", scope), baselog.F("provider", "hertz"), baselog.F("controller_count", len(components.controllers)),
		baselog.F("global_filter_count", globalFilters), baselog.F("grouped_filter_count", groupedFilters), baselog.F("logical_group_count", logicalGroups),
		baselog.F("no_route_handler_count", len(components.noRouteHandlers)), baselog.F("no_method_handler_count", len(components.noMethodHandlers)),
		baselog.F("request_observer_count", len(components.requestObservers)), baselog.F("instrumentation_bundle_count", len(components.instrumentationBundles)),
		baselog.F("recovery_enabled", input.Middleware.Recovery.Enabled), baselog.F("request_id_enabled", input.Middleware.RequestID.Enabled),
		baselog.F("access_log_enabled", input.Middleware.AccessLog.Enabled), baselog.F("cors_enabled", input.Middleware.CORS.Enabled),
		baselog.F("gzip_enabled", input.Middleware.Gzip.Enabled), baselog.F("duration_ms", time.Since(startedAt).Milliseconds()),
	)
}

// filterAssemblyCounts 统计全局 Filter、分组 Filter 和逻辑分组数量。
// 阶段角色：组装成功后只读取已经验证过的 Filter 声明，不参与路由决策。
func filterAssemblyCounts(filters []Filter) (int, int, int) {
	groups := make(map[string]struct{})
	globalCount := 0
	groupedCount := 0
	// Filter 的接口能力决定作用域，根路径仍按普通逻辑分组计数。
	for _, filter := range filters {
		grouped, ok := filter.(GroupedFilter)
		if !ok {
			globalCount++
			continue
		}
		groupedCount++
		groupPath, _ := normalizeGroupPath(grouped.GroupPath())
		groups[groupPath] = struct{}{}
	}
	return globalCount, groupedCount, len(groups)
}
