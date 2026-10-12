package zap

import (
	"errors"
	"sync"
	"sync/atomic"

	baselog "github.com/yangzhuangqiu/gobase/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Provider 是基于 zap 的日志提供者实例。
//
// 语义：
// 1. 实现 baselog.Provider，可直接被业务/框架持有；
// 2. 外部适配层通过 ApplyRuntimeConfig 显式推送运行期窄配置；
// 3. Close 只负责 Sync 与输出资源释放，不启动或拥有配置监听。
type Provider struct {
	// factory 是构造期绑定本 Provider 动态 Handler 的稳定业务入口。
	factory baselog.LoggerFactory
	// mu 串行保护 writer 与运行时资源的替换和关闭。
	mu sync.Mutex
	// runtime 保存 Handler 每次写入时读取的当前 Zap 快照。
	runtime atomic.Pointer[providerRuntime]
	// level 是当前 core 共享的动态级别控制器。
	level zap.AtomicLevel
	// writerCloser 持有文件等需要显式释放的输出资源。
	writerCloser interface{ Close() error }
	// closed 阻止关闭后的事件写入与热更新。
	closed atomic.Bool
}

// providerRuntime 是一次原子发布的 Zap 运行时快照。
type providerRuntime struct {
	// base 持有已构建的 core 与输出端。
	base *zap.Logger
	// addSource 决定是否把 slog.Record.PC 映射为 Zap caller。
	addSource bool
}

var _ baselog.Provider = (*Provider)(nil)

// NewProvider 创建一个 zap provider。
//
// 参数：
// - config: zap 初始化配置；
// - options: 可选调用链字段提取器与脱敏策略。
func NewProvider(config Config, options ...Option) (*Provider, error) {
	resolved := resolveProviderOptions(options)
	provider := &Provider{}
	if err := provider.rebuild(config); err != nil {
		return nil, err
	}
	provider.factory = baselog.NewLoggerFactory(
		newHandler(provider),
		baselog.WithContextExtractors(resolved.contextExtractors...),
		baselog.WithRedactor(resolved.redactor),
	)
	return provider, nil
}

// Factory 返回构造期创建的稳定日志工厂。
// 返回：
// - LoggerFactory: 重复调用返回同一实例；Provider.Close 后仍可调用但不再输出。
func (this *Provider) Factory() baselog.LoggerFactory {
	return this.factory
}

// ApplyRuntimeConfig 原子应用无需重建输出资源的运行期配置。
//
// 典型调用场景：app-log-zap 等装配适配层消费外部配置事件后，仅把允许动态变化的字段传入。
//
// 参数：
// - config：运行期窄配置；当前仅 Level 可变，空值表示保持现状。
//
// 返回：
// - error：字段非法时返回可分支错误，既有运行状态保持不变；Provider 已关闭时返回 nil。
func (this *Provider) ApplyRuntimeConfig(config RuntimeConfig) error {
	// 阶段一：关闭边界优先返回，避免迟到的配置事件重新激活 Provider。
	if this.closed.Load() {
		return nil
	}
	levelText, changed, err := normalizeRuntimeConfig(config)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	// 阶段二：只有已发布运行时存在时才更新共享 AtomicLevel。
	if this.runtime.Load() == nil {
		return nil
	}
	level, _ := parseZapLevel(levelText)
	// 阶段三：与 Close 的原子摘除再次竞争，关闭获胜时放弃本次更新。
	if this.closed.Load() {
		return nil
	}
	this.level.SetLevel(level)
	return nil
}

// Close 刷新并释放 Provider 持有的日志输出资源。
//
// 流程：
// 1. 原子摘除运行时引用，避免关闭期间有新回调进入；
// 2. 执行 logger.Sync 刷新缓冲；
// 3. 关闭底层 writer，并合并返回错误。
func (this *Provider) Close() error {
	this.mu.Lock()
	this.closed.Store(true)
	runtime := this.runtime.Swap(nil)
	var base *zap.Logger
	if runtime != nil {
		base = runtime.base
	}
	closer := this.writerCloser
	this.writerCloser = nil
	this.mu.Unlock()

	// 第一步：刷新旧 logger 缓冲并释放 writer。
	syncErr := syncBaseLogger(base)
	if closer == nil {
		return syncErr
	}
	return errors.Join(syncErr, closer.Close())
}

// rebuild 基于配置重建底层 zap core 并替换运行时状态。
//
// 流程：
// 1. 解析配置并构建 encoder/writeSyncer/level/core；
// 2. 生成新 base logger 后原子替换 provider 状态；
// 3. 刷新并关闭旧资源，避免句柄泄漏。
func (this *Provider) rebuild(config Config) error {
	resolved, err := ResolveConfig(config)
	if err != nil {
		return err
	}
	encoder := buildZapEncoder(resolved.Format)
	writeSyncer, writerCloser, err := buildZapWriteSyncer(resolved)
	if err != nil {
		return err
	}
	level, _ := parseZapLevel(resolved.Level)
	atomicLevel := zap.NewAtomicLevelAt(level)
	core := zapcore.NewCore(encoder, writeSyncer, atomicLevel)

	base := zap.New(core)

	this.mu.Lock()
	previousRuntime := this.runtime.Swap(&providerRuntime{
		base:      base,
		addSource: !resolved.DisableCaller,
	})
	var previousBase *zap.Logger
	if previousRuntime != nil {
		previousBase = previousRuntime.base
	}
	previousCloser := this.writerCloser
	this.level = atomicLevel
	this.writerCloser = writerCloser
	this.mu.Unlock()

	syncErr := syncBaseLogger(previousBase)
	if previousCloser == nil {
		return syncErr
	}
	return errors.Join(syncErr, previousCloser.Close())
}

// syncBaseLogger 安全执行 logger.Sync，并过滤可忽略平台错误。
func syncBaseLogger(logger *zap.Logger) error {
	if logger == nil {
		return nil
	}
	err := logger.Sync()
	if isIgnorableSyncError(err) {
		return nil
	}
	return err
}
