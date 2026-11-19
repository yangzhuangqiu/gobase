package webhertz

import (
	"context"
	"sync/atomic"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

const (
	// requestGateDrainingMask 占用状态最高位，保证准入关闭与 in-flight 增长共享同一 CAS 线性化点。
	requestGateDrainingMask uint64 = 1 << 63
	// requestGateCountMask 提取低 63 位请求数；该容量远高于单进程可同时承载的 Handler 数量。
	requestGateCountMask = requestGateDrainingMask - 1
)

// requestGate 是 required 合同独占的一次性 Handler 准入与完成屏障。
//
// 语义：状态只从 accepting 单向进入 draining；成功准入的 Handler 必须且只能 leave 一次。
type requestGate struct {
	// state 的最高位表示 draining，低 63 位表示已经放行且尚未返回的 Handler 数量。
	state atomic.Uint64
	// drained 在 BeginDrain 后且 in-flight 首次归零时关闭，广播稳定完成结果。
	drained chan struct{}
}

// newRequestGate 构造 accepting 状态的一次性请求 gate。
// 阶段角色：仅在 required 合同的 Server 构造阶段调用。
func newRequestGate() *requestGate {
	return &requestGate{drained: make(chan struct{})}
}

// middleware 把 gate 安装为绝对最外层请求边界，覆盖用户 Middleware、fallback 与业务 Handler。
// 阶段角色：draining 后的漏流量返回 503 并关闭连接，不再进入任何内层执行路径。
func (this *requestGate) middleware() app.HandlerFunc {
	return func(ctx context.Context, request *app.RequestContext) {
		if !this.enter() {
			request.SetConnectionClose()
			request.AbortWithStatus(consts.StatusServiceUnavailable)
			return
		}
		defer this.leave()
		request.Next(ctx)
	}
}

// enter 在 accepting 状态原子增加 in-flight；draining 已提交时拒绝准入。
// 阶段角色：CAS 同时校验状态和增加计数，消除“观察零请求后又放行请求”的竞态窗口。
func (this *requestGate) enter() bool {
	for {
		state := this.state.Load()
		if state&requestGateDrainingMask != 0 {
			return false
		}
		if this.state.CompareAndSwap(state, state+1) {
			return true
		}
	}
}

// leave 原子结束一个已放行 Handler，并由最后一个请求发布 drained。
// 前置条件：仅由 enter 成功后的 defer 调用一次。
func (this *requestGate) leave() {
	state := this.state.Add(^uint64(0))
	if state == requestGateDrainingMask {
		close(this.drained)
	}
}

// beginDrain 原子关闭准入；无在途 Handler 时立即发布 drained。
// 阶段角色：必须先于 Hertz Shutdown，使 transport 停止与请求完成共享确定边界。
func (this *requestGate) beginDrain() {
	for {
		state := this.state.Load()
		if state&requestGateDrainingMask != 0 {
			return
		}
		drainingState := state | requestGateDrainingMask
		if this.state.CompareAndSwap(state, drainingState) {
			if state&requestGateCountMask == 0 {
				close(this.drained)
			}
			return
		}
	}
}

// waitDrained 等待 gate 关闭后全部已放行 Handler 返回。
// 阶段角色：required 合同发布 Server 停止结果前的最终资源安全屏障。
func (this *requestGate) waitDrained() {
	<-this.drained
}

// inFlight 返回当前已经放行且尚未返回的 Handler 数量。
// 阶段角色：仅用于低频生命周期诊断和测试，不参与关闭正确性判断。
func (this *requestGate) inFlight() uint64 {
	return this.state.Load() & requestGateCountMask
}
