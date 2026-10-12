package appwebhertz

import (
	"context"
	"errors"
	"reflect"
	"testing"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/yangzhuangqiu/gobase/errcode"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// controllerFunc 把测试函数适配为 Controller，并保留显式路由注册行为。
type controllerFunc func(router *route.RouterGroup) error

// GroupPath 让测试函数直接在根 Router 注册。
func (this controllerFunc) GroupPath() string {
	return ""
}

// RegisterRoutes 在 Controller 独占 RouterGroup 中执行测试提供的路由声明。
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

// TestNewRejectsInvalidDeclarations 验证应用装配边界会在创建 Hertz 前拒绝 nil 声明。
// 测试方法：分别传入 nil Option、Controller、typed nil Controller、Filter 和 typed nil Filter，断言稳定声明错误码。
func TestNewRejectsInvalidDeclarations(t *testing.T) {
	var typedNilController *pointerController
	var typedNilFilter *pointerFilter
	tests := map[string]func() error{
		"option": func() error {
			_, err := New(webhertz.Config{}, nil, nil, nil)
			return err
		},
		"server option": func() error {
			_, err := New(webhertz.Config{}, nil, nil, WithServerOptions(nil))
			return err
		},
		"recovery handler": func() error {
			_, err := New(webhertz.Config{}, nil, nil, WithRecoveryHandler(nil))
			return err
		},
		"request ID generator": func() error {
			_, err := New(webhertz.Config{}, nil, nil, WithRequestIDGenerator(nil))
			return err
		},
		"CORS origin validator": func() error {
			_, err := New(webhertz.Config{}, nil, nil, WithCORSOriginValidator(nil))
			return err
		},
		"gzip predicate": func() error {
			_, err := New(webhertz.Config{}, nil, nil, WithGzipPredicate(nil))
			return err
		},
		"controller": func() error {
			_, err := New(webhertz.Config{}, []Controller{nil}, nil)
			return err
		},
		"typed nil controller": func() error {
			_, err := New(webhertz.Config{}, []Controller{typedNilController}, nil)
			return err
		},
		"filter": func() error {
			_, err := New(webhertz.Config{}, nil, []Filter{nil})
			return err
		},
		"typed nil filter": func() error {
			_, err := New(webhertz.Config{}, nil, []Filter{typedNilFilter})
			return err
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

// TestNewRegistersControllersInOrderAndStopsOnError 验证 Controller 顺序稳定且错误会阻止后续路由注册。
// 测试方法：前三个 Controller 依次记录阶段，第二个返回 sentinel，断言第三个未执行且错误链保留 provider cause。
func TestNewRegistersControllersInOrderAndStopsOnError(t *testing.T) {
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
	_, err := New(webhertz.Config{}, controllers, nil)
	if !errcode.IsCode[ErrorCode](err, ErrCodeServerBuildFailed) || !errors.Is(err, root) {
		t.Fatalf("expected layered server build error, got=%v", err)
	}
	if !reflect.DeepEqual(order, []string{"first", "second"}) {
		t.Fatalf("unexpected controller order: %v", order)
	}
}

// TestPrepareFiltersUsesStableOrder 验证全局 Filter 的直接方法值和稳定排序合同。
// 测试方法：输入不同及相同 Order 的 Filter，执行输出 Handler，断言低值优先且同值保持输入顺序。
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
	handlers, err := prepareFilters([]Filter{second, first, third})
	if err != nil {
		t.Fatalf("prepare filters: %v", err)
	}
	for _, handler := range handlers {
		handler(context.Background(), nil)
	}
	if !reflect.DeepEqual(order, []string{"first", "second", "third"}) {
		t.Fatalf("unexpected filter order: %v", order)
	}
}
