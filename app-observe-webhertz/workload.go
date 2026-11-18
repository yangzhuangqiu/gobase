package webhertzobserve

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"

	baseapp "github.com/yangzhuangqiu/gobase/app"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
	baseweb "github.com/yangzhuangqiu/gobase/web"
)

const (
	// observedWorkloadName 与基础 Hertz Workload 共用业务 Server 身份，阻止同一 Server 被重复启动。
	observedWorkloadName = "web.hertz"
	// metricsServerLoggerName 是根 LoggerFactory 下诊断工作负载的稳定组件名。
	metricsServerLoggerName = "app.observe.webhertz.metrics_server"
)

// metricsServerPlan 是同一原子 adapter bean 向 Workload 发布的内部监听计划。
type metricsServerPlan interface {
	// metricsConfig 返回 IOC Init 已冻结的监听配置。
	metricsConfig() MetricsServerConfig
	// metricsHandler 返回与当前 Prometheus Registry 对应的 Gather Handler。
	metricsHandler() PrometheusHandler
}

// observedRunState 保存一个内部 Server 的运行完成点和稳定结果。
type observedRunState struct {
	// Name 是错误和日志使用的低基数组件名称。
	Name string
	// Done 在对应 Server Run 完全返回后关闭。
	Done chan struct{}
	// Err 是经过取消与异常退出归一化的运行结果。
	Err error
}

// observedWorkload 把业务 Hertz 与可选 Metrics HTTP Server 组合为一个 app Workload。
type observedWorkload struct {
	// stateMu 保护 Run 发布、Shutdown 读取的本次运行状态。
	stateMu sync.RWMutex
	// ready 在 Run 已完成依赖解析、端口预绑定和内部 goroutine 发布后关闭。
	ready chan struct{}
	// readyOnce 保证失败、panic 和成功路径只发布一次 ready。
	readyOnce sync.Once
	// logger 是本次 Runtime 派生的 Metrics Server logger。
	logger baselog.Logger
	// businessServer 是 IOC 中唯一的业务 Web Server。
	businessServer baseweb.Server
	// metricsServer 是启用独立 Metrics 端口时创建的标准库 Server。
	metricsServer *http.Server
	// metricsListener 是业务 Server 启动前预绑定的诊断监听器。
	metricsListener net.Listener
	// states 是本次实际启动的内部 Server 运行状态。
	states []*observedRunState
	// triggerIndex 是首个返回并触发 app 统一 Shutdown 的内部 Server 下标。
	triggerIndex int
}

var _ baseapp.Workload = (*observedWorkload)(nil)

// Workload 返回同时监督业务 Hertz 与可选 Metrics HTTP Server 的主动工作负载。
// 典型调用场景：与 app-web-hertz Module 一起传给 app.WithWorkloads，并替代 app-web-hertz.Workload。
func Workload() baseapp.Workload {
	return &observedWorkload{
		ready:        make(chan struct{}),
		triggerIndex: -1,
	}
}

// Name 返回组合工作负载的稳定诊断名称。
func (this *observedWorkload) Name() string {
	return observedWorkloadName
}

