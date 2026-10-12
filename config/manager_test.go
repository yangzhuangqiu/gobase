package config

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testCloser 用于模拟 Close() 返回错误的 watcher。
type testCloser struct {
	err   error
	calls atomic.Int32
}

// orderedCloser 记录多个 Session 的关闭顺序。
type orderedCloser struct {
	name  string
	trace *[]string
}

// concurrentSessionCloser 记录务实关闭模型是否允许 Close 与模拟 Session.Load 阶段并发。
type concurrentSessionCloser struct {
	active            atomic.Bool
	closedWhileActive atomic.Bool
	closeCalls        atomic.Int32
}

// panicCloser 模拟扩展资源在 Close 阶段抛出可能包含敏感信息的 panic。
type panicCloser struct {
	// payload 是仅供测试触发 panic 的原始值。
	payload any
}

// Close 记录资源关闭时是否仍处于 Load 阶段。
func (this *concurrentSessionCloser) Close() error {
	if this.active.Load() {
		this.closedWhileActive.Store(true)
	}
	this.closeCalls.Add(1)
	return nil
}

// Close 抛出测试 payload，验证 Manager 只保留其动态类型。
func (this *panicCloser) Close() error {
	panic(this.payload)
}

// Close 把当前 Session 名称追加到共享顺序记录。
func (this *orderedCloser) Close() error {
	*this.trace = append(*this.trace, this.name)
	return nil
}

// Close 返回预置错误。
func (this *testCloser) Close() error {
	this.calls.Add(1)
	return this.err
}

func installInitialForTest(t *testing.T, manager *defaultManager, snapshot snapshotState) {
	t.Helper()
	if err := manager.installInitial(snapshot); err != nil {
		t.Fatalf("install initial snapshot failed: %v", err)
	}
}

// TestRefreshReturnsManagerClosedSentinel 验证关闭态 refresh 返回可分支哨兵错误。
func TestRefreshReturnsManagerClosedSentinel(t *testing.T) {
	manager := newDefaultManager()
	manager.mu.Lock()
	manager.changeManagerStateLocked(ManagerStateClosed)
	manager.mu.Unlock()

	err := manager.refresh(context.Background())
	if err == nil {
		t.Fatalf("expected refresh error when manager is closed")
	}
	if !errors.Is(err, errManagerClosed) {
		t.Fatalf("expected errors.Is(..., errManagerClosed), got: %v", err)
	}
	if !strings.Contains(err.Error(), "stage=precheck") {
		t.Fatalf("expected stage=precheck in error, got: %v", err)
	}
}

// TestRefreshReturnsRebuildNotSetSentinel 验证未注入 rebuild 时的错误语义。
func TestRefreshReturnsRebuildNotSetSentinel(t *testing.T) {
	manager := newDefaultManager()

	err := manager.refresh(context.Background())
	if err == nil {
		t.Fatalf("expected refresh error when rebuild function is not set")
	}
	if !errors.Is(err, errRebuildNotSet) {
		t.Fatalf("expected errors.Is(..., errRebuildNotSet), got: %v", err)
	}
	if !strings.Contains(err.Error(), "stage=precheck") {
		t.Fatalf("expected stage=precheck in error, got: %v", err)
	}
}

// TestRefreshWrapsRebuildErrorWithStage 验证 rebuild 异常会带 stage 上下文。
func TestRefreshWrapsRebuildErrorWithStage(t *testing.T) {
	manager := newDefaultManager()
	rebuildErr := errors.New("rebuild failed")
	manager.setRebuild(func(ctx context.Context) (snapshotState, error) {
		return snapshotState{}, rebuildErr
	})

	err := manager.refresh(context.Background())
	if err == nil {
		t.Fatalf("expected refresh error from rebuild")
	}
	if !errors.Is(err, rebuildErr) {
		t.Fatalf("expected errors.Is(..., rebuildErr), got: %v", err)
	}
	if !strings.Contains(err.Error(), "stage=rebuild_snapshot") {
		t.Fatalf("expected stage=rebuild_snapshot in error, got: %v", err)
	}
	assertErrorCode(t, err, ErrCodeConfigManagerRefreshRebuildFailed)
}

