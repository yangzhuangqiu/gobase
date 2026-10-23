package webhertz

import (
	"context"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

// newRequestObservationMiddleware 构造一次计时、受控快照和同步 fan-out 的完成态 Middleware。
// 阶段角色：调用方决定其全局链位置；本实现只负责快照一致性和 Observer 故障隔离。
func newRequestObservationMiddleware(observers []RequestObserver, logger func() baselog.Logger) app.HandlerFunc {
	resolved := append([]RequestObserver{}, observers...)
	return func(ctx context.Context, request *app.RequestContext) {
		startedAt := time.Now()
		method := normalizeObservationMethod(string(request.Request.Method()))
		// 第一阶段：仅向显式实现生命周期扩展的 Observer 发布开始事件，用于 in-flight 等瞬时状态。
		for _, observer := range resolved {
			if lifecycleObserver, ok := observer.(RequestLifecycleObserver); ok {
				notifyRequestStarted(ctx, lifecycleObserver, method, logger())
			}
		}

		// 第二阶段：无论正常返回还是 panic 展开，都先复制快照并通知，再保持原始 panic 语义。
		defer func() {
			panicValue := recover()
			observation := buildRequestObservation(request, method, startedAt, panicValue != nil)
			for _, observer := range resolved {
				if provider, ok := observer.(RequestObservationMetadataProvider); ok {
					mergeObservationMetadata(ctx, provider, request, &observation, logger())
				}
			}
			for _, observer := range resolved {
				notifyRequestObserved(ctx, observer, observation, logger())
			}
			if panicValue != nil {
				panic(panicValue)
			}
		}()
		request.Next(ctx)
	}
}

// normalizeObservationMethod 把标准 HTTP Method 保持为大写，其余客户端 token 收敛到固定值。
func normalizeObservationMethod(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	switch method {
	case consts.MethodGet, consts.MethodHead, consts.MethodPost, consts.MethodPut, consts.MethodPatch,
		consts.MethodDelete, consts.MethodConnect, consts.MethodOptions, consts.MethodTrace:
		return method
	default:
		return OtherHTTPMethod
	}
}

// buildRequestObservation 在 RequestContext 归还池前复制全部完成态字段。
func buildRequestObservation(request *app.RequestContext, method string, startedAt time.Time, panicked bool) HTTPRequestObservation {
	routePattern := strings.Clone(request.FullPath())
	if routePattern == "" {
		routePattern = UnmatchedRoutePattern
	}
	status := request.Response.StatusCode()
	if status == 0 {
		status = consts.StatusOK
	}
	outcome := outcomeForStatus(status)
	if panicked {
		outcome = RequestOutcomePanic
	}
	return HTTPRequestObservation{
		Method:        method,
		RoutePattern:  routePattern,
		Path:          strings.Clone(string(request.Path())),
		Status:        status,
		Duration:      time.Since(startedAt),
		RequestBytes:  requestBodySize(request),
		ResponseBytes: responseBodySize(request),
		Outcome:       outcome,
	}
}

// requestBodySize 返回协议声明或已物化请求体大小，避免读取未知流式 Body。
func requestBodySize(request *app.RequestContext) int64 {
	contentLength := request.Request.Header.ContentLength()
	if contentLength > 0 {
		return int64(contentLength)
	}
	if request.Request.IsBodyStream() {
		if contentLength == 0 {
			return 0
		}
		return UnknownBodySize
	}
	bodyLength := len(request.Request.BodyBytes())
	if bodyLength > 0 {
		return int64(bodyLength)
	}
	return int64(contentLength)
}

// responseBodySize 返回最终响应大小；未知流式 Body 使用固定哨兵值。
func responseBodySize(request *app.RequestContext) int64 {
	if request.Response.IsBodyStream() {
		contentLength := request.Response.Header.ContentLength()
		if contentLength >= 0 {
			return int64(contentLength)
		}
		return UnknownBodySize
	}
	return int64(len(request.Response.BodyBytes()))
}

// outcomeForStatus 把动态状态码压缩到有限结果集合。
func outcomeForStatus(status int) RequestOutcome {
	switch {
	case status >= 500:
		return RequestOutcomeServerError
	case status >= 400:
		return RequestOutcomeClientError
	default:
		return RequestOutcomeSuccess
	}
}

// mergeObservationMetadata 隔离 Provider panic，并按声明顺序采用首个非空关联值、合并 panic 标记。
func mergeObservationMetadata(ctx context.Context, provider RequestObservationMetadataProvider, request *app.RequestContext, observation *HTTPRequestObservation, logger baselog.Logger) {
	defer func() {
		if recover() != nil {
			logError(logger, ctx, "Request observation metadata provider panicked",
				baselog.F("op", "web_hertz.request_observation"),
				baselog.F("result", "failed"),
				baselog.F("stage", "metadata"),
				baselog.F("reason", "provider_panic"),
			)
		}
	}()
	metadata := provider.RequestObservationMetadata(ctx, request)
	if observation.RequestID == "" {
		observation.RequestID = strings.Clone(metadata.RequestID)
	}
	if observation.TraceID == "" {
		observation.TraceID = strings.Clone(metadata.TraceID)
	}
	if metadata.Panic {
		observation.Outcome = RequestOutcomePanic
	}
}

// notifyRequestStarted 隔离单个开始通知 panic，避免观测故障破坏请求链。
func notifyRequestStarted(ctx context.Context, observer RequestLifecycleObserver, method string, logger baselog.Logger) {
	defer func() {
		if recover() != nil {
			logError(logger, ctx, "Request start observer panicked",
				baselog.F("op", "web_hertz.request_observation"),
				baselog.F("result", "failed"),
				baselog.F("stage", "request_started"),
				baselog.F("reason", "observer_panic"),
			)
		}
	}()
	observer.RequestStarted(ctx, method)
}

// notifyRequestObserved 隔离单个完成通知 panic，并继续 fan-out 后续 Observer。
func notifyRequestObserved(ctx context.Context, observer RequestObserver, observation HTTPRequestObservation, logger baselog.Logger) {
	defer func() {
		if recover() != nil {
			logError(logger, ctx, "Request completion observer panicked",
				baselog.F("op", "web_hertz.request_observation"),
				baselog.F("result", "failed"),
				baselog.F("stage", "request_completed"),
				baselog.F("reason", "observer_panic"),
			)
		}
	}()
	observer.Observe(ctx, observation)
}
