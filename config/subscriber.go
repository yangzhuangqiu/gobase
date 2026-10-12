package config

import (
	"fmt"
	"strings"
)

// Update 描述一次已发布 Snapshot 的简化通知事件。
type Update struct {
	// Snapshot 是已经通过 validator 并完成发布的新版本。
	Snapshot Snapshot
	// Changes 是触达 Subscriber scope 的 key 级变化；初次回放时为空。
	Changes []Change
	// Initial 标记该事件是否为 Subscribe 成功前的当前版本回放。
	Initial bool
}

// Subscriber 定义配置发布后的 push 消费契约。
//
// Subscriber 失败不会回滚已经发布的 Snapshot；错误进入 refresh 错误链和异步错误处理器。
type Subscriber interface {
	// Scope 返回关注的 dotted path；空字符串表示完整配置。
	Scope() string
	// OnUpdate 同步处理已发布事件；实现应快速返回且不得同步触发 Manager refresh。
	OnUpdate(update Update) error
}

// TypedUpdate 描述包级 Subscribe 泛型适配后的 typed 事件。
type TypedUpdate[T any] struct {
	// Value 是从 Current Snapshot 的 Subscriber scope 解码得到的值。
	Value T
	// Snapshot 是已经发布、并用于本次 typed 解码的版本。
	Snapshot Snapshot
	// Changes 是触达订阅 scope 的 key 级变化；初次回放时为空。
	Changes []Change
	// Initial 标记该事件是否为注册阶段的当前版本回放。
	Initial bool
}

type typedSubscriber[T any] struct {
	scope   string
	handler func(TypedUpdate[T]) error
}

// Subscribe 为 Manager.Subscribe 提供泛型解码适配。
func Subscribe[T any](manager Manager, scope string, handler func(TypedUpdate[T]) error) (Subscription, error) {
	if isNilInterface(manager) {
		return nil, errorForConfigManagerSubscribeFailed(messagef("op=config.subscribe result=failed stage=validate_manager reason=manager_is_nil"))
	}
	if handler == nil {
		return nil, errorForConfigSubscriberInvalid(messagef("op=config.subscribe result=failed stage=validate_handler reason=handler_is_nil"))
	}
	return manager.Subscribe(&typedSubscriber[T]{scope: strings.TrimSpace(scope), handler: handler})
}

// resolveSubscriberScope 读取 Subscriber 自带的 scope，并把协议实现 panic 转换为可诊断错误。
func resolveSubscriberScope(subscriber Subscriber) (scope string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errorForConfigSubscriberPanic(messagef(fmt.Sprintf("op=config.subscriber result=failed stage=resolve_scope reason=%v", recovered)))
		}
	}()
	return strings.TrimSpace(subscriber.Scope()), nil
}

// Scope 返回 typed subscriber 关注范围。
func (this *typedSubscriber[T]) Scope() string {
	return this.scope
}

// OnUpdate 把原始 Snapshot 事件解码为 typed 事件后交给业务 handler。
func (this *typedSubscriber[T]) OnUpdate(update Update) error {
	var value T
	if update.Snapshot == nil {
		return errorForConfigManagerSubscribeFailed(messagef("op=config.subscribe result=failed stage=decode scope=%s reason=snapshot_is_nil", this.scope))
	}
	if err := update.Snapshot.Decode(this.scope, &value); err != nil {
		return errorForConfigManagerSubscribeFailed(messagef("op=config.subscribe result=failed stage=decode scope=%s target_type=%T", this.scope, value), err)
	}
	return this.handler(TypedUpdate[T]{Value: value, Snapshot: update.Snapshot, Changes: copyChanges(update.Changes), Initial: update.Initial})
}

// runSubscriber 隔离 Subscriber panic，并返回可聚合错误。
func runSubscriber(subscriber Subscriber, update Update) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errorForConfigSubscriberPanic(messagef(fmt.Sprintf("op=config.subscriber result=failed stage=callback_panic reason=%v", recovered)))
		}
	}()
	return subscriber.OnUpdate(update)
}

// filterChangesByScope 返回仅触达指定 scope 的变化副本。
func filterChangesByScope(scope string, changes []Change) []Change {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return copyChanges(changes)
	}
	result := make([]Change, 0, len(changes))
	prefix := scope + "."
	for _, change := range changes {
		if change.Key == scope || strings.HasPrefix(change.Key, prefix) {
			result = append(result, copyChange(change))
		}
	}
	return result
}

// copyChanges 深拷贝事件差异，隔离不同 Subscriber 对引用值的修改。
func copyChanges(changes []Change) []Change {
	result := make([]Change, 0, len(changes))
	for _, change := range changes {
		result = append(result, copyChange(change))
	}
	return result
}

// copyChange 深拷贝单项差异中的可变 Old/New 值。
func copyChange(change Change) Change {
	change.Old = deepCopyValue(change.Old)
	change.New = deepCopyValue(change.New)
	return change
}
