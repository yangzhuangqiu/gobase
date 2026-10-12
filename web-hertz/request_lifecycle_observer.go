package webhertz

import "context"

// RequestLifecycleObserver 定义需要请求开始通知的可选观察扩展合同。
//
// 典型调用场景：Prometheus Observer 在开始时增加 in-flight Gauge，并在 Observe 时减少。
type RequestLifecycleObserver interface {
	RequestObserver
	// RequestStarted 在进入内层请求链前同步通知归一化 Method；实现不得阻塞或启动隐藏任务。
	RequestStarted(ctx context.Context, method string)
}