// Run 解析业务 Server、预绑定可选 Metrics 端口，并阻塞到任一内部 Server 返回。
func (this *observedWorkload) Run(
	ctx context.Context,
	runtime baseapp.Runtime,
	_ baseapp.CommandContext,
) (err error) {
	defer this.markReady()
	if ctx == nil {
		return errorForWorkloadResolveFailed(
			"op=app_observe_webhertz.workload result=failed stage=precheck reason=context_required",
		)
	}
	if err = ctx.Err(); err != nil {
		return errorForWorkloadResolveFailed(
			"op=app_observe_webhertz.workload result=failed stage=precheck reason=context_done",
			err,
		)
	}
	if isNilValue(runtime) {
		return errorForWorkloadResolveFailed(
			"op=app_observe_webhertz.workload result=failed stage=precheck reason=runtime_required",
		)
	}
	container := runtime.Container()
	if isNilValue(container) {
		return errorForWorkloadResolveFailed(
			"op=app_observe_webhertz.workload result=failed stage=precheck reason=container_required",
		)
	}

	// 第一阶段：业务 Server 始终唯一；Metrics 计划允许缺失但不允许重复。
	businessServer, plans, resolveErr := resolveObservedDependencies(container)
	if resolveErr != nil {
		return resolveErr
	}
	this.logger = runtime.Logger(metricsServerLoggerName)
	this.businessServer = businessServer
	runs := []func() error{func() error { return businessServer.Run(ctx) }}
	names := []string{"business"}
	if len(plans) == 1 {
		if err = this.prepareMetricsServer(plans[0]); err != nil {
			return err
		}
		runs = append(runs, func() error {
			return normalizeMetricsServerError(this.metricsServer.Serve(this.metricsListener))
		})
		names = append(names, "metrics")
	}

	// 第二阶段：发布完整运行快照后再启动全部内部 Server，使 Shutdown 只观察完整拓扑。
	firstDone := make(chan int, 1)
	var firstOnce sync.Once
	states := make([]*observedRunState, len(runs))
	for index := range runs {
		state := &observedRunState{Name: names[index], Done: make(chan struct{})}
		states[index] = state
	}
	this.stateMu.Lock()
	this.states = states
	this.stateMu.Unlock()
	for index, run := range runs {
		go func(index int, run func() error, state *observedRunState) {
			state.Err = executeObservedRun(ctx, state.Name, run)
			firstOnce.Do(func() {
				this.stateMu.Lock()
				this.triggerIndex = index
				this.stateMu.Unlock()
				firstDone <- index
			})
			close(state.Done)
		}(index, run, states[index])
	}
	this.markReady()

	// 第三阶段：首个完成点只触发 app 统一关闭；其余结果由 Shutdown 在强屏障后聚合。
	triggerIndex := <-firstDone
	return states[triggerIndex].Err
}

// Shutdown 并发停止业务与 Metrics Server，并等待全部内部 Run 真实返回。
func (this *observedWorkload) Shutdown(
	ctx context.Context,
	_ baseapp.Runtime,
	_ baseapp.CommandContext,
) error {
	if ctx == nil {
		return errorForMetricsServerRunFailed(
			"op=app_observe_webhertz.workload result=failed stage=shutdown reason=context_required",
		)
	}
	// Run 的失败与 panic 路径也会发布 ready；等待后即可安全判断是否存在主动工作。
	<-this.ready
	this.stateMu.RLock()
	businessServer := this.businessServer
	metricsServer := this.metricsServer
	metricsListener := this.metricsListener
	states := append([]*observedRunState{}, this.states...)
	this.stateMu.RUnlock()
	if len(states) == 0 {
		if metricsServer != nil {
			return shutdownMetricsServer(ctx, metricsServer, metricsListener)
		}
		return nil
	}

	// 第一阶段：使用 app Shutdown Policy 传入的同一 context 并发停止全部内部 Server。
	actionCount := 1
	if metricsServer != nil {
		actionCount++
	}
	shutdownResults := make(chan error, actionCount)
	go func() { shutdownResults <- businessServer.Shutdown(ctx) }()
	if metricsServer != nil {
		go func() {
			shutdownResults <- shutdownMetricsServer(ctx, metricsServer, metricsListener)
		}()
	}
	results := make([]error, 0, actionCount+len(states))
	for index := 0; index < actionCount; index++ {
		results = append(results, <-shutdownResults)
	}

	// 第二阶段：无论 graceful 预算是否到期，都等待 Run 真正结束后才允许 Runtime Close。
	for _, state := range states {
		<-state.Done
	}
	this.stateMu.RLock()
	triggerIndex := this.triggerIndex
	this.stateMu.RUnlock()
	for index, state := range states {
		if index != triggerIndex && state.Err != nil {
			results = append(results, state.Err)
		}
	}
	return errors.Join(results...)
}

// markReady 发布 Run 初始化完成点；失败和 panic 同样解除 Shutdown 等待。
func (this *observedWorkload) markReady() {
	this.readyOnce.Do(func() { close(this.ready) })
}

