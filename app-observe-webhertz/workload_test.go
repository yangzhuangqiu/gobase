package webhertzobserve

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	baseapp "github.com/yangzhuangqiu/gobase/app"
	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
	appwebhertz "github.com/yangzhuangqiu/gobase/app-web-hertz"
	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/errcode"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

const (
	// workloadTestTimeout 是真实监听和并发收敛测试的固定等待预算。
	workloadTestTimeout = 3 * time.Second
)

// workloadTestRuntime 提供 Metrics Workload 所需的最小只读运行时。
type workloadTestRuntime struct {
	// container 是已经完成 Build 且持有预绑定 Metrics Resource 的测试 IOC。
	container ioc.Container
	// logger 是不输出事件但保持非 nil 语义的测试 Logger。
	logger baselog.Logger
}

// Info 返回空应用元信息；Metrics Workload 不读取该字段。
func (this *workloadTestRuntime) Info() appruntime.ApplicationInfo {
	return appruntime.ApplicationInfo{}
}

// ConfigManager 返回 nil；配置已经在 IOC Build 阶段冻结到 Resource。
func (this *workloadTestRuntime) ConfigManager() baseconfig.Manager { return nil }

// Logger 返回稳定无输出 Logger。
func (this *workloadTestRuntime) Logger(string) baselog.Logger { return this.logger }

// Container 返回已经完成 Build 的测试 IOC。
func (this *workloadTestRuntime) Container() ioc.Container { return this.container }

// TestMetricsWorkloadServesAndStopsIndependentServer 验证 observe Workload 只运行独立 Metrics Server。
//
// 测试方法：通过 Module 在 IOC Build 预绑定随机端口，启动 Workload 后请求 /metrics，再调用 Shutdown，
// 断言响应包含框架指标且 Run 在关闭后真实返回；测试容器不注册任何业务 web.Server。
func TestMetricsWorkloadServesAndStopsIndependentServer(t *testing.T) {
	// 第一阶段：Build 只创建 observe 资源，证明 Metrics Workload 不依赖业务 web.Server。
	runtime := buildMetricsWorkloadTestRuntime(t, "127.0.0.1:0")
	resource, err := ioc.ResolveAs[metricsServerResource](runtime.Container())
	if err != nil {
		t.Fatalf("resolve metrics resource: %v", err)
	}
	address, path := resource.endpoint()
	observer, err := ioc.ResolveAs[webhertz.RequestObserver](runtime.Container())
	if err != nil {
		t.Fatalf("resolve request observer: %v", err)
	}
	observer.(webhertz.RequestLifecycleObserver).RequestStarted(context.Background(), "GET")
	observer.Observe(context.Background(), webhertz.HTTPRequestObservation{
		Method: "GET", RoutePattern: "/ready", Status: http.StatusOK, Outcome: webhertz.RequestOutcomeSuccess,
	})
	workload := newMetricsWorkload()
	if workload.Name() != metricsWorkloadName {
		t.Fatalf("unexpected workload name: %s", workload.Name())
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() { runResult <- workload.Run(runCtx, runtime, nil) }()
	body := requestMetricsUntilReady(t, "http://"+address+path)
	if !strings.Contains(body, "gobase_http_server") {
		t.Fatalf("unexpected metrics body: %q", body)
	}

	// App 实际执行顺序会先取消 Run context，再调用 Shutdown；测试保持相同阶段顺序。
	cancelRun()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	shutdownErr := workload.Shutdown(shutdownCtx, runtime, nil)
	cancelShutdown()
	if shutdownErr != nil {
		t.Fatalf("shutdown metrics workload: %v", shutdownErr)
	}
	select {
	case err = <-runResult:
		if err != nil {
			t.Fatalf("metrics workload returned error: %v", err)
		}
	case <-time.After(workloadTestTimeout):
		t.Fatal("metrics workload did not converge after Shutdown")
	}
}

// TestMetricsListenerConflictFailsDuringRuntimeBuild 验证端口冲突发生在任何 Workload 启动之前。
//
// 测试方法：预占随机端口后注册零参数 observe Module，执行 IOC Build，断言返回 listen_failed；
// 不调用 Workload.Run，从而证明 listener 是 Runtime Resource 而不是运行 goroutine 内的延迟副作用。
func TestMetricsListenerConflictFailsDuringRuntimeBuild(t *testing.T) {
	// 第一阶段：由测试进程占用目标端口，建立确定的 Runtime Build 失败条件。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve occupied address: %v", err)
	}
	defer func() { _ = listener.Close() }()

	// 第二阶段：只执行 Module Register/IOC Build，不进入任何 Workload.Run。
	manager := newObservationTestManager(t, observationDefaults(listener.Addr().String()))
	t.Cleanup(func() { _ = manager.Close() })
	container := ioc.New()
	registerObservationTestManager(t, container, manager)
	if err = NewModule().Register(container); err != nil {
		t.Fatalf("register observe module: %v", err)
	}
	err = container.Build(context.Background())
	if !errcode.IsCode[ErrorCode](err, ErrCodeMetricsServerListenFailed) {
		t.Fatalf("expected listen-failed during Build, got=%v", err)
	}
}

