package webhertzobserve

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	appwebhertz "github.com/yangzhuangqiu/gobase/app-web-hertz"
	"github.com/yangzhuangqiu/gobase/errcode"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestModuleBuildsRequestLogAndPrivatePrometheusBeans 验证按需 Module 只借用 LoggerFactory，并发布可注入的日志、Metrics Observer 和 Handler。
// 测试方法：注册 borrowed Factory，声明两个能力并 Build；分别调用三个接口，断言日志事件与私有 Handler 指标后关闭容器。
func TestModuleBuildsRequestLogAndPrivatePrometheusBeans(t *testing.T) {
	container := ioc.New()
	factory := &recordingLoggerFactory{}
	if err := container.Register(factory, ioc.WithDeclare[baselog.LoggerFactory](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register borrowed logger factory: %v", err)
	}
	module := NewModule(WithRequestLog(), WithPrivatePrometheus())
	if module.Name() != moduleName {
		t.Fatalf("unexpected module name: %s", module.Name())
	}
	if err := module.Register(container); err != nil {
		t.Fatalf("register observe module: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build observe container: %v", err)
	}

	// 第二阶段：按不同语义接口解析同一批 managed bean，验证没有全局 Logger/Registry 查找。
	logObserver, err := ioc.ResolveAs[appwebhertz.RequestLogObserver](container)
	if err != nil {
		t.Fatalf("resolve request log observer: %v", err)
	}
	requestObserver, err := ioc.ResolveAs[webhertz.RequestObserver](container)
	if err != nil {
		t.Fatalf("resolve prometheus observer: %v", err)
	}
	handler, err := ioc.ResolveAs[PrometheusHandler](container)
	if err != nil {
		t.Fatalf("resolve prometheus handler: %v", err)
	}
	logObserver.ObserveRequestLog(context.Background(), webhertz.HTTPRequestObservation{Status: 200, Outcome: webhertz.RequestOutcomeSuccess}, false)
	lifecycleObserver, ok := requestObserver.(webhertz.RequestLifecycleObserver)
	if !ok {
		t.Fatal("expected prometheus bean to implement lifecycle observer")
	}
	lifecycleObserver.RequestStarted(context.Background(), "GET")
	requestObserver.Observe(context.Background(), webhertz.HTTPRequestObservation{
		Method: "GET", RoutePattern: "/ready", Status: 200, Outcome: webhertz.RequestOutcomeSuccess,
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "gobase_http_server_requests_total") {
		t.Fatalf("unexpected private prometheus handler: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	factory.mu.Lock()
	logCount := len(factory.events)
	factory.mu.Unlock()
	if logCount != 1 {
		t.Fatalf("expected one request log event, got=%d", logCount)
	}

	// 第三阶段：Container Close 只注销 Collector；borrowed Factory 保持可用且没有关闭合同。
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close observe container: %v", err)
	}
}

// TestSharedPrometheusModuleRequiresRegistryAndUnregistersCollector 验证 shared 模式依赖缺失 fail-fast，成功模式只拥有自身注册。
// 测试方法：先缺少 Registry Build 断言错误码；再注入 borrowed Registry，Build/Close 前后 Gather 并断言指标被注销而 Registry 可用。
func TestSharedPrometheusModuleRequiresRegistryAndUnregistersCollector(t *testing.T) {
	missingContainer := ioc.New()
	if err := NewModule(WithSharedPrometheus()).Register(missingContainer); err != nil {
		t.Fatalf("register missing-registry module: %v", err)
	}
	if err := missingContainer.Build(context.Background()); !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected missing shared registry error, got=%v", err)
	}

	// 第二阶段：显式 borrowed Registry 覆盖 adapter 生命周期，容器关闭后 Registry 本身仍可 Gather。
	registry := prometheus.NewRegistry()
	container := ioc.New()
	if err := container.Register(registry, ioc.WithDeclare[PrometheusRegistry](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register borrowed prometheus registry: %v", err)
	}
	if err := NewModule(WithSharedPrometheus()).Register(container); err != nil {
		t.Fatalf("register shared prometheus module: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build shared prometheus module: %v", err)
	}
	observer, err := ioc.ResolveAs[webhertz.RequestObserver](container)
	if err != nil {
		t.Fatalf("resolve shared observer: %v", err)
	}
	observer.(webhertz.RequestLifecycleObserver).RequestStarted(context.Background(), "GET")
	observer.Observe(context.Background(), webhertz.HTTPRequestObservation{Method: "GET", RoutePattern: "/", Status: 200, Outcome: webhertz.RequestOutcomeSuccess})
	families, err := registry.Gather()
	if err != nil || !containsMetricFamily(families, "gobase_http_server_requests_total") {
		t.Fatalf("expected registered HTTP metrics: count=%d err=%v", len(families), err)
	}
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close shared prometheus container: %v", err)
	}
	families, err = registry.Gather()
	if err != nil {
		t.Fatalf("gather borrowed registry after close: %v", err)
	}
	if containsMetricFamily(families, "gobase_http_server_requests_total") {
		t.Fatal("expected managed Collector registration to be removed")
	}
}

// TestModuleRejectsEmptyAndConflictingDeclarationsBeforeRegistration 验证空 Module、nil Option 和双 Registry 模式在修改 IOC Registry 前失败。
// 测试方法：表驱动调用 Register 断言 declaration_invalid；随后向同一 Container 注册探针，确认 Registry 仍处于可用状态。
func TestModuleRejectsEmptyAndConflictingDeclarationsBeforeRegistration(t *testing.T) {
	tests := map[string]Module{
		"empty":         NewModule(),
		"nil option":    NewModule(ModuleOption(nil)),
		"dual registry": NewModule(WithPrivatePrometheus(), WithSharedPrometheus()),
		"dual tracing":  NewModule(WithOpenTelemetry(), WithOpenTelemetry()),
	}
	for name, module := range tests {
		t.Run(name, func(t *testing.T) {
			container := ioc.New()
			if err := module.Register(container); !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
				t.Fatalf("expected declaration error, got=%v", err)
			}
			if err := container.Register(&struct{}{}, ioc.WithName("Probe")); err != nil {
				t.Fatalf("expected registry to remain mutable: %v", err)
			}
		})
	}
}

// TestModuleFreezesAdapterOptionsBeforeIOCInit 验证 Module 不在 Register 校验后于 Init 重放带状态 Option。
// 测试方法：使用递增调用次数的 RequestLogOption，完成 Register/Build 后断言只执行一次且适配器可解析。
func TestModuleFreezesAdapterOptionsBeforeIOCInit(t *testing.T) {
	// 第一阶段：构造带副作用的 Option，并注册真实 LoggerFactory 和适配模块。
	calls := 0
	statefulOption := RequestLogOption(func(options *requestLogOptions) error {
		calls++
		options.loggerName = "frozen.request"
		return nil
	})
	container := ioc.New()
	factory := &recordingLoggerFactory{}
	if err := container.Register(factory, ioc.WithDeclare[baselog.LoggerFactory](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register borrowed logger factory: %v", err)
	}
	if err := NewModule(WithRequestLog(statefulOption)).Register(container); err != nil {
		t.Fatalf("register stateful observe module: %v", err)
	}
	// 第二阶段：Build 只消费 Register 阶段冻结的配置，不能再次执行 Option。
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build stateful observe module: %v", err)
	}
	if calls != 1 || factory.name != "frozen.request" {
		t.Fatalf("expected one option execution and frozen value: calls=%d name=%s", calls, factory.name)
	}
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close stateful observe module: %v", err)
	}
}

