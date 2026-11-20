package appwebhertz

import (
	"context"
	"reflect"
	"testing"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route"
	baseapp "github.com/yangzhuangqiu/gobase/app"
	"github.com/yangzhuangqiu/gobase/errcode"
	"github.com/yangzhuangqiu/gobase/ioc"
	baseweb "github.com/yangzhuangqiu/gobase/web"
)

// noRouteHandlerStub 提供可控排序与执行轨迹的 404 Handler。
type noRouteHandlerStub struct {
	// order 是 fallback 链优先级。
	order int
	// name 是执行时写入 trace 的稳定标识。
	name string
	// trace 保存 Handler 的实际执行顺序。
	trace *[]string
}

// Order 返回 404 Handler 的排序值。
func (this *noRouteHandlerStub) Order() int {
	return this.order
}

// HandleNoRoute 记录当前 404 Handler 的执行位置。
func (this *noRouteHandlerStub) HandleNoRoute(context.Context, *hertzapp.RequestContext) {
	*this.trace = append(*this.trace, this.name)
}

// noMethodHandlerStub 提供可控排序与执行轨迹的 405 Handler。
type noMethodHandlerStub struct {
	// order 是 fallback 链优先级。
	order int
	// name 是执行时写入 trace 的稳定标识。
	name string
	// trace 保存 Handler 的实际执行顺序。
	trace *[]string
}

// Order 返回 405 Handler 的排序值。
func (this *noMethodHandlerStub) Order() int {
	return this.order
}

// HandleNoMethod 记录当前 405 Handler 的执行位置。
func (this *noMethodHandlerStub) HandleNoMethod(context.Context, *hertzapp.RequestContext) {
	*this.trace = append(*this.trace, this.name)
}

// workloadRuntimeStub 只替换 Workload 所需的 Container，其他 app.Runtime 能力保持未使用。
type workloadRuntimeStub struct {
	baseapp.Runtime
	// container 是已完成 Build、可解析唯一 web.Server 的测试容器。
	container ioc.Container
}

// Container 返回 Workload 本次解析 Server 使用的 IOC 容器。
func (this *workloadRuntimeStub) Container() ioc.Container {
	return this.container
}

// workloadServerStub 记录 Workload 是否把运行与关闭上下文交给唯一 Server。
type workloadServerStub struct {
	// runContext 是 Run 实际收到的上下文。
	runContext context.Context
	// shutdownContext 是 Shutdown 实际收到的上下文。
	shutdownContext context.Context
}

// Run 记录运行上下文并立即成功返回。
func (this *workloadServerStub) Run(ctx context.Context) error {
	this.runContext = ctx
	return nil
}

// Shutdown 记录语义化 Workload 传入的 graceful 上下文。
func (this *workloadServerStub) Shutdown(ctx context.Context) error {
	this.shutdownContext = ctx
	return nil
}

// TestDefinitionFactoriesPublishApplicationSemanticInterfaces 验证 app-web-hertz 工厂隐藏 IOC 声明细节。
//
// 测试方法：
// 1. 通过 Controller、Filter 和两类 fallback Define 工厂注册应用组件，其中分组能力由 Filter 实现类型决定；
// 2. 注册 Server Module 并 Build；
// 3. 断言 Controller 路由已冻结，且 managed Server 收到全部语义接口切片。
func TestDefinitionFactoriesPublishApplicationSemanticInterfaces(t *testing.T) {
	// 第一阶段：准备配置根依赖和五类应用语义组件 Definition。
	manager := newTestManager(t, map[string]any{
		"web": map[string]any{"hertz": map[string]any{"address": "127.0.0.1:0"}},
	})
	defer func() { _ = manager.Close() }()

	container := ioc.New()
	registerBorrowedManager(t, container, manager)
	routeRegistered := false
	controller := controllerFunc(func(*route.RouterGroup) error {
		routeRegistered = true
		return nil
	})
	filter := &filterStub{order: 10}
	groupedFilter := &groupedFilterStub{filterStub: &filterStub{order: 20}, groupPath: "/api"}
	noRouteTrace := []string{}
	noMethodTrace := []string{}
	noRoute := &noRouteHandlerStub{trace: &noRouteTrace}
	noMethod := &noMethodHandlerStub{trace: &noMethodTrace}
	definitions := []ioc.Definition{
		DefineController(controller, ioc.WithName("GreetingController")),
		DefineFilter(filter, ioc.WithName("AuthFilter")),
		DefineFilter(groupedFilter, ioc.WithName("APIAuditFilter")),
		DefineNoRouteHandler(noRoute, ioc.WithName("JSONNotFound")),
		DefineNoMethodHandler(noMethod, ioc.WithName("JSONMethodNotAllowed")),
	}
	for index, definition := range definitions {
		if err := container.RegisterDefinition(definition); err != nil {
			t.Fatalf("register definition index=%d: %v", index, err)
		}
	}
	// 第二阶段：Module 只注册托管 Server；Build 时由 IOC 完成切片注入与 provider 组装。
	if err := NewModule().Register(container); err != nil {
		t.Fatalf("register web module: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build web definitions: %v", err)
	}
	bean, err := ioc.ResolveAs[*managedServer](container)
	if err != nil {
		t.Fatalf("resolve managed server: %v", err)
	}
	// 第三阶段：验证分组 Filter 只需发布为 Filter，装配阶段仍按对象能力识别其逻辑分组。
	if !routeRegistered || len(bean.Controllers) != 1 || len(bean.Filters) != 2 ||
		len(bean.NoRouteHandlers) != 1 || len(bean.NoMethodHandlers) != 1 {
		t.Fatalf("unexpected injected components: controller=%d filter=%d no_route=%d no_method=%d route_registered=%t",
			len(bean.Controllers), len(bean.Filters), len(bean.NoRouteHandlers), len(bean.NoMethodHandlers), routeRegistered)
	}
	if bean.Filters[1] != groupedFilter {
		t.Fatalf("expected grouped filter to be injected through Filter contract, got=%T", bean.Filters[1])
	}
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close web definitions: %v", err)
	}
}

