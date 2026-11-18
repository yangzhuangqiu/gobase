package webhertzobserve

import (
	"context"
	"errors"
	"net"
	"net/http"

	baseapp "github.com/yangzhuangqiu/gobase/app"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
	baseweb "github.com/yangzhuangqiu/gobase/web"
)

const (
	// metricsServerLoggerName 是根 LoggerFactory 下诊断工作负载的稳定组件名。
	metricsServerLoggerName = "app.observe.webhertz.metrics_server"
)

// metricsServerPlan 是同一原子 adapter bean 向 Runner 发布的内部监听计划。
type metricsServerPlan interface {
	// metricsConfig 返回 IOC Init 已冻结的监听与关闭配置。
	metricsConfig() MetricsServerConfig
	// metricsHandler 返回与当前 Prometheus Registry 对应的 Gather Handler。
	metricsHandler() PrometheusHandler
}

// observedWorkloadResult 把工作负载名称与结束原因一起传回监督循环。
type observedWorkloadResult struct {
	// name 是日志和错误中可检索的稳定工作负载名称。
	name string
	// err 是工作负载原始运行结果；正常优雅停止为 nil。
	err error
}

// Runner 返回可同时监督业务 Hertz 与可选 Metrics HTTP Server 的应用入口。
//
// 典型调用场景:
// 1. 应用通过 app-web-hertz 发布唯一 web.Server；
// 2. 本模块可选声明 WithMetricsServer；
// 3. 应用用 app.Runner 包装返回值，避免自行复制双工作负载协调代码。
//
// 返回:
// - 未声明 Metrics Server 时，行为等价于 app-web-hertz.Runner；
// - 已声明时，先预绑定诊断端口，再并发运行和统一关闭两个 Server。
func Runner() func(context.Context, baseapp.Runtime) error {
	return func(ctx context.Context, runtime baseapp.Runtime) error {
		if ctx == nil {
			return errorForRunnerResolveFailed(
				"op=app_observe_webhertz.runner result=failed stage=precheck reason=context_required",
			)
		}
		if isNilValue(runtime) {
			return errorForRunnerResolveFailed(
				"op=app_observe_webhertz.runner result=failed stage=precheck reason=runtime_required",
			)
		}
		container := runtime.Container()
		if isNilValue(container) {
			return errorForRunnerResolveFailed(
				"op=app_observe_webhertz.runner result=failed stage=precheck reason=container_required",
			)
		}

		// 第一阶段：业务 Server 始终唯一；Metrics 计划允许缺失但不允许重复。
		businessServer, err := ioc.ResolveAs[baseweb.Server](container)
		if err != nil {
			return errorForRunnerResolveFailed(
				"op=app_observe_webhertz.runner result=failed stage=resolve_business_server reason=ioc_resolve_failed",
				err,
			)
		}
		plans, err := ioc.ResolveAllAs[metricsServerPlan](container)
		if err != nil {
			return errorForRunnerResolveFailed(
				"op=app_observe_webhertz.runner result=failed stage=resolve_metrics_server reason=ioc_resolve_failed",
				err,
			)
		}
		if len(plans) == 0 {
			return businessServer.Run(ctx)
		}
		if len(plans) != 1 || isNilValue(plans[0]) {
			return errorForRunnerResolveFailed(messagef(
				"op=app_observe_webhertz.runner result=failed stage=resolve_metrics_server reason=single_plan_required count=%d",
				len(plans),
			))
		}

		// 第二阶段：只有声明监听计划时才切换为双工作负载监督。
		return runWithMetricsServer(
			ctx,
			runtime.Logger(metricsServerLoggerName),
			businessServer,
			plans[0],
		)
	}
}

// runWithMetricsServer 先占用诊断端口，再创建并监督业务/Metrics 两个 Server。
// 阶段角色：端口冲突必须发生在业务 Server Run 之前，保证启动全有或全无。
func runWithMetricsServer(
	ctx context.Context,
	logger baselog.Logger,
	businessServer baseweb.Server,
	plan metricsServerPlan,
) error {
	config := plan.metricsConfig()
	handler := plan.metricsHandler()
	if isNilValue(handler) {
		return errorForRunnerResolveFailed(
			"op=app_observe_webhertz.runner result=failed stage=resolve_metrics_handler reason=handler_required",
		)
	}
	listener, err := net.Listen("tcp", config.Address)
	if err != nil {
		return errorForMetricsServerListenFailed(
			messagef(
				"op=app_observe_webhertz.metrics_server result=failed stage=listen reason=listen_failed address=%s",
				config.Address,
			),
			err,
		)
	}
	defer func() { _ = listener.Close() }()

	mux := http.NewServeMux()
	mux.Handle(config.Path, handler)
	metricsServer := &http.Server{
		Addr:              config.Address,
		Handler:           mux,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
	}
	logger.Info(ctx, "Metrics HTTP Server starting",
		baselog.F("op", "app_observe_webhertz.metrics_server"),
		baselog.F("result", "ok"),
		baselog.F("reason", "runner_started"),
		baselog.F("address", listener.Addr().String()),
		baselog.F("path", config.Path),
	)
	return superviseObservedWorkloads(ctx, logger, businessServer, metricsServer, listener, config)
}

