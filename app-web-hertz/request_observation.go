package appwebhertz

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	baselog "github.com/yangzhuangqiu/gobase/log"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

const (
	// requestObservationPanicKey 标记内层 Recovery 已处理 panic，供完成态桥接读取。
	requestObservationPanicKey = "gobase.app_web_hertz.request_observation_panic"
)

// requestObservationBridge 把 app 请求关联状态窄化为 web-hertz 允许的完成态元数据。
type requestObservationBridge struct{}

// Observe 是桥接 Observer 的空完成回调；实例存在只为在同一标准阶段提供 Metadata。
func (this requestObservationBridge) Observe(_ context.Context, _ webhertz.HTTPRequestObservation) {}

// RequestObservationMetadata 在请求归还池前复制 Request ID 和 Recovery panic 标记。
func (this requestObservationBridge) RequestObservationMetadata(_ context.Context, request *app.RequestContext) webhertz.RequestObservationMetadata {
	return webhertz.RequestObservationMetadata{RequestID: GetRequestID(request), Panic: RequestPanicked(request)}
}

// instrumentationMetadataBridge 把 Bundle 的可选关联元数据适配为无完成回调的 Observer。
type instrumentationMetadataBridge struct {
	// provider 是已通过 Bundle 原子校验的完成态元数据来源。
	provider webhertz.RequestObservationMetadataProvider
}

// Observe 是空完成回调；实例只参与同一 Middleware 的 Metadata 合并阶段。
func (this instrumentationMetadataBridge) Observe(_ context.Context, _ webhertz.HTTPRequestObservation) {
}

// RequestObservationMetadata 委托 Bundle 提供 Trace ID 等后端无关关联字段。
func (this instrumentationMetadataBridge) RequestObservationMetadata(ctx context.Context, request *app.RequestContext) webhertz.RequestObservationMetadata {
	return this.provider.RequestObservationMetadata(ctx, request)
}

// prepareRequestObservers 聚合内建 Access Log 和 IOC 注入的通用 Observer，保证只安装一个完成态 Middleware。
// 阶段角色：Server 构造期校验 typed 依赖并冻结完成态观察计划。
func prepareRequestObservers(
	config AccessLogConfig,
	logger baselog.Logger,
	requestObservers []webhertz.RequestObserver,
	metadataProviders []webhertz.RequestObservationMetadataProvider,
) ([]webhertz.RequestObserver, error) {
	// 第一阶段：完整校验 IOC 通用组件，避免 typed nil 推迟到请求期才 panic。
	for index, observer := range requestObservers {
		if isNilInterface(observer) {
			return nil, errorForDeclarationInvalid(messagef("op=app_web_hertz.middleware result=failed stage=build_request_observers reason=request_observer_required observer_index=%d", index))
		}
	}
	capacity := 1 + len(metadataProviders) + len(requestObservers)
	observers := make([]webhertz.RequestObserver, 0, capacity)
	if config.Enabled {
		accessLog, err := newAccessLogObserver(logger, config)
		if err != nil {
			return nil, err
		}
		observers = append(observers, accessLog)
	}
	// 第二阶段：桥接器必须先提供 Request ID/panic 元数据；通用 Observer 不受 Access Log 开关影响。
	if len(observers) > 0 || len(requestObservers) > 0 {
		bridges := make([]webhertz.RequestObserver, 0, 1+len(metadataProviders))
		bridges = append(bridges, requestObservationBridge{})
		for _, provider := range metadataProviders {
			bridges = append(bridges, instrumentationMetadataBridge{provider: provider})
		}
		observers = append(bridges, observers...)
		observers = append(observers, requestObservers...)
	}
	return observers, nil
}

// markRequestObservationPanic 在 Recovery 调用具体响应策略前记录有限 panic 状态。
func markRequestObservationPanic(request *app.RequestContext) {
	request.Set(requestObservationPanicKey, true)
}

// RequestPanicked 返回标准 Recovery 是否已经处理当前请求的 panic。
//
// 参数:
// - request: 当前 Hertz RequestContext；nil、未启用 Recovery 或未发生 panic 时返回 false。
//
// 说明:
// - 本入口只暴露布尔完成态语义，不暴露 panic 值或 Stack；Trace adapter 可据此设置稳定 error.type。
func RequestPanicked(request *app.RequestContext) bool {
	if request == nil {
		return false
	}
	_, panicked := request.Get(requestObservationPanicKey)
	return panicked
}