// resolveObservedDependencies 从 IOC 解析唯一业务 Server 和零或一个 Metrics 计划。
func resolveObservedDependencies(container ioc.Container) (baseweb.Server, []metricsServerPlan, error) {
	businessServer, err := ioc.ResolveAs[baseweb.Server](container)
	if err != nil {
		return nil, nil, errorForWorkloadResolveFailed(
			"op=app_observe_webhertz.workload result=failed stage=resolve_business_server reason=ioc_resolve_failed",
			err,
		)
	}
	plans, err := ioc.ResolveAllAs[metricsServerPlan](container)
	if err != nil {
		return nil, nil, errorForWorkloadResolveFailed(
			"op=app_observe_webhertz.workload result=failed stage=resolve_metrics_server reason=ioc_resolve_failed",
			err,
		)
	}
	if len(plans) > 1 || len(plans) == 1 && isNilValue(plans[0]) {
		return nil, nil, errorForWorkloadResolveFailed(messagef(
			"op=app_observe_webhertz.workload result=failed stage=resolve_metrics_server reason=single_plan_required count=%d",
			len(plans),
		))
	}
	return businessServer, plans, nil
}

// prepareMetricsServer 校验 Handler、预绑定端口并发布标准库 Server。
// 阶段角色：端口冲突必须发生在业务 Server Run 之前，保证启动全有或全无。
func (this *observedWorkload) prepareMetricsServer(plan metricsServerPlan) error {
	config := plan.metricsConfig()
	handler := plan.metricsHandler()
	if isNilValue(handler) {
		return errorForWorkloadResolveFailed(
			"op=app_observe_webhertz.workload result=failed stage=resolve_metrics_handler reason=handler_required",
		)
	}
	listener, err := net.Listen("tcp", config.Address)
	if err != nil {
		return errorForMetricsServerListenFailed(
			messagef("op=app_observe_webhertz.metrics_server result=failed stage=listen reason=listen_failed address=%s", config.Address),
			err,
		)
	}
	mux := http.NewServeMux()
	mux.Handle(config.Path, handler)
	this.metricsListener = listener
	this.metricsServer = &http.Server{
		Addr:              config.Address,
		Handler:           mux,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
	}
	this.logger.Info(context.Background(), "Metrics HTTP Server starting",
		baselog.F("op", "app_observe_webhertz.metrics_server"),
		baselog.F("result", "ok"),
		baselog.F("reason", "workload_started"),
		baselog.F("address", listener.Addr().String()),
		baselog.F("path", config.Path),
	)
	return nil
}

// executeObservedRun 隔离内部 Server panic，并归一化取消与意外正常退出。
func executeObservedRun(ctx context.Context, name string, run func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.Join(err, errorForMetricsServerRunFailed(messagef(
				"op=app_observe_webhertz.workload result=failed stage=run reason=panic workload=%s payload=%v",
				name, recovered,
			)))
		}
	}()
	err = run()
	if ctx.Err() != nil && isObservedCancellationOnly(err, context.Cause(ctx)) {
		return nil
	}
	if err != nil {
		return errorForMetricsServerRunFailed(
			messagef("op=app_observe_webhertz.workload result=failed stage=run reason=workload_failed workload=%s", name),
			err,
		)
	}
	if ctx.Err() == nil {
		return errorForMetricsServerRunFailed(messagef(
			"op=app_observe_webhertz.workload result=failed stage=run reason=workload_exited_unexpectedly workload=%s",
			name,
		))
	}
	return nil
}

// shutdownMetricsServer 执行 graceful→force，并关闭预绑定 Listener。
func shutdownMetricsServer(ctx context.Context, server *http.Server, listener net.Listener) error {
	shutdownErr := server.Shutdown(ctx)
	var forceErr error
	if shutdownErr != nil {
		forceErr = server.Close()
	}
	listenerErr := listener.Close()
	if errors.Is(listenerErr, net.ErrClosed) {
		listenerErr = nil
	}
	return errors.Join(shutdownErr, forceErr, listenerErr)
}

// normalizeMetricsServerError 把标准库优雅关闭哨兵收敛为成功语义。
func normalizeMetricsServerError(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// isObservedCancellationOnly 判断错误链是否只包含本次 run context 的协作式取消。
func isObservedCancellationOnly(err error, cause error) bool {
	if err == nil {
		return true
	}
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range multi.Unwrap() {
			if !isObservedCancellationOnly(child, cause) {
				return false
			}
		}
		return len(multi.Unwrap()) > 0
	}
	if child := errors.Unwrap(err); child != nil {
		return isObservedCancellationOnly(child, cause)
	}
	return errors.Is(err, context.Canceled) || cause != nil && errors.Is(err, cause)
}
