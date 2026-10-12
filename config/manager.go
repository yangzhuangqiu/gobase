package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Manager 提供当前快照、pull 观察、push 订阅与关闭能力。
type Manager interface {
	// Current 返回当前已发布的只读快照；若尚未初始化则返回 nil。
	Current() Snapshot
	// Observe 返回指定 scope 的 pull 读取视图；空 scope 表示完整配置。
	Observe(scope string) Observation
	// Subscribe 注册一个自带 scope 的 push 消费方，并在返回前回放当前版本。
	Subscribe(subscriber Subscriber) (Subscription, error)
	// Status 返回调用时刻的不可变运行状态快照。
	Status() Status
	// Close 停止监听并释放内部资源。
	Close() error
}

var (
	errManagerClosed = errorForConfigManagerClosed(messagef("reason=manager_closed"))
	errRebuildNotSet = errorForConfigRebuildNotSet(messagef("reason=rebuild_not_set"))
)

type subscriberRegistration struct {
	id           uint64
	subscriber   Subscriber
	scope        string
	subscription *defaultSubscription
	// notifyMu 保证同一订阅的初次回放与后续版本严格串行。
	notifyMu sync.Mutex
}

// defaultManager 是 Manager 接口的内部实现。
//
// 读取路径通过 atomic 快照指针实现无锁读；
// 刷新路径由 reloadMu 串行化，避免并发重算。
type defaultManager struct {
	mu       sync.RWMutex
	reloadMu sync.Mutex

	current atomic.Pointer[snapshotState]
	status  atomic.Pointer[statusSnapshot]

	subscribers        map[uint64]*subscriberRegistration
	nextSubscriberID   uint64
	snapshotValidators []SnapshotValidator

	rebuild func(ctx context.Context) (snapshotState, error)

	cancel       context.CancelFunc
	watchClosers []io.Closer
	// watcherCount 是已成功注册的 watcher 数，包含没有独立 closer 的 watcher。
	watcherCount int
	// sessions 保存 Provider Open 后移交给 Manager 的运行会话。
	sessions []io.Closer
}

// newDefaultManager 创建一个空的 manager。
func newDefaultManager() *defaultManager {
	result := &defaultManager{subscribers: make(map[uint64]*subscriberRegistration)}
	result.status.Store(newInitialStatus(time.Now()))
	return result
}

// setRebuild 设置快照重算函数。
func (this *defaultManager) setRebuild(fn func(ctx context.Context) (snapshotState, error)) {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.rebuild = fn
}

// setSnapshotValidators 设置发布前快照校验器列表。
func (this *defaultManager) setSnapshotValidators(validators []SnapshotValidator) {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.snapshotValidators = this.snapshotValidators[:0]
	for _, validator := range validators {
		if validator == nil {
			continue
		}
		this.snapshotValidators = append(this.snapshotValidators, validator)
	}
}

// setCancel 设置 manager 级 cancel 函数，供 Close 使用。
func (this *defaultManager) setCancel(cancel context.CancelFunc) {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.cancel = cancel
}

// addWatcher 记录一个成功注册的 watcher，并按需接管其关闭器。
func (this *defaultManager) addWatcher(closer io.Closer) {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.watcherCount++
	if !isNilInterface(closer) {
		this.watchClosers = append(this.watchClosers, closer)
	}
	this.updateResourceStatusLocked()
}

// addSession 接管 Provider Open 后移交的 Session。
func (this *defaultManager) addSession(item io.Closer) {
	if isNilInterface(item) {
		return
	}
	this.mu.Lock()
	defer this.mu.Unlock()
	this.sessions = append(this.sessions, item)
	this.updateResourceStatusLocked()
}

// installInitial 发布首个快照（version=1）。
func (this *defaultManager) installInitial(snapshot snapshotState) error {
	installed := this.prepareSnapshot(snapshot, 1)
	if err := this.validateSnapshot(installed); err != nil {
		return errorf("op=config.manager.install_initial result=failed stage=validate_snapshot reason=%w", err)
	}
	this.mu.Lock()
	defer this.mu.Unlock()
	this.current.Store(installed)
	this.publishSnapshotStatusLocked(installed)
	return nil
}

