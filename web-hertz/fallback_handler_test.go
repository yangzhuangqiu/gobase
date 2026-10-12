package webhertz

import (
	"context"
	"reflect"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
)

// TestWithNoRouteAppendsHandlersAndInheritsMiddleware 验证自定义 404 使用追加语义并继承全局 Middleware。
// 测试方法：分两次声明 NoRoute Handler，直接执行未匹配请求，断言执行顺序、状态和响应体。
func TestWithNoRouteAppendsHandlersAndInheritsMiddleware(t *testing.T) {
	order := make([]string, 0, 4)
	instance, err := New(
		Config{},
		WithMiddleware(func(ctx context.Context, request *app.RequestContext) {
			order = append(order, "middleware_before")
			request.Next(ctx)
			order = append(order, "middleware_after")
		}),
		WithNoRoute(func(ctx context.Context, request *app.RequestContext) {
			order = append(order, "no_route_first")
			request.Next(ctx)
		}),
		WithNoRoute(func(_ context.Context, request *app.RequestContext) {
			order = append(order, "no_route_second")
			request.String(consts.StatusNotFound, "route missing")
		}),
	)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	request := performEngineRequest(instance, consts.MethodGet, "/missing")
	if request.Response.StatusCode() != consts.StatusNotFound || string(request.Response.Body()) != "route missing" {
		t.Fatalf("unexpected no-route response: status=%d body=%q", request.Response.StatusCode(), request.Response.Body())
	}
	expected := []string{"middleware_before", "no_route_first", "no_route_second", "middleware_after"}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("unexpected no-route order: %v", order)
	}
}

// TestWithNoMethodForcesReachable405AndInheritsMiddleware 验证自定义 405 即使遇到关闭原生 Option 仍保持必达。
// 测试方法：注册 GET 路由、全局 Middleware 和多段 NoMethod，再用 POST 请求并断言最终 Option、顺序和响应。
func TestWithNoMethodForcesReachable405AndInheritsMiddleware(t *testing.T) {
	order := make([]string, 0, 4)
	instance, err := New(
		Config{},
		WithMiddleware(func(ctx context.Context, request *app.RequestContext) {
			order = append(order, "middleware_before")
			request.Next(ctx)
			order = append(order, "middleware_after")
		}),
		WithNoMethod(func(ctx context.Context, request *app.RequestContext) {
			order = append(order, "no_method_first")
			request.Next(ctx)
		}),
		WithNoMethod(func(_ context.Context, request *app.RequestContext) {
			order = append(order, "no_method_second")
			request.String(consts.StatusMethodNotAllowed, "method rejected")
		}),
		WithHertzOptions(server.WithHandleMethodNotAllowed(false)),
		WithRoutes(func(router *route.RouterGroup) error {
			router.GET("/items", emptyHandler)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if !instance.engine.GetOptions().HandleMethodNotAllowed {
		t.Fatal("expected WithNoMethod to force method-not-allowed routing")
	}

	request := performEngineRequest(instance, consts.MethodPost, "/items")
	if request.Response.StatusCode() != consts.StatusMethodNotAllowed || string(request.Response.Body()) != "method rejected" {
		t.Fatalf("unexpected no-method response: status=%d body=%q", request.Response.StatusCode(), request.Response.Body())
	}
	expected := []string{"middleware_before", "no_method_first", "no_method_second", "middleware_after"}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("unexpected no-method order: %v", order)
	}
}

// TestFallbackHandlerPanicRemainsProgrammingFailure 验证 provider 不会把未安装 Recovery 的 Handler panic 静默降级。
// 测试方法：让 NoRoute Handler panic，直接执行请求并 recover，断言原始 panic 值保持不变。
func TestFallbackHandlerPanicRemainsProgrammingFailure(t *testing.T) {
	const panicValue = "fallback panic"
	instance, err := New(Config{}, WithNoRoute(func(_ context.Context, _ *app.RequestContext) {
		panic(panicValue)
	}))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	defer func() {
		if recovered := recover(); recovered != panicValue {
			t.Fatalf("unexpected recovered value: %v", recovered)
		}
	}()
	_ = performEngineRequest(instance, consts.MethodGet, "/panic")
}

// performEngineRequest 在不启动 listener 的情况下执行一次 Hertz Router 请求。
// 阶段角色：fallback Handler 合同测试只观察构造完成的 Engine 路由语义。
func performEngineRequest(instance *Server, method string, path string) *app.RequestContext {
	request := instance.engine.NewContext()
	request.Request.SetRequestURI(path)
	request.Request.Header.SetMethod(method)
	instance.engine.ServeHTTP(context.Background(), request)
	return request
}
