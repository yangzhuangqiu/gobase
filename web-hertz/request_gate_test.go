package webhertz

import (
	"context"
	"sync"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// TestRequestGateRejectsAfterDrain 验证关闭准入后请求不会进入任何内层 Handler。
// 测试方法：先提交 drain，再执行真实 Hertz Handler 链，断言返回 503、关闭连接且业务 Handler 未调用。
func TestRequestGateRejectsAfterDrain(t *testing.T) {
	gate := newRequestGate()
	gate.beginDrain()
	request := app.NewContext(0)
	called := false
	request.SetHandlers([]app.HandlerFunc{
		gate.middleware(),
		func(context.Context, *app.RequestContext) { called = true },
	})
	request.Next(context.Background())

	if called {
		t.Fatal("expected draining gate to reject inner handler")
	}
	if request.Response.StatusCode() != consts.StatusServiceUnavailable || !request.Response.ConnectionClose() {
		t.Fatalf("unexpected drain response: status=%d connection_close=%t", request.Response.StatusCode(), request.Response.ConnectionClose())
	}
}

// TestRequestGateReleasesPanickedHandler 验证 Handler panic 展开仍会归还 gate 计数。
// 测试方法：让内层 Handler panic 并在测试边界 recover，随后开始 drain 并断言立即归零完成。
func TestRequestGateReleasesPanickedHandler(t *testing.T) {
	gate := newRequestGate()
	request := app.NewContext(0)
	request.SetHandlers([]app.HandlerFunc{
		gate.middleware(),
		func(context.Context, *app.RequestContext) { panic("test panic") },
	})
	func() {
		defer func() { _ = recover() }()
		request.Next(context.Background())
	}()
	if gate.inFlight() != 0 {
		t.Fatalf("expected panic path to release request, in_flight=%d", gate.inFlight())
	}
	gate.beginDrain()
	gate.waitDrained()
}

// TestRequestGateLinearizesAdmissionAndDrain 验证并发准入与关闭不存在“已 drained 后又进入”的窗口。
// 测试方法：重复竞争 enter 与 beginDrain；若请求获准则显式 leave，最后断言 drain 完成后永久拒绝新准入。
func TestRequestGateLinearizesAdmissionAndDrain(t *testing.T) {
	const rounds = 1000
	for round := 0; round < rounds; round++ {
		gate := newRequestGate()
		start := make(chan struct{})
		accepted := make(chan bool, 1)
		drainReturned := make(chan struct{})
		go func() {
			<-start
			accepted <- gate.enter()
		}()
		go func() {
			<-start
			gate.beginDrain()
			close(drainReturned)
		}()
		close(start)
		wasAccepted := <-accepted
		<-drainReturned
		if wasAccepted {
			gate.leave()
		}
		gate.waitDrained()
		if gate.enter() {
			t.Fatalf("round %d admitted request after drain", round)
		}
	}
}

// BenchmarkRequestGateEnterLeave 测量 required 合同请求热路径的两次原子状态变更成本。
func BenchmarkRequestGateEnterLeave(b *testing.B) {
	gate := newRequestGate()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			if !gate.enter() {
				b.Fatal("accepting gate rejected request")
			}
			gate.leave()
		}
	})
}

// TestRequestGateWaitsForAllHandlers 验证 drain 只由最后一个已放行 Handler 发布。
// 测试方法：放行多个请求并并发 leave，先确认仍有一个在途，再释放最后请求并等待完成。
func TestRequestGateWaitsForAllHandlers(t *testing.T) {
	gate := newRequestGate()
	const handlers = 8
	for index := 0; index < handlers; index++ {
		if !gate.enter() {
			t.Fatal("accepting gate rejected request")
		}
	}
	gate.beginDrain()
	var group sync.WaitGroup
	group.Add(handlers - 1)
	for index := 0; index < handlers-1; index++ {
		go func() {
			defer group.Done()
			gate.leave()
		}()
	}
	group.Wait()
	select {
	case <-gate.drained:
		t.Fatal("gate drained before final handler returned")
	default:
	}
	gate.leave()
	gate.waitDrained()
}
