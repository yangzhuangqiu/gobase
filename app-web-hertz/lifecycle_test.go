package appwebhertz

import (
	"context"
	"io"
	"net"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/network/standard"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	baseweb "github.com/yangzhuangqiu/gobase/web"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

const (
	// lifecycleTestTimeout 是本地监听和并发结果等待的最大测试预算。
	lifecycleTestTimeout = 3 * time.Second
)

// TestFilterOrderExecutesThroughNativeHertzChain 验证排序后的 Filter 确实按 Hertz Middleware 语义执行。
// 测试方法：启动真实标准 transport，访问测试路由，断言低 Order、同 Order 输入顺序和 Handler 的完整执行序列。
func TestFilterOrderExecutesThroughNativeHertzChain(t *testing.T) {
	// 第一阶段：构造可观察 Filter 顺序和最终 Handler。
	var mu sync.Mutex
	order := make([]string, 0, 4)
	appendStage := func(stage string) {
		mu.Lock()
		order = append(order, stage)
		mu.Unlock()
	}
	newFilter := func(priority int, stage string) Filter {
		return &filterStub{
			order: priority,
			handler: func(ctx context.Context, request *hertzapp.RequestContext) {
				appendStage(stage)
				request.Next(ctx)
			},
		}
	}
	controller := controllerFunc(func(router *route.RouterGroup) error {
		router.GET("/order", func(_ context.Context, request *hertzapp.RequestContext) {
			appendStage("handler")
			request.String(consts.StatusOK, "ok")
		})
		return nil
	})
	// 第二阶段：启动真实 Server 并完成一次请求和稳定停止。
	webServer, address := newStandardTestHost(
		t,
		[]Controller{controller},
		[]Filter{newFilter(20, "second"), newFilter(10, "first"), newFilter(20, "third")},
	)
	runResult := make(chan error, 1)
	go func() { runResult <- webServer.Run(context.Background()) }()
	requestUntilReady(t, "http://"+address+"/order")
	if err := webServer.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown server: %v", err)
	}
	if err := waitForLifecycleError(t, runResult); err != nil {
		t.Fatalf("run result: %v", err)
	}
	// 第三阶段：断言排序后的原生 Middleware 链没有改变执行语义。
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(order, []string{"first", "second", "third", "handler"}) {
		t.Fatalf("unexpected native middleware order: %v", order)
	}
}

// newStandardTestHost 构造使用预绑定 listener 和标准 transport 的应用 Server。
// 阶段角色：调用方负责执行 Run/Shutdown；测试清理仅兜底关闭 listener。
func newStandardTestHost(
	t *testing.T,
	controllers []Controller,
	filters []Filter,
	options ...Option,
) (baseweb.Server, string) {
	t.Helper()
	// 第一阶段：预绑定地址，消除端口选择和 Hertz 内部 listener 创建的不确定性。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test address: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	// 第二阶段：显式选择标准 transport，并把其他测试 Option 保持在其后。
	nativeOption := WithServerOptions(webhertz.WithHertzOptions(
		server.WithListener(listener),
		server.WithTransport(standard.NewTransporter),
	))
	allOptions := append([]Option{nativeOption}, options...)
	// 第三阶段：构造未运行 Server，运行和停止仍由具体测试控制。
	webServer, err := New(
		webhertz.Config{ExitWaitTime: time.Second},
		controllers,
		filters,
		allOptions...,
	)
	if err != nil {
		t.Fatalf("new standard test server: %v", err)
	}
	return webServer, listener.Addr().String()
}

// requestUntilReady 重试本地 HTTP 请求直到 Server 可用并消费响应体。
// 阶段角色：消除 Run goroutine 与首次请求之间的监听建立竞态。
func requestUntilReady(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(lifecycleTestTimeout)
	// Server Run 与 listener 可用之间存在异步窗口，只在固定预算内重试连接错误。
	for {
		response, err := http.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			err = response.Body.Close()
			if err == nil {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("request did not become ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForLifecycleError 等待并发运行或停止结果，超时则终止测试。
func waitForLifecycleError(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(lifecycleTestTimeout):
		t.Fatal("timeout waiting for lifecycle result")
		return nil
	}
}
