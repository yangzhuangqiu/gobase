package config

import (
	"time"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// Status 定义 Config Manager 某一时刻的只读运行状态快照。
//
// 典型调用场景：健康检查、运行诊断和资源观测。调用方取得的 Status 不会随 Manager 后续变化。
type Status interface {
	statusMarker()

	// State 返回 Config Manager 生命周期状态。
	State() ManagerState
	// Health 返回配置读取与刷新能力的健康状态。
	Health() HealthStatus
	// Snapshot 返回当前已发布配置快照的运行元信息。
	Snapshot() SnapshotStatus
	// Refresh 返回最近一次配置刷新及当前刷新阶段状态。
	Refresh() RefreshStatus
	// Notification 返回最近一次已发布变更的 Subscriber 通知状态。
	Notification() NotificationStatus
	// Resources 返回 Manager 当前接管的运行资源数量。
	Resources() ResourceStatus
	// Available 报告当前是否仍有可读取的有效配置快照。
	Available() bool
}

// ManagerState 表示 Config Manager 生命周期阶段。
type ManagerState string

const (
	// ManagerStateUnknown 表示 Manager 尚未完成首个快照发布。
	ManagerStateUnknown ManagerState = ""
	// ManagerStateRunning 表示 Manager 正常提供读取、刷新和订阅能力。
	ManagerStateRunning ManagerState = "running"
	// ManagerStateClosing 表示 Manager 已拒绝新操作，正在释放运行资源。
	ManagerStateClosing ManagerState = "closing"
	// ManagerStateClosed 表示 Manager 已完成关闭；最后快照仍允许读取。
	ManagerStateClosed ManagerState = "closed"
)

// HealthLevel 表示 Manager 核心配置能力的健康等级。
type HealthLevel string

const (
	// HealthLevelUnknown 表示尚无足够事实判断健康状态。
	HealthLevelUnknown HealthLevel = ""
	// HealthLevelHealthy 表示有效快照可用，且最近一次刷新没有失败。
	HealthLevelHealthy HealthLevel = "healthy"
	// HealthLevelDegraded 表示旧快照仍可用，但最近一次刷新失败。
	HealthLevelDegraded HealthLevel = "degraded"
	// HealthLevelUnavailable 表示 Manager 无法继续提供完整运行能力。
	HealthLevelUnavailable HealthLevel = "unavailable"
)

// HealthReason 表示健康等级的稳定、可分支原因。
type HealthReason string

const (
	// HealthReasonSnapshotMissing 表示尚未发布有效快照。
	HealthReasonSnapshotMissing HealthReason = "snapshot_missing"
	// HealthReasonReady 表示 Manager 正在使用有效快照正常运行。
	HealthReasonReady HealthReason = "ready"
	// HealthReasonRefreshFailed 表示最近一次刷新失败，当前继续使用旧快照。
	HealthReasonRefreshFailed HealthReason = "refresh_failed"
	// HealthReasonManagerClosing 表示 Manager 正在关闭。
	HealthReasonManagerClosing HealthReason = "manager_closing"
	// HealthReasonManagerClosed 表示 Manager 已关闭。
	HealthReasonManagerClosed HealthReason = "manager_closed"
)

// OperationResult 表示一次运行操作最近的完成结果。
type OperationResult string

const (
	// OperationResultUnknown 表示操作尚未发生或尚未完成。
	OperationResultUnknown OperationResult = ""
	// OperationResultSucceeded 表示最近一次操作成功完成。
	OperationResultSucceeded OperationResult = "succeeded"
	// OperationResultFailed 表示最近一次操作以失败结束。
	OperationResultFailed OperationResult = "failed"
)

// HealthStatus 描述 Manager 核心配置能力的派生健康状态。
type HealthStatus struct {
	// Level 是 healthy/degraded/unavailable 健康等级。
	Level HealthLevel
	// Reason 是当前等级的稳定原因码。
	Reason HealthReason
	// Since 是当前 Level 与 Reason 组合开始生效的时间。
	Since time.Time
}

// Healthy 判断当前健康等级是否为完全健康。
func (this HealthStatus) Healthy() bool {
	return this.Level == HealthLevelHealthy
}

// SnapshotStatus 描述当前已发布配置快照的运行元信息。
type SnapshotStatus struct {
	// Available 表示当前存在可读取且已经通过校验的快照。
	Available bool
	// Version 是当前快照版本；尚未发布时为 0。
	Version uint64
	// PublishedAt 是当前快照的发布时间；尚未发布时为零值。
	PublishedAt time.Time
}

// OperationFailure 描述最近一次操作失败的安全诊断信息。
type OperationFailure struct {
	// Valid 表示当前结构是否包含一次真实失败。
	Valid bool
	// At 是失败被 Manager 确认的时间。
	At time.Time
	// Code 是错误链最外层 Config 语义错误码；无法提取时为空。
	Code ErrorCode
	// Op 是失败所属稳定操作名，不包含配置内容。
	Op string
	// Stage 是失败发生的稳定阶段名，不包含配置内容。
	Stage string
}

// RefreshStatus 描述配置刷新操作的当前阶段和最近结果。
type RefreshStatus struct {
	// InProgress 表示当前存在一次已经开始但尚未完成的刷新。
	InProgress bool
	// LastResult 是最近一次已完成刷新的结果；尚未刷新时为空。
	LastResult OperationResult
	// LastAttemptAt 是最近一次刷新开始时间。
	LastAttemptAt time.Time
	// LastSuccessAt 是最近一次成功发布快照的时间。
	LastSuccessAt time.Time
	// LastFailure 保留最近一次刷新失败的安全诊断，即使后续刷新已经恢复。
	LastFailure OperationFailure
	// ConsecutiveFailures 是最近连续失败次数；刷新成功后归零。
	ConsecutiveFailures uint64
}

// NotificationStatus 描述已发布变更向 Subscriber 投递的当前阶段和最近结果。
type NotificationStatus struct {
	// InProgress 表示当前存在一次已经开始但尚未结束的通知轮次。
	InProgress bool
	// LastResult 是最近一次已完成通知轮次的结果；尚未通知时为空。
	LastResult OperationResult
	// LastAttemptAt 是最近一次通知轮次开始时间。
	LastAttemptAt time.Time
	// LastSuccessAt 是最近一次全部目标 Subscriber 通知成功的时间。
	LastSuccessAt time.Time
	// LastFailure 保留最近一次通知失败的安全诊断。
	LastFailure OperationFailure
}

// ResourceStatus 描述 Manager 当前拥有的运行资源数量。
type ResourceStatus struct {
	// SubscriptionCount 是当前仍有效的 push 订阅数量。
	SubscriptionCount int
	// WatchCount 是成功注册的 watcher 数量，包含没有独立 closer 的 watcher。
	WatchCount int
	// ProviderSessionCount 是 Manager 接管且尚未释放的 Provider Session 数量。
	ProviderSessionCount int
}

// statusSnapshot 是 Status 的不可变内部实现；发布后不得修改任何字段。
type statusSnapshot struct {
	manager      ManagerState
	health       HealthStatus
	snapshot     SnapshotStatus
	refresh      RefreshStatus
	notification NotificationStatus
	resources    ResourceStatus
}

var _ Status = (*statusSnapshot)(nil)

// statusForNilManager 是 typed nil Manager 的稳定关闭态返回值。
var statusForNilManager = func() *statusSnapshot {
	now := time.Now()
	result := newInitialStatus(now)
	result.manager = ManagerStateClosed
	result.health = deriveHealth(result, result.health, now)
	return result
}()

// statusMarker 封闭 Status 实现集合，确保内核可以安全演进状态存储。
func (this *statusSnapshot) statusMarker() {}

// State 返回该不可变快照记录的 Manager 生命周期阶段。
func (this *statusSnapshot) State() ManagerState { return this.manager }

// Health 返回该不可变快照记录的健康状态。
func (this *statusSnapshot) Health() HealthStatus { return this.health }

// Snapshot 返回该不可变快照记录的配置快照元信息。
func (this *statusSnapshot) Snapshot() SnapshotStatus { return this.snapshot }

// Refresh 返回该不可变快照记录的刷新状态。
func (this *statusSnapshot) Refresh() RefreshStatus { return this.refresh }

// Notification 返回该不可变快照记录的通知状态。
func (this *statusSnapshot) Notification() NotificationStatus { return this.notification }

// Resources 返回该不可变快照记录的资源数量。
func (this *statusSnapshot) Resources() ResourceStatus { return this.resources }

// Available 判断当前是否存在可读取的有效配置快照。
func (this *statusSnapshot) Available() bool {
	return this != nil && this.snapshot.Available
}

// newInitialStatus 创建尚未发布首个快照的初始状态。
func newInitialStatus(now time.Time) *statusSnapshot {
	return &statusSnapshot{
		manager: ManagerStateUnknown,
		health:  HealthStatus{Level: HealthLevelUnavailable, Reason: HealthReasonSnapshotMissing, Since: now},
	}
}

// operationFailureOf 把完整错误收敛为可公开诊断且不包含错误文本的状态。
func operationFailureOf(now time.Time, op string, stage string, err error) OperationFailure {
	code, _ := errcode.CodeOf[ErrorCode](err)
	return OperationFailure{Valid: true, At: now, Code: code, Op: op, Stage: stage}
}

// deriveHealth 从生命周期、快照和刷新事实计算健康状态，避免维护第二套可漂移状态源。
func deriveHealth(current *statusSnapshot, previous HealthStatus, now time.Time) HealthStatus {
	next := HealthStatus{}
	// 生命周期终止优先于快照可读性：关闭后的快照只提供兼容读取，不代表 Manager 仍可运行。
	switch current.manager {
	case ManagerStateClosing:
		next = HealthStatus{Level: HealthLevelUnavailable, Reason: HealthReasonManagerClosing}
	case ManagerStateClosed:
		next = HealthStatus{Level: HealthLevelUnavailable, Reason: HealthReasonManagerClosed}
	default:
		// 运行态先保证存在已验证快照，再根据最近一次刷新结果判断是否降级。
		if !current.snapshot.Available {
			next = HealthStatus{Level: HealthLevelUnavailable, Reason: HealthReasonSnapshotMissing}
		} else if current.refresh.LastResult == OperationResultFailed {
			next = HealthStatus{Level: HealthLevelDegraded, Reason: HealthReasonRefreshFailed}
		} else {
			next = HealthStatus{Level: HealthLevelHealthy, Reason: HealthReasonReady}
		}
	}
	if previous.Level == next.Level && previous.Reason == next.Reason && !previous.Since.IsZero() {
		next.Since = previous.Since
	} else {
		next.Since = now
	}
	return next
}

// publishStatusLocked 基于上一份不可变状态生成并原子发布下一份状态。
// 前置条件：调用方必须持有 defaultManager.mu 写锁，保证多个状态事件不会互相覆盖。
func (this *defaultManager) publishStatusLocked(mutate func(next *statusSnapshot, now time.Time)) {
	previous := this.status.Load()
	next := statusSnapshot{}
	if previous != nil {
		next = *previous
	}
	now := time.Now()
	previousHealth := next.health
	mutate(&next, now)
	next.health = deriveHealth(&next, previousHealth, now)
	this.status.Store(&next)
}

// managerStoppingLocked 判断 Manager 是否已经拒绝新的运行操作。
// 前置条件：调用方必须至少持有 defaultManager.mu 读锁。
func (this *defaultManager) managerStoppingLocked() bool {
	status := this.status.Load()
	return status != nil && (status.manager == ManagerStateClosing || status.manager == ManagerStateClosed)
}

// updateResourceStatusLocked 从 Manager 当前资源集合发布准确计数。
// 前置条件：调用方必须持有 defaultManager.mu 写锁，并已经完成资源集合修改。
func (this *defaultManager) updateResourceStatusLocked() {
	this.publishStatusLocked(func(next *statusSnapshot, _ time.Time) {
		next.resources = ResourceStatus{
			SubscriptionCount:    len(this.subscribers),
			WatchCount:           this.watcherCount,
			ProviderSessionCount: len(this.sessions),
		}
	})
}

// publishSnapshotStatusLocked 发布一个已经校验并写入 current 的快照状态。
// 前置条件：调用方必须持有 defaultManager.mu 写锁，snapshot 必须与 current 指向同一版本。
func (this *defaultManager) publishSnapshotStatusLocked(snapshot *snapshotState) {
	this.publishStatusLocked(func(next *statusSnapshot, _ time.Time) {
		next.manager = ManagerStateRunning
		next.snapshot = SnapshotStatus{Available: true, Version: snapshot.version, PublishedAt: snapshot.updatedAt}
	})
}

// beginRefreshStatusLocked 标记一次已经通过运行前置检查的刷新开始。
// 前置条件：调用方必须持有 defaultManager.mu 写锁。
func (this *defaultManager) beginRefreshStatusLocked() {
	this.publishStatusLocked(func(next *statusSnapshot, now time.Time) {
		next.refresh.InProgress = true
		next.refresh.LastAttemptAt = now
	})
}

// finishRefreshSuccessStatusLocked 标记刷新已经成功发布新快照。
// 前置条件：调用方必须持有 defaultManager.mu 写锁，snapshot 已经写入 current。
func (this *defaultManager) finishRefreshSuccessStatusLocked(snapshot *snapshotState) {
	this.publishStatusLocked(func(next *statusSnapshot, now time.Time) {
		next.manager = ManagerStateRunning
		next.snapshot = SnapshotStatus{Available: true, Version: snapshot.version, PublishedAt: snapshot.updatedAt}
		next.refresh.InProgress = false
		next.refresh.LastResult = OperationResultSucceeded
		next.refresh.LastSuccessAt = now
		next.refresh.ConsecutiveFailures = 0
	})
}

// finishRefreshFailureStatusLocked 标记刷新在发布前失败并保留最后有效快照。
// 前置条件：调用方必须持有 defaultManager.mu 写锁。
func (this *defaultManager) finishRefreshFailureStatusLocked(stage string, err error) {
	this.publishStatusLocked(func(next *statusSnapshot, now time.Time) {
		next.refresh.InProgress = false
		next.refresh.LastResult = OperationResultFailed
		next.refresh.LastFailure = operationFailureOf(now, "config.manager.refresh", stage, err)
		next.refresh.ConsecutiveFailures++
	})
}

// beginNotificationStatusLocked 标记一次有实际 diff 和目标 Subscriber 的通知轮次开始。
// 前置条件：调用方必须持有 defaultManager.mu 写锁。
func (this *defaultManager) beginNotificationStatusLocked() {
	this.publishStatusLocked(func(next *statusSnapshot, now time.Time) {
		next.notification.InProgress = true
		next.notification.LastAttemptAt = now
	})
}

// finishNotificationStatusLocked 标记最近通知轮次的最终结果。
// 前置条件：调用方必须持有 defaultManager.mu 写锁。
func (this *defaultManager) finishNotificationStatusLocked(err error) {
	this.publishStatusLocked(func(next *statusSnapshot, now time.Time) {
		next.notification.InProgress = false
		if err == nil {
			next.notification.LastResult = OperationResultSucceeded
			next.notification.LastSuccessAt = now
			return
		}
		next.notification.LastResult = OperationResultFailed
		next.notification.LastFailure = operationFailureOf(now, "config.manager.notify", "notify_subscribers", err)
	})
}

// changeManagerStateLocked 发布 Manager 生命周期迁移。
// 前置条件：调用方必须持有 defaultManager.mu 写锁。
func (this *defaultManager) changeManagerStateLocked(state ManagerState) {
	this.publishStatusLocked(func(next *statusSnapshot, _ time.Time) {
		next.manager = state
	})
}
