package appwebhertz

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// routeVerb 保存方法名前缀与 Hertz HTTP method 的稳定映射。
type routeVerb struct {
	// token 是 Controller 方法名中大小写敏感的动词片段。
	token string
	// method 是传给 Hertz Handle 的标准 HTTP method；Any 由专用分支处理。
	method string
	// any 表示该动词应调用 Hertz Any，而不是只注册一个 method。
	any bool
}

// automaticRoute 是构造阶段从一个 Controller 方法解析出的不可变路由计划。
type automaticRoute struct {
	// verbs 是按方法名声明顺序去重后的 HTTP 动词。
	verbs []routeVerb
	// path 是已经应用 RoutePathStyle 的根相对路径。
	path string
	// handler 是绑定具体 Controller 实例的方法值。
	handler hertzapp.HandlerFunc
}

// controllerRoutePlan 保存一个 Controller 的分组、手动扩展与自动路由计划。
type controllerRoutePlan struct {
	// controllerIndex 是输入或 IOC 聚合顺序，用于错误定位。
	controllerIndex int
	// groupPath 是归一化后的 Controller RouterGroup 前缀；空声明统一表示为 `/`。
	groupPath string
	// registrar 是 Controller 可选实现的原生路由扩展。
	registrar RouteRegistrar
	// automaticRoutes 按导出方法名顺序保存自动路由。
	automaticRoutes []automaticRoute
}

var (
	// automaticRouteVerbs 是自动路由支持的 Hertz method 集合和解析优先级。
	automaticRouteVerbs = []routeVerb{
		{token: "Options", method: consts.MethodOptions},
		{token: "Delete", method: consts.MethodDelete},
		{token: "Patch", method: consts.MethodPatch},
		{token: "Post", method: consts.MethodPost},
		{token: "Head", method: consts.MethodHead},
		{token: "Get", method: consts.MethodGet},
		{token: "Put", method: consts.MethodPut},
		{token: "Any", any: true},
	}
	// contextType 和 requestContextType 用于拒绝看似路由但签名不符合 Hertz 的方法。
	contextType        = reflect.TypeOf((*context.Context)(nil)).Elem()
	requestContextType = reflect.TypeOf((*hertzapp.RequestContext)(nil))
	// handlerType 用于把已校验的反射方法值一次转换为原生 Hertz Handler。
	handlerType = reflect.TypeOf(hertzapp.HandlerFunc(nil))
)

// prepareControllers 校验 Controller 并构建保持输入顺序的 RouteFunc。
// 阶段角色：只做反射扫描和不可变计划，不执行 RegisterRoutes 或修改 Hertz Router。
func prepareControllers(controllers []Controller, style RoutePathStyle) ([]webhertz.RouteFunc, error) {
	routes := make([]webhertz.RouteFunc, 0, len(controllers))
	for index, controller := range controllers {
		if isNilInterface(controller) {
			return nil, errorForDeclarationInvalid(messagef("op=app_web_hertz.new result=failed stage=validate_controllers reason=controller_required controller_index=%d", index))
		}
		plan, err := buildControllerRoutePlan(controller, index, style)
		if err != nil {
			return nil, err
		}
		routes = append(routes, plan.register)
	}
	return routes, nil
}

// buildControllerRoutePlan 解析单个 Controller 的分组、可选手动注册和自动路由。
// 阶段角色：Server 构造前置校验；没有任何路由能力的 Controller 会 fail-fast。
func buildControllerRoutePlan(controller Controller, index int, style RoutePathStyle) (*controllerRoutePlan, error) {
	groupPath := strings.TrimSpace(controller.GroupPath())
	if groupPath == "" {
		groupPath = "/"
	}
	if !strings.HasPrefix(groupPath, "/") {
		return nil, errorForDeclarationInvalid(messagef("op=app_web_hertz.new result=failed stage=validate_controller_group reason=leading_slash_required controller_index=%d group_path=%q", index, groupPath))
	}

	// 手动注册是可选能力；自动扫描始终执行，两种声明最终叠加到同一个分组。
	registrar, _ := controller.(RouteRegistrar)
	automaticRoutes, err := scanAutomaticRoutes(controller, index, style)
	if err != nil {
		return nil, err
	}
	if registrar == nil && len(automaticRoutes) == 0 {
		return nil, errorForDeclarationInvalid(messagef("op=app_web_hertz.new result=failed stage=validate_controller_routes reason=route_required controller_index=%d controller_type=%T", index, controller))
	}
	return &controllerRoutePlan{
		controllerIndex: index,
		groupPath:       groupPath,
		registrar:       registrar,
		automaticRoutes: automaticRoutes,
	}, nil
}