// TestOpenTelemetryModuleRequiresBorrowedDependenciesAndPublishesBundle 验证 OTel Module 不使用全局 fallback，并把单一 managed bean 发布为原子 Bundle。
// 测试方法：先在空容器 Build 断言依赖缺失，再显式注册 borrowed Provider/Propagator，Build 后解析 Bundle 并验证配对内容。
func TestOpenTelemetryModuleRequiresBorrowedDependenciesAndPublishesBundle(t *testing.T) {
	// 第一阶段：只声明 adapter 而没有 Provider/Propagator 时，IOC Init 必须失败。
	missing := ioc.New()
	if err := NewModule(WithOpenTelemetry()).Register(missing); err != nil {
		t.Fatalf("register missing tracing module: %v", err)
	}
	if err := missing.Build(context.Background()); !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected missing provider/propagator error, got=%v", err)
	}

	// 第二阶段：Provider 与 Propagator 均显式 borrowed；adapter Close 不拥有两者。
	provider := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	providerReference := &tracerProviderStub{provider: provider, samplingPolicyReference: "always_on"}
	propagator := propagation.TraceContext{}
	container := ioc.New()
	if err := container.Register(providerReference, ioc.WithDeclare[TracerProvider](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register borrowed tracer provider: %v", err)
	}
	if err := container.Register(propagator, ioc.WithDeclare[propagation.TextMapPropagator](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register borrowed propagator: %v", err)
	}
	if err := NewModule(WithOpenTelemetry(WithSamplingPolicyReference("always_on"))).Register(container); err != nil {
		t.Fatalf("register tracing module: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build tracing module: %v", err)
	}
	bundle, err := ioc.ResolveAs[appwebhertz.InstrumentationBundle](container)
	if err != nil {
		t.Fatalf("resolve instrumentation bundle: %v", err)
	}
	if len(bundle.HertzOptions()) != 1 || len(bundle.Middleware()) != 1 {
		t.Fatalf("expected paired bundle: options=%d middleware=%d", len(bundle.HertzOptions()), len(bundle.Middleware()))
	}
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close tracing container: %v", err)
	}
}

// containsMetricFamily 判断 Gather 结果是否包含稳定指标名。
func containsMetricFamily(families []*io_prometheus_client.MetricFamily, name string) bool {
	for _, family := range families {
		if family.GetName() == name {
			return true
		}
	}
	return false
}
