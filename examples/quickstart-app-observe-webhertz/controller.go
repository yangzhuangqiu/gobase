package main

import (
	"context"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	appwebhertz "github.com/yangzhuangqiu/gobase/app-web-hertz"
	"go.opentelemetry.io/otel/trace"
)

// observationResponse 展示业务响应如何关联 Request ID 与当前 Server Span。
type observationResponse struct {
	// Message 是本次请求的教学业务结果。
	Message string `json:"message"`
	// RequestID 是 app-web-hertz 标准 Middleware 生成的请求关联标识。
	RequestID string `json:"requestId"`
	// TraceID 是当前有效 OTel Server Span 的标识；未采样或无 Span 时为空。
	TraceID string `json:"traceId,omitempty"`
}

// observabilityController 提供可观察成功、动态路由和 panic 三类请求。
type observabilityController struct{}

// GroupPath 把业务路由绑定到共享 `/api` 逻辑分组。
func (this *observabilityController) GroupPath() string { return "/api" }

// GetHello 返回当前 Request ID 与 Trace ID，便于和日志、Span 交叉定位。
func (this *observabilityController) GetHello(ctx context.Context, request *hertzapp.RequestContext) {
	request.JSON(consts.StatusOK, newObservationResponse(ctx, request, "hello from observable Hertz"))
}

// GetPanic 主动触发 panic，用于观察 Recovery、Access Log、Metrics 与 Span 的最终失败语义。
func (this *observabilityController) GetPanic(context.Context, *hertzapp.RequestContext) {
	panic("quickstart observable panic")
}

// RegisterRoutes 注册动态订单路由，证明指标和 Span 使用模板 `/api/orders/:id` 而非原始路径。
func (this *observabilityController) RegisterRoutes(router *route.RouterGroup) error {
	router.GET("/orders/:id", func(ctx context.Context, request *hertzapp.RequestContext) {
		message := "loaded order " + request.Param("id")
		request.JSON(consts.StatusOK, newObservationResponse(ctx, request, message))
	})
	return nil
}

// healthController 提供业务端口健康检查，并由配置排除 Access Log 与 Trace。
type healthController struct{}

// GroupPath 把健康检查注册到根逻辑分组。
func (this *healthController) GroupPath() string { return "/" }

// GetHealthz 返回业务 Server 存活状态；Prometheus 仍可按需统计该请求。
func (this *healthController) GetHealthz(_ context.Context, request *hertzapp.RequestContext) {
	request.JSON(consts.StatusOK, map[string]string{"status": "ok"})
}

// newObservationResponse 从当前请求复制安全关联标识，不持有池化 RequestContext。
func newObservationResponse(ctx context.Context, request *hertzapp.RequestContext, message string) observationResponse {
	traceID := ""
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.IsValid() {
		traceID = spanContext.TraceID().String()
	}
	return observationResponse{
		Message:   message,
		RequestID: appwebhertz.GetRequestID(request),
		TraceID:   traceID,
	}
}
