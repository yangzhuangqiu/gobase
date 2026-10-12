package global

import (
	"context"
	"errors"
	stdlog "log"
	"sync"

	applogzap "github.com/yangzhuangqiu/gobase/app-log-zap"
	"github.com/yangzhuangqiu/gobase/config"
	baselog "github.com/yangzhuangqiu/gobase/log"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
)

var runtimeInstance = newGlobalRuntime()

// globalRuntime 承载全局配置、日志核心与关闭状态。
//
// 语义：
// 1. ConfigManager 由 global 创建并最后关闭；
// 2. app-log-zap 组合 Provider 自己拥有动态订阅并先于 Manager 关闭；
// 3. 该状态只服务 global 子包，不作为业务依赖注入入口。
type globalRuntime struct {
	// manager 是全局入口创建并独占的配置管理器。
	manager config.Manager
	// provider 同时拥有日志核心与 Manager subscription。
	provider baselog.Provider
	// factory 是 provider 构造期返回的稳定 LoggerFactory。
	factory baselog.LoggerFactory
	// logger 是全局 root logger 快捷入口。
	logger baselog.Logger
	// closeOnce 保证全局资源只执行一次关闭事务。
	closeOnce sync.Once
	// closeErr 保存首次关闭 Provider 与 Manager 的聚合结果。
	closeErr error
}

// newGlobalRuntime 创建尚未取得任何运行资源的全局状态。
// 阶段角色：包初始化和测试重置共用的状态构造入口。
func newGlobalRuntime() *globalRuntime {
	return &globalRuntime{}
}

// assignManager 记录由 global 独占的 ConfigManager。
// 阶段角色：配置资源获取成功后的所有权转移点。
func (this *globalRuntime) assignManager(manager config.Manager) {
	this.manager = manager
}

// assignProvider 安装组合 Provider，并派生稳定 factory/root logger。
// 阶段角色：全局日志出口的原子初始化阶段。
func (this *globalRuntime) assignProvider(provider baselog.Provider) {
	this.provider = provider
	this.factory = provider.Factory()
	this.logger = this.factory.Get("root")
}

// clearProvider 清理已关闭的 Provider 及其派生引用。
// 阶段角色：shutdown 完成后的不可用状态发布。
func (this *globalRuntime) clearProvider() {
	this.provider = nil
	this.factory = nil
	this.logger = nil
}

// resetAfterClose 清理全部已关闭资源引用。
// 阶段角色：全局关闭事务的最终状态收口。
func (this *globalRuntime) resetAfterClose() {
	this.clearProvider()
	this.assignManager(nil)
}

// init 在包加载阶段构建一次全局日志运行时。
// 阶段角色：提供 app-log-zap/global 的开箱可用入口。
func init() {
	runtimeInstance.bootstrap()
}

// bootstrap 按 ConfigManager→组合 Provider 顺序构建全局运行时，失败时降级为纯 Zap Provider。
// 阶段角色：包初始化期间的资源获取、回滚与降级事务。
func (this *globalRuntime) bootstrap() {
	// 阶段一：按 config 默认文件策略创建全局 Manager。
	manager, err := this.buildManagerForGlobal()
	if err != nil {
		this.degradeToFallback(err)
		return
	}
	// 阶段二：Manager 成功后创建拥有 subscription 的组合 Provider。
	if err = this.activateWithManager(manager); err != nil {
		_ = manager.Close()
		this.assignManager(nil)
		this.degradeToFallback(err)
		return
	}
}

// buildManagerForGlobal 构建全局配置管理器并开启本地文件监听。
// 阶段角色：global bootstrap 的第一资源获取阶段。
func (this *globalRuntime) buildManagerForGlobal() (config.Manager, error) {
	manager, err := config.New(config.File(config.FileWatch())).Build(context.Background())
	if err != nil {
		return nil, errorForGlobalManagerBuildFailed("op=app_log_zap.global.init result=failed stage=build_manager reason=%w", err)
	}
	return manager, nil
}

// buildProviderFromManager 通过 app-log-zap 适配入口构建组合 Provider。
// 阶段角色：global bootstrap 的第二资源获取阶段。
func (this *globalRuntime) buildProviderFromManager(manager config.Manager) (baselog.Provider, error) {
	provider, err := applogzap.NewProviderFromManager(context.Background(), manager, applogzap.DefaultManagerScope)
	if err != nil {
		return nil, errorForGlobalProviderBuildFailed("op=app_log_zap.global.provider result=failed stage=build_from_manager scope=%s reason=%w", applogzap.DefaultManagerScope, err)
	}
	return provider, nil
}

// activateWithManager 接管 Manager 并安装基于其当前 Snapshot 的组合 Provider。
// 阶段角色：测试与 bootstrap 共用的资源所有权切换路径。
func (this *globalRuntime) activateWithManager(manager config.Manager) error {
	this.assignManager(manager)
	provider, err := this.buildProviderFromManager(manager)
	if err != nil {
		return err
	}
	this.assignProvider(provider)
	return nil
}

// degradeToFallback 在 config 路径失败时安装不持有 Manager 的默认 Zap Provider。
// 阶段角色：global bootstrap 的可观测降级路径。
func (this *globalRuntime) degradeToFallback(err error) {
	stdlog.Printf("op=app_log_zap.global.init result=degraded reason=%v", err)
	provider, fallbackErr := this.buildFallbackProvider()
	if fallbackErr != nil {
		stdlog.Printf("op=app_log_zap.global.init result=failed reason=%v", fallbackErr)
		return
	}
	this.assignManager(nil)
	this.assignProvider(provider)
}

// buildFallbackProvider 构建使用推荐默认配置的纯 Zap Provider。
// 阶段角色：ConfigManager 不可用时的最终日志出口准备。
func (this *globalRuntime) buildFallbackProvider() (baselog.Provider, error) {
	provider, err := logzap.NewProvider(logzap.DefaultConfig())
	if err != nil {
		return nil, errorForGlobalFallbackBuildFailed("op=app_log_zap.global.provider result=failed stage=build_fallback reason=%w", err)
	}
	return provider, nil
}

// shutdown 按组合 Provider→ConfigManager 顺序关闭资源，并保留首次聚合结果。
// 阶段角色：显式 Close 的幂等资源回收事务。
func (this *globalRuntime) shutdown() error {
	this.closeOnce.Do(func() {
		// 阶段一：先关闭组合 Provider，使 subscription 在 Manager 存活期间完成取消。
		var providerErr error
		if this.provider != nil {
			providerErr = this.provider.Close()
		}
		// 阶段二：日志核心与订阅结束后关闭其配置来源。
		var managerErr error
		if this.manager != nil {
			managerErr = this.manager.Close()
		}
		joinedErr := errors.Join(providerErr, managerErr)
		if joinedErr != nil {
			this.closeErr = errorForGlobalCloseFailed("op=app_log_zap.global.close result=failed stage=close_resources reason=%w", joinedErr)
		}
		this.resetAfterClose()
	})
	return this.closeErr
}
