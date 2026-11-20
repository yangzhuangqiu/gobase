package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// httpServerWorkload 把真实 net/http Server 适配为声明式主动工作负载。
type httpServerWorkload struct {
	// server 是 Run 与 Shutdown 共享的同一服务实例。
	server *http.Server
	// listener 是测试预先创建、由 Run 阻塞服务的监听器。
	listener net.Listener
	// shutdownCalled 记录 app 是否执行语义化停止动作。
	shutdownCalled *atomic.Bool
}

// Name 返回真实 HTTP 测试工作负载的稳定名称。
func (this *httpServerWorkload) Name() string {
	return "http"
}

// Run 阻塞执行 Serve，并把正常 Server 关闭归一为成功。
func (this *httpServerWorkload) Run(context.Context, Runtime, CommandContext) error {
	err := this.server.Serve(this.listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown 请求 HTTP Server 排空在途请求；app 会继续等待 Run 返回。
func (this *httpServerWorkload) Shutdown(ctx context.Context, _ Runtime, _ CommandContext) error {
	this.shutdownCalled.Store(true)
	shutdownErr := this.server.Shutdown(ctx)
	if shutdownErr == nil {
		return nil
	}
	// graceful 到期后强制关闭，真实 Run 收敛点仍由 app 监督器等待。
	return errors.Join(shutdownErr, this.server.Close())
}

// TestHTTPServerGracefulShutdownIntegration 验证原生 net/http Server 可通过 Workload 接入 app 优雅关闭。
//
// 测试方法：启动真实 HTTP listener 和阻塞中的请求，取消宿主 context 后断言 app 调用 Shutdown、等待在途请求和 Run 收敛，再返回宿主原因。
func TestHTTPServerGracefulShutdownIntegration(t *testing.T) {
	// 第一阶段：建立真实 listener 和一个由测试控制完成时间的在途请求。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test server: %v", err)
	}
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var shutdownCalled atomic.Bool
	server := &http.Server{
		Handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			close(requestStarted)
			<-releaseRequest
			_, _ = fmt.Fprintln(response, "ok")
		}),
	}
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})

	hostCtx, cancelHost := context.WithCancelCause(context.Background())
	hostCause := errors.New("host stopping")
	// 第二阶段：Workload Run 阻塞 Serve，Shutdown 执行 graceful→必要时 force，app 负责最终 wait。
	application := New(
		WithModules(newTestWorkloadModule("http-server", &httpServerWorkload{server: server, listener: listener, shutdownCalled: &shutdownCalled})),
		WithShutdownPolicy(ShutdownPolicy{WorkloadGracefulTimeout: time.Second}),
	)
	result := make(chan error, 1)
	go func() { result <- application.ExecuteContext(hostCtx, nil) }()

	// 第三阶段：确认请求已进入 Handler 后触发宿主取消，验证 app 不会提前关闭 Runtime/返回。
	requestResult := make(chan error, 1)
	go func() {
		response, requestErr := http.Get("http://" + listener.Addr().String())
		if requestErr == nil {
			requestErr = response.Body.Close()
		}
		requestResult <- requestErr
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("request did not reach test server")
	}
	cancelHost(hostCause)

	select {
	case err := <-result:
		t.Fatalf("app returned before active request completed: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	// 第四阶段：放行在途请求，并确认 HTTP Run、Shutdown 与 app 依次收敛。
	close(releaseRequest)
	if err := <-requestResult; err != nil {
		t.Fatalf("active request should complete during graceful shutdown: %v", err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, hostCause) {
			t.Fatalf("expected host shutdown cause, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("http server and app did not converge")
	}
	if !shutdownCalled.Load() {
		t.Fatal("http workload shutdown was not called")
	}
}
