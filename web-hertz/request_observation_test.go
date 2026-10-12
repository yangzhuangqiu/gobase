package webhertz

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/yangzhuangqiu/gobase/errcode"
)

// requestObserverFunc 把测试函数适配为完成态 Observer。
type requestObserverFunc func(context.Context, HTTPRequestObservation)

// Observe 同步执行测试回调。
func (this requestObserverFunc) Observe(ctx context.Context, observation HTTPRequestObservation) {
	this(ctx, observation)
}

// lifecycleObserverStub 同时记录开始通知、受控元数据和完成态快照。
type lifecycleObserverStub struct {
	// mu 保护并发请求写入的事件和快照。
	mu sync.Mutex
	// events 保存开始/完成通知顺序。
	events []string
	// observations 保存完成态值副本。
	observations []HTTPRequestObservation
	// metadata 是每次请求返回的受控关联字段。
	metadata RequestObservationMetadata
}

// RequestStarted 记录请求开始事件和归一化 Method。
func (this *lifecycleObserverStub) RequestStarted(_ context.Context, method string) {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.events = append(this.events, "start:"+method)
}

// Observe 记录完成事件和快照副本。
func (this *lifecycleObserverStub) Observe(_ context.Context, observation HTTPRequestObservation) {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.events = append(this.events, "finish:"+string(observation.Outcome))
	this.observations = append(this.observations, observation)
}

// RequestObservationMetadata 返回不持有 RequestContext 的固定关联元数据。
func (this *lifecycleObserverStub) RequestObservationMetadata(_ context.Context, _ *app.RequestContext) RequestObservationMetadata {
	return this.metadata
}

