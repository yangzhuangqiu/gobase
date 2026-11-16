package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	webhertzobserve "github.com/yangzhuangqiu/gobase/app-observe-webhertz"
	appwebhertz "github.com/yangzhuangqiu/gobase/app-web-hertz"
	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
	baseweb "github.com/yangzhuangqiu/gobase/web"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// TestLoadAddressesUseDefaultsAndEnvironmentOverrides 验证业务与 Metrics 地址使用独立、确定的覆盖入口。
// 测试方法：分别写入空白和带空格的环境变量，断言默认端口与 trim 后覆盖值。
func TestLoadAddressesUseDefaultsAndEnvironmentOverrides(t *testing.T) {
	t.Setenv(businessAddressEnv, "  ")
	t.Setenv(metricsAddressEnv, "")
	if actual := loadBusinessAddress(); actual != defaultBusinessAddress {
		t.Fatalf("unexpected default business address: %q", actual)
	}
	if actual := loadMetricsAddress(); actual != defaultMetricsAddress {
		t.Fatalf("unexpected default metrics address: %q", actual)
	}

	t.Setenv(businessAddressEnv, " 127.0.0.1:28082 ")
	t.Setenv(metricsAddressEnv, " 127.0.0.1:29082 ")
	if actual := loadBusinessAddress(); actual != "127.0.0.1:28082" {
		t.Fatalf("unexpected business override: %q", actual)
	}
	if actual := loadMetricsAddress(); actual != "127.0.0.1:29082" {
		t.Fatalf("unexpected metrics override: %q", actual)
	}
}

// TestApplicationDefinitionsBuildObservableServerGraph 验证高级示例能构建 Server、Observer、Handler、Bundle 与 Provider 完整依赖图。
// 测试方法：模拟 app-runtime 注册 borrowed 基础设施后 Build 两个 Module，按语义接口解析并写入一次观测，再抓取 Metrics 断言稳定名称和路由模板。
func TestApplicationDefinitionsBuildObservableServerGraph(t *testing.T) {
	manager, err := baseconfig.New(
		baseconfig.File(baseconfig.FileDisabled()),
		baseconfig.Defaults(defaultConfig()),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build example config manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	var loggingConfig logzap.Config
	if err := manager.Current().Decode("log.zap", &loggingConfig); err != nil {
		t.Fatalf("decode required logging scope: %v", err)
	}
	if _, err := logzap.ResolveConfig(loggingConfig); err != nil {
		t.Fatalf("resolve required logging config: %v", err)
	}

	// 第一阶段：复刻 app-runtime 的基础设施 bean，再按生产顺序声明应用与模块。
	container := ioc.New()
	if err := container.Register(manager, ioc.WithDeclare[baseconfig.Manager](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register config manager: %v", err)
	}
	loggerFactory := baselog.NewLoggerFactory(nil)
	if err := container.Register(loggerFactory, ioc.WithDeclare[baselog.LoggerFactory](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register logger factory: %v", err)
	}
	for index, definition := range applicationDefinitions() {
		if err := container.RegisterDefinition(definition); err != nil {
			t.Fatalf("register application definition index=%d: %v", index, err)
		}
	}
	if err := observationModule().Register(container); err != nil {
		t.Fatalf("register observation module: %v", err)
	}
	if err := appwebhertz.NewModule().Register(container); err != nil {
		t.Fatalf("register web module: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build observable server graph: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Close(context.Background()); err != nil {
			t.Errorf("close observable server graph: %v", err)
		}
	})

	// 第二阶段：每个接口均从 IOC 解析，证明 adapter 无需全局变量或手工回填。
	server, serverErr := ioc.ResolveAs[baseweb.Server](container)
	observer, observerErr := ioc.ResolveAs[webhertz.RequestObserver](container)
	handler, handlerErr := ioc.ResolveAs[webhertzobserve.PrometheusHandler](container)
	bundle, bundleErr := ioc.ResolveAs[appwebhertz.InstrumentationBundle](container)
	provider, providerErr := ioc.ResolveAs[webhertzobserve.TracerProvider](container)
	if serverErr != nil || observerErr != nil || handlerErr != nil || bundleErr != nil || providerErr != nil ||
		server == nil || observer == nil || handler == nil || bundle == nil || provider == nil {
		t.Fatalf("unexpected graph errors: server=%v observer=%v handler=%v bundle=%v provider=%v",
			serverErr, observerErr, handlerErr, bundleErr, providerErr)
	}
	if len(bundle.HertzOptions()) != 1 || len(bundle.Middleware()) != 1 {
		t.Fatalf("unexpected instrumentation bundle: options=%d middleware=%d", len(bundle.HertzOptions()), len(bundle.Middleware()))
	}

	// 第三阶段：用稳定完成态快照驱动 Collector，确认动态资源值不会进入 route label。
	lifecycleObserver, ok := observer.(webhertz.RequestLifecycleObserver)
	if !ok {
		t.Fatalf("observer does not expose lifecycle contract: %T", observer)
	}
	lifecycleObserver.RequestStarted(context.Background(), "GET")
	observer.Observe(context.Background(), webhertz.HTTPRequestObservation{
		Method:        "GET",
		RoutePattern:  "/api/orders/:id",
		Path:          "/api/orders/42",
		Status:        200,
		RequestBytes:  webhertz.UnknownBodySize,
		ResponseBytes: webhertz.UnknownBodySize,
		Outcome:       webhertz.RequestOutcomeSuccess,
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	body := recorder.Body.String()
	if recorder.Code != 200 || !strings.Contains(body, "quickstart_http_server_requests_total") ||
		!strings.Contains(body, `route="/api/orders/:id"`) {
		t.Fatalf("unexpected metrics response: status=%d body=%s", recorder.Code, body)
	}
}
