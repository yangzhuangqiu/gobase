package appwebhertz

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/yangzhuangqiu/gobase/errcode"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// requestLogObserverFunc 把测试函数适配为 Request Log Observer。
type requestLogObserverFunc func(context.Context, webhertz.HTTPRequestObservation, bool)

// ObserveRequestLog 同步执行测试回调。
func (this requestLogObserverFunc) ObserveRequestLog(ctx context.Context, observation webhertz.HTTPRequestObservation, slow bool) {
	this(ctx, observation, slow)
}

// TestPrepareStandardMiddlewareKeepsFixedOrder 验证 Request ID、Recovery、CORS、Gzip 与应用 Filter 的相对阶段稳定。
// 测试方法：用 typed policy 和末端 Handler 记录可观察事件，执行真实 Hertz Handler 链并断言 before/after 顺序。
func TestPrepareStandardMiddlewareKeepsFixedOrder(t *testing.T) {
	// 第一阶段：注入可记录事件的 typed policy，构造全部启用的标准 Middleware 链。
	order := make([]string, 0, 6)
	config, err := resolveMiddlewareConfig(MiddlewareConfig{
		Recovery:  RecoveryConfig{Enabled: true},
		RequestID: RequestIDConfig{Enabled: true},
		CORS:      CORSConfig{Enabled: true},
		Gzip:      GzipConfig{Enabled: true, MinLength: 1},
	})
	if err != nil {
		t.Fatalf("resolve middleware config: %v", err)
	}
	options := defaultOptions()
	options.requestIDGenerator = func(_ context.Context, _ *app.RequestContext) (string, error) {
		order = append(order, "request_id")
		return "generated-id", nil
	}
	options.corsOriginValidator = func(_ context.Context, _ *app.RequestContext, _ string) bool {
		order = append(order, "cors")
		return true
	}
	options.gzipPredicate = func(_ context.Context, _ *app.RequestContext) bool {
		order = append(order, "gzip_after")
		return false
	}
	plan, err := prepareStandardMiddleware(config, options)
	if err != nil {
		t.Fatalf("prepare standard middleware: %v", err)
	}
	// 第二阶段：追加应用 Filter 与业务 Handler，通过 before/after 事件观察嵌套顺序。
	handlers := append([]app.HandlerFunc{}, plan.beforeObservation...)
	handlers = append(handlers, plan.afterObservation...)
	handlers = append(handlers,
		func(ctx context.Context, request *app.RequestContext) {
			order = append(order, "filter_before")
			request.Next(ctx)
			order = append(order, "filter_after")
		},
		func(_ context.Context, request *app.RequestContext) {
			order = append(order, "handler")
			request.String(consts.StatusOK, "response body")
		},
	)
	request := newMiddlewareTestRequest("GET", "/resource")
	request.Request.Header.Set("Origin", "https://dynamic.example")
	request.Request.Header.Set("Accept-Encoding", "gzip")
	executeMiddlewareTestChain(request, handlers)
	if !reflect.DeepEqual(order, []string{"request_id", "cors", "filter_before", "handler", "filter_after", "gzip_after"}) {
		t.Fatalf("unexpected middleware order: %v", order)
	}
}

