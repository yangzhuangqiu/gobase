package appwebhertz

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/yangzhuangqiu/gobase/errcode"
)

// automaticMethodController 提供分组内单路径多 method 的自动路由样例。
type automaticMethodController struct{}

// GroupPath 把自动路由放入 `/api` 分组。
func (this *automaticMethodController) GroupPath() string {
	return "/api"
}

// GetOrPostHealthCheck 让 GET 和 POST 共享同一个 Hertz Handler。
func (this *automaticMethodController) GetOrPostHealthCheck(_ context.Context, request *hertzapp.RequestContext) {
	request.String(consts.StatusOK, string(request.Method()))
}

// GetMode 为同一路径的 GET 请求提供独立处理逻辑。
func (this *automaticMethodController) GetMode(_ context.Context, request *hertzapp.RequestContext) {
	request.String(consts.StatusOK, "get-handler")
}

// PostMode 为同一路径的 POST 请求提供独立处理逻辑。
func (this *automaticMethodController) PostMode(_ context.Context, request *hertzapp.RequestContext) {
	request.String(consts.StatusOK, "post-handler")
}

// mixedRouteController 同时声明自动静态路由和手动动态路由。
type mixedRouteController struct{}

// GroupPath 把当前 Controller 的两类路由放入同一分组。
func (this *mixedRouteController) GroupPath() string {
	return "/items"
}

// RegisterRoutes 使用 Hertz 原生参数语法注册自动方法名无法表达的动态路由。
func (this *mixedRouteController) RegisterRoutes(router *route.RouterGroup) error {
	router.GET("/:id", func(_ context.Context, request *hertzapp.RequestContext) {
		request.String(consts.StatusOK, "id="+request.Param("id"))
	})
	return nil
}

// GetList 自动注册分组内的 `/list` 静态路由。
func (this *mixedRouteController) GetList(_ context.Context, request *hertzapp.RequestContext) {
	request.String(consts.StatusOK, "list")
}

// rootController 用于验证空路径和 `/` 都归一化为根 Router。
type rootController struct {
	// groupPath 是测试输入的 Controller 分组声明。
	groupPath string
}

// GroupPath 返回测试指定的根路径表达。
func (this *rootController) GroupPath() string {
	return this.groupPath
}

// GetRoot 提供一条最小自动路由，使 Controller 计划有效。
func (this *rootController) GetRoot(_ context.Context, _ *hertzapp.RequestContext) {}

// scopedRootController 在根 URL group 内安装只属于自己的 Middleware。
type scopedRootController struct{}

// GroupPath 使用无 URL 前缀的 Controller group。
func (this *scopedRootController) GroupPath() string {
	return ""
}

// RegisterRoutes 在当前 group 写入作用域标记，并继续执行自动路由 Handler。
func (this *scopedRootController) RegisterRoutes(router *route.RouterGroup) error {
	router.Use(func(ctx context.Context, request *hertzapp.RequestContext) {
		request.Set("controller_scope", "scoped")
		request.Next(ctx)
	})
	return nil
}

// GetFirst 证明当前 Controller 的自动路由能观察到自身 group Middleware。
func (this *scopedRootController) GetFirst(_ context.Context, request *hertzapp.RequestContext) {
	value, exists := request.Get("controller_scope")
	if exists {
		request.String(consts.StatusOK, value.(string))
		return
	}
	request.String(consts.StatusInternalServerError, "missing")
}

// plainRootController 验证另一个根 URL Controller 不继承前一 group 的 Middleware。
type plainRootController struct{}

// GroupPath 使用 `/` 表达无 URL 前缀。
func (this *plainRootController) GroupPath() string {
	return "/"
}

// GetSecond 在未发生 Middleware 泄漏时返回 clean。
func (this *plainRootController) GetSecond(_ context.Context, request *hertzapp.RequestContext) {
	if _, exists := request.Get("controller_scope"); exists {
		request.String(consts.StatusInternalServerError, "leaked")
		return
	}
	request.String(consts.StatusOK, "clean")
}

// groupScopeController 注册可区分逻辑分组与 Controller 局部作用域的测试路由。
type groupScopeController struct {
	// groupPath 是 Controller 所属的逻辑分组。
	groupPath string
	// routePath 是注册到逻辑分组下的相对路径。
	routePath string
	// localHeader 非空时由 Controller 局部 Middleware 写入响应头。
	localHeader string
}

