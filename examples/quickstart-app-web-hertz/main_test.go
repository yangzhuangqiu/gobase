package main

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	webhertzobserve "github.com/yangzhuangqiu/gobase/app-observe-webhertz"
	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
	appwebhertz "github.com/yangzhuangqiu/gobase/app-web-hertz"
	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
	"go.opentelemetry.io/otel/propagation"
)

// TestWebHertzModuleImplementsAppRuntimeContract 验证同模块 IOC 适配可直接交给 app-runtime 使用。
// 测试方法：执行编译期接口赋值，确保不需要额外 app-web-hertz-ioc 模块或运行期类型转换。
func TestWebHertzModuleImplementsAppRuntimeContract(t *testing.T) {
	var module appruntime.Module = appwebhertz.NewModule("")
	if module.Name() != "app-web-hertz" {
		t.Fatalf("unexpected module name: %s", module.Name())
	}
	var observeModule appruntime.Module = webhertzobserve.NewModule(webhertzobserve.WithRequestLog())
	if observeModule.Name() != "app-observe-webhertz" {
		t.Fatalf("unexpected observe module name: %s", observeModule.Name())
	}
}

// TestLoadAddressUsesDefaultAndEnvironmentOverride 验证独立 Web 配置具备确定默认地址和环境覆盖入口。
// 测试方法：分别写入空白与显式环境变量，断言 trim 后地址符合启动语义。
func TestLoadAddressUsesDefaultAndEnvironmentOverride(t *testing.T) {
	t.Setenv(quickstartAddressEnv, "  ")
	if actual := loadAddress(); actual != defaultQuickstartAddress {
		t.Fatalf("unexpected default address: %q", actual)
	}
	t.Setenv(quickstartAddressEnv, " 127.0.0.1:28081 ")
	if actual := loadAddress(); actual != "127.0.0.1:28081" {
		t.Fatalf("unexpected override address: %q", actual)
	}
}

// TestGreetingControllerRoutes 验证自动 GET/POST 和手动路径参数路由可叠加在同一 Controller 分组。
// 测试方法：按 app-web-hertz 约定装配原生 RouterGroup，分别请求三个入口并断言状态与响应。
func TestGreetingControllerRoutes(t *testing.T) {
	engine := route.NewEngine(config.NewOptions(nil))
	controller := &GreetingController{}
	group := engine.Group("/").Group(controller.GroupPath())
	if err := controller.RegisterRoutes(group); err != nil {
		t.Fatalf("register manual routes: %v", err)
	}
	group.GET("/hello", controller.GetOrPostHello)
	group.POST("/hello", controller.GetOrPostHello)

	assertResponseContains(t, engine, "GET", "/api/hello", `"method":"GET"`)
	assertResponseContains(t, engine, "POST", "/api/hello", `"method":"POST"`)
	assertResponseContains(t, engine, "GET", "/api/users/gobase", `"message":"hello, gobase"`)
}

