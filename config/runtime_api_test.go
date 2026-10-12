package config

import (
	"context"
	"math"
	"testing"
	"time"
)

type runtimeAPIServiceConfig struct {
	// Name 是 Observe/Subscribe typed 解码的断言字段。
	Name string `yaml:"name"`
}

type panicScopeSubscriber struct{}

// Scope 模拟 Subscriber 在解析关注范围时 panic。
func (this *panicScopeSubscriber) Scope() string {
	panic("scope failed")
}

// OnUpdate 不参与本测试；若被调用则直接成功返回。
func (this *panicScopeSubscriber) OnUpdate(Update) error {
	return nil
}

// TestSnapshotDecodeAndExplain 验证解码与来源解释属于同一个不可变 Snapshot 版本。
// 测试方法：安装带来源元信息的初始快照，分别调用 Decode/Explain 并断言值、来源和版本一致。
func TestSnapshotDecodeAndExplain(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{
		raw: map[string]any{"service": map[string]any{"name": "orders"}},
		meta: map[string]ValueMeta{
			"service.name": {Source: SourceKindDefault, Layer: "default", Priority: 100},
		},
	})

	snapshot := manager.Current()
	var value runtimeAPIServiceConfig
	if err := snapshot.Decode("service", &value); err != nil {
		t.Fatalf("decode snapshot failed: %v", err)
	}
	meta, ok := snapshot.Explain("service.name")
	if !ok || value.Name != "orders" || meta.Layer != "default" || meta.Version != snapshot.Version() {
		t.Fatalf("unexpected snapshot result: value=%+v meta=%+v version=%d", value, meta, snapshot.Version())
	}
}

// TestSnapshotDecodeRejectsMissingScope 验证非空 scope 不存在时 fail-fast 且不覆盖调用方目标。
// 测试方法：用预置目标解码拼写错误的 scope，断言 scope_not_found 错误并保持原值。
func TestSnapshotDecodeRejectsMissingScope(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"service": map[string]any{"name": "orders"}}})
	target := runtimeAPIServiceConfig{Name: "keep"}
	err := manager.Current().Decode("servcie", &target)
	assertErrorCode(t, err, ErrCodeConfigDecodeScopeNotFound)
	if target.Name != "keep" {
		t.Fatalf("missing scope must not overwrite target: %+v", target)
	}
}

// TestSnapshotScalarGettersRejectLossyConversions 验证 scalar getter 不接受模糊或有损类型转换。
// 测试方法：写入数字、浮点、超大无符号数和整数时长，断言各 getter 返回显式默认值。
func TestSnapshotScalarGettersRejectLossyConversions(t *testing.T) {
	snapshot := &snapshotState{flat: map[string]any{
		"text":     42,
		"integer":  1.5,
		"overflow": uint64(math.MaxUint64),
		"boolean":  1,
		"duration": int64(time.Second),
	}}
	if got := snapshot.String("text", "fallback"); got != "fallback" {
		t.Fatalf("expected strict String fallback, got=%q", got)
	}
	if got := snapshot.Int("integer", 7); got != 7 {
		t.Fatalf("expected strict Int fallback for float, got=%d", got)
	}
	if got := snapshot.Int("overflow", 7); got != 7 {
		t.Fatalf("expected strict Int fallback for overflow, got=%d", got)
	}
	if got := snapshot.Bool("boolean", true); !got {
		t.Fatalf("expected strict Bool fallback for integer")
	}
	if got := snapshot.Duration("duration", time.Minute); got != time.Minute {
		t.Fatalf("expected explicit Duration fallback for integer, got=%s", got)
	}
}

