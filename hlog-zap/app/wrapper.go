package apphlogzap

import (
	"context"
	"errors"

	"github.com/yangzhuangqiu/gobase/config"
	hlogzap "github.com/yangzhuangqiu/gobase/hlog-zap"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

const (
	// integrationLoggerName 是安装与恢复全局 hlog 事件使用的稳定 Logger 名称。
	integrationLoggerName = "hlog.zap.app"
)

// Wrapper 把 delegate Provider 与 Hertz 全局 hlog 租约组合为单一 Runtime 日志资源。
//
// 语义:
// 1. Provider 成功后、IOC Build 前安装 hlog；
// 2. Runtime 先关闭 IOC/Hertz Server，再由组合 Provider 恢复 hlog；
// 3. 最后关闭 Zap Provider，保证恢复前的框架日志仍有有效出口。
type Wrapper struct {
	// logging 是调用方显式选择的应用日志实现；包装器不创建或配置 delegate。
	logging Logging
}

// Wrap 为既有应用 Logging 增加可恢复的 Hertz hlog 安装能力。
//
// 参数:
// - logging: 必填；其 Build 结果必须通过 ZapProvider() 暴露同生命周期的 Zap 核心。
func Wrap(logging Logging) *Wrapper {
	return &Wrapper{logging: logging}
}

// Build 先委托 Logging 创建 Provider，再原子安装借用型 Hertz hlog 租约。
//
// 返回:
// - Provider: Factory 与 delegate 相同，Close 负责先恢复 hlog 再关闭 delegate；
// - error: 任一步失败都完成已取得资源的逆序回滚。
func (this *Wrapper) Build(ctx context.Context, manager config.Manager) (baselog.Provider, error) {
	if this == nil || isNilLogging(this.logging) {
		return nil, errorForBuildFailed("op=hlog_zap.app.build result=failed stage=precheck reason=logging_required")
	}
	// 第一阶段：delegate 独立完成配置读取和 Provider 构造，包装器只接管成功交付的资源。
	provider, err := this.logging.Build(ctx, manager)
	if err != nil {
		return nil, errorForBuildFailed("op=hlog_zap.app.build result=failed stage=build_logging reason=delegate_failed: %w", err)
	}
	core, ok := resolveZapProvider(provider)
	if !ok {
		if isNilProvider(provider) {
			return nil, errorForBuildFailed("op=hlog_zap.app.build result=failed stage=resolve_provider reason=provider_required")
		}
		closeErr := provider.Close()
		if closeErr != nil {
			return nil, errorForBuildFailed("op=hlog_zap.app.build result=failed stage=resolve_provider reason=zap_provider_capability_required: %w", closeErr)
		}
		return nil, errorForBuildFailed("op=hlog_zap.app.build result=failed stage=resolve_provider reason=zap_provider_capability_required")
	}

	// 第二阶段：hlog 是进程全局状态；只有租约交付成功后，组合 Provider 才能对 Runtime 可见。
	lease, err := hlogzap.InstallFromProvider(core)
	if err != nil {
		closeErr := provider.Close()
		return nil, errorForBuildFailed(
			"op=hlog_zap.app.build result=failed stage=install_hlog reason=install_failed: %w",
			errors.Join(err, closeErr),
		)
	}
	// 第三阶段：安装成功后再发布组合资源，关闭时按 Lease → delegate Provider 逆序释放。
	logger := provider.Factory().Get(integrationLoggerName)
	logger.Info(ctx, "Hertz hlog installed",
		baselog.F("op", "hlog_zap.app.install"), baselog.F("result", "ok"), baselog.F("stage", "complete"),
		baselog.F("adapter", "hlog-zap"), baselog.F("ownership", "borrowed"),
	)
	return newRuntimeProvider(provider, lease, logger), nil
}
