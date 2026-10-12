package appwebhertz

import (
	"context"

	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// RequestLogObserver 定义 Access Log 适配器消费完成态快照的语义合同。
//
// 典型调用场景：可选观测适配模块把根 LoggerFactory 映射为该接口，并由 app-web-hertz 根据 Config 决定是否调用。
type RequestLogObserver interface {
	// ObserveRequestLog 同步记录一次受控请求事件；实现必须并发安全、快速且不得持有请求池对象。
	//
	// 参数:
	// - observation: 已复制的完成态快照；
	// - slow: Duration 达到非零 slowThreshold 时为 true。
	ObserveRequestLog(ctx context.Context, observation webhertz.HTTPRequestObservation, slow bool)
}
