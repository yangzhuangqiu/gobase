package applogzap

import (
	"context"
	"strings"
	"sync"

	"github.com/yangzhuangqiu/gobase/config"
	baselog "github.com/yangzhuangqiu/gobase/log"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
)

// runtimeProvider 组合 Zap 核心与 ConfigManager 订阅，并作为单个日志资源交给 app-runtime。
type runtimeProvider struct {
	// core 是不感知配置来源的 Zap 日志核心。
	core *logzap.Provider
	// subscription 是 app-log-zap 独占的日志 scope 配置订阅。
	subscription config.Subscription
	// watchCancel 只终止等待 lifecycle context 的辅助 goroutine。
	watchCancel context.CancelFunc
	// closeOnce 保证订阅与核心资源只回收一次。
	closeOnce sync.Once
	// closeErr 保存首次关闭 Zap 核心的结果。
	closeErr error
}

// runtimeConfigBridge 串行衔接 ConfigManager 初次回放、Provider 创建与后续动态更新。
type runtimeConfigBridge struct {
	// mu 保证 Provider 创建期间不会丢失或穿插配置通知。
	mu sync.Mutex
	// latest 是 Provider 创建前最近一次回放的完整启动配置。
	latest logzap.Config
	// core 在完整启动配置固化后赋值，后续事件只投递运行期窄配置。
	core *logzap.Provider
}

var _ baselog.Provider = (*runtimeProvider)(nil)

// NewProviderFromManager 基于 ConfigManager 当前版本创建 Zap 核心，并由组合 Provider 接管动态订阅。
//
// 典型调用场景：不使用 app-runtime、但仍希望复用 config.Manager 生命周期和日志动态 level 的宿主装配层。
//
// 参数：
// - ctx：订阅生命周期 context；取消后停止配置推送，但不会提前关闭日志核心；
// - manager：已完成首次 Snapshot 发布的配置管理器，必填；
// - scope：日志配置 dotted path；空值回退 DefaultManagerScope；
// - options：仅用于 Zap 核心构造的 Provider Option。
//
// 返回：
// - Provider：同时拥有 ConfigManager subscription 与 Zap 核心，调用方必须 Close；
// - error：订阅、初始配置解码或核心创建失败时返回，并回滚已取得资源。
func NewProviderFromManager(ctx context.Context, manager config.Manager, scope string, options ...logzap.Option) (baselog.Provider, error) {
	return newRuntimeProvider(ctx, manager, scope, options)
}

// newRuntimeProvider 从 ConfigManager 当前版本原子衔接 Zap 核心，并接管订阅生命周期。
// 阶段角色：app-log-zap 的配置来源适配与资源所有权边界。
func newRuntimeProvider(ctx context.Context, manager config.Manager, scope string, options []logzap.Option) (baselog.Provider, error) {
	resolvedScope := strings.TrimSpace(scope)
	if resolvedScope == "" {
		resolvedScope = defaultLogScope
	}
	bridge := &runtimeConfigBridge{}

	// 阶段一：先订阅并同步接收当前版本，避免读取 Snapshot 与注册监听之间丢失更新。
	subscription, err := config.Subscribe[logzap.Config](manager, resolvedScope, bridge.onUpdate)
	if err != nil {
		return nil, errorForProviderSubscribeFailed("op=app_log_zap.provider result=failed stage=subscribe scope=%s reason=%w", resolvedScope, err)
	}

	// 阶段二：在通知串行锁内基于最新完整配置创建纯 Zap 核心；失败时补偿订阅。
	core, err := bridge.buildCore(options)
	if err != nil {
		subscription.Cancel()
		return nil, errorForProviderCoreBuildFailed("op=app_log_zap.provider result=failed stage=new_core scope=%s reason=%w", resolvedScope, err)
	}

	// 阶段三：lifecycle context 只停止后台订阅；核心输出资源仍由 Runtime.Close 确定性释放。
	if ctx == nil {
		ctx = context.Background()
	}
	watchCtx, watchCancel := context.WithCancel(ctx)
	result := &runtimeProvider{core: core, subscription: subscription, watchCancel: watchCancel}
	go cancelSubscriptionOnContext(watchCtx, subscription)
	return result, nil
}

// onUpdate 保存初次完整配置；Provider 建成后只转发明确允许热更新的字段。
// 阶段角色：ConfigManager typed 事件到 log-zap RuntimeConfig 的唯一转换边界。
func (this *runtimeConfigBridge) onUpdate(update config.TypedUpdate[logzap.Config]) error {
	// 阶段一：串行保存完整配置，Provider 创建前只承担初次回放缓存职责。
	this.mu.Lock()
	defer this.mu.Unlock()
	this.latest = update.Value
	if this.core == nil {
		return nil
	}
	// 阶段二：核心交付后只应用运行期窄配置，并用适配层错误码保留通知阶段。
	if err := this.core.ApplyRuntimeConfig(logzap.RuntimeConfig{Level: update.Value.Level}); err != nil {
		version := uint64(0)
		if update.Snapshot != nil {
			version = update.Snapshot.Version()
		}
		return errorForProviderRuntimeConfigApplyFailed(
			"op=app_log_zap.provider result=failed stage=apply_runtime_config version=%d initial=%t reason=%w",
			version,
			update.Initial,
			err,
		)
	}
	return nil
}

// buildCore 在配置通知串行区内创建核心，确保初始配置对应订阅观察到的最新版本。
// 阶段角色：订阅初次回放与动态更新之间的原子切换点。
func (this *runtimeConfigBridge) buildCore(options []logzap.Option) (*logzap.Provider, error) {
	this.mu.Lock()
	defer this.mu.Unlock()
	providerOptions := append([]logzap.Option{}, options...)
	core, err := logzap.NewProvider(this.latest, providerOptions...)
	if err != nil {
		return nil, err
	}
	this.core = core
	return core, nil
}

// cancelSubscriptionOnContext 在应用生命周期结束时停止配置推送，不提前关闭日志输出核心。
// 阶段角色：Build context 到 ConfigManager Subscription 的单向取消桥接。
func cancelSubscriptionOnContext(ctx context.Context, subscription config.Subscription) {
	select {
	case <-ctx.Done():
		subscription.Cancel()
	case <-subscription.Done():
	}
}

// Factory 返回核心构造期创建的稳定 LoggerFactory。
func (this *runtimeProvider) Factory() baselog.LoggerFactory {
	return this.core.Factory()
}

// Close 先停止配置投递，再关闭 Zap 核心；允许并发和重复调用。
func (this *runtimeProvider) Close() error {
	this.closeOnce.Do(func() {
		if this.watchCancel != nil {
			this.watchCancel()
		}
		if this.subscription != nil {
			this.subscription.Cancel()
		}
		if this.core != nil {
			if err := this.core.Close(); err != nil {
				this.closeErr = errorForProviderCloseFailed("op=app_log_zap.provider result=failed stage=close_core reason=%w", err)
			}
		}
	})
	return this.closeErr
}