// TestWithRequestObserversBuildsStableCompletionSnapshot 验证直接 web-hertz 使用完成态能力时获得模板路由、最终状态和受控关联字段。
// 测试方法：注册动态路由和 Lifecycle/Metadata Observer，发送带 Body 的请求，断言开始/结束顺序及完整快照。
func TestWithRequestObserversBuildsStableCompletionSnapshot(t *testing.T) {
	observer := &lifecycleObserverStub{metadata: RequestObservationMetadata{RequestID: "request-1", TraceID: "trace-1"}}
	instance, err := New(
		Config{},
		WithRequestObservers(observer),
		WithRoutes(func(router *route.RouterGroup) error {
			router.POST("/users/:id", func(_ context.Context, request *app.RequestContext) {
				request.String(consts.StatusCreated, "created")
			})
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("new observed server: %v", err)
	}

	// 执行阶段：使用动态 URL 和已知请求体，确保原始 path 不会进入 RoutePattern。
	request := instance.engine.NewContext()
	request.Request.SetRequestURI("/users/42?secret=hidden")
	request.Request.Header.SetMethod(consts.MethodPost)
	request.Request.SetBodyString("input")
	instance.engine.ServeHTTP(context.Background(), request)

	observer.mu.Lock()
	defer observer.mu.Unlock()
	if !reflect.DeepEqual(observer.events, []string{"start:POST", "finish:success"}) || len(observer.observations) != 1 {
		t.Fatalf("unexpected observation events: events=%v count=%d", observer.events, len(observer.observations))
	}
	observation := observer.observations[0]
	if observation.RequestID != "request-1" || observation.TraceID != "trace-1" || observation.Method != consts.MethodPost {
		t.Fatalf("unexpected correlation fields: %+v", observation)
	}
	if observation.RoutePattern != "/users/:id" || observation.Path != "/users/42" || observation.Status != consts.StatusCreated {
		t.Fatalf("unexpected route result: %+v", observation)
	}
	if observation.RequestBytes != int64(len("input")) || observation.ResponseBytes != int64(len("created")) || observation.Duration < 0 {
		t.Fatalf("unexpected size/duration fields: %+v", observation)
	}
}

// TestRequestObservationUsesUnmatchedRouteAndIsolatesObserverPanic 验证未知动态 URL 固定收敛为 _unmatched，且单个 Observer 故障不阻断后续通知。
// 测试方法：首个 Observer panic、第二个收集两条不同 404 URL，断言响应未改变且 RoutePattern 相同。
func TestRequestObservationUsesUnmatchedRouteAndIsolatesObserverPanic(t *testing.T) {
	observations := make([]HTTPRequestObservation, 0, 2)
	instance, err := New(
		Config{},
		WithRequestObservers(
			requestObserverFunc(func(context.Context, HTTPRequestObservation) { panic("observer failed") }),
			requestObserverFunc(func(_ context.Context, observation HTTPRequestObservation) {
				observations = append(observations, observation)
			}),
		),
	)
	if err != nil {
		t.Fatalf("new observed server: %v", err)
	}

	for _, path := range []string{"/orders/10001", "/orders/20002"} {
		request := performEngineRequest(instance, consts.MethodGet, path)
		if request.Response.StatusCode() != consts.StatusNotFound {
			t.Fatalf("unexpected unmatched status: %d", request.Response.StatusCode())
		}
	}
	if len(observations) != 2 {
		t.Fatalf("expected later observer to receive both requests: %d", len(observations))
	}
	for _, observation := range observations {
		if observation.RoutePattern != UnmatchedRoutePattern || observation.Outcome != RequestOutcomeClientError {
			t.Fatalf("unexpected unmatched observation: %+v", observation)
		}
	}
}

// TestRequestObservationNormalizesCustomMethod 验证客户端自定义 Method 不会形成无界指标维度。
// 测试方法：直接发送自定义 token，断言完成态 Method 固定收敛为 _other。
func TestRequestObservationNormalizesCustomMethod(t *testing.T) {
	var observed HTTPRequestObservation
	instance, err := New(Config{}, WithRequestObservers(requestObserverFunc(func(_ context.Context, observation HTTPRequestObservation) {
		observed = observation
	})))
	if err != nil {
		t.Fatalf("new custom method server: %v", err)
	}
	_ = performEngineRequest(instance, "TENANT_SPECIFIC_METHOD", "/missing")
	if observed.Method != OtherHTTPMethod {
		t.Fatalf("expected bounded custom method, got=%q", observed.Method)
	}
}

// TestRequestObservationPreservesRawPanicSemantics 验证未安装 Recovery 时完成态记录 panic，但 provider 不吞掉原始编程错误。
// 测试方法：路由 panic 固定值，在外层 recover，断言 Observer 收到 panic outcome 且 recover 值保持相同。
func TestRequestObservationPreservesRawPanicSemantics(t *testing.T) {
	const panicValue = "raw panic"
	var observed HTTPRequestObservation
	instance, err := New(
		Config{},
		WithRequestObservers(requestObserverFunc(func(_ context.Context, observation HTTPRequestObservation) { observed = observation })),
		WithRoutes(func(router *route.RouterGroup) error {
			router.GET("/panic", func(_ context.Context, _ *app.RequestContext) { panic(panicValue) })
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("new panic server: %v", err)
	}
	defer func() {
		if recovered := recover(); recovered != panicValue {
			t.Fatalf("unexpected recovered panic: %v", recovered)
		}
		if observed.Outcome != RequestOutcomePanic || observed.RoutePattern != "/panic" {
			t.Fatalf("unexpected panic observation: %+v", observed)
		}
	}()
	_ = performEngineRequest(instance, consts.MethodGet, "/panic")
}

// TestWithRequestObserversRejectsNilAndSupportsConcurrentRequests 验证声明边界拒绝 typed nil，且完成态 fan-out 可并发执行。
// 测试方法：先构造 typed nil 断言 declaration_invalid，再并发执行请求并用原子计数确认无丢失。
func TestWithRequestObserversRejectsNilAndSupportsConcurrentRequests(t *testing.T) {
	var typedNil *lifecycleObserverStub
	_, err := New(Config{}, WithRequestObservers(typedNil))
	if !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected typed nil declaration error, got=%v", err)
	}

	var observed atomic.Int64
	instance, err := New(
		Config{},
		WithRequestObservers(requestObserverFunc(func(context.Context, HTTPRequestObservation) { observed.Add(1) })),
		WithRoutes(func(router *route.RouterGroup) error {
			router.GET("/ready", emptyHandler)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("new concurrent server: %v", err)
	}
	const requestCount = 64
	var waitGroup sync.WaitGroup
	waitGroup.Add(requestCount)
	for index := 0; index < requestCount; index++ {
		go func() {
			defer waitGroup.Done()
			_ = performEngineRequest(instance, consts.MethodGet, "/ready")
		}()
	}
	waitGroup.Wait()
	if observed.Load() != requestCount {
		t.Fatalf("unexpected concurrent observation count: %d", observed.Load())
	}
}

// BenchmarkRequestObservation 记录单 Observer 完成态快照的请求期开销与分配基线。
func BenchmarkRequestObservation(b *testing.B) {
	instance, err := New(
		Config{},
		WithRequestObservers(requestObserverFunc(func(context.Context, HTTPRequestObservation) {})),
		WithRoutes(func(router *route.RouterGroup) error {
			router.GET("/items/:id", emptyHandler)
			return nil
		}),
	)
	if err != nil {
		b.Fatalf("new benchmark server: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		_ = performEngineRequest(instance, consts.MethodGet, "/items/42")
	}
}
