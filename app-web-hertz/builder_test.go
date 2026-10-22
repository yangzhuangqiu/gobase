package appwebhertz

import (
	"context"
	"errors"
	"reflect"
	"testing"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/yangzhuangqiu/gobase/errcode"
)

// controllerFunc 把测试函数适配为 Controller，并保留显式路由注册行为。
type controllerFunc func(router *route.RouterGroup) error

// GroupPath 让测试函数直接在根 Router 注册。
func (this controllerFunc) GroupPath() string {
	return ""
}

// RegisterRoutes 在 Controller 独占子 RouterGroup 中执行测试提供的路由声明。
func (this controllerFunc) RegisterRoutes(router *route.RouterGroup) error {
	return this(router)
}

// pointerController 用于构造 Controller 接口中的 typed nil。
type pointerController struct{}

// GroupPath 为 typed nil 测试提供最小 Controller 合同。
func (this *pointerController) GroupPath() string {
	return ""
}

// pointerFilter 用于构造 Filter 接口中的 typed nil。
type pointerFilter struct{}

// Order 为 typed nil 测试提供最小排序合同。
func (this *pointerFilter) Order() int {
	return 0
}

// DoFilter 为 typed nil 测试提供最小执行合同。
func (this *pointerFilter) DoFilter(_ context.Context, _ *hertzapp.RequestContext) {}

// filterStub 提供可控排序和直接执行行为。
type filterStub struct {
	// order 是测试输入的 Middleware 优先级。
	order int
	// handler 是 DoFilter 应执行的测试逻辑。
	handler hertzapp.HandlerFunc
}

// groupedFilterStub 为测试 Filter 增加逻辑分组声明，不改变 Handler 与排序行为。
type groupedFilterStub struct {
	*filterStub
	// groupPath 是测试指定的逻辑分组表达。
	groupPath string
}

// GroupPath 返回测试指定的分组路径。
func (this *groupedFilterStub) GroupPath() string {
	return this.groupPath
}

// Order 返回测试声明的 Middleware 优先级。
func (this *filterStub) Order() int {
	return this.order
}

// DoFilter 直接执行测试声明的 Hertz Middleware 逻辑。
func (this *filterStub) DoFilter(ctx context.Context, request *hertzapp.RequestContext) {
	if this.handler != nil {
		this.handler(ctx, request)
	}
}

// TestBuildServerRejectsInvalidDeclarations 验证私有统一装配边界会在创建 Hertz 前拒绝 nil 声明。
// 测试方法：分别传入 nil Option、Controller、typed nil Controller、Filter 和 typed nil Filter，断言稳定声明错误码。
func TestBuildServerRejectsInvalidDeclarations(t *testing.T) {
	var typedNilController *pointerController
	var typedNilFilter *pointerFilter
	tests := map[string]func() error{
		"option": func() error {
			return buildServerTestError(Config{}, serverComponents{}, nil)
		},
		"server option": func() error {
			return buildServerTestError(Config{}, serverComponents{}, WithServerOptions(nil))
		},
		"recovery handler": func() error {
			return buildServerTestError(Config{}, serverComponents{}, WithRecoveryHandler(nil))
		},
		"request ID generator": func() error {
			return buildServerTestError(Config{}, serverComponents{}, WithRequestIDGenerator(nil))
		},
		"CORS origin validator": func() error {
			return buildServerTestError(Config{}, serverComponents{}, WithCORSOriginValidator(nil))
		},
		"gzip predicate": func() error {
			return buildServerTestError(Config{}, serverComponents{}, WithGzipPredicate(nil))
		},
		"controller": func() error {
			return buildServerTestError(Config{}, serverComponents{controllers: []Controller{nil}})
		},
		"typed nil controller": func() error {
			return buildServerTestError(Config{}, serverComponents{controllers: []Controller{typedNilController}})
		},
		"filter": func() error {
			return buildServerTestError(Config{}, serverComponents{filters: []Filter{nil}})
		},
		"typed nil filter": func() error {
			return buildServerTestError(Config{}, serverComponents{filters: []Filter{typedNilFilter}})
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			if err := run(); !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
				t.Fatalf("expected declaration error, got=%v", err)
			}
		})
	}
}