// TestRefreshRejectsPublishWhenSnapshotValidatorFails 验证 refresh 校验失败时保留旧快照。
func TestRefreshRejectsPublishWhenSnapshotValidatorFails(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"service": map[string]any{"address": ":8080"}}})
	manager.setSnapshotValidators([]SnapshotValidator{
		func(snapshot Snapshot) error {
			if snapshot.String("service.address", "") == ":9090" {
				return errors.New("reject_new_snapshot")
			}
			return nil
		},
	})
	manager.setRebuild(func(ctx context.Context) (snapshotState, error) {
		return snapshotState{raw: map[string]any{"service": map[string]any{"address": ":9090"}}}, nil
	})

	err := manager.refresh(context.Background())
	if err == nil {
		t.Fatalf("expected refresh validation error")
	}
	if !strings.Contains(err.Error(), "stage=validate_snapshot") {
		t.Fatalf("expected stage=validate_snapshot in error, got: %v", err)
	}
	assertErrorCode(t, err, ErrCodeConfigManagerSnapshotValidateFailed)

	latest := manager.Current()
	if latest == nil {
		t.Fatalf("expected latest snapshot")
	}
	if latest.String("service.address", "") != ":8080" {
		t.Fatalf("expected old snapshot to be kept, got: %s", latest.String("service.address", ""))
	}
	if latest.Version() != 1 {
		t.Fatalf("expected version to stay 1, got: %d", latest.Version())
	}
}

