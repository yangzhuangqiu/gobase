package webhertzobserve

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	baseapp "github.com/yangzhuangqiu/gobase/app"
	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
	appwebhertz "github.com/yangzhuangqiu/gobase/app-web-hertz"
	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/errcode"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
	baseweb "github.com/yangzhuangqiu/gobase/web"
)

const (
	// workloadTestTimeout 是真实监听和并发收敛测试的固定等待预算。
	workloadTestTimeout = 3 * time.Second
)

// workloadTestRuntime 提供 Workload 所需的最小只读运行时。
type workloadTestRuntime struct {
	// container 是已经完成 Build 的测试 IOC。
	container ioc.Container
	// logger 是不输出事件但保持非 nil 语义的测试 Logger。
	logger baselog.Logger
}

// Info 返回空应用元信息；Workload 不读取该字段。
func (this *workloadTestRuntime) Info() appruntime.ApplicationInfo {
	return appruntime.ApplicationInfo{}
}

// ConfigManager 返回 nil；Metrics 配置已在 plan 中冻结。
func (this *workloadTestRuntime) ConfigManager() baseconfig.Manager { return nil }

// Logger 返回稳定无输出 Logger。
func (this *workloadTestRuntime) Logger(string) baselog.Logger { return this.logger }

// Container 返回已经完成 Build 的测试 IOC。
func (this *workloadTestRuntime) Container() ioc.Container { return this.container }

// workloadTestServer 模拟必须收到显式 Shutdown 才会结束 Run 的业务 Server。
type workloadTestServer struct {
	// started 标记 Run 是否真正进入。
	started atomic.Bool
	// shutdown 标记 Workload 是否调用了显式 Shutdown。
	shutdown atomic.Bool
	// stopped 是 Run 的真实完成屏障。
	stopped chan struct{}
	// stopOnce 保证重复 Shutdown 不会关闭两次通道。
	stopOnce sync.Once
}

// Run 标记启动并等待显式 Shutdown，故意不依赖 run context。
func (this *workloadTestServer) Run(context.Context) error {
	this.started.Store(true)
	<-this.stopped
	return nil
}

// Shutdown 标记调用并真实结束 Run。
func (this *workloadTestServer) Shutdown(context.Context) error {
	this.shutdown.Store(true)
	this.stopOnce.Do(func() { close(this.stopped) })
	return nil
}

// workloadMetricsPlanStub 提供测试监听配置和固定 Handler。
type workloadMetricsPlanStub struct {
	// config 是 Workload 读取的冻结配置。
	config MetricsServerConfig
	// handler 是 Metrics HTTP 响应实现。
	handler PrometheusHandler
}

// metricsConfig 返回测试配置。
func (this *workloadMetricsPlanStub) metricsConfig() MetricsServerConfig { return this.config }

// metricsHandler 返回测试 Handler。
func (this *workloadMetricsPlanStub) metricsHandler() PrometheusHandler { return this.handler }

// TestWorkloadServesMetricsAndStopsBothServers 验证组合 Workload 使用 app 关闭 context 并建立真实收敛屏障。
//
// 测试方法：启动不响应 run context 的业务 Server 和真实 Metrics 端口；取消 run context 后显式调用 Shutdown，
// 断言两个 Server 均结束、业务 Shutdown 被调用且 Workload Run/Shutdown 都成功返回。
func TestWorkloadServesMetricsAndStopsBothServers(t *testing.T) {
	address := reserveWorkloadTestAddress(t)
	server := &workloadTestServer{stopped: make(chan struct{})}
	plan := &workloadMetricsPlanStub{
		config: MetricsServerConfig{
			Address:           address,
			Path:              "/metrics",
			ReadHeaderTimeout: time.Second,
		},
		handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, "workload_metric 1\n")
		}),
	}
	runtime := buildWorkloadTestRuntime(t, server, plan)
	workload := Workload()
	if workload.Name() != observedWorkloadName {
		t.Fatalf("unexpected workload name: %s", workload.Name())
	}
	runCtx, cancelRun := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() { runResult <- workload.Run(runCtx, runtime, nil) }()

	body := requestWorkloadMetricsUntilReady(t, "http://"+address+"/metrics")
	if body != "workload_metric 1\n" {
		t.Fatalf("unexpected metrics body: %q", body)
	}
	cancelRun()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	shutdownErr := workload.Shutdown(shutdownCtx, runtime, nil)
	cancelShutdown()
	if shutdownErr != nil {
		t.Fatalf("shutdown observed workload: %v", shutdownErr)
	}
	select {
	case err := <-runResult:
		if err != nil {
			t.Fatalf("workload returned error: %v", err)
		}
	case <-time.After(workloadTestTimeout):
		t.Fatal("workload did not converge after Shutdown")
	}
	if !server.started.Load() || !server.shutdown.Load() {
		t.Fatalf("unexpected business lifecycle: started=%t shutdown=%t", server.started.Load(), server.shutdown.Load())
	}
}

