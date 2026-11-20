package webhertzobserve

import (
	"context"
	"errors"
	"net"
	"net/http"
)

// metricsServerResource 定义独立 Prometheus HTTP Server 的内部主动资源合同。
// 典型调用场景：Module 在 IOC Init 预绑定资源，Metrics Workload 只负责 Run/Shutdown。
type metricsServerResource interface {
	// run 使用已经预绑定的 listener 提供 Metrics HTTP 服务，并阻塞到 Server 真正停止。
	run(context.Context) error
	// shutdown 在 app graceful 预算内停止 HTTP Server，超时后执行 force close。
	shutdown(context.Context) error
	// endpoint 返回启动日志使用的实际监听地址和 Prometheus 路由。
	endpoint() (address string, path string)
}

// managedMetricsServer 持有独立 Metrics HTTP Server 的 listener 和标准库 Server。
type managedMetricsServer struct {
	// config 是 IOC Init 阶段冻结并完成默认值补齐的启动配置。
	config MetricsServerConfig
	// listener 在任何 Workload 启动前预绑定，确保端口冲突阻止整个 Runtime 启动。
	listener net.Listener
	// server 只挂载 Prometheus Handler，不暴露业务、health 或 pprof 路由。
	server *http.Server
}

var _ metricsServerResource = (*managedMetricsServer)(nil)

// newManagedMetricsServer 校验 Handler、预绑定端口并构造尚未 Serve 的 Metrics Server Resource。
// 阶段角色：IOC Init 资源获取阶段；失败会阻止业务和 Metrics Workload 启动。
func newManagedMetricsServer(config MetricsServerConfig, handler PrometheusHandler) (*managedMetricsServer, error) {
	if isNilValue(handler) {
		return nil, errorForDeclarationInvalid(
			"op=app_observe_webhertz.metrics_server result=failed stage=prepare reason=handler_required",
		)
	}
	listener, err := net.Listen("tcp", config.Address)
	if err != nil {
		return nil, errorForMetricsServerListenFailed(
			messagef("op=app_observe_webhertz.metrics_server result=failed stage=listen reason=listen_failed address=%s", config.Address),
			err,
		)
	}

	// listener 成功后才构造并发布完整 Resource；后续构造步骤不再包含可失败动作。
	mux := http.NewServeMux()
	mux.Handle(config.Path, handler)
	return &managedMetricsServer{
		config:   config,
		listener: listener,
		server: &http.Server{
			Addr:              config.Address,
			Handler:           mux,
			ReadHeaderTimeout: config.ReadHeaderTimeout,
		},
	}, nil
}

// run 使用预绑定 listener 提供 Metrics HTTP 服务，并归一化标准优雅关闭哨兵。
func (this *managedMetricsServer) run(ctx context.Context) error {
	// 第一阶段：在进入阻塞 Serve 前拒绝无效运行上下文和未初始化 Resource。
	if ctx == nil {
		return errorForMetricsServerRunFailed(
			"op=app_observe_webhertz.metrics_server result=failed stage=run reason=context_required",
		)
	}
	if err := ctx.Err(); err != nil {
		return errorForMetricsServerRunFailed(
			"op=app_observe_webhertz.metrics_server result=failed stage=run reason=context_done",
			err,
		)
	}
	if this == nil || this.server == nil || this.listener == nil {
		return errorForMetricsServerRunFailed(
			"op=app_observe_webhertz.metrics_server result=failed stage=run reason=resource_not_initialized",
		)
	}
	// 第二阶段：Serve 只因 Shutdown/Close 或真实网络错误返回；标准关闭哨兵归一化为成功。
	err := this.server.Serve(this.listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	if err != nil {
		return errorForMetricsServerRunFailed(
			"op=app_observe_webhertz.metrics_server result=failed stage=run reason=serve_failed",
			err,
		)
	}
	return nil
}

// shutdown 执行 graceful→force，并保证预绑定 listener 最终关闭。
func (this *managedMetricsServer) shutdown(ctx context.Context) error {
	if ctx == nil {
		return errorForMetricsServerRunFailed(
			"op=app_observe_webhertz.metrics_server result=failed stage=shutdown reason=context_required",
		)
	}
	if this == nil || this.server == nil || this.listener == nil {
		return nil
	}

	// graceful 失败通常表示预算到期；此时必须 force close，确保 Workload.Run 最终返回。
	shutdownErr := this.server.Shutdown(ctx)
	var forceErr error
	if shutdownErr != nil {
		forceErr = this.server.Close()
	}
	listenerErr := this.listener.Close()
	if errors.Is(listenerErr, net.ErrClosed) {
		listenerErr = nil
	}
	return errors.Join(shutdownErr, forceErr, listenerErr)
}

// endpoint 返回预绑定后的实际地址和冻结路由，供 Workload 输出稳定启动日志。
func (this *managedMetricsServer) endpoint() (string, string) {
	if this == nil || this.listener == nil {
		return "", ""
	}
	return this.listener.Addr().String(), this.config.Path
}