// refresh 触发一次重算并发布最新快照。
func (this *defaultManager) refresh(ctx context.Context) error {
	_, err := this.refreshWithPublishStatus(ctx)
	return err
}

// refreshWithPublishStatus 执行刷新，并报告候选 Snapshot 是否已经完成发布。
// 返回：
// - published：仅在 current.Store 已执行后为 true；Subscriber 失败不改变该结果。
// - error：包含发布前失败或发布后的 Subscriber 通知失败。
func (this *defaultManager) refreshWithPublishStatus(ctx context.Context) (published bool, err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	this.reloadMu.Lock()
	defer this.reloadMu.Unlock()

	this.mu.Lock()
	if this.managerStoppingLocked() {
		this.mu.Unlock()
		return false, errorf("op=config.manager.refresh result=failed stage=precheck reason=%w", errManagerClosed)
	}
	rebuild := this.rebuild
	this.beginRefreshStatusLocked()
	if rebuild == nil {
		refreshErr := errorf("op=config.manager.refresh result=failed stage=precheck reason=%w", errRebuildNotSet)
		this.finishRefreshFailureStatusLocked("precheck", refreshErr)
		this.mu.Unlock()
		return false, refreshErr
	}
	this.mu.Unlock()

	oldSnapshot := this.current.Load()
	newSnapshotRaw, err := rebuild(ctx)
	if err != nil {
		refreshErr := errorForConfigManagerRefreshRebuildFailed(messagef("op=config.manager.refresh result=failed stage=rebuild_snapshot"), err)
		this.mu.Lock()
		this.finishRefreshFailureStatusLocked("rebuild_snapshot", refreshErr)
		this.mu.Unlock()
		return false, refreshErr
	}

	newVersion := uint64(1)
	if oldSnapshot != nil {
		newVersion = oldSnapshot.version + 1
	}
	newSnapshot := this.prepareSnapshot(newSnapshotRaw, newVersion)
	if err = this.validateSnapshot(newSnapshot); err != nil {
		refreshErr := errorf("op=config.manager.refresh result=failed stage=validate_snapshot reason=%w", err)
		this.mu.Lock()
		this.finishRefreshFailureStatusLocked("validate_snapshot", refreshErr)
		this.mu.Unlock()
		return false, refreshErr
	}
	diff := diffSnapshots(oldSnapshot, newSnapshot)

	// 发布点与 Close 共用状态锁，形成“先发布或先关闭”的稳定线性化顺序。
	this.mu.Lock()
	if this.managerStoppingLocked() {
		refreshErr := errorf("op=config.manager.refresh result=failed stage=publish_precheck reason=%w", errManagerClosed)
		this.finishRefreshFailureStatusLocked("publish_precheck", refreshErr)
		this.mu.Unlock()
		return false, refreshErr
	}
	this.current.Store(newSnapshot)
	this.finishRefreshSuccessStatusLocked(newSnapshot)
	subscribers := this.snapshotSubscribersLocked()
	notifyStarted := len(diff) > 0 && len(subscribers) > 0
	if notifyStarted {
		this.beginNotificationStatusLocked()
	}
	this.mu.Unlock()

	if !notifyStarted {
		return true, nil
	}

	// 通知结果独立于快照发布结果：Subscriber 失败不把已经成功的 refresh 标记为失败。
	finishNotification := func(notifyErr error) {
		this.mu.Lock()
		this.finishNotificationStatusLocked(notifyErr)
		this.mu.Unlock()
	}
	var subscriberErrs []error
	for _, item := range subscribers {
		select {
		case <-ctx.Done():
			subscriberErrs = append(subscriberErrs, errorForConfigManagerRefreshNotifyPrecheckFailed(messagef("op=config.manager.refresh result=failed stage=notify_precheck"), ctx.Err()))
			notifyErr := errors.Join(subscriberErrs...)
			finishNotification(notifyErr)
			return true, notifyErr
		default:
		}

		item.notifyMu.Lock()
		this.mu.RLock()
		closed := this.managerStoppingLocked()
		activeItem, active := this.subscribers[item.id]
		active = active && activeItem == item
		this.mu.RUnlock()
		if closed {
			item.notifyMu.Unlock()
			subscriberErrs = append(subscriberErrs, errorf("op=config.manager.refresh result=failed stage=notify_precheck reason=%w", errManagerClosed))
			notifyErr := errors.Join(subscriberErrs...)
			finishNotification(notifyErr)
			return true, notifyErr
		}
		if !active || !scopeTouched(item.scope, diff) {
			item.notifyMu.Unlock()
			continue
		}
		update := Update{Snapshot: newSnapshot, Changes: filterChangesByScope(item.scope, diff)}
		if err = runSubscriber(item.subscriber, update); err != nil {
			subscriberErrs = append(subscriberErrs, errorForConfigManagerRefreshSubscriberFailed(messagef("op=config.manager.refresh_subscriber result=failed scope=%s subscription_id=%d",
				item.scope, item.id), err,
			))
		}
		item.notifyMu.Unlock()
	}
	notifyErr := errors.Join(subscriberErrs...)
	finishNotification(notifyErr)
	return true, notifyErr
}