// TestInstallInitialConvertsSnapshotValidatorPanicToError 验证校验器 panic 会转为可观测错误。
func TestInstallInitialConvertsSnapshotValidatorPanicToError(t *testing.T) {
	manager := newDefaultManager()
	manager.setSnapshotValidators([]SnapshotValidator{
		func(snapshot Snapshot) error {
			panic("validator panic")
		},
	})

	err := manager.installInitial(snapshotState{raw: map[string]any{"service": map[string]any{"address": ":8080"}}})
	if err == nil {
		t.Fatalf("expected install initial validation error")
	}
	if !strings.Contains(err.Error(), "stage=validate_snapshot") {
		t.Fatalf("expected stage=validate_snapshot in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "validator_panic") {
		t.Fatalf("expected validator_panic marker in error, got: %v", err)
	}
	assertErrorCode(t, err, ErrCodeConfigManagerSnapshotValidateFailed)
}

// TestCloseWrapsWatcherCloseErrorWithStage 验证 watcher 关闭失败可观测。
func TestCloseWrapsWatcherCloseErrorWithStage(t *testing.T) {
	manager := newDefaultManager()
	closeErr := errors.New("close watcher failed")
	manager.watchClosers = []io.Closer{&testCloser{err: closeErr}}

	err := manager.Close()
	if err == nil {
		t.Fatalf("expected close error")
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("expected errors.Is(..., closeErr), got: %v", err)
	}
	if !strings.Contains(err.Error(), "stage=close_watcher") {
		t.Fatalf("expected stage=close_watcher in error, got: %v", err)
	}
	assertErrorCode(t, err, ErrCodeConfigManagerCloseWatcherFailed)
}

// TestCloseReleasesProviderSessions 验证 Manager 在 watcher 之后释放 Provider Sessions。
// 测试方法：注入 Session closer，连续调用 Close，断言 Session 只关闭一次且状态被清空。
func TestCloseReleasesProviderSessions(t *testing.T) {
	manager := newDefaultManager()
	resource := &testCloser{}
	manager.addSession(resource)
	if err := manager.Close(); err != nil {
		t.Fatalf("close manager failed: %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("repeat close manager failed: %v", err)
	}
	if calls := resource.calls.Load(); calls != 1 {
		t.Fatalf("expected resource closed once, got=%d", calls)
	}
}

// TestCloseReleasesProviderSessionsInReverseOrder 验证 Session 按 Open 的逆序释放。
// 测试方法：按 first/second 顺序接管 Session，调用 Close 并断言关闭顺序为 second/first。
func TestCloseReleasesProviderSessionsInReverseOrder(t *testing.T) {
	trace := make([]string, 0, 2)
	manager := newDefaultManager()
	manager.addSession(&orderedCloser{name: "first", trace: &trace})
	manager.addSession(&orderedCloser{name: "second", trace: &trace})

	if err := manager.Close(); err != nil {
		t.Fatalf("close manager failed: %v", err)
	}
	if strings.Join(trace, ",") != "second,first" {
		t.Fatalf("expected reverse session close order, got=%v", trace)
	}
}

// TestCloseWrapsProviderSessionError 验证 Provider Session 关闭错误保留稳定 code 和阶段。
// 测试方法：注入返回错误的 Session closer，调用 Close 并断言错误链和 close_session stage。
func TestCloseWrapsProviderSessionError(t *testing.T) {
	manager := newDefaultManager()
	closeErr := errors.New("close provider resource failed")
	manager.addSession(&testCloser{err: closeErr})
	err := manager.Close()
	if !errors.Is(err, closeErr) {
		t.Fatalf("expected resource close cause, got=%v", err)
	}
	assertErrorCode(t, err, ErrCodeConfigManagerCloseResourceFailed)
	assertErrorContains(t, err, "stage=close_session")
}

// TestCloseRedactsResourcePanicPayload 验证资源关闭 panic 不会把敏感 payload 写入错误链。
// 测试方法：让 Session.Close 抛出包含模拟密钥的字符串，断言错误只包含 payload 类型和关闭阶段。
func TestCloseRedactsResourcePanicPayload(t *testing.T) {
	const sensitivePayload = "token=close-secret"
	manager := newDefaultManager()
	manager.addSession(&panicCloser{payload: sensitivePayload})

	err := manager.Close()
	if err == nil {
		t.Fatalf("expected resource close panic error")
	}
	assertErrorContains(t, err, "stage=close_session")
	assertErrorContains(t, err, "payload_type=string")
	if strings.Contains(err.Error(), sensitivePayload) {
		t.Fatalf("resource close panic error leaked payload: %v", err)
	}
}

// TestDiffSnapshotsTreatsTypeChangeAsModified 验证类型变化也算配置变更。
func TestDiffSnapshotsTreatsTypeChangeAsModified(t *testing.T) {
	oldSnapshot := &snapshotState{
		flat: map[string]any{"worker.concurrency": 1},
		meta: map[string]ValueMeta{"worker.concurrency": {Source: SourceKindDefault, Layer: "default", Priority: 100, Version: 1}},
	}
	newSnapshot := &snapshotState{
		flat: map[string]any{"worker.concurrency": "1"},
		meta: map[string]ValueMeta{"worker.concurrency": {Source: SourceKindFile, Layer: "file", Priority: 200, Version: 2}},
	}

	diff := diffSnapshots(oldSnapshot, newSnapshot)
	if len(diff) != 1 {
		t.Fatalf("expected one diff item, got: %d", len(diff))
	}
	if diff[0].Key != "worker.concurrency" {
		t.Fatalf("expected key worker.concurrency, got: %s", diff[0].Key)
	}
	if diff[0].Old != 1 {
		t.Fatalf("expected old value 1, got: %#v", diff[0].Old)
	}
	if diff[0].New != "1" {
		t.Fatalf("expected new value \"1\", got: %#v", diff[0].New)
	}
}

// countingSubscriber 用于统计订阅回调次数。
type countingSubscriber struct {
	// scope 是订阅者关注的配置范围。
	scope string
	// reloadCnt 记录回调执行次数。
	reloadCnt atomic.Int32
	// err 是回调完成后返回的可控错误。
	err error
}

// Scope 返回监听范围。
func (this *countingSubscriber) Scope() string {
	return this.scope
}

// OnUpdate 增加回调计数。
func (this *countingSubscriber) OnUpdate(Update) error {
	this.reloadCnt.Add(1)
	return this.err
}

// TestRefreshWithPublishStatusReportsPublishedBeforeSubscriberError 验证发布结果不受 Subscriber 错误影响。
// 测试方法：先成功注册订阅者，再让刷新回调返回错误，断言 Snapshot 已更新且 published=true。
func TestRefreshWithPublishStatusReportsPublishedBeforeSubscriberError(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"service": map[string]any{"address": ":8080"}}})
	manager.setRebuild(func(context.Context) (snapshotState, error) {
		return snapshotState{raw: map[string]any{"service": map[string]any{"address": ":9090"}}}, nil
	})

	subscriberErr := errors.New("subscriber failed")
	subscriber := &countingSubscriber{scope: "service"}
	if _, err := manager.Subscribe(subscriber); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	subscriber.err = subscriberErr

	published, err := manager.refreshWithPublishStatus(context.Background())
	if !published {
		t.Fatalf("expected snapshot to be published before subscriber error")
	}
	if !errors.Is(err, subscriberErr) {
		t.Fatalf("expected subscriber error, got: %v", err)
	}
	if actual := manager.Current().String("service.address", ""); actual != ":9090" {
		t.Fatalf("expected published address :9090, got: %s", actual)
	}
}

