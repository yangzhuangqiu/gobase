package webhertzobserve

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	baseapp "github.com/yangzhuangqiu/gobase/app"
	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/errcode"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
	baseweb "github.com/yangzhuangqiu/gobase/web"
)

const (
	// runnerTestTimeout 是真实监听和并发收敛测试的固定等待预算。
	runnerTestTimeout = 3 * time.Second
)

// runnerTestRuntime 提供 Runner 所需的最小只读运行时。
type runnerTestRuntime struct {
	// container 是已经完成 Build 的测试 IOC。
	container ioc.Container
	// logger 是不输出事件但保持非 nil 语义的测试 Logger。
	logger baselog.Logger
}

// Info 返回空应用元信息；Runner 不读取该字段。
func (this *runnerTestRuntime) Info() appruntime.ApplicationInfo {
	return appruntime.ApplicationInfo{}
}

// ConfigManager 返回 nil；Metrics 配置已在 plan 中冻结。
func (this *runnerTestRuntime) ConfigManager() baseconfig.Manager {
	return nil
}

// Logger 返回稳定无输出 Logger。
func (this *runnerTestRuntime) Logger(string) baselog.Logger {
	return this.logger
}

// Container 返回已经完成 Build 的测试 IOC。
func (this *runnerTestRuntime) Container() ioc.Container {
	return this.container
}

// runnerTestServer 模拟遵守 web.Server Context/Shutdown 合同的业务工作负载。
type runnerTestServer struct {
	// started 标记 Run 是否真正进入。
	started atomic.Bool
	// shutdown 标记 Runner 是否调用了显式 Shutdown。
	shutdown atomic.Bool
}

// Run 标记启动并等待 Context 取消。
func (this *runnerTestServer) Run(ctx context.Context) error {
	this.started.Store(true)
	<-ctx.Done()
	return nil
}

// Shutdown 标记调用并立即完成幂等停止。
func (this *runnerTestServer) Shutdown(context.Context) error {
	this.shutdown.Store(true)
	return nil
}

// runnerMetricsPlanStub 提供测试监听配置和固定 Handler。
type runnerMetricsPlanStub struct {
	// config 是 Runner 读取的冻结配置。
	config MetricsServerConfig
	// handler 是 Metrics HTTP 响应实现。
	handler PrometheusHandler
}

// metricsConfig 返回测试配置。
func (this *runnerMetricsPlanStub) metricsConfig() MetricsServerConfig {
	return this.config
}

// metricsHandler 返回测试 Handler。
func (this *runnerMetricsPlanStub) metricsHandler() PrometheusHandler {
	return this.handler
}

// TestRunnerServesMetricsAndStopsBothWorkloads 验证正式 Runner 提供可抓取端点并统一收敛两个 Server。
//
// 测试方法：在空闲地址启动 fake 业务 Server 和 Metrics plan，轮询抓取固定响应；取消 Context 后断言 Runner 成功返回且业务 Shutdown 被调用。
func TestRunnerServesMetricsAndStopsBothWorkloads(t *testing.T) {
	address := reserveRunnerTestAddress(t)
	server := &runnerTestServer{}
	plan := &runnerMetricsPlanStub{
		config: MetricsServerConfig{
			Address:           address,
			Path:              "/metrics",
			ReadHeaderTimeout: time.Second,
			ShutdownTimeout:   time.Second,
		},
		handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, "runner_metric 1\n")
		}),
	}
	runtime := buildRunnerTestRuntime(t, server, plan)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- Runner()(ctx, runtime)
	}()

	body := requestRunnerMetricsUntilReady(t, "http://"+address+"/metrics")
	if body != "runner_metric 1\n" {
		t.Fatalf("unexpected metrics body: %q", body)
	}
	healthResponse, err := (&http.Client{Timeout: 200 * time.Millisecond}).Get("http://" + address + "/healthz")
	if err != nil {
		t.Fatalf("request unregistered health endpoint: %v", err)
	}
	_ = healthResponse.Body.Close()
	if healthResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("metrics server must not expose health endpoint: status=%d", healthResponse.StatusCode)
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("runner returned error: %v", err)
		}
	case <-time.After(runnerTestTimeout):
		t.Fatal("runner did not stop after cancellation")
	}
	if !server.started.Load() || !server.shutdown.Load() {
		t.Fatalf("unexpected business lifecycle: started=%t shutdown=%t", server.started.Load(), server.shutdown.Load())
	}
}

// TestRunnerListenFailurePreventsBusinessStart 验证 Metrics 端口冲突在业务 Server Run 前失败。
//
// 测试方法：预占随机端口后执行 Runner，断言 listen_failed 错误码且 fake 业务 Server 从未启动。
func TestRunnerListenFailurePreventsBusinessStart(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve occupied address: %v", err)
	}
	defer func() { _ = listener.Close() }()

	server := &runnerTestServer{}
	plan := &runnerMetricsPlanStub{
		config: MetricsServerConfig{
			Address:           listener.Addr().String(),
			Path:              "/metrics",
			ReadHeaderTimeout: time.Second,
			ShutdownTimeout:   time.Second,
		},
		handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	}
	runtime := buildRunnerTestRuntime(t, server, plan)
	err = Runner()(context.Background(), runtime)
	if !errcode.IsCode[ErrorCode](err, ErrCodeMetricsServerListenFailed) {
		t.Fatalf("expected listen-failed error, got=%v", err)
	}
	if server.started.Load() {
		t.Fatal("business server must not start after metrics listener conflict")
	}
}

// buildRunnerTestRuntime 注册业务 Server 与内部 Metrics plan 并完成 IOC Build。
// 阶段角色：为真实 Runner 测试提供与生产解析路径一致的容器。
func buildRunnerTestRuntime(
	t *testing.T,
	server baseweb.Server,
	plan metricsServerPlan,
) baseapp.Runtime {
	t.Helper()
	container := ioc.New()
	if err := container.Register(server, ioc.WithDeclare[baseweb.Server](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register business server: %v", err)
	}
	if err := container.Register(plan, ioc.WithDeclare[metricsServerPlan](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register metrics plan: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build runner container: %v", err)
	}
	t.Cleanup(func() { _ = container.Close(context.Background()) })
	return &runnerTestRuntime{
		container: container,
		logger:    baselog.NewLoggerFactory(nil).Get("runner.test"),
	}
}

// reserveRunnerTestAddress 返回当前可绑定的回环地址；关闭后的短窗口只由单个测试占用。
func reserveRunnerTestAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve runner address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release runner address: %v", err)
	}
	return address
}

// requestRunnerMetricsUntilReady 在固定预算内轮询 Metrics 端口并返回响应正文。
func requestRunnerMetricsUntilReady(t *testing.T, target string) string {
	t.Helper()
	deadline := time.Now().Add(runnerTestTimeout)
	for {
		client := &http.Client{Timeout: 200 * time.Millisecond}
		response, err := client.Get(target)
		if err == nil {
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil {
				t.Fatalf("read metrics response: %v", readErr)
			}
			return string(body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("metrics endpoint did not become ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

var (
	_ baseapp.Runtime   = (*runnerTestRuntime)(nil)
	_ baseweb.Server    = (*runnerTestServer)(nil)
	_ metricsServerPlan = (*runnerMetricsPlanStub)(nil)
)
