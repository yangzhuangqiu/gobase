package webhertzobserve

import (
	"context"
	"io"
	"math"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/yangzhuangqiu/gobase/errcode"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// TestPrivatePrometheusRuntimePublishesStableMetricContract 验证私有 Registry 只发布约定的 HTTP 指标、单位和有限 Label。
// 测试方法：发送开始/完成事件，通过 Runtime Handler 抓取文本，断言名称、模板路由、状态和 in-flight 回零。
func TestPrivatePrometheusRuntimePublishesStableMetricContract(t *testing.T) {
	// 第一阶段：创建完全私有的 Registry/registration，不读取 DefaultRegistry。
	runtime, err := NewPrivatePrometheusRuntime()
	if err != nil {
		t.Fatalf("new private prometheus runtime: %v", err)
	}
	defer func() { _ = runtime.Close() }()

	// 第二阶段：通过开始/完成合同更新指标，再由不拥有 Server 的 Handler 采集。
	runtime.RequestStarted(context.Background(), "GET")
	runtime.Observe(context.Background(), webhertz.HTTPRequestObservation{
		Method: "GET", RoutePattern: "/orders/:id", Status: 200, Outcome: webhertz.RequestOutcomeSuccess,
		Duration: 25 * time.Millisecond, RequestBytes: 10, ResponseBytes: 20,
	})
	recorder := httptest.NewRecorder()
	runtime.ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	body, err := io.ReadAll(recorder.Result().Body)
	if err != nil {
		t.Fatalf("read metrics response: %v", err)
	}
	text := string(body)
	for _, metric := range []string{
		"gobase_http_server_requests_total", "gobase_http_server_request_duration_seconds",
		"gobase_http_server_request_bytes", "gobase_http_server_response_bytes", "gobase_http_server_in_flight_requests",
	} {
		if !strings.Contains(text, metric) {
			t.Fatalf("metric contract missing %s:\n%s", metric, text)
		}
	}
	if !strings.Contains(text, `method="GET",outcome="success",route="/orders/:id",status="200"} 1`) {
		t.Fatalf("unexpected request labels:\n%s", text)
	}
	if !strings.Contains(text, `gobase_http_server_in_flight_requests{method="GET"} 0`) {
		t.Fatalf("expected in-flight gauge to return zero:\n%s", text)
	}
}

// TestPrometheusRuntimesIsolateRegistriesAndOwnOnlyRegistration 验证多个私有 Registry 可并存，共享模式只注销自身 Collector。
// 测试方法：创建两个同名私有 Runtime；再在共享 Registry 制造重复注册、关闭首个后重试并断言 Registry 可继续使用。
func TestPrometheusRuntimesIsolateRegistriesAndOwnOnlyRegistration(t *testing.T) {
	// 第一阶段：两个私有 Registry 允许完全相同的指标名并独立关闭。
	firstPrivate, err := NewPrivatePrometheusRuntime()
	if err != nil {
		t.Fatalf("new first private runtime: %v", err)
	}
	secondPrivate, err := NewPrivatePrometheusRuntime()
	if err != nil {
		t.Fatalf("new second private runtime: %v", err)
	}
	if err := firstPrivate.Close(); err != nil {
		t.Fatalf("close first private runtime: %v", err)
	}
	if err := secondPrivate.Close(); err != nil {
		t.Fatalf("close second private runtime: %v", err)
	}

	// 第二阶段：共享 Registry 拒绝重复 Collector；注销后可复用且 Registry 所有权不转移。
	registry := prometheus.NewRegistry()
	firstShared, err := NewSharedPrometheusRuntime(registry)
	if err != nil {
		t.Fatalf("new first shared runtime: %v", err)
	}
	if _, err := NewSharedPrometheusRuntime(registry); !errcode.IsCode[ErrorCode](err, ErrCodeRegistrationFailed) {
		t.Fatalf("expected duplicate registration error, got=%v", err)
	}
	if err := firstShared.Close(); err != nil {
		t.Fatalf("close first shared runtime: %v", err)
	}
	secondShared, err := NewSharedPrometheusRuntime(registry)
	if err != nil {
		t.Fatalf("expected shared registry reuse after unregister: %v", err)
	}
	if err := secondShared.Close(); err != nil {
		t.Fatalf("close second shared runtime: %v", err)
	}
	if _, err := registry.Gather(); err != nil {
		t.Fatalf("expected borrowed registry to remain usable: %v", err)
	}
}

// TestPrometheusObserverSupportsConcurrentCompletion 验证指标向量可并发消费完成态且未知字节不会创建非法样本。
// 测试方法：并发发布固定模板请求，等待后 Gather 并断言 requests counter 等于总数。
func TestPrometheusObserverSupportsConcurrentCompletion(t *testing.T) {
	// 第一阶段：在共享 Registry 注册单个并发安全 Observer。
	registry := prometheus.NewRegistry()
	runtime, err := NewSharedPrometheusRuntime(registry)
	if err != nil {
		t.Fatalf("new concurrent prometheus runtime: %v", err)
	}
	defer func() { _ = runtime.Close() }()
	// 第二阶段：并发发布相同有限维度，并在全部完成后采集累计值。
	const requestCount = 128
	var waitGroup sync.WaitGroup
	waitGroup.Add(requestCount)
	for index := 0; index < requestCount; index++ {
		go func() {
			defer waitGroup.Done()
			runtime.RequestStarted(context.Background(), "POST")
			runtime.Observe(context.Background(), webhertz.HTTPRequestObservation{
				Method: "POST", RoutePattern: webhertz.UnmatchedRoutePattern, Status: 404,
				Outcome: webhertz.RequestOutcomeClientError, RequestBytes: webhertz.UnknownBodySize, ResponseBytes: 0,
			})
		}()
	}
	waitGroup.Wait()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather concurrent metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() == "gobase_http_server_requests_total" {
			if len(family.Metric) != 1 || family.Metric[0].GetCounter().GetValue() != requestCount {
				t.Fatalf("unexpected concurrent request counter: %+v", family.Metric)
			}
			return
		}
	}
	t.Fatal("request counter family missing")
}

// TestPrometheusOptionsRejectInvalidBuckets 验证非法 Histogram bucket 在注册 Registry 前 fail-fast。
// 测试方法：传入空、非递增和负数 bucket，断言稳定 declaration_invalid 错误码。
func TestPrometheusOptionsRejectInvalidBuckets(t *testing.T) {
	for name, option := range map[string]PrometheusOption{
		"empty":          WithDurationBuckets(),
		"not increasing": WithRequestByteBuckets(1, 1),
		"negative":       WithResponseByteBuckets(-1, 1),
		"nan":            WithDurationBuckets(math.NaN()),
		"metric name":    WithPrometheusNamespace("bad-name"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewPrometheusObserver(option); !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
				t.Fatalf("expected invalid bucket error, got=%v", err)
			}
		})
	}
}