// validateSnapshot 在快照发布前执行语义校验。
func (this *defaultManager) validateSnapshot(snapshot Snapshot) error {
	this.mu.RLock()
	validators := append([]SnapshotValidator{}, this.snapshotValidators...)
	this.mu.RUnlock()
	if len(validators) == 0 {
		return nil
	}
	for index, validator := range validators {
		if validator == nil {
			continue
		}
		if err := runSnapshotValidator(validator, snapshot); err != nil {
			return errorForConfigManagerSnapshotValidateFailed(messagef("op=config.manager.snapshot_validate result=failed stage=run_validator validator_index=%d",
				index), err,
			)
		}
	}
	return nil
}

// runSnapshotValidator 执行单个校验器并把 panic 转换为 error。
func runSnapshotValidator(validator SnapshotValidator, snapshot Snapshot) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errorf("op=config.manager.snapshot_validate result=failed stage=validator_panic reason=%v", recovered)
		}
	}()
	return validator(snapshot)
}

// prepareSnapshot 在发布前补齐元信息并深拷贝可变 map。
func (this *defaultManager) prepareSnapshot(snapshot snapshotState, version uint64) *snapshotState {
	snapshot.version = version
	snapshot.updatedAt = time.Now()

	raw := deepCopyMap(snapshot.raw)
	flat := snapshot.flat
	if len(flat) == 0 {
		flat = flattenMap(raw)
	}

	snapshot.raw = raw
	snapshot.flat = deepCopyFlat(flat)
	snapshot.meta = copyMeta(snapshot.meta, snapshot.version)
	return &snapshot
}

// Current 返回当前已发布的只读快照。
func (this *defaultManager) Current() Snapshot {
	current := this.current.Load()
	if current == nil {
		return nil
	}
	return current
}

// Observe 返回固定 scope 的 pull 读取视图。
func (this *defaultManager) Observe(scope string) Observation {
	return &defaultObservation{manager: this, scope: strings.TrimSpace(scope)}
}

// Status 返回调用时刻的不可变 Manager 运行状态快照。
func (this *defaultManager) Status() Status {
	if this == nil {
		return statusForNilManager
	}
	status := this.status.Load()
	if status == nil {
		return statusForNilManager
	}
	return status
}

