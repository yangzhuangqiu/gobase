package main

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
	appwebhertz "github.com/yangzhuangqiu/gobase/app-web-hertz"
	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
	baseweb "github.com/yangzhuangqiu/gobase/web"
)

// TestWebHertzModuleImplementsAppRuntimeContract 验证 Web Module 可直接交给 app-runtime。
// 测试方法：执行编译期接口赋值并断言稳定名称，避免额外 IOC 适配层。
func TestWebHertzModuleImplementsAppRuntimeContract(t *testing.T) {
	var module appruntime.Module = appwebhertz.NewModule()
	if module.Name() != "app-web-hertz" {
		t.Fatalf("unexpected module name: %s", module.Name())
	}
}

// TestLoadAddressUsesDefaultAndEnvironmentOverride 验证监听地址具备确定默认值和环境覆盖入口。
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

// TestGreetingControllerRoutes 验证自动 GET/POST 和手动路径参数路由可叠加在同一逻辑分组。
// 测试方法：按 Controller 合同装配 RouterGroup，分别请求三个入口并断言状态与响应。
func TestGreetingControllerRoutes(t *testing.T) {
	// 第一阶段：用与生产合同相同的 GroupPath 构造 Controller 路由分组。
	engine := route.NewEngine(config.NewOptions(nil))
	controller := &GreetingController{}
	group := engine.Group("/").Group(controller.GroupPath())
	if err := controller.RegisterRoutes(group); err != nil {
		t.Fatalf("register manual routes: %v", err)
	}
	group.GET("/hello", controller.GetOrPostHello)
	group.POST("/hello", controller.GetOrPostHello)

	// 第二阶段：分别覆盖自动多 method 和手动动态路由。
	assertResponseContains(t, engine, "GET", "/api/hello", `"method":"GET"`)
	assertResponseContains(t, engine, "POST", "/api/hello", `"method":"POST"`)
	assertResponseContains(t, engine, "GET", "/api/users/gobase", `"message":"hello, gobase"`)
}

// TestApplicationDefinitionsBuildCompleteServerGraph 验证最新 Definition + Module 路径能构建完整 Server 依赖图。
// 测试方法：注册 borrowed Manager/LoggerFactory、全部示例 Definitions 和 Web Module，Build 后解析各语义组件与唯一 Server。
func TestApplicationDefinitionsBuildCompleteServerGraph(t *testing.T) {
	manager, err := baseconfig.New(
		baseconfig.File(baseconfig.FileDisabled()),
		baseconfig.Defaults(defaultConfig()),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build quickstart config manager: %v", err)
	}
	defer func() { _ = manager.Close() }()

	// 第一阶段：裸 IOC 测试显式发布 Runtime 通常提供的 Manager/LoggerFactory，再声明组件和 Server Module。
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
	if err := appwebhertz.NewModule().Register(container); err != nil {
		t.Fatalf("register web module: %v", err)
	}

	// 第二阶段：IOC 注入完整组件集合，managed Server 在 Init 中完成 provider 装配。
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build quickstart component graph: %v", err)
	}
	controllers, controllerErr := ioc.ResolveAllAs[appwebhertz.Controller](container)
	filters, filterErr := ioc.ResolveAllAs[appwebhertz.Filter](container)
	noRouteHandlers, noRouteErr := ioc.ResolveAllAs[appwebhertz.NoRouteHandler](container)
	noMethodHandlers, noMethodErr := ioc.ResolveAllAs[appwebhertz.NoMethodHandler](container)
	server, serverErr := ioc.ResolveAs[baseweb.Server](container)
	if controllerErr != nil || filterErr != nil || noRouteErr != nil || noMethodErr != nil || serverErr != nil ||
		len(controllers) != 1 || len(filters) != 2 || len(noRouteHandlers) != 1 || len(noMethodHandlers) != 1 || server == nil {
		t.Fatalf("unexpected graph: controllers=%d filters=%d no_route=%d no_method=%d errors=[%v %v %v %v %v]",
			len(controllers), len(filters), len(noRouteHandlers), len(noMethodHandlers),
			controllerErr, filterErr, noRouteErr, noMethodErr, serverErr)
	}
	if _, grouped := filters[1].(appwebhertz.GroupedFilter); !grouped {
		t.Fatalf("expected second Filter to expose grouped capability, got=%T", filters[1])
	}
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close quickstart component graph: %v", err)
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
