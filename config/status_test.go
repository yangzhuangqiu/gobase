package config

import (
	"context"
	"errors"
	"testing"
	"time"
)

// statusTestSubscriber 用于分别控制初次回放和后续通知是否失败。
type statusTestSubscriber struct {
	fail bool
}

// Scope 订阅完整配置，确保测试刷新一定进入通知阶段。
func (this *statusTestSubscriber) Scope() string {
	return ""
}

// OnUpdate 按测试阶段返回预置失败，验证通知状态与刷新状态相互隔离。
func (this *statusTestSubscriber) OnUpdate(Update) error {
	if this.fail {
		return errors.New("subscriber failed")
	}
	return nil
}

// statusBlockingCloser 在 Close 阶段阻塞，供测试观测 closing 中间态。
type statusBlockingCloser struct {
	entered chan struct{}
	release chan struct{}
}

// Close 暴露已经进入资源释放阶段的信号，并等待测试允许结束。
func (this *statusBlockingCloser) Close() error {
	close(this.entered)
	<-this.release
	return nil
}

// TestStatusIsImmutablePointInTimeSnapshot 验证 Status 是不可变时点快照而非 Manager 实时视图。
// 测试方法：保存 version=1 状态后刷新并增加 watcher，断言旧 Status 保持原版本和资源计数，新 Status 反映最新事实。
func TestStatusIsImmutablePointInTimeSnapshot(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"name": "v1"}})
	before := manager.Status()
	manager.setRebuild(func(context.Context) (snapshotState, error) {
		return snapshotState{raw: map[string]any{"name": "v2"}}, nil
	})
	if err := manager.refresh(context.Background()); err != nil {
		t.Fatalf("refresh manager failed: %v", err)
	}
	manager.addWatcher(nil)
	after := manager.Status()

	if before.Snapshot().Version != 1 || before.Resources().WatchCount != 0 {
		t.Fatalf("old status changed after manager transition: snapshot=%+v resources=%+v", before.Snapshot(), before.Resources())
	}
	if after.Snapshot().Version != 2 || after.Resources().WatchCount != 1 || !after.Health().Healthy() || !after.Available() {
		t.Fatalf("unexpected latest status: snapshot=%+v resources=%+v health=%+v", after.Snapshot(), after.Resources(), after.Health())
	}
}

// TestStatusTracksRefreshDegradationAndRecovery 验证刷新失败只把可用旧快照降级，后续成功能够恢复健康。
// 测试方法：先让 rebuild 返回错误并断言 degraded/available 和错误码，再成功刷新并断言失败计数清零且历史失败仍可诊断。
func TestStatusTracksRefreshDegradationAndRecovery(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"name": "v1"}})
	manager.setRebuild(func(context.Context) (snapshotState, error) {
		return snapshotState{}, errors.New("remote unavailable")
	})
	if err := manager.refresh(context.Background()); err == nil {
		t.Fatal("expected refresh failure")
	}
	failed := manager.Status()
	if failed.Health().Level != HealthLevelDegraded || failed.Health().Reason != HealthReasonRefreshFailed || !failed.Available() {
		t.Fatalf("unexpected degraded health: %+v", failed.Health())
	}
	if failed.Refresh().LastResult != OperationResultFailed || failed.Refresh().ConsecutiveFailures != 1 ||
		failed.Refresh().LastFailure.Code != ErrCodeConfigManagerRefreshRebuildFailed {
		t.Fatalf("unexpected refresh failure status: %+v", failed.Refresh())
	}

	manager.setRebuild(func(context.Context) (snapshotState, error) {
		return snapshotState{raw: map[string]any{"name": "v2"}}, nil
	})
	if err := manager.refresh(context.Background()); err != nil {
		t.Fatalf("refresh recovery failed: %v", err)
	}
	recovered := manager.Status()
	if !recovered.Health().Healthy() || recovered.Refresh().LastResult != OperationResultSucceeded || recovered.Refresh().ConsecutiveFailures != 0 {
		t.Fatalf("unexpected recovered status: health=%+v refresh=%+v", recovered.Health(), recovered.Refresh())
	}
	if !recovered.Refresh().LastFailure.Valid {
		t.Fatal("expected last historical failure retained after recovery")
	}
}