// superviseObservedWorkloads 并发运行两个 Server，在取消或任一结束后统一关闭并等待。
// 阶段角色：Runner 的主动工作负载监督核心，保留运行和关闭阶段的全部失败原因。
func superviseObservedWorkloads(
	ctx context.Context,
	logger baselog.Logger,
	businessServer baseweb.Server,
	metricsServer *http.Server,
	listener net.Listener,
	config MetricsServerConfig,
) error {
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	results := make(chan observedWorkloadResult, 2)
	go func() {
		results <- observedWorkloadResult{name: "business", err: businessServer.Run(runCtx)}
	}()
	go func() {
		results <- observedWorkloadResult{
			name: "metrics",
			err:  normalizeMetricsServerError(metricsServer.Serve(listener)),
		}
	}()

	// 第一阶段：主 Context 取消或任一工作负载退出都会进入同一停止事务。
	received := 0
	runErrors := make([]error, 0, 3)
	select {
	case first := <-results:
		received++
		runErrors = appendObservedWorkloadResult(runErrors, first, ctx.Err() == nil)
	case <-ctx.Done():
	}
	cancelRun()

	// 第二阶段：使用脱离已取消父 Context 的独立预算并发停止两个 Server。
	shutdownCtx, cancelShutdown := context.WithTimeout(context.WithoutCancel(ctx), config.ShutdownTimeout)
	shutdownErr := shutdownObservedWorkloads(shutdownCtx, businessServer, metricsServer)

	// 第三阶段：等待尚未回报的 Run goroutine，避免 Runner 返回后遗留活跃工作负载。
waitLoop:
	for received < 2 {
		select {
		case current := <-results:
			received++
			runErrors = appendObservedWorkloadResult(runErrors, current, false)
		case <-shutdownCtx.Done():
			runErrors = append(runErrors, errorForMetricsServerRunFailed(
				"op=app_observe_webhertz.metrics_server result=failed stage=wait_workloads reason=shutdown_deadline_exceeded",
				shutdownCtx.Err(),
			))
			break waitLoop
		}
	}
	cancelShutdown()

	result := errors.Join(append(runErrors, shutdownErr)...)
	if result != nil {
		logger.Error(context.WithoutCancel(ctx), "Observed Web workloads stopped with error",
			baselog.F("op", "app_observe_webhertz.metrics_server"),
			baselog.F("result", "failed"),
			baselog.F("reason", "workload_failed"),
			baselog.Err(result),
		)
		return result
	}
	logger.Info(context.WithoutCancel(ctx), "Observed Web workloads stopped",
		baselog.F("op", "app_observe_webhertz.metrics_server"),
		baselog.F("result", "ok"),
		baselog.F("reason", "graceful_shutdown"),
	)
	return nil
}

// appendObservedWorkloadResult 把单个运行结果转换为稳定错误语义。
// unexpected 为 true 时，活跃 Context 下的 nil 返回同样视为异常退出。
func appendObservedWorkloadResult(
	target []error,
	result observedWorkloadResult,
	unexpected bool,
) []error {
	if result.err != nil {
		return append(target, errorForMetricsServerRunFailed(
			messagef(
				"op=app_observe_webhertz.metrics_server result=failed stage=run reason=workload_failed workload=%s",
				result.name,
			),
			result.err,
		))
	}
	if unexpected {
		return append(target, errorForMetricsServerRunFailed(messagef(
			"op=app_observe_webhertz.metrics_server result=failed stage=supervise reason=workload_exited_unexpectedly workload=%s",
			result.name,
		)))
	}
	return target
}

// shutdownObservedWorkloads 并发停止两个 Server，避免单个慢分支耗尽另一分支的宽限时间。
func shutdownObservedWorkloads(
	ctx context.Context,
	businessServer baseweb.Server,
	metricsServer *http.Server,
) error {
	results := make(chan error, 2)
	go func() {
		results <- metricsServer.Shutdown(ctx)
	}()
	go func() {
		results <- businessServer.Shutdown(ctx)
	}()
	return errors.Join(<-results, <-results)
}

// normalizeMetricsServerError 把标准库优雅关闭哨兵收敛为成功语义。
func normalizeMetricsServerError(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
