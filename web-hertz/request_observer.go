package webhertz

import "context"

// RequestObserver 定义请求完成后同步消费不可变快照的合同。
//
// 典型调用场景:
// 1. Access Log 把快照映射为受控结构化日志；
// 2. Metrics Observer 只使用有限基数字段更新本地指标；
// 3. 一个 Middleware 对多个 Observer 执行同步 fan-out。
type RequestObserver interface {
	// Observe 同步消费单次完成态；实现必须快速、并发安全且不得持有请求池对象。
	Observe(ctx context.Context, observation HTTPRequestObservation)
}