// TestRecoveryProducesFinal500AndPreservesRequestID 验证内层 Filter panic 由标准 Recovery 收敛，外层 Request ID 仍回写响应。
// 测试方法：启用 Request ID/Recovery，末端 panic，执行链后断言 JSON 500、稳定 code 和关联 Header。
func TestRecoveryProducesFinal500AndPreservesRequestID(t *testing.T) {
	// 第一阶段：构造外层 Request ID 与内层 Recovery，并固定生成值以便关联断言。
	config, err := resolveMiddlewareConfig(MiddlewareConfig{
		Recovery:  RecoveryConfig{Enabled: true},
		RequestID: RequestIDConfig{Enabled: true},
	})
	if err != nil {
		t.Fatalf("resolve middleware config: %v", err)
	}
	options := defaultOptions()
	options.requestIDGenerator = func(_ context.Context, _ *app.RequestContext) (string, error) { return "stable-id", nil }
	plan, err := prepareStandardMiddleware(config, options)
	if err != nil {
		t.Fatalf("prepare standard middleware: %v", err)
	}
	// 第二阶段：触发业务 panic，验证 Recovery 收敛响应且不泄露 panic 值。
	handlers := append([]app.HandlerFunc{}, plan.beforeObservation...)
	handlers = append(handlers, plan.afterObservation...)
	handlers = append(handlers, func(_ context.Context, _ *app.RequestContext) { panic("secret panic value") })
	request := newMiddlewareTestRequest("GET", "/panic")
	executeMiddlewareTestChain(request, handlers)
	if request.Response.StatusCode() != consts.StatusInternalServerError || request.Response.Header.Get(defaultRequestIDHeader) != "stable-id" {
		t.Fatalf("unexpected recovery response: status=%d request_id=%s", request.Response.StatusCode(), request.Response.Header.Get(defaultRequestIDHeader))
	}
	if !strings.Contains(string(request.Response.Body()), "internal_server_error") || strings.Contains(string(request.Response.Body()), "secret panic value") {
		t.Fatalf("unexpected recovery body: %s", request.Response.Body())
	}
	if !RequestPanicked(request) {
		t.Fatal("expected public panic completion marker after standard Recovery")
	}
}

// TestRequestIDTrustAndGenerationFailure 验证入站信任为显式策略，且生成失败不会让无标识请求进入业务 Handler。
// 测试方法：先信任合法入站值并断言透传，再关闭信任且让 Generator 失败，断言稳定 500 和下游未执行。
func TestRequestIDTrustAndGenerationFailure(t *testing.T) {
	// 第一阶段：显式信任合法入站 ID，Generator 不应被调用且响应应回写同一值。
	trusted := newMiddlewareTestRequest("GET", "/trusted")
	trusted.Request.Header.Set("X-Correlation-ID", "incoming-id")
	handler := newRequestIDMiddleware(RequestIDConfig{Header: "X-Correlation-ID", TrustIncoming: true}, func(_ context.Context, _ *app.RequestContext) (string, error) {
		t.Fatal("generator should not run for trusted valid input")
		return "", nil
	})
	executeMiddlewareTestChain(trusted, []app.HandlerFunc{handler, func(_ context.Context, request *app.RequestContext) {
		if GetRequestID(request) != "incoming-id" {
			t.Fatalf("unexpected request ID: %s", GetRequestID(request))
		}
	}})
	if trusted.Response.Header.Get("X-Correlation-ID") != "incoming-id" {
		t.Fatalf("expected trusted response header, got=%s", trusted.Response.Header.Get("X-Correlation-ID"))
	}

	// 第二阶段：关闭信任并模拟熵源失败，验证失败封闭且业务 Handler 不执行。
	downstreamCalled := false
	failed := newMiddlewareTestRequest("GET", "/failed")
	failedHandler := newRequestIDMiddleware(RequestIDConfig{Header: defaultRequestIDHeader}, func(_ context.Context, _ *app.RequestContext) (string, error) {
		return "", errors.New("entropy unavailable")
	})
	executeMiddlewareTestChain(failed, []app.HandlerFunc{failedHandler, func(_ context.Context, _ *app.RequestContext) { downstreamCalled = true }})
	if failed.Response.StatusCode() != consts.StatusInternalServerError || downstreamCalled {
		t.Fatalf("unexpected generation failure: status=%d downstream=%t", failed.Response.StatusCode(), downstreamCalled)
	}
}

