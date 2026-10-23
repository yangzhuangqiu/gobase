package apphlogzap

import (
	"context"
	"errors"
	"sync"

	hlogzap "github.com/yangzhuangqiu/gobase/hlog-zap"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

// runtimeProvider 组合 delegate 日志资源与进程级 hlog 恢复权。
type runtimeProvider struct {
	// provider 是 delegate 成功交付的日志资源，包装器接管其关闭责任。
	provider baselog.Provider
	// lease 唯一拥有本次 hlog 安装的恢复权。
	lease hlogzap.Lease
	// logger 在 Provider 关闭前记录恢复结果。
	logger baselog.Logger
	// closeOnce 保证恢复和底层关闭只执行一次。
	closeOnce sync.Once
	// closeErr 保存首次完整收口结果。
	closeErr error
}

var _ baselog.Provider = (*runtimeProvider)(nil)

// newRuntimeProvider 创建只允许逆序释放的组合日志资源。
// 阶段角色：仅在 Provider 与 hlog Lease 均构造成功后调用。
func newRuntimeProvider(provider baselog.Provider, lease hlogzap.Lease, logger baselog.Logger) baselog.Provider {
	return &runtimeProvider{provider: provider, lease: lease, logger: logger}
}

// Factory 返回 delegate 构造期创建的稳定 LoggerFactory。
func (this *runtimeProvider) Factory() baselog.LoggerFactory {
	return this.provider.Factory()
}

// Close 先恢复 Hertz 全局 hlog，再关闭 delegate Provider；并发和重复调用共享首次结果。
func (this *runtimeProvider) Close() error {
	if this == nil {
		return nil
	}
	this.closeOnce.Do(func() {
		// Runtime 已先关闭 IOC Server，此处恢复后不应再有 Hertz goroutine 写入旧 Provider。
		restoreErr := this.lease.Close()
		if restoreErr != nil {
			this.logger.Error(context.Background(), "Hertz hlog restore failed",
				baselog.F("op", "hlog_zap.app.restore"), baselog.F("result", "failed"), baselog.F("stage", "complete"),
				baselog.F("reason", "lease_close_failed"), baselog.Err(restoreErr),
			)
		} else {
			this.logger.Info(context.Background(), "Hertz hlog restored",
				baselog.F("op", "hlog_zap.app.restore"), baselog.F("result", "ok"), baselog.F("stage", "complete"),
			)
		}
		providerErr := this.provider.Close()
		if restoreErr != nil || providerErr != nil {
			this.closeErr = errorForCloseFailed(
				"op=hlog_zap.app.close result=failed stage=release reason=resource_close_failed: %w",
				errors.Join(restoreErr, providerErr),
			)
		}
	})
	return this.closeErr
}
