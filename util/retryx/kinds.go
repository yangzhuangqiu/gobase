package retryx

import "github.com/yangzhuangqiu/gobase/util/errorsx"

const (
	// KindInvalidOperation 表示 operation 为空，调用方必须提供实际执行函数。
	KindInvalidOperation errorsx.Kind = "retryx.invalid_operation"
	// KindInvalidOptions 表示重试配置不满足次数、延迟、倍率或抖动约束。
	KindInvalidOptions errorsx.Kind = "retryx.invalid_options"
	// KindContextDone 表示上下文取消或超时，调用方应停止当前业务链路。
	KindContextDone errorsx.Kind = "retryx.context_done"
	// KindNonRetryable 表示错误策略明确拒绝继续重试，调用方应处理原始 cause。
	KindNonRetryable errorsx.Kind = "retryx.non_retryable"
	// KindExhausted 表示尝试次数已经耗尽，调用方可降级、告警或返回最后 cause。
	KindExhausted errorsx.Kind = "retryx.exhausted"
)