// TestCORSMiddlewareAppliesSafePreflightAndRejectsUnknownOrigin 验证 CORS 只回显白名单 Origin，并正确终结预检和拒绝未知来源。
// 测试方法：用凭据安全的精确 Origin 构造 Handler，分别发送允许预检和未知普通请求，断言 Header/204/403 与下游行为。
func TestCORSMiddlewareAppliesSafePreflightAndRejectsUnknownOrigin(t *testing.T) {
	// 第一阶段：构造允许凭据的精确 Origin 策略，验证合法预检在进入业务前以 204 终结。
	config, err := resolveCORSConfig(CORSConfig{
		Enabled:          true,
		AllowOrigins:     []string{"https://example.com"},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
	})
	if err != nil {
		t.Fatalf("resolve CORS config: %v", err)
	}
	handler, err := newCORSMiddleware(config, nil)
	if err != nil {
		t.Fatalf("build CORS middleware: %v", err)
	}
	downstreamCalled := false
	preflight := newMiddlewareTestRequest("OPTIONS", "/resource")
	preflight.Request.Header.Set("Origin", "https://example.com")
	preflight.Request.Header.Set("Access-Control-Request-Method", "POST")
	executeMiddlewareTestChain(preflight, []app.HandlerFunc{handler, func(_ context.Context, _ *app.RequestContext) { downstreamCalled = true }})
	if preflight.Response.StatusCode() != consts.StatusNoContent || downstreamCalled || preflight.Response.Header.Get("Access-Control-Allow-Origin") != "https://example.com" || preflight.Response.Header.Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("unexpected preflight response: status=%d headers=%s downstream=%t", preflight.Response.StatusCode(), preflight.Response.Header.Header(), downstreamCalled)
	}

	// 第二阶段：未知 Origin 必须明确拒绝，不可继承上一请求的允许结果。
	denied := newMiddlewareTestRequest("GET", "/resource")
	denied.Request.Header.Set("Origin", "https://attacker.example")
	executeMiddlewareTestChain(denied, []app.HandlerFunc{handler, func(_ context.Context, _ *app.RequestContext) { downstreamCalled = true }})
	if denied.Response.StatusCode() != consts.StatusForbidden {
		t.Fatalf("expected forbidden origin, got=%d", denied.Response.StatusCode())
	}
}

// TestCORSEnabledWithoutStaticOrTypedPolicyFailsDeclaration 验证动态 Origin 模式缺少 typed policy 时不静默拒绝全部流量。
// 测试方法：解析 enabled 且无静态 Origin 的合法配置，构造 Middleware 时断言 declaration_invalid。
func TestCORSEnabledWithoutStaticOrTypedPolicyFailsDeclaration(t *testing.T) {
	config, err := resolveCORSConfig(CORSConfig{Enabled: true})
	if err != nil {
		t.Fatalf("resolve dynamic CORS config: %v", err)
	}
	_, err = newCORSMiddleware(config, nil)
	if !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected missing typed policy error, got=%v", err)
	}
}

// TestPrepareRequestObserversRequiresAdapterAndAppliesAccessLogPolicy 验证 Access Log 开关不被通用 Metrics Observer 冒充，并正确应用跳过/慢请求策略。
// 测试方法：先只提供通用 Observer 断言缺少日志适配器失败，再提供日志适配器并手动发送快照验证精确 skip 和 slow 标记。
func TestPrepareRequestObserversRequiresAdapterAndAppliesAccessLogPolicy(t *testing.T) {
	options := defaultOptions()
	options.requestObservers = []webhertz.RequestObserver{requestObservationBridge{}}
	_, err := prepareRequestObservers(AccessLogConfig{Enabled: true}, options, nil)
	if !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected missing request log observer error, got=%v", err)
	}

	// 第二阶段：日志策略只过滤自身，不影响同一完成态链中的通用 Observer。
	called := 0
	wasSlow := false
	options.requestLogObservers = []RequestLogObserver{requestLogObserverFunc(func(_ context.Context, _ webhertz.HTTPRequestObservation, slow bool) {
		called++
		wasSlow = slow
	})}
	observers, err := prepareRequestObservers(AccessLogConfig{Enabled: true, SkipPaths: []string{"/healthz"}, SlowThreshold: time.Second}, options, nil)
	if err != nil {
		t.Fatalf("prepare access log observers: %v", err)
	}
	for _, observer := range observers {
		observer.Observe(context.Background(), webhertz.HTTPRequestObservation{Path: "/healthz", Duration: 2 * time.Second})
		observer.Observe(context.Background(), webhertz.HTTPRequestObservation{Path: "/orders", Duration: 2 * time.Second})
	}
	if called != 1 || !wasSlow {
		t.Fatalf("unexpected access log policy result: called=%d slow=%t", called, wasSlow)
	}
}