// GroupPath 返回测试指定的逻辑分组。
func (this *groupScopeController) GroupPath() string {
	return this.groupPath
}

// RegisterRoutes 在 Controller 子分组安装可选局部 Middleware 并注册响应路由。
func (this *groupScopeController) RegisterRoutes(router *route.RouterGroup) error {
	if this.localHeader != "" {
		router.Use(func(ctx context.Context, request *hertzapp.RequestContext) {
			request.Response.Header.Set("X-Controller-Scope", this.localHeader)
			appendScopeStage(request, this.localHeader)
			request.Next(ctx)
		})
	}
	router.GET(this.routePath, func(_ context.Context, request *hertzapp.RequestContext) {
		request.Response.Header.Set("X-Scope-Trace", strings.Join(scopeStages(request), ","))
		request.String(consts.StatusOK, "ok")
	})
	return nil
}

// emptyController 用于验证没有手动或自动路由能力时构建失败。
type emptyController struct{}

// GroupPath 返回根 Router，不额外声明路由。
func (this *emptyController) GroupPath() string {
	return ""
}

// invalidSuffixController 用于验证命中动词但缺少路径后缀时不会静默忽略。
type invalidSuffixController struct{}

// GroupPath 返回根 Router。
func (this *invalidSuffixController) GroupPath() string {
	return ""
}

// Get 故意缺少路径后缀，用于触发声明错误。
func (this *invalidSuffixController) Get(_ context.Context, _ *hertzapp.RequestContext) {}

// invalidSignatureController 用于验证自动路由必须采用 Hertz Handler 签名。
type invalidSignatureController struct{}

// GroupPath 返回根 Router。
func (this *invalidSignatureController) GroupPath() string {
	return ""
}

// GetBroken 故意使用非法签名，用于触发声明错误。
func (this *invalidSignatureController) GetBroken() {}

// invalidGroupController 用于验证非根分组必须以 `/` 开头。
type invalidGroupController struct{}

// GroupPath 故意返回缺少前导斜线的分组。
func (this *invalidGroupController) GroupPath() string {
	return "api"
}

// GetReady 提供有效 Handler，使测试只命中分组校验。
func (this *invalidGroupController) GetReady(_ context.Context, _ *hertzapp.RequestContext) {}

// TestAutomaticRouteSupportsMultipleHTTPMethods 验证同一 URL 可让 GET/POST 共享或分别使用 Handler。
// 测试方法：启动真实 Hertz Server，验证 GetOrPost 共享 Handler、Get/Post 独立 Handler，且未声明的 PUT 不匹配。
func TestAutomaticRouteSupportsMultipleHTTPMethods(t *testing.T) {
	host, address := newStandardTestHost(t, []Controller{&automaticMethodController{}}, nil)
	runResult := make(chan error, 1)
	go func() { runResult <- host.Run(context.Background()) }()
	url := "http://" + address + "/api/health-check"
	requestUntilReady(t, url)

	// Server 可用后逐个验证 method 集合，响应体同时证明命中的是同一个 Handler。
	for _, testCase := range []struct {
		method string
		status int
		body   string
	}{
		{method: http.MethodGet, status: http.StatusOK, body: http.MethodGet},
		{method: http.MethodPost, status: http.StatusOK, body: http.MethodPost},
		{method: http.MethodPut, status: http.StatusNotFound},
	} {
		status, body := performHTTPMethod(t, testCase.method, url)
		if status != testCase.status || (testCase.body != "" && body != testCase.body) {
			t.Fatalf("unexpected response method=%s status=%d body=%q", testCase.method, status, body)
		}
	}
	modeURL := "http://" + address + "/api/mode"
	for method, expected := range map[string]string{
		http.MethodGet:  "get-handler",
		http.MethodPost: "post-handler",
	} {
		status, body := performHTTPMethod(t, method, modeURL)
		if status != http.StatusOK || body != expected {
			t.Fatalf("unexpected separate handler method=%s status=%d body=%q", method, status, body)
		}
	}

	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown host: %v", err)
	}
	if err := waitForLifecycleError(t, runResult); err != nil {
		t.Fatalf("run result: %v", err)
	}
}

