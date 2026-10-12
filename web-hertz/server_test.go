package webhertz

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/app/server/registry"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/yangzhuangqiu/gobase/errcode"
)

// registryStub 仅用于验证 provider 拒绝 Hertz Registry 配置。
type registryStub struct{}

// Register 满足 Hertz Registry 合同；测试不会进入实际注册阶段。
func (this *registryStub) Register(_ *registry.Info) error {
	return nil
}

// Deregister 满足 Hertz Registry 合同；测试不会进入实际注销阶段。
func (this *registryStub) Deregister(_ *registry.Info) error {
	return nil
}

// TestNewRejectsUnsupportedRegistry 验证直接 Run 模式不会接受不完整的 Registry 生命周期。
// 测试方法：通过最终优先级原生 Option 配置 Registry，断言构造阶段返回稳定能力错误。
func TestNewRejectsUnsupportedRegistry(t *testing.T) {
	_, err := New(
		Config{},
		WithHertzOptions(server.WithRegistry(&registryStub{}, &registry.Info{ServiceName: "test"})),
	)
	if !errcode.IsCode[ErrorCode](err, ErrCodeRegistryUnsupported) {
		t.Fatalf("expected registry unsupported error, got=%v", err)
	}
}

// TestNewRejectsNilDeclarations 验证 nil 构造声明在进入 Hertz 前 fail-fast。
// 测试方法：分别传入 nil Option、原生 Option、Middleware 和 RouteFunc，断言统一声明错误码。
func TestNewRejectsNilDeclarations(t *testing.T) {
	tests := map[string][]Option{
		"option":         {nil},
		"hertz option":   {WithHertzOptions(config.Option{})},
		"middleware":     {WithMiddleware(app.HandlerFunc(nil))},
		"route function": {WithRoutes(RouteFunc(nil))},
		"no route":       {WithNoRoute(app.HandlerFunc(nil))},
		"no method":      {WithNoMethod(app.HandlerFunc(nil))},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := New(Config{}, options...)
			if !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
				t.Fatalf("expected declaration error, got=%v", err)
			}
		})
	}
}

// TestNewRegistersRoutesInDeclarationOrder 验证 RouteFunc 顺序稳定且共享 Hertz 原生 Router。
// 测试方法：两个 RouteFunc 依次记录阶段并注册不同路由，断言构造回调顺序未改变。
func TestNewRegistersRoutesInDeclarationOrder(t *testing.T) {
	order := make([]string, 0, 2)
	var firstRouter *route.RouterGroup
	_, err := New(
		Config{},
		WithRoutes(
			func(router *route.RouterGroup) error {
				order = append(order, "first")
				firstRouter = router
				router.GET("/first", emptyHandler)
				return nil
			},
			func(router *route.RouterGroup) error {
				order = append(order, "second")
				if router != firstRouter {
					t.Fatal("expected route functions to share the root router group")
				}
				router.GET("/second", emptyHandler)
				return nil
			},
		),
	)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if !reflect.DeepEqual(order, []string{"first", "second"}) {
		t.Fatalf("unexpected route order: %v", order)
	}
}

// TestNewPassesLifecycleIsolatedRootGroup 验证 RouteFunc 只能访问根路由能力，不能取得 Hertz 生命周期对象。
// 测试方法：记录 RouterGroup 根路径并尝试对动态值断言 *server.Hertz，断言路径为根且生命周期类型不可见。
func TestNewPassesLifecycleIsolatedRootGroup(t *testing.T) {
	basePath := ""
	lifecycleExposed := false
	_, err := New(Config{}, WithRoutes(func(router *route.RouterGroup) error {
		basePath = router.BasePath()
		_, lifecycleExposed = any(router).(*server.Hertz)
		return nil
	}))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if basePath != "/" {
		t.Fatalf("expected root router group, got=%q", basePath)
	}
	if lifecycleExposed {
		t.Fatal("expected Hertz lifecycle object to remain hidden")
	}
}

// TestNewStopsAfterRouteError 验证显式路由错误停止后续构造且保留 cause。
// 测试方法：首个 RouteFunc 返回 sentinel，断言后续函数未执行、错误码与错误链均保留。
func TestNewStopsAfterRouteError(t *testing.T) {
	root := errors.New("route rejected")
	called := false
	_, err := New(
		Config{},
		WithRoutes(
			func(_ *route.RouterGroup) error { return root },
			func(_ *route.RouterGroup) error {
				called = true
				return nil
			},
		),
	)
	if called {
		t.Fatal("expected later route function not to run")
	}
	if !errcode.IsCode[ErrorCode](err, ErrCodeRouteRegistrationFailed) || !errors.Is(err, root) {
		t.Fatalf("expected coded route error with cause, got=%v", err)
	}
}

// TestNewPreservesRoutePanicSemantics 验证路由编程错误补充阶段后仍以 panic fail-fast。
// 测试方法：RouteFunc panic 一个 sentinel error，recover 后断言阶段文本和 errors.Is 身份。
func TestNewPreservesRoutePanicSemantics(t *testing.T) {
	root := errors.New("duplicate route")
	defer func() {
		recovered := recover()
		panicErr, ok := recovered.(error)
		if !ok {
			t.Fatalf("expected error panic, got=%T", recovered)
		}
		if !strings.Contains(panicErr.Error(), "stage=register_routes") || !errors.Is(panicErr, root) {
			t.Fatalf("unexpected route panic: %v", panicErr)
		}
	}()
	_, _ = New(Config{}, WithRoutes(func(_ *route.RouterGroup) error { panic(root) }))
}

// TestNewDoesNotMutateGlobalHlog 验证 provider 构造不会安装或替换进程级 hlog。
// 测试方法：记录构造前后的 DefaultLogger/SystemLogger，断言对象身份保持不变。
func TestNewDoesNotMutateGlobalHlog(t *testing.T) {
	defaultLogger := hlog.DefaultLogger()
	systemLogger := hlog.SystemLogger()
	if _, err := New(Config{}); err != nil {
		t.Fatalf("new server: %v", err)
	}
	if hlog.DefaultLogger() != defaultLogger || hlog.SystemLogger() != systemLogger {
		t.Fatal("expected global hloggers to remain unchanged")
	}
}

// emptyHandler 提供不产生响应内容的 Hertz 原生测试 Handler。
func emptyHandler(_ context.Context, _ *app.RequestContext) {}