// TestGzipMiddlewareCompressesEligibleResponseAndRespectsGuards 验证 Gzip 压缩完整响应，同时尊重 q=0 和媒体类型排除。
// 测试方法：对合格文本响应解压校验原文，再发送 q=0 与 image/png 响应并断言保持未压缩。
func TestGzipMiddlewareCompressesEligibleResponseAndRespectsGuards(t *testing.T) {
	// 第一阶段：压缩满足协商、长度和媒体类型条件的完整响应，并解压验证内容无损。
	config, err := resolveGzipConfig(GzipConfig{Enabled: true, MinLength: 4})
	if err != nil {
		t.Fatalf("resolve gzip config: %v", err)
	}
	handler := newGzipMiddleware(config, nil)
	compressed := newMiddlewareTestRequest("GET", "/text")
	compressed.Request.Header.Set("Accept-Encoding", "br, gzip")
	executeMiddlewareTestChain(compressed, []app.HandlerFunc{handler, func(_ context.Context, request *app.RequestContext) {
		request.Response.Header.SetContentType("text/plain")
		request.String(consts.StatusOK, "hello gzip")
	}})
	if compressed.Response.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip response, headers=%s", compressed.Response.Header.Header())
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed.Response.Body()))
	if err != nil {
		t.Fatalf("open gzip body: %v", err)
	}
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != "hello gzip" {
		t.Fatalf("unexpected decompressed body: body=%q err=%v", body, err)
	}
	_ = reader.Close()

	// 第二阶段：分别验证客户端 q=0 与已排除媒体类型都保持原始响应。
	for name, testCase := range map[string][2]string{
		"quality zero":  {"gzip;q=0", "text/plain"},
		"excluded type": {"gzip", "image/png"},
	} {
		t.Run(name, func(t *testing.T) {
			request := newMiddlewareTestRequest("GET", "/guard")
			request.Request.Header.Set("Accept-Encoding", testCase[0])
			executeMiddlewareTestChain(request, []app.HandlerFunc{handler, func(_ context.Context, request *app.RequestContext) {
				request.String(consts.StatusOK, "plain body")
				request.Response.Header.SetContentType(testCase[1])
			}})
			if request.Response.Header.Get("Content-Encoding") != "" || string(request.Response.Body()) != "plain body" {
				t.Fatalf("expected uncompressed response: headers=%s body=%q", request.Response.Header.Header(), request.Response.Body())
			}
		})
	}
}

// newMiddlewareTestRequest 创建具有真实 Hertz 初始 handler index 的请求上下文。
// 阶段角色：Middleware 单元测试入口，不建立 listener 或 goroutine。
func newMiddlewareTestRequest(method, path string) *app.RequestContext {
	request := app.NewContext(0)
	request.Request.SetMethod(method)
	request.Request.SetRequestURI(path)
	return request
}

// executeMiddlewareTestChain 使用 Hertz Next 机制执行完整 Handler 链。
// 阶段角色：保留生产 before/after 嵌套语义，便于验证固定顺序、Abort 和 panic 恢复。
func executeMiddlewareTestChain(request *app.RequestContext, handlers []app.HandlerFunc) {
	request.SetHandlers(handlers)
	request.Next(context.Background())
}