// TestManualAndAutomaticRoutesShareControllerGroup 验证动态参数路由与自动静态路由可在同一 Controller 中叠加。
// 测试方法：手动注册 `/:id` 并自动发现 GetList，真实请求两条路径并断言均带有 `/items` 分组。
func TestManualAndAutomaticRoutesShareControllerGroup(t *testing.T) {
	host, address := newStandardTestHost(t, []Controller{&mixedRouteController{}}, nil)
	runResult := make(chan error, 1)
	go func() { runResult <- host.Run(context.Background()) }()
	listURL := "http://" + address + "/items/list"
	requestUntilReady(t, listURL)

	// 静态路由和参数路由分别断言，避免只证明 RouterGroup 前缀而漏掉一种注册路径。
	for url, expected := range map[string]string{
		listURL:                           "list",
		"http://" + address + "/items/42": "id=42",
	} {
		status, body := performHTTPMethod(t, http.MethodGet, url)
		if status != http.StatusOK || body != expected {
			t.Fatalf("unexpected response url=%s status=%d body=%q", url, status, body)
		}
	}

	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown host: %v", err)
	}
	if err := waitForLifecycleError(t, runResult); err != nil {
		t.Fatalf("run result: %v", err)
	}
}

// TestRoutePathStylesMapMethodSuffix 验证三种可选命名风格共享一致的缩写拆词规则。
// 测试方法：把 HTTPStatusCheck 后缀分别交给默认中划线、驼峰和下划线风格，断言精确 URL。
func TestRoutePathStylesMapMethodSuffix(t *testing.T) {
	tests := map[RoutePathStyle]string{
		RoutePathStyleKebab: "/http-status-check",
		RoutePathStyleCamel: "/httpStatusCheck",
		RoutePathStyleSnake: "/http_status_check",
	}
	for style, expected := range tests {
		if actual := style.routePath("HTTPStatusCheck"); actual != expected {
			t.Fatalf("unexpected route path style=%d got=%s want=%s", style, actual, expected)
		}
	}
	defaultOptions, err := resolveOptions(nil)
	if err != nil || defaultOptions.routePathStyle != RoutePathStyleKebab {
		t.Fatalf("expected kebab default, style=%d err=%v", defaultOptions.routePathStyle, err)
	}
	customOptions, err := resolveOptions([]Option{WithRoutePathStyle(RoutePathStyleCamel)})
	if err != nil {
		t.Fatalf("resolve custom route style: %v", err)
	}
	plan, err := buildControllerRoutePlan(&automaticMethodController{}, 0, customOptions.routePathStyle)
	if err != nil {
		t.Fatalf("build controller with camel option: %v", err)
	}
	foundHealthCheck := false
	for _, item := range plan.automaticRoutes {
		foundHealthCheck = foundHealthCheck || item.path == "/healthCheck"
	}
	if !foundHealthCheck {
		t.Fatalf("expected wired camel option, routes=%v", plan.automaticRoutes)
	}
}

// TestRootGroupPathUsesSameLogicalGroup 验证空字符串与 `/` 都归一化为同一根逻辑分组。
// 测试方法：分别构建两种 Controller 计划，断言 groupPath 均使用缓存键 `/`。
func TestRootGroupPathUsesSameLogicalGroup(t *testing.T) {
	for _, groupPath := range []string{"", "/"} {
		plan, err := buildControllerRoutePlan(&rootController{groupPath: groupPath}, 0, RoutePathStyleKebab)
		if err != nil {
			t.Fatalf("build root controller plan group=%q: %v", groupPath, err)
		}
		if plan.groupPath != "/" {
			t.Fatalf("expected root logical group=%q got=%q", groupPath, plan.groupPath)
		}
	}
}