// TestModuleCloseReleasesPreboundMetricsListener 验证未启动 Workload 时 IOC Close 仍会释放 listener。
//
// 测试方法：Build 零参数 Module 后记录系统分配地址，不运行 Metrics Workload，直接关闭 Container，
// 再绑定同一地址并断言成功，证明 Resource Close 覆盖命令未进入 Run 的回滚/短路路径。
func TestModuleCloseReleasesPreboundMetricsListener(t *testing.T) {
	// 第一阶段：Build 后读取实际预绑定地址，但故意不调用 Workload.Run。
	manager := newObservationTestManager(t, observationDefaults("127.0.0.1:0"))
	t.Cleanup(func() { _ = manager.Close() })
	container := ioc.New()
	registerObservationTestManager(t, container, manager)
	if err := NewModule().Register(container); err != nil {
		t.Fatalf("register observe module: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build observe module: %v", err)
	}
	resource, err := ioc.ResolveAs[metricsServerResource](container)
	if err != nil {
		t.Fatalf("resolve metrics resource: %v", err)
	}
	address, _ := resource.endpoint()
	if err = container.Close(context.Background()); err != nil {
		t.Fatalf("close observe container: %v", err)
	}

	// 第二阶段：立即复用同一地址，验证 IOC Closer 已释放未运行的 listener。
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("expected released metrics address %s: %v", address, err)
	}
	_ = listener.Close()
}

// TestBusinessAndMetricsWorkloadsHaveIndependentIdentities 验证两个 Server 可以作为并列 Workload 声明。
//
// 测试方法：以 WithoutRuntime 执行同时包含两个 Workload 的 App；断言错误来自缺少 Runtime，
// 而不是声明期 name_duplicate，从而固定 web.hertz 与 web.hertz.metrics 的独立语义身份。
func TestBusinessAndMetricsWorkloadsHaveIndependentIdentities(t *testing.T) {
	// 两个 Workload 在声明期必须通过唯一名称校验；WithoutRuntime 只用于让执行快速进入依赖错误。
	application := baseapp.New(
		baseapp.WithoutRuntime(),
		baseapp.WithModules(appwebhertz.NewModule(), NewModule()),
	)
	err := application.ExecuteContext(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "reason=runtime_required") {
		t.Fatalf("expected runtime-required execution error, got=%v", err)
	}
	if strings.Contains(err.Error(), "reason=name_duplicate") {
		t.Fatalf("business and metrics workloads must have independent names: %v", err)
	}
}

// buildMetricsWorkloadTestRuntime 构建含私有 Prometheus 与预绑定 Metrics Resource 的最小 Runtime。
func buildMetricsWorkloadTestRuntime(t *testing.T, address string) baseapp.Runtime {
	t.Helper()
	manager := newObservationTestManager(t, observationDefaults(address))
	t.Cleanup(func() { _ = manager.Close() })
	container := ioc.New()
	registerObservationTestManager(t, container, manager)
	if err := NewModule().Register(container); err != nil {
		t.Fatalf("register observe module: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build observe container: %v", err)
	}
	t.Cleanup(func() { _ = container.Close(context.Background()) })
	return &workloadTestRuntime{
		container: container,
		logger:    baselog.NewLoggerFactory(nil).Get("workload.test"),
	}
}

// observationDefaults 返回默认统一 scope 下的测试监听配置。
func observationDefaults(address string) map[string]any {
	return map[string]any{
		"observability": map[string]any{
			"webHertz": map[string]any{
				"prometheus": map[string]any{
					"server": map[string]any{
						"address":           address,
						"path":              "/metrics",
						"readHeaderTimeout": "1s",
					},
				},
			},
		},
	}
}

// requestMetricsUntilReady 在固定预算内轮询 Metrics 端口并返回响应正文。
func requestMetricsUntilReady(t *testing.T, target string) string {
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
