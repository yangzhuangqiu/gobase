package ioc

import (
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

// registerOptions 是 Register 阶段的入参语义模型。
type registerOptions struct {
	// name 是 bean 在各声明类型范围内共享的唯一名称；为空时会按具体类型推导默认名称。
	name string
	// declares 是额外声明类型集合，用于建立类型索引。
	declares []reflect.Type
	// injectEnable 控制该 bean 是否参与 Build 阶段字段注入。
	injectEnable bool
	// injectConfigured 表示调用方是否显式声明过注入策略，用于区分默认值与明确意图。
	injectConfigured bool
	// managedLifecycle 控制容器是否托管该 bean 的 Init/Close；默认 true。
	managedLifecycle bool
}

// beanKey 是声明类型范围内的名称索引键。
//
// 语义：同一 bean 会为每个 declareType 建立一个键；名称只需在相同 declareType 下唯一。
type beanKey struct {
	// declareType 是解析和注入使用的精确声明类型，包含已实例化的泛型参数。
	declareType reflect.Type
	// name 是 bean 的最终名称，用于该声明类型内的按名解析和字段分流。
	name string
}

// beanDefinition 是容器内部 bean 元信息快照。
type beanDefinition struct {
	// name 是 bean 的唯一名称，用于日志及各声明类型范围内的按名解析和字段分流。
	name string
	// declares 是该 bean 支持的声明类型集合，用于 Resolve/注入匹配。
	declares []reflect.Type
	// instance 是原始实例对象，用于生命周期接口断言。
	instance any
	// instanceValue 是 instance 的反射值缓存，避免重复反射开销。
	instanceValue reflect.Value
	// injectEnable 控制该 bean 是否参与字段注入阶段。
	injectEnable bool
	// managedLifecycle 表示 bean 生命周期是否归容器所有；false 表示仅借用实例。
	managedLifecycle bool
	// seq 是注册顺序编号，用于稳定排序（解析顺序/拓扑并列顺序）。
	seq int
}

// CircularDegradeSnapshot 表示循环依赖降级执行的最近一次记录。
type CircularDegradeSnapshot struct {
	// Chain 是检测到的循环链路（例如 A -> B -> A）。
	Chain string
	// Order 是降级模式下实际执行的 bean 顺序。
	Order []string
	// OccurredAt 是本次降级记录产生时间。
	OccurredAt time.Time
}

// MetricsSnapshot 是容器运行期可观测指标快照。
type MetricsSnapshot struct {
	// RegisterTotal 是成功注册 bean 的累计次数。
	RegisterTotal uint64
	// BuildTotal 是 Build 调用累计次数。
	BuildTotal uint64
	// BuildFailedTotal 是 Build 失败累计次数。
	BuildFailedTotal uint64
	// BuildCircularRejectedTotal 是强语义下循环依赖被拒绝次数。
	BuildCircularRejectedTotal uint64
	// BuildCircularDegradedTotal 是弱约束模式循环依赖降级执行次数。
	BuildCircularDegradedTotal uint64
	// InitFailedTotal 是 Init 阶段失败累计次数。
	InitFailedTotal uint64
	// CloseTotal 是从 created/built 接受并执行的首次 Close 事务累计次数。
	CloseTotal uint64
	// CloseJoinedTotal 是加入正在执行的共享关闭事务的并发 Close 次数。
	CloseJoinedTotal uint64
	// CloseWaitCanceledTotal 是 waiter 因自身 context 结束而放弃等待的次数，不影响共享事务。
	CloseWaitCanceledTotal uint64
	// CloseFailedTotal 是 Build 回滚或正常 Close 中失败的 bean Closer 累计次数。
	CloseFailedTotal uint64
	// CloseTimeoutTotal 是启用单 bean timeout 后关闭等待超时的累计次数。
	CloseTimeoutTotal uint64
	// ClosePanicTotal 是 Closer panic 被恢复并转换为关闭错误的累计次数。
	ClosePanicTotal uint64
	// LastCircularDegrade 是最近一次循环依赖降级记录；无记录时为 nil。
	LastCircularDegrade *CircularDegradeSnapshot
}

type containerMetrics struct {
	// registerTotal 记录成功注册次数。
	registerTotal atomic.Uint64
	// buildTotal 记录 Build 调用次数。
	buildTotal atomic.Uint64
	// buildFailedTotal 记录 Build 失败次数。
	buildFailedTotal atomic.Uint64
	// buildCircularRejectedTotal 记录循环依赖拒绝次数。
	buildCircularRejectedTotal atomic.Uint64
	// buildCircularDegradedTotal 记录循环依赖降级次数。
	buildCircularDegradedTotal atomic.Uint64
	// initFailedTotal 记录 Init 失败次数。
	initFailedTotal atomic.Uint64
	// closeTotal 记录接受并执行的首次 Close 事务次数。
	closeTotal atomic.Uint64
	// closeJoinedTotal 记录并发 Close 加入共享关闭事务的次数。
	closeJoinedTotal atomic.Uint64
	// closeWaitCanceledTotal 记录 waiter 独立取消等待的次数。
	closeWaitCanceledTotal atomic.Uint64
	// closeFailedTotal 记录失败的 bean Closer 次数。
	closeFailedTotal atomic.Uint64
	// closeTimeoutTotal 记录单 bean Close timeout 次数。
	closeTimeoutTotal atomic.Uint64
	// closePanicTotal 记录 Closer panic 被恢复的次数。
	closePanicTotal atomic.Uint64

	// degradeMu 保护最近降级记录的并发读写。
	degradeMu sync.RWMutex
	// lastCircularDegrade 保存最近一次循环依赖降级记录。
	lastCircularDegrade *CircularDegradeSnapshot
}

// setLastCircularDegrade 记录最近一次循环依赖降级信息。
// 阶段角色：Build 降级路径的可观测收口。
func (this *containerMetrics) setLastCircularDegrade(chain string, order []string) {
	record := &CircularDegradeSnapshot{
		Chain:      chain,
		Order:      append([]string{}, order...),
		OccurredAt: time.Now(),
	}
	this.degradeMu.Lock()
	defer this.degradeMu.Unlock()
	this.lastCircularDegrade = record
}

// snapshot 返回指标快照。
// 阶段角色：对外暴露只读可观测视图。
func (this *containerMetrics) snapshot() MetricsSnapshot {
	result := MetricsSnapshot{
		RegisterTotal:              this.registerTotal.Load(),
		BuildTotal:                 this.buildTotal.Load(),
		BuildFailedTotal:           this.buildFailedTotal.Load(),
		BuildCircularRejectedTotal: this.buildCircularRejectedTotal.Load(),
		BuildCircularDegradedTotal: this.buildCircularDegradedTotal.Load(),
		InitFailedTotal:            this.initFailedTotal.Load(),
		CloseTotal:                 this.closeTotal.Load(),
		CloseJoinedTotal:           this.closeJoinedTotal.Load(),
		CloseWaitCanceledTotal:     this.closeWaitCanceledTotal.Load(),
		CloseFailedTotal:           this.closeFailedTotal.Load(),
		CloseTimeoutTotal:          this.closeTimeoutTotal.Load(),
		ClosePanicTotal:            this.closePanicTotal.Load(),
	}
	this.degradeMu.RLock()
	defer this.degradeMu.RUnlock()
	if this.lastCircularDegrade != nil {
		copied := *this.lastCircularDegrade
		copied.Order = append([]string{}, this.lastCircularDegrade.Order...)
		result.LastCircularDegrade = &copied
	}
	return result
}
