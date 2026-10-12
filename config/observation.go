package config

import (
	"fmt"
)

// Observation 定义某个配置 scope 的 pull 读取视图。
//
// 典型调用场景：业务长期持有 Observation，并在请求路径主动读取 Manager 的最新 Snapshot。
// Observation 不注册回调、不启动 goroutine，也不持有需要独立关闭的资源。
type Observation interface {
	// Scope 返回该视图绑定的 dotted path；空字符串表示完整配置。
	Scope() string
	// Decode 从调用时最新 Snapshot 解码该 Observation 绑定的 scope。
	Decode(out any) error
}

type defaultObservation struct {
	manager *defaultManager
	scope   string
}

// Observer 提供某个 scope 的类型安全 pull 读取能力。
type Observer[T any] struct {
	observation Observation
}

// Observe 为 Manager.Observe 提供泛型解码适配，并在创建阶段验证当前版本可解码。
func Observe[T any](manager Manager, scope string) (*Observer[T], error) {
	if isNilInterface(manager) {
		return nil, errorForConfigObserveFailed(messagef("op=config.observe result=failed stage=validate_manager reason=manager_is_nil"))
	}
	result := &Observer[T]{observation: manager.Observe(scope)}
	if _, err := result.Current(); err != nil {
		return nil, err
	}
	return result, nil
}

// Scope 返回该 pull 视图绑定的 scope。
func (this *defaultObservation) Scope() string {
	if this == nil {
		return ""
	}
	return this.scope
}

// Decode 从 Manager 最新 Snapshot 解码绑定 scope；关闭后仍读取最后发布版本。
func (this *defaultObservation) Decode(out any) error {
	if this == nil || this.manager == nil {
		return errorForConfigObserveFailed(messagef("op=config.observation.decode result=failed stage=load scope=%s reason=observation_is_nil", this.Scope()))
	}
	snapshot := this.manager.Current()
	if snapshot == nil {
		return errorForConfigObserveFailed(messagef("op=config.observation.decode result=failed stage=load scope=%s reason=snapshot_not_initialized", this.Scope()))
	}
	if err := snapshot.Decode(this.Scope(), out); err != nil {
		return errorForConfigObserveFailed(messagef("op=config.observation.decode result=failed stage=decode scope=%s", this.Scope()), err)
	}
	return nil
}

// Scope 返回 typed pull 视图绑定的 scope。
func (this *Observer[T]) Scope() string {
	if this == nil || this.observation == nil {
		return ""
	}
	return this.observation.Scope()
}

// Current 从 Manager 最新 Snapshot 解码并返回绑定 scope 的 typed 配置块。
func (this *Observer[T]) Current() (T, error) {
	var result T
	if this == nil || this.observation == nil {
		return result, errorForConfigObserveFailed(messagef("op=config.observe result=failed stage=current reason=observation_is_nil"))
	}
	if err := this.observation.Decode(&result); err != nil {
		return result, errorForConfigObserveFailed(messagef("op=config.observe result=failed stage=current scope=%s target_type=%T", this.observation.Scope(), result), err)
	}
	return result, nil
}

// String 返回便于诊断的 observation 标识，不包含配置内容。
func (this *defaultObservation) String() string {
	return fmt.Sprintf("Observation[%s]", this.Scope())
}
