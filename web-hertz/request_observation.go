package webhertz

import "time"

const (
	// UnmatchedRoutePattern 是未命中注册路由时的固定模板，防止原始 URL 扩张指标基数。
	UnmatchedRoutePattern = "_unmatched"
	// UnknownBodySize 表示流式或协议层无法确定的请求/响应体字节数。
	UnknownBodySize int64 = -1
	// OtherHTTPMethod 收敛非标准 Method，防止客户端自定义 token 扩张指标基数。
	OtherHTTPMethod = "_other"
)

// RequestOutcome 是请求完成结果的有限枚举，不携带动态错误文本。
type RequestOutcome string

const (
	// RequestOutcomeSuccess 表示最终状态小于 400。
	RequestOutcomeSuccess RequestOutcome = "success"
	// RequestOutcomeClientError 表示最终状态为 4xx。
	RequestOutcomeClientError RequestOutcome = "client_error"
	// RequestOutcomeServerError 表示最终状态为 5xx。
	RequestOutcomeServerError RequestOutcome = "server_error"
	// RequestOutcomePanic 表示请求期间出现 panic，无论最终是否由 Recovery 转为 500。
	RequestOutcomePanic RequestOutcome = "panic"
)

// HTTPRequestObservation 是单次 HTTP 请求结束前复制出的不可变完成态快照。
//
// 语义:
// 1. 不持有 Hertz RequestContext、Header、Body 或其他池化对象；
// 2. Method、RoutePattern、Status 和 Outcome 可作为受控指标维度；
// 3. RequestID、TraceID 和 Path 只用于日志/Trace 关联，不得作为默认 Metrics Label。
type HTTPRequestObservation struct {
	// RequestID 是请求关联标识；未提供关联元数据时为空。
	RequestID string
	// TraceID 是当前有效 Span 标识；未提供或无有效 Span 时为空。
	TraceID string
	// Method 是大写归一化 HTTP Method。
	Method string
	// RoutePattern 是注册路由模板；未匹配时固定为 UnmatchedRoutePattern。
	RoutePattern string
	// Path 是不含 query 的请求路径安全副本，仅供受控日志使用。
	Path string
	// Status 是 Middleware 链结束后的最终 HTTP 状态码。
	Status int
	// Duration 是从完成态 Middleware 进入到内层链全部返回的耗时。
	Duration time.Duration
	// RequestBytes 是协议声明或已物化请求体的字节数；未知时为 UnknownBodySize。
	RequestBytes int64
	// ResponseBytes 是最终响应体字节数；未知流式响应为 UnknownBodySize。
	ResponseBytes int64
	// Outcome 是有限结果枚举；panic 标记优先于状态码分类。
	Outcome RequestOutcome
}

// RequestObservationMetadata 是完成态快照允许由请求链补充的受控关联元数据。
type RequestObservationMetadata struct {
	// RequestID 补充应用生成并校验后的请求关联标识；空值表示不提供。
	RequestID string
	// TraceID 补充有效 Span 标识；空值表示不提供。
	TraceID string
	// Panic 表示内层 Recovery 已收敛 panic，完成态 Outcome 必须固定为 panic。
	Panic bool
}
