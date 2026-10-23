package appwebhertz

import (
	"context"
	"reflect"
	"strings"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
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

// automaticRouteParseReason 表示方法名已选择自动路由但声明非法的稳定原因。
// 说明：解析器只返回原因，完整 Controller 上下文由调用边界统一写入 coded error。
type automaticRouteParseReason string

const (
	// automaticRouteParseAnyCombined 表示 Any 不能与其他 HTTP 动词组合。
	automaticRouteParseAnyCombined automaticRouteParseReason = "any_cannot_combine"
	// automaticRouteParseVerbDuplicated 表示同一 HTTP 动词在方法名前缀中重复声明。
	automaticRouteParseVerbDuplicated automaticRouteParseReason = "verb_duplicated"
	// automaticRouteParsePathRequired 表示动词前缀后没有可转换为路径的名称后缀。
	automaticRouteParsePathRequired automaticRouteParseReason = "path_suffix_required"
)

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

// scanAutomaticRoutes 扫描 Controller 导出方法并生成确定性的静态路由计划。
// 阶段角色：方法名前缀未命中时忽略；命中后签名、动词组合和路径必须全部有效。
func scanAutomaticRoutes(controller Controller, controllerIndex int, style RoutePathStyle) ([]automaticRoute, error) {
	controllerType := reflect.TypeOf(controller)
	controllerValue := reflect.ValueOf(controller)
	routes := make([]automaticRoute, 0, controllerType.NumMethod())

	// reflect.Type.Method 按导出方法名词法顺序枚举，固定自动路由注册顺序。
	for methodIndex := 0; methodIndex < controllerType.NumMethod(); methodIndex++ {
		method := controllerType.Method(methodIndex)
		verbs, suffix, matched, parseReason := parseAutomaticRouteMethod(method.Name)
		if !matched {
			continue
		}
		if parseReason != "" {
			return nil, errorForDeclarationInvalid(messagef(
				"op=app_web_hertz.build result=failed stage=scan_controller_routes reason=method_name_invalid parse_reason=%s controller_index=%d controller_type=%s method=%s",
				parseReason, controllerIndex, controllerType, method.Name,
			))
		}
		if !isHertzHandlerMethod(method.Type) {
			return nil, errorForDeclarationInvalid(messagef("op=app_web_hertz.build result=failed stage=scan_controller_routes reason=handler_signature_invalid controller_index=%d controller_type=%s method=%s expected=func(context.Context,*app.RequestContext)", controllerIndex, controllerType, method.Name))
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
func parseAutomaticRouteMethod(name string) ([]routeVerb, string, bool, automaticRouteParseReason) {
	first, ok := consumeRouteVerb(name)
	if !ok {
		return nil, "", false, ""
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
			return nil, "", true, automaticRouteParseAnyCombined
		}
		if _, duplicated := seen[next.token]; duplicated {
			return nil, "", true, automaticRouteParseVerbDuplicated
		}
		verbs = append(verbs, next)
		seen[next.token] = struct{}{}
		remainder = strings.TrimPrefix(strings.TrimPrefix(remainder, "Or"), next.token)
	}
	if remainder == "" {
		return nil, "", true, automaticRouteParsePathRequired
	}
	return verbs, remainder, true, ""
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