// TestChangeCopiesProtectSnapshotAndSubscribers 验证 Change 引用值不会泄漏 Snapshot 或跨订阅共享。
// 测试方法：修改 diff 和过滤结果中的 slice，断言新 Snapshot 与原始 diff 均保持不变。
func TestChangeCopiesProtectSnapshotAndSubscribers(t *testing.T) {
	oldSnapshot := &snapshotState{flat: map[string]any{"service.items": []any{"old"}}}
	newSnapshot := &snapshotState{flat: map[string]any{"service.items": []any{"new"}}}
	changes := diffSnapshots(oldSnapshot, newSnapshot)
	changes[0].New.([]any)[0] = "mutated-event"
	value, _ := newSnapshot.Get("service.items")
	if got := value.([]any)[0]; got != "new" {
		t.Fatalf("change mutation corrupted snapshot: %v", got)
	}

	isolated := filterChangesByScope("service", changes)
	isolated[0].New.([]any)[0] = "mutated-subscriber"
	if got := changes[0].New.([]any)[0]; got != "mutated-event" {
		t.Fatalf("subscriber changes share mutable values: %v", got)
	}
}

// TestCombineLayersKeepsOnlyFinalProvenance 验证结构覆盖后 Explain 元数据只对应最终真实叶子。
// 测试方法：分别执行 map→scalar 与 scalar→map 覆盖，断言陈旧子键/祖先被移除，且远端来源不受 layer 名称前缀影响。
func TestCombineLayersKeepsOnlyFinalProvenance(t *testing.T) {
	t.Run("map_to_scalar", func(t *testing.T) {
		merged, meta := combineLayers(
			mergeLayer{Name: "default", Source: SourceKindDefault, Priority: 100, Data: map[string]any{"service": map[string]any{"name": "orders"}}},
			mergeLayer{Name: "file-provider:main", Source: SourceKindRemote, Priority: 300, Data: map[string]any{"service": "disabled"}},
		)
		if got := merged["service"]; got != "disabled" {
			t.Fatalf("expected scalar override, got=%#v", got)
		}
		if _, exists := meta["service.name"]; exists {
			t.Fatalf("stale child provenance must be removed: %#v", meta)
		}
		if got := meta["service"].Source; got != SourceKindRemote {
			t.Fatalf("expected explicit remote source, got=%s", got)
		}
	})

	t.Run("scalar_to_map", func(t *testing.T) {
		_, meta := combineLayers(
			mergeLayer{Name: "default", Source: SourceKindDefault, Priority: 100, Data: map[string]any{"service": "disabled"}},
			mergeLayer{Name: "file:application.yaml", Source: SourceKindFile, Priority: 200, Data: map[string]any{"service": map[string]any{"name": "orders"}}},
		)
		if _, exists := meta["service"]; exists {
			t.Fatalf("stale ancestor provenance must be removed: %#v", meta)
		}
		if got := meta["service.name"].Source; got != SourceKindFile {
			t.Fatalf("expected file child source, got=%s", got)
		}
	})
}

// TestDiffSnapshotsDistinguishesNullAndProvenance 验证 Change 能区分 null 与不存在，并报告来源变化。
// 测试方法：构造新增/删除 nil、值更新和仅来源更新，断言 Kind、稳定 key 顺序及仅 Version 变化不触发事件。
func TestDiffSnapshotsDistinguishesNullAndProvenance(t *testing.T) {
	oldSnapshot := &snapshotState{
		flat: map[string]any{"nullable": nil, "origin": "same", "removed": nil, "updated": 1},
		meta: map[string]ValueMeta{
			"nullable": {Source: SourceKindDefault, Layer: "default", Priority: 100, Version: 1},
			"origin":   {Source: SourceKindFile, Layer: "file:a", Priority: 200, Version: 1},
			"removed":  {Source: SourceKindDefault, Layer: "default", Priority: 100, Version: 1},
			"updated":  {Source: SourceKindDefault, Layer: "default", Priority: 100, Version: 1},
		},
	}
	newSnapshot := &snapshotState{
		flat: map[string]any{"added": nil, "nullable": nil, "origin": "same", "updated": 2},
		meta: map[string]ValueMeta{
			"added":    {Source: SourceKindRemote, Layer: "remote:main", Priority: 300, Version: 2},
			"nullable": {Source: SourceKindDefault, Layer: "default", Priority: 100, Version: 2},
			"origin":   {Source: SourceKindRemote, Layer: "remote:main", Priority: 300, Version: 2},
			"updated":  {Source: SourceKindDefault, Layer: "default", Priority: 100, Version: 2},
		},
	}

	changes := diffSnapshots(oldSnapshot, newSnapshot)
	expectedKeys := []string{"added", "origin", "removed", "updated"}
	if len(changes) != len(expectedKeys) {
		t.Fatalf("unexpected change count: %+v", changes)
	}
	for index, key := range expectedKeys {
		if changes[index].Key != key {
			t.Fatalf("changes must be sorted, index=%d got=%s want=%s", index, changes[index].Key, key)
		}
	}
	if changes[0].Kind != ChangeKindAdded || changes[0].New != nil {
		t.Fatalf("unexpected nil addition: %+v", changes[0])
	}
	if changes[1].Kind != ChangeKindUpdated {
		t.Fatalf("provenance-only update must be reported: %+v", changes[1])
	}
	if changes[2].Kind != ChangeKindRemoved || changes[2].Old != nil {
		t.Fatalf("unexpected nil removal: %+v", changes[2])
	}
	if changes[3].Kind != ChangeKindUpdated || changes[3].Old != 1 || changes[3].New != 2 {
		t.Fatalf("unexpected value update: %+v", changes[3])
	}
}

