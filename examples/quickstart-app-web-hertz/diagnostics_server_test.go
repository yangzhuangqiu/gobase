package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	// diagnosticsTestTimeout 是本地 listener 和并发关闭测试的最长等待预算。
	diagnosticsTestTimeout = 3 * time.Second
)

// TestDiagnosticsMuxKeepsPprofDisabledAndUsesPrivateRoutes 验证 Metrics 与 pprof 路由独立且 pprof 默认关闭。
// 测试方法：向显式 mux 请求 Metrics 和 pprof，再启用 pprof 验证 index；全过程不使用 DefaultServeMux。
func TestDiagnosticsMuxKeepsPprofDisabledAndUsesPrivateRoutes(t *testing.T) {
	previousDefaultMux := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	defer func() { http.DefaultServeMux = previousDefaultMux }()

	// 第一阶段：只挂 Metrics，证明 pprof 不会因导入标准库包而隐式出现在私有 mux。
	metrics := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("metric_value 1\n"))
	})
	mux, err := newDiagnosticsMux(diagnosticsConfig{MetricsEnabled: true, MetricsPath: "/metrics"}, metrics)
	if err != nil {
		t.Fatalf("build metrics mux: %v", err)
	}
	metricsResponse := httptest.NewRecorder()
	mux.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metricsResponse.Code != http.StatusOK || !strings.Contains(metricsResponse.Body.String(), "metric_value") {
		t.Fatalf("unexpected metrics response: status=%d body=%s", metricsResponse.Code, metricsResponse.Body.String())
	}
	pprofResponse := httptest.NewRecorder()
	mux.ServeHTTP(pprofResponse, httptest.NewRequest(http.MethodGet, diagnosticsPprofPrefix, nil))
	if pprofResponse.Code != http.StatusNotFound {
		t.Fatalf("expected disabled pprof 404, got=%d", pprofResponse.Code)
	}

	// 第二阶段：显式启用后只检查私有 mux 的 pprof index。
	pprofMux, err := newDiagnosticsMux(diagnosticsConfig{PprofEnabled: true}, nil)
	if err != nil {
		t.Fatalf("build pprof mux: %v", err)
	}
	enabledResponse := httptest.NewRecorder()
	pprofMux.ServeHTTP(enabledResponse, httptest.NewRequest(http.MethodGet, diagnosticsPprofPrefix, nil))
	if enabledResponse.Code != http.StatusOK || !strings.Contains(enabledResponse.Body.String(), "profile") {
		t.Fatalf("unexpected pprof response: status=%d body=%s", enabledResponse.Code, enabledResponse.Body.String())
	}
	globalResponse := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(globalResponse, httptest.NewRequest(http.MethodGet, diagnosticsPprofPrefix, nil))
	if globalResponse.Code != http.StatusNotFound {
		t.Fatalf("expected DefaultServeMux to remain untouched, got=%d", globalResponse.Code)
	}
	profileResponse := httptest.NewRecorder()
	pprofMux.ServeHTTP(profileResponse, httptest.NewRequest(http.MethodGet, diagnosticsPprofPrefix+"goroutine?debug=1", nil))
	if profileResponse.Code != http.StatusOK || !strings.Contains(profileResponse.Body.String(), "goroutine profile") {
		t.Fatalf("unexpected runtime profile response: status=%d body=%s", profileResponse.Code, profileResponse.Body.String())
	}
}

