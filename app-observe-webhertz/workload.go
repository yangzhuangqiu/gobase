package webhertzobserve

import (
	"context"

	baseapp "github.com/yangzhuangqiu/gobase/app"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

const (
	// metricsWorkloadName 是独立 Prometheus HTTP Server 在 app 监督器中的稳定语义名称。
	metricsWorkloadName = "web.hertz.metrics"
	// metricsServerLoggerName 是根 LoggerFactory 下 Metrics Workload 的稳定组件名。
	metricsServerLoggerName = "app.observe.webhertz.metrics_server"
)

// metricsWorkload 只适配独立 Metrics Server Resource，不拥有或启动业务 Hertz Server。
type metricsWorkload struct{}

var _ baseapp.Workload = (*metricsWorkload)(nil)

// newMetricsWorkload 创建独立 Prometheus HTTP Server 的纯内存生命周期适配器。
// 阶段角色：Module.Workloads 收集阶段，不解析 IOC 或绑定诊断端口。
func newMetricsWorkload() baseapp.Workload {
	return &metricsWorkload{}
}

// Name 返回独立 Metrics Server 的稳定工作负载名称。
func (this *metricsWorkload) Name() string {
	return metricsWorkloadName
}

// Run 解析 Module 在 IOC Init 预绑定的 Metrics Server Resource，并阻塞到其真正停止。
func (this *metricsWorkload) Run(
	ctx context.Context,
	runtime baseapp.Runtime,
	_ baseapp.CommandContext,
) error {
	// 第一阶段：只解析 observe Module 发布的 Metrics Resource，不触碰业务 web.Server。
	resource, err := resolveMetricsServerResource(runtime, "run")
	if err != nil {
		return err
	}
	// 第二阶段：记录实际预绑定地址后进入阻塞 Serve，最终收敛由 App 监督器等待。
	address, path := resource.endpoint()
	runtime.Logger(metricsServerLoggerName).Info(ctx, "Metrics HTTP Server starting",
		baselog.F("op", "app_observe_webhertz.metrics_server"),
		baselog.F("result", "ok"),
		baselog.F("reason", "workload_started"),
		baselog.F("address", address),
		baselog.F("path", path),
	)
	return resource.run(ctx)
}

// Shutdown 解析同一 Metrics Server Resource，并在 app graceful 预算内请求停止。
// 阶段角色：只停止独立诊断端口；app 监督器继续等待本 Workload.Run 真实返回。
func (this *metricsWorkload) Shutdown(
	ctx context.Context,
	runtime baseapp.Runtime,
	_ baseapp.CommandContext,
) error {
	resource, err := resolveMetricsServerResource(runtime, "shutdown")
	if err != nil {
		return err
	}
	return resource.shutdown(ctx)
}

// resolveMetricsServerResource 从本次 Runtime 解析唯一 Metrics Server Resource，并保留分层错误语义。
// 阶段角色：Metrics Workload Run/Shutdown 共用的 IOC 解析边界。
func resolveMetricsServerResource(runtime baseapp.Runtime, stage string) (metricsServerResource, error) {
	// 第一阶段：Workload 必须运行在已构建 Runtime 中，WithoutRuntime 节点不支持诊断 Server。
	if isNilValue(runtime) {
		return nil, errorForWorkloadResolveFailed(messagef(
			"op=app_observe_webhertz.workload result=failed stage=%s reason=runtime_required",
			stage,
		))
	}
	container := runtime.Container()
	if isNilValue(container) {
		return nil, errorForWorkloadResolveFailed(messagef(
			"op=app_observe_webhertz.workload result=failed stage=%s reason=container_required",
			stage,
		))
	}
	// 第二阶段：按内部唯一接口解析 Module owned Resource，缺失 Module 时保留 IOC cause。
	resource, err := ioc.ResolveAs[metricsServerResource](container)
	if err != nil {
		return nil, errorForWorkloadResolveFailed(messagef(
			"op=app_observe_webhertz.workload result=failed stage=%s reason=ioc_resolve_failed",
			stage,
		), err)
	}
	return resource, nil
}