// TestGenericObserverCurrentPullsLatestBlock 验证泛型 Observer 每次 Current 都读取最新配置块。
// 测试方法：读取初始值，刷新到第二版本后再次 Current，断言 typed 块同步更新。
func TestGenericObserverCurrentPullsLatestBlock(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"service": map[string]any{"name": "v1"}}})
	manager.setRebuild(func(context.Context) (snapshotState, error) {
		return snapshotState{raw: map[string]any{"service": map[string]any{"name": "v2"}}}, nil
	})

	observation, err := Observe[runtimeAPIServiceConfig](manager, "service")
	if err != nil {
		t.Fatalf("create typed observation failed: %v", err)
	}
	initial, err := observation.Current()
	if err != nil || initial.Name != "v1" {
		t.Fatalf("unexpected initial observation: value=%+v err=%v", initial, err)
	}
	if err = manager.refresh(context.Background()); err != nil {
		t.Fatalf("refresh manager failed: %v", err)
	}
	current, err := observation.Current()
	if err != nil || current.Name != "v2" {
		t.Fatalf("unexpected refreshed observation: value=%+v err=%v", current, err)
	}
}

// TestObservationDecodeUsesBoundScope 验证非泛型 Observation 的 scope 直接参与解码行为。
// 测试方法：创建 service Observation，调用 Decode 且不重复传 scope，断言得到绑定块的值。
func TestObservationDecodeUsesBoundScope(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"service": map[string]any{"name": "orders"}}})
	observation := manager.Observe("service")
	var value runtimeAPIServiceConfig
	err := observation.Decode(&value)
	if err != nil || value.Name != "orders" {
		t.Fatalf("unexpected observation decode: value=%+v err=%v", value, err)
	}
}