// TestRefreshReturnsManagerClosedWhenCloseHappensDuringRebuild 验证 Close 与 refresh 并发时不发布新快照。
func TestRefreshReturnsManagerClosedWhenCloseHappensDuringRebuild(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"service": map[string]any{"address": ":8080"}}})

	rebuildStarted := make(chan struct{})
	rebuildRelease := make(chan struct{})
	manager.setRebuild(func(ctx context.Context) (snapshotState, error) {
		close(rebuildStarted)
		<-rebuildRelease
		return snapshotState{raw: map[string]any{"service": map[string]any{"address": ":9090"}}}, nil
	})

	refreshErrCh := make(chan error, 1)
	go func() {
		refreshErrCh <- manager.refresh(context.Background())
	}()

	<-rebuildStarted
	if err := manager.Close(); err != nil {
		t.Fatalf("close manager failed: %v", err)
	}
	close(rebuildRelease)

	refreshErr := <-refreshErrCh
	if refreshErr == nil {
		t.Fatalf("expected refresh to fail after close")
	}
	if !errors.Is(refreshErr, errManagerClosed) {
		t.Fatalf("expected errors.Is(..., errManagerClosed), got: %v", refreshErr)
	}
	if !strings.Contains(refreshErr.Error(), "stage=publish_precheck") {
		t.Fatalf("expected stage=publish_precheck, got: %v", refreshErr)
	}

	latest := manager.Current()
	if latest == nil {
		t.Fatalf("expected latest snapshot")
	}
	if latest.String("service.address", "") != ":8080" {
		t.Fatalf("expected latest service.address to stay :8080, got: %s", latest.String("service.address", ""))
	}
	if latest.Version() != 1 {
		t.Fatalf("expected latest version stay 1, got: %d", latest.Version())
	}
}

// TestCloseDoesNotWaitForSessionUsingRebuild 验证务实关闭模型不 join 已开始的 Provider Load。
// 测试方法：让 rebuild 持有模拟 Load 阶段，调用 Close 后断言 Session 立即关闭；随后释放 rebuild，并断言关闭后的候选未发布。
func TestCloseDoesNotWaitForSessionUsingRebuild(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"service": map[string]any{"address": ":8080"}}})
	resource := &concurrentSessionCloser{}
	manager.addSession(resource)

	rebuildStarted := make(chan struct{})
	rebuildRelease := make(chan struct{})
	manager.setRebuild(func(context.Context) (snapshotState, error) {
		resource.active.Store(true)
		close(rebuildStarted)
		<-rebuildRelease
		resource.active.Store(false)
		return snapshotState{raw: map[string]any{"service": map[string]any{"address": ":9090"}}}, nil
	})

	refreshErrCh := make(chan error, 1)
	go func() { refreshErrCh <- manager.refresh(context.Background()) }()
	<-rebuildStarted

	if err := manager.Close(); err != nil {
		t.Fatalf("close manager failed: %v", err)
	}
	if !resource.closedWhileActive.Load() || resource.closeCalls.Load() != 1 {
		t.Fatalf("expected one concurrent session close, concurrent=%v calls=%d", resource.closedWhileActive.Load(), resource.closeCalls.Load())
	}

	close(rebuildRelease)
	if err := <-refreshErrCh; err == nil || !errors.Is(err, errManagerClosed) {
		t.Fatalf("expected closed refresh result, got=%v", err)
	}
	if actual := manager.Current().String("service.address", ""); actual != ":8080" {
		t.Fatalf("closed manager published rebuild result: %s", actual)
	}
}

