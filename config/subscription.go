package config

import "sync"

// Subscription 表示一次成功注册后的 push 订阅生命周期。
type Subscription interface {
	// Cancel 从注册表移除订阅并停止后续事件投递，允许重复调用；已开始的回调可能继续完成。
	Cancel()
	// Done 在订阅从注册表移除后关闭，不表示已经开始的回调完成。
	Done() <-chan struct{}
}

type defaultSubscription struct {
	manager *defaultManager
	id      uint64
	done    chan struct{}
	once    sync.Once
}

// Cancel 注销本次订阅并关闭 Done。
func (this *defaultSubscription) Cancel() {
	if this == nil {
		return
	}
	this.once.Do(func() {
		if this.manager != nil {
			this.manager.unsubscribe(this.id)
		}
		close(this.done)
	})
}

// Done 返回订阅完成信号。
func (this *defaultSubscription) Done() <-chan struct{} {
	if this == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return this.done
}

// finish 由 Manager Close 结束订阅，不再次回调 Manager。
func (this *defaultSubscription) finish() {
	if this == nil {
		return
	}
	this.once.Do(func() {
		close(this.done)
	})
}