// TestGenericSubscribeReplaysUpdatesAndCancels 验证泛型 Subscribe 的初次回放、变化推送和取消状态。
// 测试方法：订阅 service，断言初次事件；刷新后断言 typed 变化；取消后确认 Done 关闭且状态计数归零。
func TestGenericSubscribeReplaysUpdatesAndCancels(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"service": map[string]any{"name": "v1"}}})
	next := "v2"
	manager.setRebuild(func(context.Context) (snapshotState, error) {
		return snapshotState{raw: map[string]any{"service": map[string]any{"name": next}}}, nil
	})

	updates := make([]TypedUpdate[runtimeAPIServiceConfig], 0, 2)
	subscription, err := Subscribe[runtimeAPIServiceConfig](manager, "service", func(update TypedUpdate[runtimeAPIServiceConfig]) error {
		updates = append(updates, update)
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	if len(updates) != 1 || !updates[0].Initial || updates[0].Value.Name != "v1" {
		t.Fatalf("unexpected initial update: %+v", updates)
	}
	if status := manager.Status(); status.State() != ManagerStateRunning || status.Snapshot().Version != 1 || status.Resources().SubscriptionCount != 1 {
		t.Fatalf("unexpected active manager status: %+v", status)
	}

	if err = manager.refresh(context.Background()); err != nil {
		t.Fatalf("refresh manager failed: %v", err)
	}
	if len(updates) != 2 || updates[1].Initial || updates[1].Value.Name != "v2" || len(updates[1].Changes) != 1 {
		t.Fatalf("unexpected refreshed update: %+v", updates)
	}

	subscription.Cancel()
	select {
	case <-subscription.Done():
	default:
		t.Fatalf("expected subscription Done closed after cancel")
	}
	if status := manager.Status(); status.Resources().SubscriptionCount != 0 {
		t.Fatalf("expected no active subscriptions, got=%+v", status)
	}

	next = "v3"
	if err = manager.refresh(context.Background()); err != nil {
		t.Fatalf("refresh after cancel failed: %v", err)
	}
	if len(updates) != 2 {
		t.Fatalf("canceled subscription received update: %+v", updates)
	}
}

// TestManagerCloseFinishesSubscriptions 验证 Manager Close 会结束全部 Subscription 并保留最终状态可读。
// 测试方法：注册订阅后关闭 Manager，断言 Done 关闭、Manager=closed 且最后版本保持不变。
func TestManagerCloseFinishesSubscriptions(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"service": map[string]any{"name": "v1"}}})
	subscription, err := Subscribe[runtimeAPIServiceConfig](manager, "service", func(TypedUpdate[runtimeAPIServiceConfig]) error { return nil })
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	if err = manager.Close(); err != nil {
		t.Fatalf("close manager failed: %v", err)
	}
	select {
	case <-subscription.Done():
	default:
		t.Fatalf("expected subscription Done closed by manager")
	}
	status := manager.Status()
	if status.State() != ManagerStateClosed || status.Snapshot().Version != 1 || status.Resources().SubscriptionCount != 0 {
		t.Fatalf("unexpected closed manager status: %+v", status)
	}
}

// TestStatusCountsWatchersAndResources 验证 Status 使用运行语义计数而非 closer slice 长度。
// 测试方法：注册无 closer watcher 和一个 Plan 资源，断言活动计数；Close 后断言两项归零。
func TestStatusCountsWatchersAndResources(t *testing.T) {
	manager := newDefaultManager()
	manager.addWatcher(nil)
	manager.addSession(&testCloser{})
	status := manager.Status()
	if status.Resources().WatchCount != 1 || status.Resources().ProviderSessionCount != 1 {
		t.Fatalf("unexpected active resource status: %+v", status)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("close manager failed: %v", err)
	}
	status = manager.Status()
	if status.Resources().WatchCount != 0 || status.Resources().ProviderSessionCount != 0 {
		t.Fatalf("closed manager retained resource counts: %+v", status)
	}
}

// TestGenericRuntimeAPIsRejectTypedNilManager 验证泛型入口能识别装入接口后的 typed nil Manager。
// 测试方法：构造 typed nil Manager，分别调用 Observe/Subscribe，并断言返回错误而不是 panic。
func TestGenericRuntimeAPIsRejectTypedNilManager(t *testing.T) {
	var implementation *defaultManager
	var manager Manager = implementation
	if _, err := Observe[runtimeAPIServiceConfig](manager, "service"); err == nil {
		t.Fatalf("expected Observe to reject typed nil manager")
	}
	if _, err := Subscribe[runtimeAPIServiceConfig](manager, "service", func(TypedUpdate[runtimeAPIServiceConfig]) error { return nil }); err == nil {
		t.Fatalf("expected Subscribe to reject typed nil manager")
	}
}

// TestManagerSubscribeRecoversScopePanic 验证 Subscriber.Scope panic 被转成订阅错误。
// 测试方法：注册 Scope 会 panic 的 Subscriber，断言 Subscribe 返回错误且注册表保持为空。
func TestManagerSubscribeRecoversScopePanic(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"service": map[string]any{"name": "v1"}}})
	if _, err := manager.Subscribe(&panicScopeSubscriber{}); err == nil {
		t.Fatalf("expected Subscribe to recover Scope panic")
	}
	if status := manager.Status(); status.Resources().SubscriptionCount != 0 {
		t.Fatalf("scope panic must not leave registration: %+v", status)
	}
}