// TestBuildServerRegistersControllersInOrderAndStopsOnError 验证 Controller 顺序稳定且错误会阻止后续路由注册。
// 测试方法：前三个 Controller 依次记录阶段，第二个返回 sentinel，断言第三个未执行且错误链保留 provider cause。
func TestBuildServerRegistersControllersInOrderAndStopsOnError(t *testing.T) {
	root := errors.New("route rejected")
	order := make([]string, 0, 3)
	controllers := []Controller{
		controllerFunc(func(_ *route.RouterGroup) error {
			order = append(order, "first")
			return nil
		}),
		controllerFunc(func(_ *route.RouterGroup) error {
			order = append(order, "second")
			return root
		}),
		controllerFunc(func(_ *route.RouterGroup) error {
			order = append(order, "third")
			return nil
		}),
	}
	err := buildServerTestError(Config{}, serverComponents{controllers: controllers})
	if !errcode.IsCode[ErrorCode](err, ErrCodeServerBuildFailed) || !errors.Is(err, root) {
		t.Fatalf("expected layered server build error, got=%v", err)
	}
	if !reflect.DeepEqual(order, []string{"first", "second"}) {
		t.Fatalf("unexpected controller order: %v", order)
	}
}

// buildServerTestError 解析测试 Option 并调用私有统一装配核心，只返回待断言错误。
// 阶段角色：公共显式构造器删除后，测试仍覆盖 Option、组件和 provider 的完整失败边界。
func buildServerTestError(input Config, components serverComponents, options ...Option) error {
	resolvedOptions, err := resolveOptions(options)
	if err != nil {
		return err
	}
	_, err = buildServer(input, components, resolvedOptions)
	return err
}

// TestPrepareFiltersUsesStableOrder 验证全局 Filter 的直接方法值和稳定排序合同。
// 测试方法：输入不同及相同 Order 的普通 Filter，执行全局 Handler，断言低值优先且同值保持输入顺序。
func TestPrepareFiltersUsesStableOrder(t *testing.T) {
	order := make([]string, 0, 3)
	newFilter := func(priority int, name string) *filterStub {
		return &filterStub{
			order: priority,
			handler: func(_ context.Context, _ *hertzapp.RequestContext) {
				order = append(order, name)
			},
		}
	}
	second := newFilter(20, "second")
	first := newFilter(10, "first")
	third := newFilter(20, "third")
	plan, err := prepareFilters([]Filter{second, first, third})
	if err != nil {
		t.Fatalf("prepare filters: %v", err)
	}
	for _, handler := range plan.globalHandlers {
		handler(context.Background(), nil)
	}
	if len(plan.groupedFilters) != 0 || !reflect.DeepEqual(order, []string{"first", "second", "third"}) {
		t.Fatalf("unexpected filter order: %v", order)
	}
}

// TestPrepareFiltersClassifiesGroupedScopeByInterface 验证 Filter 的接口类型而非 GroupPath 文本决定全局性。
// 测试方法：混合普通 Filter、空路径与 `/` 分组 Filter，断言普通项进入全局链，两个根表达合并且独立排序。
func TestPrepareFiltersClassifiesGroupedScopeByInterface(t *testing.T) {
	trace := make([]string, 0, 3)
	newHandler := func(name string) hertzapp.HandlerFunc {
		return func(context.Context, *hertzapp.RequestContext) { trace = append(trace, name) }
	}
	global := &filterStub{order: 100, handler: newHandler("global")}
	rootLater := &groupedFilterStub{filterStub: &filterStub{order: 20, handler: newHandler("root-later")}, groupPath: "/"}
	rootFirst := &groupedFilterStub{filterStub: &filterStub{order: 10, handler: newHandler("root-first")}, groupPath: ""}

	plan, err := prepareFilters([]Filter{rootLater, global, rootFirst})
	if err != nil {
		t.Fatalf("prepare grouped filters: %v", err)
	}
	if len(plan.globalHandlers) != 1 || len(plan.groupedFilters) != 1 || plan.groupedFilters[0].groupPath != "/" {
		t.Fatalf("unexpected scope plan: global=%d grouped=%+v", len(plan.globalHandlers), plan.groupedFilters)
	}
	plan.globalHandlers[0](context.Background(), nil)
	for _, handler := range plan.groupedFilters[0].handlers {
		handler(context.Background(), nil)
	}
	if !reflect.DeepEqual(trace, []string{"global", "root-first", "root-later"}) {
		t.Fatalf("unexpected scoped filter order: %v", trace)
	}
}

// TestPrepareFiltersRejectsInvalidGroupedPath 验证非根分组仍必须使用绝对逻辑路径。
// 测试方法：传入 GroupPath 缺少前导斜线的 GroupedFilter，断言构造阶段返回稳定声明错误码。
func TestPrepareFiltersRejectsInvalidGroupedPath(t *testing.T) {
	filter := &groupedFilterStub{filterStub: &filterStub{}, groupPath: "api"}
	_, err := prepareFilters([]Filter{filter})
	if !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected grouped filter declaration error, got=%v", err)
	}
}
