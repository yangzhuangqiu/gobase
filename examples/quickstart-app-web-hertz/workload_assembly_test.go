package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	webhertzobserve "github.com/yangzhuangqiu/gobase/app-observe-webhertz"
	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
	appwebhertz "github.com/yangzhuangqiu/gobase/app-web-hertz"
	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/ioc"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// TestWorkloadAssemblyDisabledDiagnosticsCreatesOnlyBusinessWorkload 验证关闭诊断后不创建诊断 Server、路由或 goroutine。
// 测试方法：覆盖应用私有配置为 disabled，构建真实 Runtime 并 Prepare，断言 Controller 仅包含业务 Server 后关闭。
func TestWorkloadAssemblyDisabledDiagnosticsCreatesOnlyBusinessWorkload(t *testing.T) {
	// 第一阶段：关闭应用私有诊断开关并构建其余真实 Module 图。
	config := defaultConfig()
	config["quickstart"].(map[string]any)["diagnostics"].(map[string]any)["enabled"] = false
	runtime := buildQuickstartRuntime(t, config)
	assembly := &workloadAssembly{}
	if err := assembly.Prepare(context.Background(), runtime, nil); err != nil {
		t.Fatalf("prepare disabled workloads: %v", err)
	}
	controller, err := assembly.loadController("test")
	if err != nil {
		t.Fatalf("load disabled controller: %v", err)
	}
	if len(controller.workloads) != 1 || controller.workloads[0].name != "business-web" {
		t.Fatalf("unexpected disabled workloads: %+v", controller.workloads)
	}
	// 第二阶段：未运行的业务 Server 仍可经 Controller/Runtime 两层幂等关闭。
	if err := controller.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown disabled workloads: %v", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("close disabled runtime: %v", err)
	}
}

// TestWorkloadAssemblyExportsRealPrometheusAndStopsTogether 验证 private Registry 经独立诊断端口导出并与业务 Server 同步收敛。
// 测试方法：使用两个端口 0 构建真实 Runtime，写入一条 Observation，外部抓取 /metrics 后取消 Controller 并关闭全部资源。
func TestWorkloadAssemblyExportsRealPrometheusAndStopsTogether(t *testing.T) {
	t.Setenv(quickstartAddressEnv, "127.0.0.1:0")
	t.Setenv(quickstartDiagnosticsAddressEnv, "127.0.0.1:0")
	runtime := buildQuickstartRuntime(t, defaultConfig())
	assembly := &workloadAssembly{}
	if err := assembly.Prepare(context.Background(), runtime, nil); err != nil {
		t.Fatalf("prepare enabled workloads: %v", err)
	}
	controller, err := assembly.loadController("test")
	if err != nil {
		t.Fatalf("load enabled controller: %v", err)
	}
	if len(controller.workloads) != 2 {
		t.Fatalf("expected business and diagnostics workloads, got=%d", len(controller.workloads))
	}
	diagnostics, ok := controller.workloads[1].workload.(*diagnosticsServer)
	if !ok {
		t.Fatalf("unexpected diagnostics workload type: %T", controller.workloads[1].workload)
	}

	// 第一阶段：通过与 Handler 同一 managed bean 的 Observer 生成稳定指标样本。
	observer, err := ioc.ResolveAs[webhertz.RequestObserver](runtime.Container())
	if err != nil {
		t.Fatalf("resolve prometheus observer: %v", err)
	}
	if lifecycle, ok := observer.(webhertz.RequestLifecycleObserver); ok {
		lifecycle.RequestStarted(context.Background(), http.MethodGet)
	}
	observer.Observe(context.Background(), webhertz.HTTPRequestObservation{
		Method: http.MethodGet, RoutePattern: "/api/hello", Status: http.StatusOK,
		Duration: time.Millisecond, RequestBytes: 0, ResponseBytes: 2, Outcome: webhertz.RequestOutcomeSuccess,
	})

	// 第二阶段：统一 Controller 启动两个真实 Server，并从独立 listener 外部抓取 Metrics。
	runCtx, cancel := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() { runResult <- controller.Run(runCtx) }()
	address := waitForDiagnosticsAddress(t, diagnostics)
	response, err := http.Get("http://" + address + defaultMetricsPath)
	if err != nil {
		t.Fatalf("get real prometheus endpoint: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK ||
		!strings.Contains(string(body), "gobase_http_server_requests_total") ||
		strings.Contains(string(body), "gobase_ioc_") ||
		strings.Contains(string(body), "go_gc_duration_seconds") ||
		strings.Contains(string(body), "process_cpu_seconds_total") {
		t.Fatalf("unexpected real metrics: status=%d body=%s read_err=%v", response.StatusCode, body, readErr)
	}

	// 第三阶段：取消 Runner 后由同一 Controller 完成两成员 Shutdown，再关闭 IOC/Registry 所有权链。
	cancel()
	if err := waitForDiagnosticsError(t, runResult); err != nil {
		t.Fatalf("run after cancellation: %v", err)
	}
	if err := controller.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown enabled workloads: %v", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("close enabled runtime: %v", err)
	}
}

// TestWorkloadAssemblyDiagnosticsPortConflictStopsBusiness 验证真实诊断监听失败会使 Runner 返回并触发业务 Server 收敛。
// 测试方法：预占诊断地址、以端口 0 启动业务 Hertz，断言 Controller Run 保留 listen cause，随后统一 Shutdown/Runtime Close 成功。
func TestWorkloadAssemblyDiagnosticsPortConflictStopsBusiness(t *testing.T) {
	// 第一阶段：预占诊断地址，并以相同地址构建完整应用 Runtime 和 Controller。
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve diagnostics address: %v", err)
	}
	defer func() { _ = occupied.Close() }()
	t.Setenv(quickstartAddressEnv, "127.0.0.1:0")
	t.Setenv(quickstartDiagnosticsAddressEnv, occupied.Addr().String())
	runtime := buildQuickstartRuntime(t, defaultConfig())
	assembly := &workloadAssembly{}
	if err := assembly.Prepare(context.Background(), runtime, nil); err != nil {
		t.Fatalf("prepare conflicted workloads: %v", err)
	}
	controller, err := assembly.loadController("test")
	if err != nil {
		t.Fatalf("load conflicted controller: %v", err)
	}

	// 第二阶段：诊断成员 listen 失败成为首发错误，并取消已经并发启动的业务 Hertz。
	runErr := controller.Run(context.Background())
	if runErr == nil || !strings.Contains(runErr.Error(), "name=diagnostics-http") || !strings.Contains(runErr.Error(), "reason=listen_failed") {
		t.Fatalf("expected diagnostics listen failure, got=%v", runErr)
	}
	if err := controller.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown after diagnostics conflict: %v", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("close conflicted runtime: %v", err)
	}
}

// buildQuickstartRuntime 构建与 main 相同的真实 IOC Module 图，失败时直接终止测试。
func buildQuickstartRuntime(t *testing.T, defaults map[string]any) *appruntime.Runtime {
	t.Helper()
	application := appruntime.New(
		appruntime.WithConfig(baseconfig.File(baseconfig.FileDisabled()), baseconfig.Defaults(defaults)),
		appruntime.WithBeans(&GreetingController{}),
		appruntime.WithModules(
			newApplicationMetricsModule(applicationMetricsConfig{}),
			webhertzobserve.NewModule(webhertzobserve.WithRequestLog(), webhertzobserve.WithSharedPrometheus()),
			appwebhertz.NewModule(""),
		),
	)
	runtime, err := application.Build(context.Background())
	if err != nil {
		t.Fatalf("build quickstart runtime: %v", err)
	}
	return runtime
}