// TestRefreshSkipsSubscriberNotifyWhenContextCanceled 验证取消上下文后跳过订阅通知。
func TestRefreshSkipsSubscriberNotifyWhenContextCanceled(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"service": map[string]any{"address": ":8080"}}})

	subscriber := &countingSubscriber{}
	_, err := manager.Subscribe(subscriber)
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	if subscriber.reloadCnt.Load() != 1 {
		t.Fatalf("expected initial subscriber count 1, got: %d", subscriber.reloadCnt.Load())
	}

	manager.setRebuild(func(ctx context.Context) (snapshotState, error) {
		return snapshotState{raw: map[string]any{"service": map[string]any{"address": ":9090"}}}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = manager.refresh(ctx)
	if err == nil {
		t.Fatalf("expected refresh to fail when context canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected errors.Is(..., context.Canceled), got: %v", err)
	}
	if !strings.Contains(err.Error(), "stage=notify_precheck") {
		t.Fatalf("expected stage=notify_precheck, got: %v", err)
	}
	assertErrorCode(t, err, ErrCodeConfigManagerRefreshNotifyPrecheckFailed)
	if subscriber.reloadCnt.Load() != 1 {
		t.Fatalf("expected subscriber not notified after canceled refresh, got: %d", subscriber.reloadCnt.Load())
	}

	latest := manager.Current()
	if latest == nil {
		t.Fatalf("expected latest snapshot")
	}
	if latest.String("service.address", "") != ":9090" {
		t.Fatalf("expected latest service.address to publish :9090, got: %s", latest.String("service.address", ""))
	}
}

// serviceAddressConfig 用于验证 service scope 的回调值。
type serviceAddressConfig struct {
	Address string `yaml:"address"`
}

// closeMatrixServiceConfig 用于 Close 后注册回放的类型断言。
type closeMatrixServiceConfig struct {
	Address string `yaml:"address"`
}

// closeMatrixSubscriber 是 Close 边界矩阵测试的最小订阅者实现。
type closeMatrixSubscriber struct {
	scope string
}

// Scope 返回监听范围。
func (this *closeMatrixSubscriber) Scope() string {
	return this.scope
}

// OnUpdate 无副作用；仅用于接口满足。
func (this *closeMatrixSubscriber) OnUpdate(Update) error { return nil }

// addressSubscriber 记录 service address 的回调历史。
type addressSubscriber struct {
	scope       string
	mu          sync.Mutex
	reloadCnt   int
	lastAddress string
}

// Scope 返回监听范围。
func (this *addressSubscriber) Scope() string {
	return this.scope
}

// OnUpdate 解码并记录最新 address 与回调次数。
func (this *addressSubscriber) OnUpdate(update Update) error {
	var typed serviceAddressConfig
	if err := update.Snapshot.Decode(this.scope, &typed); err != nil {
		return err
	}
	this.mu.Lock()
	defer this.mu.Unlock()
	this.reloadCnt++
	this.lastAddress = typed.Address
	return nil
}

// snapshot 返回当前统计快照。
func (this *addressSubscriber) snapshot() (reloadCnt int, lastAddress string) {
	this.mu.Lock()
	defer this.mu.Unlock()
	return this.reloadCnt, this.lastAddress
}

// blockingSubscriber 用于构造 Subscribe 初次回放与 Close 的并发窗口。
type blockingSubscriber struct {
	scope   string
	started chan struct{}
	release <-chan struct{}
	once    sync.Once
}

// Scope 返回监听范围。
func (this *blockingSubscriber) Scope() string {
	return this.scope
}

// OnUpdate 首次进入时发出 started 信号，并等待 release 放行。
func (this *blockingSubscriber) OnUpdate(Update) error {
	this.once.Do(func() {
		close(this.started)
	})
	if this.release != nil {
		<-this.release
	}
	return nil
}

// TestRefreshNotifiesSubscriberRegisteredDuringRebuild 验证 rebuild 期间新订阅不会漏通知。
func TestRefreshNotifiesSubscriberRegisteredDuringRebuild(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"service": map[string]any{"address": ":8080"}}})

	rebuildStarted := make(chan struct{})
	rebuildRelease := make(chan struct{})
	manager.setRebuild(func(ctx context.Context) (snapshotState, error) {
		close(rebuildStarted)
		<-rebuildRelease
		return snapshotState{raw: map[string]any{"service": map[string]any{"address": ":9090"}}}, nil
	})

	refreshErrCh := make(chan error, 1)
	go func() {
		refreshErrCh <- manager.refresh(context.Background())
	}()

	<-rebuildStarted
	subscriber := &addressSubscriber{scope: "service"}
	_, err := manager.Subscribe(subscriber)
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	close(rebuildRelease)
	if refreshErr := <-refreshErrCh; refreshErr != nil {
		t.Fatalf("refresh failed: %v", refreshErr)
	}

	reloadCnt, lastAddress := subscriber.snapshot()
	if reloadCnt != 2 {
		t.Fatalf("expected listener to receive initial + refresh reload, got: %d", reloadCnt)
	}
	if lastAddress != ":9090" {
		t.Fatalf("expected listener last address :9090, got: %s", lastAddress)
	}
}