// TestRootControllerMiddlewareDoesNotLeak 验证 `/` 路径相同的 Controller 仍拥有独立 Middleware 作用域。
// 测试方法：首个 Controller group 设置请求标记，分别请求首个和后续 Controller，断言标记只在首个路由可见。
func TestRootControllerMiddlewareDoesNotLeak(t *testing.T) {
	host, address := newStandardTestHost(
		t,
		[]Controller{&scopedRootController{}, &plainRootController{}},
		nil,
	)
	runResult := make(chan error, 1)
	go func() { runResult <- host.Run(context.Background()) }()
	requestUntilReady(t, "http://"+address+"/first")

	// 两条路由都位于根 URL，但只有首个 Controller 的 group 应包含作用域 Middleware。
	for path, expected := range map[string]string{"/first": "scoped", "/second": "clean"} {
		status, body := performHTTPMethod(t, http.MethodGet, "http://"+address+path)
		if status != http.StatusOK || body != expected {
			t.Fatalf("unexpected root group response path=%s status=%d body=%q", path, status, body)
		}
	}

	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown host: %v", err)
	}
	if err := waitForLifecycleError(t, runResult); err != nil {
		t.Fatalf("run result: %v", err)
	}
}

// TestGroupedFiltersShareLogicalGroupWithoutLeakingControllerMiddleware 验证最终分组装配合同。
// 测试方法：为 `/api`、根路径和全局分别声明 Filter，请求同组、异组、根路由与 404，断言共享、隔离及 fallback 作用域。
func TestGroupedFiltersShareLogicalGroupWithoutLeakingControllerMiddleware(t *testing.T) {
	global := &filterStub{handler: func(ctx context.Context, request *hertzapp.RequestContext) {
		request.Response.Header.Set("X-Global-Filter", "true")
		appendScopeStage(request, "global")
		request.Next(ctx)
	}}
	api := &groupedFilterStub{filterStub: &filterStub{handler: func(ctx context.Context, request *hertzapp.RequestContext) {
		request.Response.Header.Set("X-API-Filter", "true")
		appendScopeStage(request, "api")
		request.Next(ctx)
	}}, groupPath: "/api"}
	root := &groupedFilterStub{filterStub: &filterStub{handler: func(ctx context.Context, request *hertzapp.RequestContext) {
		request.Response.Header.Set("X-Root-Filter", "true")
		appendScopeStage(request, "root")
		request.Next(ctx)
	}}, groupPath: ""}
	controllers := []Controller{
		&groupScopeController{groupPath: "/api", routePath: "/first", localHeader: "first"},
		&groupScopeController{groupPath: "/api", routePath: "/second"},
		&groupScopeController{groupPath: "/other", routePath: "/third"},
		&groupScopeController{groupPath: "/", routePath: "/root"},
	}
	host, address := newStandardTestHost(t, controllers, []Filter{api, global, root})
	runResult := make(chan error, 1)
	go func() { runResult <- host.Run(context.Background()) }()
	requestUntilReady(t, "http://"+address+"/api/first")

	// 每个请求同时证明全局 Filter、逻辑分组 Filter 和 Controller 子分组的边界。
	assertScopeHeaders(t, "http://"+address+"/api/first", true, false, "first", "global,api,first")
	assertScopeHeaders(t, "http://"+address+"/api/second", true, false, "", "global,api")
	assertScopeHeaders(t, "http://"+address+"/other/third", false, false, "", "global")
	assertScopeHeaders(t, "http://"+address+"/root", false, true, "", "global,root")
	assertScopeHeaders(t, "http://"+address+"/missing", false, false, "", "")

	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown host: %v", err)
	}
	if err := waitForLifecycleError(t, runResult); err != nil {
		t.Fatalf("run result: %v", err)
	}
}