// Subscribe 注册 push 消费方，并在与 refresh 串行的临界区内完成当前版本回放。
func (this *defaultManager) Subscribe(subscriber Subscriber) (Subscription, error) {
	if isNilInterface(subscriber) {
		return nil, errorForConfigSubscriberInvalid(messagef("op=config.manager.subscribe result=failed stage=validate_subscriber reason=subscriber_is_nil"))
	}
	scope, err := resolveSubscriberScope(subscriber)
	if err != nil {
		return nil, errorForConfigManagerSubscribeFailed(messagef("op=config.manager.subscribe result=failed stage=resolve_scope"), err)
	}

	registration := &subscriberRegistration{subscriber: subscriber, scope: scope}
	// 在注册项对 refresh 可见前先占有通知锁，确保初次回放一定先于后续版本。
	registration.notifyMu.Lock()
	this.mu.Lock()
	if this.managerStoppingLocked() {
		this.mu.Unlock()
		registration.notifyMu.Unlock()
		return nil, errorf("op=config.manager.subscribe result=failed stage=precheck reason=%w", errManagerClosed)
	}
	this.nextSubscriberID++
	id := this.nextSubscriberID
	subscription := &defaultSubscription{manager: this, id: id, done: make(chan struct{})}
	registration.id = id
	registration.subscription = subscription
	this.subscribers[id] = registration
	this.updateResourceStatusLocked()
	current := this.current.Load()
	this.mu.Unlock()

	if current == nil {
		registration.notifyMu.Unlock()
		this.unsubscribe(id)
		subscription.finish()
		return nil, errorForConfigManagerSubscribeFailed(messagef("op=config.manager.subscribe result=failed stage=initial_replay scope=%s reason=snapshot_not_initialized", scope))
	}
	if err = runSubscriber(subscriber, Update{Snapshot: current, Initial: true}); err != nil {
		registration.notifyMu.Unlock()
		this.unsubscribe(id)
		subscription.finish()
		return nil, errorForConfigManagerSubscribeFailed(messagef("op=config.manager.subscribe result=failed stage=initial_replay scope=%s subscription_id=%d", scope, id), err)
	}
	registration.notifyMu.Unlock()

	this.mu.RLock()
	activeItem, active := this.subscribers[id]
	closed := this.managerStoppingLocked()
	this.mu.RUnlock()
	if closed || !active || activeItem != registration {
		subscription.finish()
		return nil, errorf("op=config.manager.subscribe result=failed stage=post_replay_precheck reason=%w", errManagerClosed)
	}
	return subscription, nil
}

// unsubscribe 从后续通知集合中移除订阅；Subscription 自己负责关闭 Done。
func (this *defaultManager) unsubscribe(id uint64) {
	this.mu.Lock()
	defer this.mu.Unlock()
	delete(this.subscribers, id)
	this.updateResourceStatusLocked()
}

// snapshotSubscribersLocked 返回按注册顺序排列的订阅快照。
func (this *defaultManager) snapshotSubscribersLocked() []*subscriberRegistration {
	result := make([]*subscriberRegistration, 0, len(this.subscribers))
	for _, item := range this.subscribers {
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].id < result[j].id
	})
	return result
}

// isNilInterface 识别普通 nil 与装入接口后的 typed nil。
func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

// Close 停止监听并释放内部持有资源；不等待已经开始的 Provider Load 返回。
func (this *defaultManager) Close() error {
	this.mu.Lock()
	if this.managerStoppingLocked() {
		this.mu.Unlock()
		return nil
	}
	this.changeManagerStateLocked(ManagerStateClosing)
	cancel := this.cancel
	closers := append([]io.Closer{}, this.watchClosers...)
	sessions := append([]io.Closer{}, this.sessions...)
	subscriptions := make([]*defaultSubscription, 0, len(this.subscribers))
	for _, item := range this.subscribers {
		subscriptions = append(subscriptions, item.subscription)
	}
	this.watchClosers = nil
	this.watcherCount = 0
	this.sessions = nil
	this.subscribers = map[uint64]*subscriberRegistration{}
	this.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, subscription := range subscriptions {
		subscription.finish()
	}

	var errs []error
	for index, closer := range closers {
		if isNilInterface(closer) {
			continue
		}
		if err := closeManagedResource(closer); err != nil {
			errs = append(errs, errorForConfigManagerCloseWatcherFailed(messagef("op=config.manager.close result=failed stage=close_watcher watcher_index=%d",
				index), err,
			))
		}
	}
	// watcher 已停止后再关闭其依赖的 Provider Session；Session 必须容忍在途 Load。
	for index := len(sessions) - 1; index >= 0; index-- {
		item := sessions[index]
		if isNilInterface(item) {
			continue
		}
		if err := closeManagedResource(item); err != nil {
			errs = append(errs, errorForConfigManagerCloseResourceFailed(messagef("op=config.manager.close result=failed stage=close_session session_index=%d",
				index), err,
			))
		}
	}
	this.mu.Lock()
	this.updateResourceStatusLocked()
	this.changeManagerStateLocked(ManagerStateClosed)
	this.mu.Unlock()
	return errors.Join(errs...)
}