// TestWorkloadListenFailurePreventsBusinessStart 验证 Metrics 端口冲突在业务 Server Run 前失败。
//
// 测试方法：预占随机端口后执行 Workload，断言 listen_failed 错误码且业务 Server 从未启动。
func TestWorkloadListenFailurePreventsBusinessStart(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve occupied address: %v", err)
	}
	defer func() { _ = listener.Close() }()

	server := &workloadTestServer{stopped: make(chan struct{})}
	plan := &workloadMetricsPlanStub{
		config: MetricsServerConfig{
			Address:           listener.Addr().String(),
			Path:              "/metrics",
			ReadHeaderTimeout: time.Second,
		},
		handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	}
	runtime := buildWorkloadTestRuntime(t, server, plan)
	workload := Workload()
	err = workload.Run(context.Background(), runtime, nil)
	if !errcode.IsCode[ErrorCode](err, ErrCodeMetricsServerListenFailed) {
		t.Fatalf("expected listen-failed error, got=%v", err)
	}
	if server.started.Load() {
		t.Fatal("business server must not start after metrics listener conflict")
	}
	if shutdownErr := workload.Shutdown(context.Background(), runtime, nil); shutdownErr != nil {
		t.Fatalf("shutdown failed setup: %v", shutdownErr)
	}
}

// TestObservedAndBaseWorkloadCannotStartSameServerTwice 验证两个适配器共享业务 Server 语义身份。
//
// 测试方法：同时声明基础 Hertz Workload 与观测复合 Workload，执行前断言 app 以名称重复失败，
// 避免使用者因组合两个看似独立的适配器而重复启动同一个业务 Server。
func TestObservedAndBaseWorkloadCannotStartSameServerTwice(t *testing.T) {
	application := baseapp.New(baseapp.WithWorkloads(appwebhertz.Workload(), Workload()))
	err := application.ExecuteContext(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "reason=name_duplicate name="+observedWorkloadName) {
		t.Fatalf("expected duplicate business workload identity, got=%v", err)
	}
}

// buildWorkloadTestRuntime 注册业务 Server 与内部 Metrics plan 并完成 IOC Build。
func buildWorkloadTestRuntime(t *testing.T, server baseweb.Server, plan metricsServerPlan) baseapp.Runtime {
	t.Helper()
	container := ioc.New()
	if err := container.Register(server, ioc.WithDeclare[baseweb.Server](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register business server: %v", err)
	}
	if err := container.Register(plan, ioc.WithDeclare[metricsServerPlan](), ioc.WithBorrowed()); err != nil {
		t.Fatalf("register metrics plan: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build workload container: %v", err)
	}
	t.Cleanup(func() { _ = container.Close(context.Background()) })
	return &workloadTestRuntime{
		container: container,
		logger:    baselog.NewLoggerFactory(nil).Get("workload.test"),
	}
}

// reserveWorkloadTestAddress 返回当前可绑定的回环地址；关闭后的短窗口只由单个测试占用。
func reserveWorkloadTestAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve workload address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release workload address: %v", err)
	}
	return address
}

// requestWorkloadMetricsUntilReady 在固定预算内轮询 Metrics 端口并返回响应正文。
func requestWorkloadMetricsUntilReady(t *testing.T, target string) string {
	t.Helper()
	deadline := time.Now().Add(workloadTestTimeout)
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
	_ baseapp.Runtime   = (*workloadTestRuntime)(nil)
	_ baseweb.Server    = (*workloadTestServer)(nil)
	_ metricsServerPlan = (*workloadMetricsPlanStub)(nil)
)