// TestCloseConcurrentSubscribeAndRefresh 验证 Close 与两次 Subscribe/refresh 并发时语义可收敛。
func TestCloseConcurrentSubscribeAndRefresh(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"service": map[string]any{"address": ":8080"}}})

	rebuildStarted := make(chan struct{})
	rebuildRelease := make(chan struct{})
	reloadStarted := make(chan struct{})
	reloadRelease := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() {
		releaseOnce.Do(func() {
			close(rebuildRelease)
			close(reloadRelease)
		})
	}
	defer releaseAll()

	manager.setRebuild(func(ctx context.Context) (snapshotState, error) {
		close(rebuildStarted)
		<-rebuildRelease
		return snapshotState{raw: map[string]any{"service": map[string]any{"address": ":9090"}}}, nil
	})

	refreshErrCh := make(chan error, 1)
	go func() {
		refreshErrCh <- manager.refresh(context.Background())
	}()

	select {
	case <-rebuildStarted:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting rebuild start")
	}

	registerSubscriber := &blockingSubscriber{
		scope:   "service",
		started: reloadStarted,
		release: reloadRelease,
	}
	registerResultCh := make(chan error, 1)
	registerCancelCh := make(chan Subscription, 1)
	go func() {
		subscription, err := manager.Subscribe(registerSubscriber)
		if err == nil {
			registerCancelCh <- subscription
		}
		registerResultCh <- err
	}()

	select {
	case <-reloadStarted:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting register initial reload start")
	}

	bindResultCh := make(chan error, 1)
	bindCancelCh := make(chan Subscription, 1)
	go func() {
		subscription, err := manager.Subscribe(&closeMatrixSubscriber{scope: "service"})
		if err == nil {
			bindCancelCh <- subscription
		}
		bindResultCh <- err
	}()

	closeErrCh := make(chan error, 1)
	go func() {
		closeErrCh <- manager.Close()
	}()
	if !waitForCondition(time.Second, func() bool { return manager.Status().State() != ManagerStateRunning }) {
		t.Fatalf("timeout waiting manager enter stopping state")
	}
	releaseAll()
	if err := <-closeErrCh; err != nil {
		t.Fatalf("close manager failed: %v", err)
	}

	refreshErr := <-refreshErrCh
	if refreshErr == nil {
		t.Fatalf("expected refresh error when close races with rebuild")
	}
	if !errors.Is(refreshErr, errManagerClosed) {
		t.Fatalf("expected errors.Is(..., errManagerClosed), got: %v", refreshErr)
	}
	if !strings.Contains(refreshErr.Error(), "stage=publish_precheck") {
		t.Fatalf("expected stage=publish_precheck in refresh error, got: %v", refreshErr)
	}

	registerErr := <-registerResultCh
	if registerErr != nil {
		if !errors.Is(registerErr, errManagerClosed) {
			t.Fatalf("expected register err nil or manager closed, got: %v", registerErr)
		}
		if !strings.Contains(registerErr.Error(), "stage=precheck") && !strings.Contains(registerErr.Error(), "stage=post_replay_precheck") {
			t.Fatalf("unexpected subscribe close stage, got: %v", registerErr)
		}
	} else {
		subscription := <-registerCancelCh
		if subscription == nil {
			t.Fatalf("expected subscription")
		}
		subscription.Cancel()
		subscription.Cancel()
	}

	bindErr := <-bindResultCh
	if bindErr != nil {
		if !errors.Is(bindErr, errManagerClosed) {
			t.Fatalf("expected subscribe err nil or manager closed, got: %v", bindErr)
		}
		if !strings.Contains(bindErr.Error(), "stage=precheck") && !strings.Contains(bindErr.Error(), "stage=post_replay_precheck") {
			t.Fatalf("unexpected subscribe close stage, got: %v", bindErr)
		}
	} else {
		subscription := <-bindCancelCh
		if subscription == nil {
			t.Fatalf("expected subscription")
		}
		subscription.Cancel()
		subscription.Cancel()
	}

	latest := manager.Current()
	if latest == nil {
		t.Fatalf("expected latest snapshot")
	}
	if latest.String("service.address", "") != ":8080" {
		t.Fatalf("expected latest service.address keep :8080, got: %s", latest.String("service.address", ""))
	}
	if latest.Version() != 1 {
		t.Fatalf("expected latest version keep 1, got: %d", latest.Version())
	}

	manager.mu.RLock()
	listenerCount := len(manager.subscribers)
	manager.mu.RUnlock()
	if listenerCount != 0 {
		t.Fatalf("expected no retained listeners after close, got: %d", listenerCount)
	}
}

