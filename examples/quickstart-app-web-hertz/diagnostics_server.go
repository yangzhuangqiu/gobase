package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	webhertzobserve "github.com/yangzhuangqiu/gobase/app-observe-webhertz"
)

// diagnosticsServerState 表示诊断 Server 的单运行周期状态。
type diagnosticsServerState uint8

const (
	// diagnosticsServerStateNew 表示尚未获取 listener。
	diagnosticsServerStateNew diagnosticsServerState = iota
	// diagnosticsServerStateRunning 表示 listener 已取得且 Serve 正在运行。
	diagnosticsServerStateRunning
	// diagnosticsServerStateStopped 表示本实例运行周期已终结。
	diagnosticsServerStateStopped
)

// diagnosticsServer 拥有独立 net/http Server、listener 和 mux，不使用 DefaultServeMux。
type diagnosticsServer struct {
	// server 是仅服务 Metrics/pprof 的独立 HTTP Server。
	server *http.Server
	// address 是 listener 获取前的配置地址，支持测试使用端口 0。
	address string
	// mu 保护状态、实际地址和运行结果。
	mu sync.Mutex
	// state 是当前单运行周期状态。
	state diagnosticsServerState
	// actualAddress 是 listener 成功后可访问的实际地址。
	actualAddress string
	// done 在 Serve 返回后关闭；Shutdown-before-Run 也会关闭它。
	done chan struct{}
	// runErr 保存 Serve 的最终非 ServerClosed 结果。
	runErr error
}

// newDiagnosticsServer 构造尚未监听的独立诊断 Server；禁用配置返回 nil。
func newDiagnosticsServer(config diagnosticsConfig, metrics webhertzobserve.PrometheusHandler) (*diagnosticsServer, error) {
	// 第一阶段：先解析纯值配置；禁用时不创建 mux、Server 或任何后台活动。
	resolved, err := resolveDiagnosticsConfig(config)
	if err != nil {
		return nil, err
	}
	if !resolved.Enabled {
		return nil, nil
	}
	// 第二阶段：路由完整构造成功后才发布未监听的 Server 对象。
	mux, err := newDiagnosticsMux(resolved, metrics)
	if err != nil {
		return nil, err
	}
	return &diagnosticsServer{
		server: &http.Server{
			Addr:              resolved.Address,
			Handler:           mux,
			ReadHeaderTimeout: resolved.ReadHeaderTimeout,
		},
		address: resolved.Address,
		state:   diagnosticsServerStateNew,
		done:    make(chan struct{}),
	}, nil
}

// newDiagnosticsMux 显式注册 Metrics/pprof 路由，不读取或修改 http.DefaultServeMux。
func newDiagnosticsMux(config diagnosticsConfig, metrics webhertzobserve.PrometheusHandler) (*http.ServeMux, error) {
	mux := http.NewServeMux()
	if config.MetricsEnabled {
		if metrics == nil {
			return nil, fmt.Errorf("op=quickstart.diagnostics result=failed stage=build_routes reason=prometheus_handler_required")
		}
		mux.Handle(config.MetricsPath, metrics)
	}
	if config.PprofEnabled {
		registerPprofRoutes(mux)
	}
	return mux, nil
}

// Run 获取 listener 并阻塞到诊断 Server 停止；context 取消触发后台 graceful shutdown。
func (this *diagnosticsServer) Run(ctx context.Context) error {
	if this == nil || ctx == nil {
		return fmt.Errorf("op=quickstart.diagnostics result=failed stage=run_precheck reason=server_and_context_required")
	}
	this.mu.Lock()
	if this.state != diagnosticsServerStateNew {
		this.mu.Unlock()
		return fmt.Errorf("op=quickstart.diagnostics result=failed stage=run_precheck reason=run_not_allowed")
	}
	if ctx.Err() != nil {
		this.state = diagnosticsServerStateStopped
		close(this.done)
		this.mu.Unlock()
		return nil
	}
	// 第一阶段：在锁内获取 listener，使并发 Shutdown 不能穿过 starting 窗口。
	listener, err := net.Listen("tcp", this.address)
	if err != nil {
		this.state = diagnosticsServerStateStopped
		this.runErr = fmt.Errorf("op=quickstart.diagnostics result=failed stage=listen reason=listen_failed address=%s: %w", this.address, err)
		close(this.done)
		result := this.runErr
		this.mu.Unlock()
		return result
	}
	this.state = diagnosticsServerStateRunning
	this.actualAddress = listener.Addr().String()
	this.mu.Unlock()

	// 第二阶段：context 取消只发起 graceful；应用 Hook 仍以自己的 deadline 监督并在超时后 Force Close。
	go this.shutdownOnContext(ctx)
	err = this.server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	// 第三阶段：先发布稳定 Run 结果再关闭 done，解除全部 Shutdown 等待者。
	this.mu.Lock()
	this.state = diagnosticsServerStateStopped
	if err != nil {
		this.runErr = fmt.Errorf("op=quickstart.diagnostics result=failed stage=serve reason=serve_failed: %w", err)
	}
	close(this.done)
	result := this.runErr
	this.mu.Unlock()
	return result
}

// shutdownOnContext 把 Runner context 取消转换为 graceful 停止请求，不接管应用 deadline。
func (this *diagnosticsServer) shutdownOnContext(ctx context.Context) {
	select {
	case <-ctx.Done():
		_ = this.server.Shutdown(context.Background())
	case <-this.done:
	}
}

// Shutdown 优雅停止诊断 Server；deadline 到期后 Force Close，并等待 Serve 返回。
func (this *diagnosticsServer) Shutdown(ctx context.Context) error {
	if this == nil {
		return nil
	}
	if ctx == nil {
		return fmt.Errorf("op=quickstart.diagnostics result=failed stage=shutdown_precheck reason=context_required")
	}
	this.mu.Lock()
	if this.state == diagnosticsServerStateNew {
		this.state = diagnosticsServerStateStopped
		close(this.done)
		this.mu.Unlock()
		return nil
	}
	done := this.done
	state := this.state
	this.mu.Unlock()
	if state == diagnosticsServerStateStopped {
		return nil
	}

	// graceful 失败时必须 Force Close，保证 Hook 返回后不再访问 Runtime。
	shutdownErr := this.server.Shutdown(ctx)
	var closeErr error
	if shutdownErr != nil {
		closeErr = this.server.Close()
	}
	<-done
	if shutdownErr != nil || (closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed)) {
		return fmt.Errorf("op=quickstart.diagnostics result=failed stage=shutdown reason=graceful_or_force_failed: %w", errors.Join(shutdownErr, closeErr))
	}
	return nil
}

// Address 返回 listener 成功后的实际地址；尚未运行时为空。
func (this *diagnosticsServer) Address() string {
	if this == nil {
		return ""
	}
	this.mu.Lock()
	defer this.mu.Unlock()
	return this.actualAddress
}
