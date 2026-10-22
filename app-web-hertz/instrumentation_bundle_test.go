package appwebhertz

import (
	"context"
	"reflect"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
	"github.com/yangzhuangqiu/gobase/errcode"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// instrumentationBundleStub 为原子校验和完成态元数据测试提供可控声明。
type instrumentationBundleStub struct {
	// options 是 Bundle 返回的原生 Hertz Option。
	options []hertzconfig.Option
	// middleware 是 Bundle 返回的固定阶段 Handler。
	middleware []app.HandlerFunc
	// traceID 是完成态桥接应复制的关联标识。
	traceID string
}

// HertzOptions 返回测试声明的 Option 副本。
func (this *instrumentationBundleStub) HertzOptions() []hertzconfig.Option {
	return append([]hertzconfig.Option{}, this.options...)
}

// Middleware 返回测试声明的 Handler 副本。
func (this *instrumentationBundleStub) Middleware() []app.HandlerFunc {
	return append([]app.HandlerFunc{}, this.middleware...)
}

// RequestObservationMetadata 返回测试声明的 Trace ID。
func (this *instrumentationBundleStub) RequestObservationMetadata(_ context.Context, _ *app.RequestContext) webhertz.RequestObservationMetadata {
	return webhertz.RequestObservationMetadata{TraceID: this.traceID}
}

// TestPrepareInstrumentationBundlesRejectsPartialDeclaration 验证 Bundle 任一半非法时不会形成可安装计划。
// 测试方法：分别输入 typed nil、空 Hertz Option 和 nil Middleware，断言稳定声明错误且返回计划为空。
func TestPrepareInstrumentationBundlesRejectsPartialDeclaration(t *testing.T) {
	// 第一阶段：覆盖接口 typed nil 与 Bundle 内两个组成部分的 nil 声明。
	var typedNil *instrumentationBundleStub
	tests := map[string]InstrumentationBundle{
		"typed nil":        typedNil,
		"hertz option nil": &instrumentationBundleStub{options: []hertzconfig.Option{{}}},
		"middleware nil":   &instrumentationBundleStub{middleware: []app.HandlerFunc{nil}},
	}
	for name, bundle := range tests {
		// 第二阶段：每个失败都必须返回全空计划，不能保留此前或当前 Bundle 的一半。
		t.Run(name, func(t *testing.T) {
			plan, err := prepareInstrumentationBundles([]InstrumentationBundle{bundle})
			if !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
				t.Fatalf("expected declaration error, got=%v", err)
			}
			if len(plan.hertzOptions) != 0 || len(plan.middleware) != 0 || len(plan.metadataProviders) != 0 {
				t.Fatalf("expected empty failed plan: %+v", plan)
			}
		})
	}
}

// TestPrepareInstrumentationBundlesPreservesOrderAndMetadata 验证多个 Bundle 的 Option/Middleware 顺序及可选元数据合同稳定。
// 测试方法：声明两个 Bundle，执行合并后的 Handler 并断言输入顺序，再验证两个 metadata provider 均被发现。
func TestPrepareInstrumentationBundlesPreservesOrderAndMetadata(t *testing.T) {
	// 第一阶段：构造两个同时提供 Option、Middleware 和 Metadata 的有效 Bundle。
	order := make([]string, 0, 2)
	newBundle := func(name string) *instrumentationBundleStub {
		return &instrumentationBundleStub{
			options: []hertzconfig.Option{{F: func(_ *hertzconfig.Options) {}}},
			middleware: []app.HandlerFunc{func(_ context.Context, _ *app.RequestContext) {
				order = append(order, name)
			}},
			traceID: name,
		}
	}
	plan, err := prepareInstrumentationBundles([]InstrumentationBundle{newBundle("first"), newBundle("second")})
	if err != nil {
		t.Fatalf("prepare instrumentation: %v", err)
	}
	// 第二阶段：执行 Handler 并联合断言三个输出维度均保留声明顺序。
	for _, handler := range plan.middleware {
		handler(context.Background(), nil)
	}
	if !reflect.DeepEqual(order, []string{"first", "second"}) || len(plan.hertzOptions) != 2 || len(plan.metadataProviders) != 2 {
		t.Fatalf("unexpected instrumentation plan: order=%v options=%d metadata=%d", order, len(plan.hertzOptions), len(plan.metadataProviders))
	}
}

// TestInstrumentationMetadataFlowsIntoCompletionObservation 验证 Bundle Trace ID 与 Request ID 在同一次完成态快照中合并。
// 测试方法：构造 metadata provider 和通用 Observer，执行真实 Hertz Next 链，断言最终快照同时包含两个关联标识。
func TestInstrumentationMetadataFlowsIntoCompletionObservation(t *testing.T) {
	// 第一阶段：聚合应用 Request ID bridge、Bundle Trace ID bridge 与真实消费 Observer。
	observed := make(chan webhertz.HTTPRequestObservation, 1)
	requestObservers := []webhertz.RequestObserver{requestObserverFunc(func(_ context.Context, observation webhertz.HTTPRequestObservation) {
		observed <- observation
	})}
	bundle := &instrumentationBundleStub{traceID: "trace-123"}
	observers, err := prepareRequestObservers(AccessLogConfig{}, nil, requestObservers, []webhertz.RequestObservationMetadataProvider{bundle})
	if err != nil {
		t.Fatalf("prepare observers: %v", err)
	}
	request := newMiddlewareTestRequest("GET", "/orders/42")
	request.Set(requestIDContextKey, "request-123")
	executeMiddlewareTestChain(request, []app.HandlerFunc{
		webhertzRequestObservationMiddleware(observers),
		func(_ context.Context, request *app.RequestContext) { request.SetFullPath("/orders/:id") },
	})
	// 第二阶段：通过 Hertz Next 执行完成态桥接，验证两个关联标识不互相覆盖。
	observation := <-observed
	if observation.RequestID != "request-123" || observation.TraceID != "trace-123" || observation.RoutePattern != "/orders/:id" {
		t.Fatalf("unexpected observation metadata: %+v", observation)
	}
}

// requestObserverFunc 把测试函数适配为通用完成态 Observer。
type requestObserverFunc func(context.Context, webhertz.HTTPRequestObservation)

// Observe 调用测试函数并复制完成态值。
func (this requestObserverFunc) Observe(ctx context.Context, observation webhertz.HTTPRequestObservation) {
	this(ctx, observation)
}

// webhertzRequestObservationMiddleware 通过公开 Option 不可直接取私有 Handler，测试使用等价最小完成态驱动。
// 阶段角色：只验证 app-web-hertz metadata bridge，不重复 web-hertz 自身快照行为。
func webhertzRequestObservationMiddleware(observers []webhertz.RequestObserver) app.HandlerFunc {
	return func(ctx context.Context, request *app.RequestContext) {
		request.Next(ctx)
		observation := webhertz.HTTPRequestObservation{RoutePattern: request.FullPath()}
		for _, observer := range observers {
			if provider, ok := observer.(webhertz.RequestObservationMetadataProvider); ok {
				metadata := provider.RequestObservationMetadata(ctx, request)
				if observation.RequestID == "" {
					observation.RequestID = metadata.RequestID
				}
				if observation.TraceID == "" {
					observation.TraceID = metadata.TraceID
				}
			}
		}
		for _, observer := range observers {
			observer.Observe(ctx, observation)
		}
	}
}