// TestSnapshotAccessorsReturnImmutableCopies 验证 Raw/Flat 返回值修改不会污染内部快照。
func TestSnapshotAccessorsReturnImmutableCopies(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{
		raw: map[string]any{
			"service": map[string]any{"address": ":8080"},
		},
		meta: map[string]ValueMeta{
			"service.address": {Source: SourceKindDefault, Layer: "default", Priority: 100},
		},
	})

	snapshot := manager.Current()
	if snapshot == nil {
		t.Fatalf("expected latest snapshot")
	}

	raw := snapshot.Raw()
	service, ok := raw["service"].(map[string]any)
	if !ok {
		t.Fatalf("expected service map in raw snapshot")
	}
	service["address"] = ":9999"
	raw["new"] = "raw_mutation"

	flat := snapshot.Flat()
	flat["service.address"] = ":9999"
	flat["new"] = "flat_mutation"

	if got := snapshot.String("service.address", ""); got != ":8080" {
		t.Fatalf("expected immutable snapshot value :8080, got: %s", got)
	}
	if _, exists := snapshot.Get("new"); exists {
		t.Fatalf("expected no key new in immutable snapshot")
	}

	rawAgain := snapshot.Raw()
	serviceAgain, ok := rawAgain["service"].(map[string]any)
	if !ok {
		t.Fatalf("expected service map in second raw snapshot")
	}
	if serviceAgain["address"] != ":8080" {
		t.Fatalf("expected raw copy unaffected, got: %#v", serviceAgain["address"])
	}
	if _, exists := rawAgain["new"]; exists {
		t.Fatalf("expected no raw new key after mutation")
	}

	flatAgain := snapshot.Flat()
	if flatAgain["service.address"] != ":8080" {
		t.Fatalf("expected flat copy unaffected, got: %#v", flatAgain["service.address"])
	}
	if _, exists := flatAgain["new"]; exists {
		t.Fatalf("expected no flat new key after mutation")
	}

	meta, exists := snapshot.Explain("service.address")
	if !exists || meta.Layer != "default" {
		t.Fatalf("expected immutable explain metadata, got=%+v exists=%v", meta, exists)
	}
}

// TestCloseBehaviorMatrixAfterClose 验证 Close 后 Subscribe/Observe/refresh 的边界语义。
func TestCloseBehaviorMatrixAfterClose(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"service": map[string]any{"address": ":8080"}}})

	if err := manager.Close(); err != nil {
		t.Fatalf("close manager failed: %v", err)
	}

	t.Run("subscribe_after_close", func(t *testing.T) {
		_, err := manager.Subscribe(&closeMatrixSubscriber{scope: "service"})
		if err == nil {
			t.Fatalf("expected subscribe error after close")
		}
		if !errors.Is(err, errManagerClosed) {
			t.Fatalf("expected errors.Is(..., errManagerClosed), got: %v", err)
		}
		if !strings.Contains(err.Error(), "stage=precheck") {
			t.Fatalf("expected stage=precheck in subscribe error, got: %v", err)
		}
	})

	t.Run("observe_after_close", func(t *testing.T) {
		var target closeMatrixServiceConfig
		err := manager.Observe("service").Decode(&target)
		if err != nil || target.Address != ":8080" {
			t.Fatalf("expected Observe to retain final snapshot after close, target=%+v err=%v", target, err)
		}
	})

	t.Run("refresh_after_close", func(t *testing.T) {
		err := manager.refresh(context.Background())
		if err == nil {
			t.Fatalf("expected refresh error after close")
		}
		if !errors.Is(err, errManagerClosed) {
			t.Fatalf("expected errors.Is(..., errManagerClosed), got: %v", err)
		}
		if !strings.Contains(err.Error(), "stage=precheck") {
			t.Fatalf("expected stage=precheck in refresh error, got: %v", err)
		}
	})

	t.Run("close_is_idempotent", func(t *testing.T) {
		if err := manager.Close(); err != nil {
			t.Fatalf("expected idempotent close, got: %v", err)
		}
	})
}