// TestStatusExposesRefreshInProgress 验证 Status 能无锁观测正在执行的刷新，同时继续报告旧快照健康可用。
// 测试方法：阻塞 rebuild 并等待其进入，断言 InProgress 和尝试时间，再释放并断言刷新成功完成。
func TestStatusExposesRefreshInProgress(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"name": "v1"}})
	started := make(chan struct{})
	release := make(chan struct{})
	manager.setRebuild(func(context.Context) (snapshotState, error) {
		close(started)
		<-release
		return snapshotState{raw: map[string]any{"name": "v2"}}, nil
	})
	refreshDone := make(chan error, 1)
	go func() {
		refreshDone <- manager.refresh(context.Background())
	}()
	<-started
	running := manager.Status()
	if !running.Refresh().InProgress || running.Refresh().LastAttemptAt.IsZero() || !running.Health().Healthy() || !running.Available() {
		t.Fatalf("unexpected in-progress status: health=%+v refresh=%+v", running.Health(), running.Refresh())
	}

	close(release)
	select {
	case err := <-refreshDone:
		if err != nil {
			t.Fatalf("refresh failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting refresh finish")
	}
	finished := manager.Status()
	if finished.Refresh().InProgress || finished.Refresh().LastResult != OperationResultSucceeded {
		t.Fatalf("unexpected finished refresh status: %+v", finished.Refresh())
	}
}

// TestStatusSeparatesRefreshAndNotificationFailure 验证快照发布成功后 Subscriber 失败不会污染刷新健康状态。
// 测试方法：初次回放成功后开启 Subscriber 失败并刷新，断言 refresh=succeeded、notification=failed 且 Manager 仍健康可用。
func TestStatusSeparatesRefreshAndNotificationFailure(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"name": "v1"}})
	subscriber := &statusTestSubscriber{}
	if _, err := manager.Subscribe(subscriber); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	subscriber.fail = true
	manager.setRebuild(func(context.Context) (snapshotState, error) {
		return snapshotState{raw: map[string]any{"name": "v2"}}, nil
	})
	if err := manager.refresh(context.Background()); err == nil {
		t.Fatal("expected notification failure")
	}
	status := manager.Status()
	if status.Refresh().LastResult != OperationResultSucceeded || status.Notification().LastResult != OperationResultFailed {
		t.Fatalf("unexpected operation separation: refresh=%+v notification=%+v", status.Refresh(), status.Notification())
	}
	if !status.Health().Healthy() || !status.Available() || status.Notification().LastFailure.Code != ErrCodeConfigManagerRefreshSubscriberFailed {
		t.Fatalf("unexpected health after notification failure: health=%+v notification=%+v", status.Health(), status.Notification())
	}
}

// TestStatusExposesClosingAndClosedLifecycle 验证同步 Close 期间和完成后的生命周期、健康及资源语义。
// 测试方法：用阻塞 Session 暂停资源释放，依次断言 closing/unavailable/资源仍在处理，以及 closed/资源归零。
func TestStatusExposesClosingAndClosedLifecycle(t *testing.T) {
	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"name": "v1"}})
	closer := &statusBlockingCloser{entered: make(chan struct{}), release: make(chan struct{})}
	manager.addSession(closer)
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- manager.Close()
	}()
	<-closer.entered
	closing := manager.Status()
	if closing.State() != ManagerStateClosing || closing.Health().Reason != HealthReasonManagerClosing ||
		!closing.Available() || closing.Resources().ProviderSessionCount != 1 {
		t.Fatalf("unexpected closing status: state=%s health=%+v resources=%+v", closing.State(), closing.Health(), closing.Resources())
	}

	close(closer.release)
	if err := <-closeDone; err != nil {
		t.Fatalf("close manager failed: %v", err)
	}
	closed := manager.Status()
	if closed.State() != ManagerStateClosed || closed.Health().Reason != HealthReasonManagerClosed ||
		!closed.Available() || closed.Resources().ProviderSessionCount != 0 {
		t.Fatalf("unexpected closed status: state=%s health=%+v resources=%+v", closed.State(), closed.Health(), closed.Resources())
	}
}