// TestControllerRouteDeclarationsFailFast 验证自动路由声明错误在创建 Hertz 前可分支失败。
// 测试方法：覆盖无路由、空后缀、非法签名、非法分组、非法风格和 Any 组合，断言统一声明错误码。
func TestControllerRouteDeclarationsFailFast(t *testing.T) {
	// 第一阶段：所有公开构造可触发的非法声明必须归入同一稳定错误码。
	tests := map[string]func() error{
		"route required": func() error {
			_, err := prepareControllers([]Controller{&emptyController{}}, RoutePathStyleKebab)
			return err
		},
		"suffix required": func() error {
			_, err := prepareControllers([]Controller{&invalidSuffixController{}}, RoutePathStyleKebab)
			return err
		},
		"signature invalid": func() error {
			_, err := prepareControllers([]Controller{&invalidSignatureController{}}, RoutePathStyleKebab)
			return err
		},
		"group invalid": func() error {
			_, err := prepareControllers([]Controller{&invalidGroupController{}}, RoutePathStyleKebab)
			return err
		},
		"style invalid": func() error {
			_, err := resolveOptions([]Option{WithRoutePathStyle(RoutePathStyle(99))})
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
	// 第二阶段：Any 的非法组合由方法名解析器明确拒绝，不退化成部分注册。
	if _, _, _, err := parseAutomaticRouteMethod("AnyOrGetEverything"); err == nil {
		t.Fatal("expected Any combination error")
	}
}

// TestParseAutomaticRouteMethodKeepsOrderPrefixAsPath 验证 `Or` 仅在后接完整动词时作为组合符。
// 测试方法：解析 GetOrder 与 GetOrPostOrder，断言前者路径后缀未被误拆，后者得到两个精确动词。
func TestParseAutomaticRouteMethodKeepsOrderPrefixAsPath(t *testing.T) {
	verbs, suffix, matched, err := parseAutomaticRouteMethod("GetOrder")
	if err != nil || !matched || len(verbs) != 1 || suffix != "Order" {
		t.Fatalf("unexpected GetOrder parse verbs=%v suffix=%s matched=%t err=%v", verbs, suffix, matched, err)
	}
	verbs, suffix, matched, err = parseAutomaticRouteMethod("GetOrPostOrder")
	if err != nil || !matched || len(verbs) != 2 || suffix != "Order" || verbs[1].method != consts.MethodPost {
		t.Fatalf("unexpected GetOrPostOrder parse verbs=%v suffix=%s matched=%t err=%v", verbs, suffix, matched, err)
	}
}

// performHTTPMethod 执行一次指定 method 的测试请求并完整读取响应。
// 阶段角色：Server ready 后验证路由 method/path/handler，不承担启动重试。
func performHTTPMethod(t *testing.T, method string, url string) (int, string) {
	t.Helper()
	// 第一阶段：构建并发送不携带请求体的路由验证请求。
	request, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("new request method=%s url=%s: %v", method, url, err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("perform request method=%s url=%s: %v", method, url, err)
	}
	defer func() { _ = response.Body.Close() }()
	// 第二阶段：完整读取响应，确保连接可复用且 Handler 输出可断言。
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response method=%s url=%s: %v", method, url, err)
	}
	return response.StatusCode, strings.TrimSpace(string(body))
}

// assertScopeHeaders 请求测试路由并断言全局、分组和 Controller 局部 Middleware 的可见边界。
// 阶段角色：Server ready 后复用同一组响应头断言，避免各场景重复网络清理代码。
func assertScopeHeaders(t *testing.T, url string, apiExpected bool, rootExpected bool, controllerExpected string, traceExpected string) {
	t.Helper()
	// 第一阶段：发送请求并完整消费响应，保证后续生命周期停止不受连接复用影响。
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("get scope route url=%s: %v", url, err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close scope response url=%s: %v", url, err)
	}
	// 第二阶段：全局头必须存在，其余三个头分别表达逻辑分组和 Controller 局部边界。
	if response.Header.Get("X-Global-Filter") != "true" ||
		(response.Header.Get("X-API-Filter") == "true") != apiExpected ||
		(response.Header.Get("X-Root-Filter") == "true") != rootExpected ||
		response.Header.Get("X-Controller-Scope") != controllerExpected ||
		response.Header.Get("X-Scope-Trace") != traceExpected {
		t.Fatalf("unexpected scope headers url=%s headers=%v", url, response.Header)
	}
}

// appendScopeStage 把 Middleware 的进入顺序追加到当前请求私有轨迹。
// 阶段角色：仅用于集成测试观察 global、grouped 与 Controller-local 的相对顺序。
func appendScopeStage(request *hertzapp.RequestContext, stage string) {
	stages := scopeStages(request)
	request.Set("scope_stages", append(stages, stage))
}

// scopeStages 读取当前请求已记录的 Middleware 轨迹；缺失时返回空切片。
// 阶段角色：测试 Handler 生成可经 HTTP 响应头断言的最终顺序。
func scopeStages(request *hertzapp.RequestContext) []string {
	value, exists := request.Get("scope_stages")
	if !exists {
		return nil
	}
	return value.([]string)
}