// closeManagedResource 把扩展资源 Close panic 转换为 error，确保后续资源仍可继续释放。
func closeManagedResource(item io.Closer) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errorForConfigInternal(messagef("op=config.manager.resource result=failed stage=close reason=panic payload_type=%T", recovered))
		}
	}()
	return item.Close()
}

// decodeScopeIntoTarget 将整份配置或 scope 子块解码到 target。
func decodeScopeIntoTarget(scope string, snapshot *snapshotState, target any) error {
	if target == nil {
		return errorForConfigDecodeTargetNil(messagef("target_is_nil"))
	}
	if snapshot == nil {
		return nil
	}
	if scope == "" {
		if err := decodeAnyToTarget(snapshot.raw, target); err != nil {
			return errorf("decode_full_config_failed scope=%s reason=%w", scope, err)
		}
		return nil
	}
	value, ok := lookupByDottedKey(snapshot.raw, scope)
	if !ok {
		return errorForConfigDecodeScopeNotFound(messagef("op=config.decode_scope result=failed stage=lookup scope=%s reason=scope_not_found", scope))
	}
	if err := decodeAnyToTarget(value, target); err != nil {
		return errorf("decode_scope_block_failed scope=%s reason=%w", scope, err)
	}
	return nil
}

// newScopeTarget 按 out 的类型分配新的零值指针对象。
func newScopeTarget(out any) (any, error) {
	elemType, err := outElemType(out)
	if err != nil {
		return nil, err
	}
	return reflect.New(elemType).Interface(), nil
}

// outElemType 校验 out 指针并返回其元素类型。
func outElemType(out any) (reflect.Type, error) {
	value := reflect.ValueOf(out)
	if !value.IsValid() {
		return nil, errorForConfigOutInvalid(messagef("out_is_invalid"))
	}
	if value.Kind() != reflect.Ptr || value.IsNil() {
		return nil, errorForConfigOutPointerRequired(messagef("out_must_be_non_nil_pointer"))
	}
	return value.Elem().Type(), nil
}

// copyDecodedTarget 将解码后的指针值复制到目标指针。
func copyDecodedTarget(out any, decoded any) error {
	outValue := reflect.ValueOf(out)
	if !outValue.IsValid() || outValue.Kind() != reflect.Ptr || outValue.IsNil() {
		return errorForConfigOutPointerRequired(messagef("out_must_be_non_nil_pointer"))
	}
	decodedValue := reflect.ValueOf(decoded)
	if !decodedValue.IsValid() || decodedValue.Kind() != reflect.Ptr || decodedValue.IsNil() {
		return errorForConfigDecodedPointerRequired(messagef("decoded_must_be_non_nil_pointer"))
	}
	if !decodedValue.Elem().Type().AssignableTo(outValue.Elem().Type()) {
		return errorForConfigDecodedTypeMismatch(messagef(fmt.Sprintf("decoded_type_mismatch out=%s decoded=%s", outValue.Elem().Type(), decodedValue.Elem().Type())))
	}
	outValue.Elem().Set(decodedValue.Elem())
	return nil
}

// scopeTouched 判断本次 diff 是否触达给定 scope。
func scopeTouched(scope string, diff []Change) bool {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return len(diff) > 0
	}
	prefix := scope + "."
	for _, item := range diff {
		if item.Key == scope || strings.HasPrefix(item.Key, prefix) {
			return true
		}
	}
	return false
}
