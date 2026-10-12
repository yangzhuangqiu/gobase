package ioc

import (
	"context"

	baselog "github.com/yangzhuangqiu/gobase/log"
)

// warnFailure 记录调用方可修复、可重试或状态不满足导致的失败。
// 阶段角色：统一 IOC 公共操作的 Warn 结构化字段，保留原始错误链。
func (this *container) warnFailure(ctx context.Context, op string, err error) {
	this.logger.Warn(ctx, "ioc operation rejected",
		baselog.Op(op),
		baselog.Result(baselog.ResultFailed),
		baselog.Reason("operation_rejected"),
		baselog.Err(err),
	)
}

// errorFailure 记录 Build/Close 等已进入执行阶段后的失败。
// 阶段角色：统一 IOC 核心生命周期失败的 Error 结构化字段。
func (this *container) errorFailure(ctx context.Context, op string, err error) {
	this.logger.Error(ctx, "ioc operation failed",
		baselog.Op(op),
		baselog.Result(baselog.ResultFailed),
		baselog.Reason("operation_failed"),
		baselog.Err(err),
	)
}