// TestPrepareFallbackHandlersSortsStablyAndRejectsTypedNil 验证 fallback 组装遵守 Order 与声明顺序。
//
// 测试方法：输入乱序、同 Order 和 typed nil Handler，执行转换后的方法值断言稳定顺序，并检查非法声明错误。
func TestPrepareFallbackHandlersSortsStablyAndRejectsTypedNil(t *testing.T) {
	// 第一阶段：404 Handler 按 Order 排序，相同 Order 保留 Definition 注册顺序。
	noRouteTrace := []string{}
	noRouteHandlers, err := prepareNoRouteHandlers([]NoRouteHandler{
		&noRouteHandlerStub{order: 20, name: "last", trace: &noRouteTrace},
		&noRouteHandlerStub{order: 10, name: "first", trace: &noRouteTrace},
		&noRouteHandlerStub{order: 10, name: "second", trace: &noRouteTrace},
	})
	if err != nil {
		t.Fatalf("prepare no-route handlers: %v", err)
	}
	for _, handler := range noRouteHandlers {
		handler(context.Background(), &hertzapp.RequestContext{})
	}
	if !reflect.DeepEqual(noRouteTrace, []string{"first", "second", "last"}) {
		t.Fatalf("unexpected no-route order: %v", noRouteTrace)
	}

	// 第二阶段：405 Handler 使用同一稳定排序合同。
	noMethodTrace := []string{}
	noMethodHandlers, err := prepareNoMethodHandlers([]NoMethodHandler{
		&noMethodHandlerStub{order: 2, name: "last", trace: &noMethodTrace},
		&noMethodHandlerStub{order: 1, name: "first", trace: &noMethodTrace},
	})
	if err != nil {
		t.Fatalf("prepare no-method handlers: %v", err)
	}
	for _, handler := range noMethodHandlers {
		handler(context.Background(), &hertzapp.RequestContext{})
	}
	if !reflect.DeepEqual(noMethodTrace, []string{"first", "last"}) {
		t.Fatalf("unexpected no-method order: %v", noMethodTrace)
	}

	// 第三阶段：typed nil 在组装前返回可分支声明错误，不把 panic 推迟到请求期。
	var typedNilNoRoute *noRouteHandlerStub
	if _, err := prepareNoRouteHandlers([]NoRouteHandler{typedNilNoRoute}); !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected typed nil no-route declaration error, got=%v", err)
	}
	var typedNilNoMethod *noMethodHandlerStub
	if _, err := prepareNoMethodHandlers([]NoMethodHandler{typedNilNoMethod}); !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected typed nil no-method declaration error, got=%v", err)
	}
}

// TestWorkloadResolvesRunsAndShutsDownUniqueServer 验证推荐 Workload 适配器复用同一 IOC Server。
//
// 测试方法：在容器声明唯一 web.Server，分别调用 Workload Run/Shutdown，断言名称稳定且上下文原样传递。
func TestWorkloadResolvesRunsAndShutsDownUniqueServer(t *testing.T) {
	container := ioc.New()
	server := &workloadServerStub{}
	if err := container.Register(server, ioc.WithDeclare[baseweb.Server]()); err != nil {
		t.Fatalf("register workload server: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build workload container: %v", err)
	}
	defer func() { _ = container.Close(context.Background()) }()

	workload := newServerWorkload()
	if workload.Name() != workloadName {
		t.Fatalf("unexpected workload name: %s", workload.Name())
	}
	runtime := &workloadRuntimeStub{container: container}
	runCtx := context.WithValue(context.Background(), struct{ name string }{"run"}, "run")
	shutdownCtx := context.WithValue(context.Background(), struct{ name string }{"shutdown"}, "shutdown")
	if err := workload.Run(runCtx, runtime, nil); err != nil {
		t.Fatalf("run workload server: %v", err)
	}
	if err := workload.Shutdown(shutdownCtx, runtime, nil); err != nil {
		t.Fatalf("shutdown workload server: %v", err)
	}
	if server.runContext != runCtx || server.shutdownContext != shutdownCtx {
		t.Fatalf("expected contexts to be preserved: run=%v shutdown=%v", server.runContext, server.shutdownContext)
	}
}