// TestDiagnosticsServerServesMetricsAndStopsOnCancellation 验证真实诊断 listener 可访问且 Runner context 取消能收敛。
// 测试方法：使用端口 0 启动 Server，外部 HTTP 请求 Metrics 后取消 context，并断言 Run 正常返回。
func TestDiagnosticsServerServesMetricsAndStopsOnCancellation(t *testing.T) {
	// 第一阶段：构造端口 0 Server 并等待 listener 发布实际地址。
	metrics := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("quickstart_metric 1\n"))
	})
	server, err := newDiagnosticsServer(diagnosticsConfig{
		Enabled: true, Address: "127.0.0.1:0", MetricsEnabled: true, MetricsPath: "/metrics",
	}, metrics)
	if err != nil {
		t.Fatalf("new diagnostics server: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() { runResult <- server.Run(ctx) }()
	address := waitForDiagnosticsAddress(t, server)

	// 第二阶段：经真实网络访问 Metrics，再取消运行 context 验证自然收敛。
	response, err := http.Get("http://" + address + "/metrics")
	if err != nil {
		t.Fatalf("get diagnostics metrics: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), "quickstart_metric") {
		t.Fatalf("unexpected external metrics: status=%d body=%s read_err=%v", response.StatusCode, body, readErr)
	}
	cancel()
	if err := waitForDiagnosticsError(t, runResult); err != nil {
		t.Fatalf("run after cancellation: %v", err)
	}
}

// TestDiagnosticsServerReportsPortConflict 验证诊断端口冲突进入稳定 listen 阶段且不返回半运行对象。
// 测试方法：预占 loopback 端口后启动诊断 Server，断言错误包含 listen_failed，随后 Shutdown 幂等成功。
func TestDiagnosticsServerReportsPortConflict(t *testing.T) {
	// 第一阶段：预占真实 loopback 地址，并让诊断 Server 尝试重复绑定。
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve address: %v", err)
	}
	defer func() { _ = occupied.Close() }()
	server, err := newDiagnosticsServer(diagnosticsConfig{
		Enabled: true, Address: occupied.Addr().String(), MetricsEnabled: true, MetricsPath: "/metrics",
	}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if err != nil {
		t.Fatalf("new diagnostics server: %v", err)
	}
	// 第二阶段：监听失败可定位，随后终结性 Shutdown 仍保持幂等。
	if err := server.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "stage=listen") || !strings.Contains(err.Error(), "reason=listen_failed") {
		t.Fatalf("expected listen conflict, got=%v", err)
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed listener: %v", err)
	}
}

// TestDiagnosticsServerShutdownDeadlineForcesActiveRequest 验证 graceful deadline 到期后 Force Close 活跃连接。
// 测试方法：让 Handler 阻塞到请求 context 取消，以短 deadline Shutdown，断言 deadline 可分支且 Run/请求都收敛。
func TestDiagnosticsServerShutdownDeadlineForcesActiveRequest(t *testing.T) {
	// 第一阶段：启动会一直持有连接、直到 Force Close 取消请求 context 的 Handler。
	requestStarted := make(chan struct{})
	metrics := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
	})
	server, err := newDiagnosticsServer(diagnosticsConfig{
		Enabled: true, Address: "127.0.0.1:0", MetricsEnabled: true, MetricsPath: "/metrics",
	}, metrics)
	if err != nil {
		t.Fatalf("new diagnostics server: %v", err)
	}
	runResult := make(chan error, 1)
	go func() { runResult <- server.Run(context.Background()) }()
	address := waitForDiagnosticsAddress(t, server)
	requestResult := make(chan error, 1)
	go func() {
		response, requestErr := http.Get("http://" + address + "/metrics")
		if requestErr == nil {
			_ = response.Body.Close()
		}
		requestResult <- requestErr
	}()
	waitForDiagnosticsSignal(t, requestStarted, "active request")

	// 第二阶段：让 graceful deadline 到期，断言错误可分支且所有 goroutine 最终返回。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr == nil || !errors.Is(shutdownErr, context.DeadlineExceeded) {
		t.Fatalf("expected forced shutdown deadline, got=%v", shutdownErr)
	}
	if err := waitForDiagnosticsError(t, runResult); err != nil {
		t.Fatalf("run after force close: %v", err)
	}
	_ = waitForDiagnosticsError(t, requestResult)
}

// waitForDiagnosticsAddress 等待真实 listener 地址发布，超时终止测试。
func waitForDiagnosticsAddress(t *testing.T, server *diagnosticsServer) string {
	t.Helper()
	deadline := time.Now().Add(diagnosticsTestTimeout)
	for time.Now().Before(deadline) {
		if address := server.Address(); address != "" {
			return address
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for diagnostics address")
	return ""
}

// waitForDiagnosticsError 等待并发结果，超时终止测试。
func waitForDiagnosticsError(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(diagnosticsTestTimeout):
		t.Fatal("timed out waiting for diagnostics result")
		return nil
	}
}

// waitForDiagnosticsSignal 等待测试阶段信号，超时终止测试。
func waitForDiagnosticsSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(diagnosticsTestTimeout):
		t.Fatalf("timed out waiting for %s", name)
	}
}
