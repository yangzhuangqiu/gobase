package zap

import (
	"slices"

	"go.uber.org/zap/zapcore"
)

// providerCore 是绑定 Provider 当前运行时的稳定 Zap Core 视图。
//
// 语义:
// 1. 每次 Enabled/Check/Write 都读取 Provider 当前快照，共享动态 level 和输出资源；
// 2. Provider Close 后变为 no-op，不延长 writer 生命周期；
// 3. Sync 是 no-op，底层刷新与关闭仍由 Provider 独占负责。
type providerCore struct {
	// provider 是底层 Zap 运行时和资源的唯一所有者。
	provider *Provider
	// fields 是 With 派生时绑定的不可变 Zap 字段副本。
	fields []zapcore.Field
}

var _ zapcore.Core = (*providerCore)(nil)

// Core 返回绑定当前 Provider 生命周期的稳定 Zap Core。
//
// 典型调用场景:
// 1. 框架适配模块复用同一 Provider 的 level、encoder 和 writer；
// 2. Provider 关闭后，既有 Core 保持可调用但不再输出；
// 3. 返回值只借用底层，Sync 为 no-op 且不提供 Close，不取得资源所有权。
func (this *Provider) Core() zapcore.Core {
	return &providerCore{provider: this}
}

// Enabled 判断当前 Provider 快照是否接受指定 Zap level。
func (this *providerCore) Enabled(level zapcore.Level) bool {
	runtimeState := this.current()
	return runtimeState != nil && runtimeState.base.Core().Enabled(level)
}

// With 派生绑定固定字段且继续跟随 Provider 当前快照的 Core。
func (this *providerCore) With(fields []zapcore.Field) zapcore.Core {
	result := slices.Clone(this.fields)
	result = append(result, fields...)
	return &providerCore{provider: this.provider, fields: result}
}

// Check 使用当前底层 Core 的 level 判定，并把最终写入委托回动态视图。
func (this *providerCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if !this.Enabled(entry.Level) {
		return checked
	}
	return checked.AddCore(entry, this)
}

// Write 把事件写入调用时最新的 Provider Core，并遵守 Provider caller 配置。
func (this *providerCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	runtimeState := this.current()
	if runtimeState == nil {
		return nil
	}
	if !runtimeState.addSource {
		entry.Caller = zapcore.EntryCaller{}
	}
	return runtimeState.base.Core().With(this.fields).Write(entry, fields)
}

// Sync 保持借用所有权边界，不刷新或关闭 Provider 底层资源。
func (this *providerCore) Sync() error {
	return nil
}

// current 读取当前可写 Provider 快照；nil/关闭状态统一返回 nil。
// 阶段角色：所有 Core 热路径共享同一生命周期门禁。
func (this *providerCore) current() *providerRuntime {
	if this == nil || this.provider == nil || this.provider.closed.Load() {
		return nil
	}
	runtimeState := this.provider.runtime.Load()
	if runtimeState == nil || runtimeState.base == nil {
		return nil
	}
	return runtimeState
}