// TestObservationAndWebModulesBuildTogether 验证 Quickstart 的 Request Log Adapter、配置与 Web Server 形成可关闭的真实 IOC 依赖图。
// 测试方法：注册 borrowed Manager/LoggerFactory 和 Controller，按 main 顺序注册两个 Module，Build 后关闭并断言无依赖或生命周期错误。
func TestObservationAndWebModulesBuildTogether(t *testing.T) {
	// 第一阶段：使用示例默认配置创建单快照 Manager，并声明两个 borrowed 根依赖。
	manager, err := baseconfig.New(baseconfig.File(baseconfig.FileDisabled()), baseconfig.Defaults(defaultConfig())).Build(context.Background())
	if err != nil {
		t.Fatalf("build quickstart config manager: %v", err)
	}
	defer func() { _ = manager.Close() }()
	container := ioc.New()
	if err := container.Register(manager, ioc.WithDeclare[baseconfig.Manager](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register config manager: %v", err)
	}
	loggerFactory := baselog.NewLoggerFactory(nil)
	if err := container.Register(loggerFactory, ioc.WithDeclare[baselog.LoggerFactory](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register logger factory: %v", err)
	}
	if err := container.Register(&GreetingController{}, ioc.WithDeclare[appwebhertz.Controller]()); err != nil {
		t.Fatalf("register greeting controller: %v", err)
	}

	// 第二阶段：Adapter 先声明语义接口，Web Module 自动注入后构造并由 Close 逆序收敛。
	if err := webhertzobserve.NewModule(webhertzobserve.WithRequestLog()).Register(container); err != nil {
		t.Fatalf("register observation module: %v", err)
	}
	if err := appwebhertz.NewModule("").Register(container); err != nil {
		t.Fatalf("register web module: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build quickstart module graph: %v", err)
	}
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close quickstart module graph: %v", err)
	}
}

// TestTracingProviderAdapterAndWebCloseAsOneDependencyGraph 验证 Provider owner 覆盖 adapter/Web 生命周期并在最后 Shutdown。
// 测试方法：显式注册 Provider、OTel adapter 和 Web Module，Build 后解析 Bundle，再 Close 容器并断言 Provider 引用已清空。
func TestTracingProviderAdapterAndWebCloseAsOneDependencyGraph(t *testing.T) {
	// 第一阶段：准备 Web Server 所需配置根依赖和 Controller。
	manager, err := baseconfig.New(baseconfig.File(baseconfig.FileDisabled()), baseconfig.Defaults(defaultConfig())).Build(context.Background())
	if err != nil {
		t.Fatalf("build tracing config manager: %v", err)
	}
	defer func() { _ = manager.Close() }()
	container := ioc.New()
	if err := container.Register(manager, ioc.WithDeclare[baseconfig.Manager](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register tracing config manager: %v", err)
	}
	if err := container.Register(&GreetingController{}, ioc.WithDeclare[appwebhertz.Controller]()); err != nil {
		t.Fatalf("register tracing controller: %v", err)
	}
	loggerFactory := baselog.NewLoggerFactory(nil)
	if err := container.Register(loggerFactory, ioc.WithDeclare[baselog.LoggerFactory](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register tracing logger factory: %v", err)
	}

	// 第二阶段：按 owner -> adapter -> consumer 声明模块，IOC 依赖图决定真实 Init/Close 顺序。
	providerModule := newApplicationTracingModule(applicationTracingConfig{
		ServiceName:             "test-service",
		SamplingPolicyReference: defaultApplicationSamplingPolicyReference,
	})
	if err := providerModule.Register(container); err != nil {
		t.Fatalf("register tracing provider module: %v", err)
	}
	if err := webhertzobserve.NewModule(
		webhertzobserve.WithRequestLog(),
		webhertzobserve.WithOpenTelemetry(
			webhertzobserve.WithSamplingPolicyReference(defaultApplicationSamplingPolicyReference),
		),
	).Register(container); err != nil {
		t.Fatalf("register tracing adapter module: %v", err)
	}
	if err := appwebhertz.NewModule("").Register(container); err != nil {
		t.Fatalf("register tracing web module: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build tracing dependency graph: %v", err)
	}
	bundle, err := ioc.ResolveAs[appwebhertz.InstrumentationBundle](container)
	if err != nil || len(bundle.HertzOptions()) != 1 || len(bundle.Middleware()) != 1 {
		t.Fatalf("unexpected resolved tracing bundle: bundle=%v err=%v", bundle, err)
	}
	propagator, err := ioc.ResolveAs[propagation.TextMapPropagator](container)
	if err != nil || len(propagator.Fields()) == 0 {
		t.Fatalf("unexpected resolved propagator: propagator=%v err=%v", propagator, err)
	}
	provider, err := ioc.ResolveAs[*applicationTracerProvider](container)
	if err != nil || provider.OTelTracerProvider() == nil {
		t.Fatalf("unexpected initialized provider: provider=%v err=%v", provider, err)
	}

	// 第三阶段：Container 逆序先关闭 Web/adapter，再 Flush/Shutdown Provider owner。
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close tracing dependency graph: %v", err)
	}
	if provider.OTelTracerProvider() != nil {
		t.Fatal("expected tracer provider shutdown after dependent Web/adapter close")
	}
}

// assertResponseContains 执行无网络 Hertz 请求并校验教学响应中的关键语义。
func assertResponseContains(t *testing.T, engine *route.Engine, method string, path string, expected string) {
	t.Helper()
	response := ut.PerformRequest(engine, method, path, nil).Result()
	if response.StatusCode() != consts.StatusOK || !strings.Contains(string(response.Body()), expected) {
		t.Fatalf("unexpected response: method=%s path=%s status=%d body=%s", method, path, response.StatusCode(), response.Body())
	}
}