// scanAutomaticRoutes 扫描 Controller 导出方法并生成确定性的静态路由计划。
// 阶段角色：方法名前缀未命中时忽略；命中后签名、动词组合和路径必须全部有效。
func scanAutomaticRoutes(controller Controller, controllerIndex int, style RoutePathStyle) ([]automaticRoute, error) {
	controllerType := reflect.TypeOf(controller)
	controllerValue := reflect.ValueOf(controller)
	routes := make([]automaticRoute, 0, controllerType.NumMethod())

	// reflect.Type.Method 按导出方法名词法顺序枚举，固定自动路由注册顺序。
	for methodIndex := 0; methodIndex < controllerType.NumMethod(); methodIndex++ {
		method := controllerType.Method(methodIndex)
		verbs, suffix, matched, err := parseAutomaticRouteMethod(method.Name)
		if !matched {
			continue
		}
		if err != nil {
			return nil, errorForDeclarationInvalid(
				messagef("op=app_web_hertz.new result=failed stage=scan_controller_routes reason=method_name_invalid controller_index=%d controller_type=%s method=%s", controllerIndex, controllerType, method.Name),
				err,
			)
		}
		if !isHertzHandlerMethod(method.Type) {
			return nil, errorForDeclarationInvalid(messagef("op=app_web_hertz.new result=failed stage=scan_controller_routes reason=handler_signature_invalid controller_index=%d controller_type=%s method=%s expected=func(context.Context,*app.RequestContext)", controllerIndex, controllerType, method.Name))
		}
		handler := controllerValue.Method(methodIndex).Convert(handlerType).Interface().(hertzapp.HandlerFunc)
		routes = append(routes, automaticRoute{
			verbs:   verbs,
			path:    style.routePath(suffix),
			handler: handler,
		})
	}
	return routes, nil
}

// parseAutomaticRouteMethod 解析 GetOrPostXxx、AnyXxx 等方法名前缀。
// 阶段角色：区分普通业务方法与选择加入自动注册但声明非法的方法。
func parseAutomaticRouteMethod(name string) ([]routeVerb, string, bool, error) {
	first, ok := consumeRouteVerb(name)
	if !ok {
		return nil, "", false, nil
	}
	verbs := []routeVerb{first}
	remainder := strings.TrimPrefix(name, first.token)
	seen := map[string]struct{}{first.token: {}}

	// `Or` 只有后接完整受支持动词时才是组合符，GetOrder 仍解析为 GET + Order。
	for strings.HasPrefix(remainder, "Or") {
		next, found := consumeRouteVerb(strings.TrimPrefix(remainder, "Or"))
		if !found {
			break
		}
		if first.any || next.any {
			return nil, "", true, fmt.Errorf("any_cannot_combine")
		}
		if _, duplicated := seen[next.token]; duplicated {
			return nil, "", true, fmt.Errorf("verb_duplicated verb=%s", next.token)
		}
		verbs = append(verbs, next)
		seen[next.token] = struct{}{}
		remainder = strings.TrimPrefix(strings.TrimPrefix(remainder, "Or"), next.token)
	}
	if remainder == "" {
		return nil, "", true, fmt.Errorf("path_suffix_required")
	}
	return verbs, remainder, true, nil
}

// consumeRouteVerb 从字符串开头读取一个受支持的 Controller 动词。
// 阶段角色：由单动词和 Or 组合解析共同使用，保持动词集合唯一。
func consumeRouteVerb(value string) (routeVerb, bool) {
	for _, verb := range automaticRouteVerbs {
		if strings.HasPrefix(value, verb.token) {
			return verb, true
		}
	}
	return routeVerb{}, false
}

// isHertzHandlerMethod 判断反射类型是否为带 receiver 的标准 Hertz Handler 方法。
// 阶段角色：路由注册前校验，避免请求到达后才因反射调用不匹配而 panic。
func isHertzHandlerMethod(methodType reflect.Type) bool {
	return methodType.NumIn() == 3 &&
		methodType.In(1) == contextType &&
		methodType.In(2) == requestContextType &&
		methodType.NumOut() == 0
}

// register 在 Hertz 构造期把一个 Controller 计划注册到独占 RouterGroup。
// 阶段角色：先执行可选 RegisterRoutes，再按稳定顺序叠加方法名自动路由。
func (this *controllerRoutePlan) register(router *route.RouterGroup) error {
	// 每个 Controller 都创建 group；`/` 只表示无 URL 前缀，不共享根 Router 的 Middleware 作用域。
	target := router.Group(this.groupPath)

	// 手动声明优先，使动态路由和 group Middleware 在自动静态路由前完成装配。
	if this.registrar != nil {
		if err := this.registrar.RegisterRoutes(target); err != nil {
			return fmt.Errorf("op=app_web_hertz.register_routes result=failed stage=manual controller_index=%d group_path=%q reason=registrar_failed: %w", this.controllerIndex, this.groupPath, err)
		}
	}
	for _, item := range this.automaticRoutes {
		for _, verb := range item.verbs {
			if verb.any {
				target.Any(item.path, item.handler)
				continue
			}
			target.Handle(verb.method, item.path, item.handler)
		}
	}
	return nil
}
