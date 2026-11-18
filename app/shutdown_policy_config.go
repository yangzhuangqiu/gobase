package app

import (
	"strings"
	"time"

	"github.com/yangzhuangqiu/gobase/config"
)

const (
	// shutdownWorkloadGracePeriodKey 是启动快照中 Workload graceful 预算的完整配置键。
	shutdownWorkloadGracePeriodKey = "app.shutdown.workloadGracePeriod"
	// shutdownRuntimeCloseGracePeriodKey 是启动快照中 Runtime/Resource 关闭预算的完整配置键。
	shutdownRuntimeCloseGracePeriodKey = "app.shutdown.runtimeCloseGracePeriod"
)

// executionShutdownPolicy 是一次命令执行冻结后的有效时间预算。
// 说明：Signals 在 Runtime Build 前已由 App 声明确定，不进入该运行期值对象。
type executionShutdownPolicy struct {
	// WorkloadGracefulTimeout 是本次 Workload 与高级 Hook 共享的 graceful 预算。
	WorkloadGracefulTimeout time.Duration
	// RuntimeCloseTimeout 是本次 Runtime/Resource Close 使用的独立协作式预算。
	RuntimeCloseTimeout time.Duration
}

// executionPolicy 返回只包含内建默认值与显式声明的基础执行策略。
// 阶段角色：RuntimeModeDisabled 的最终策略，也是配置解析失败时关闭已构建 Runtime 的安全回退。
func (this shutdownConfiguration) executionPolicy() executionShutdownPolicy {
	return executionShutdownPolicy{
		WorkloadGracefulTimeout: this.WorkloadGracefulTimeout,
		RuntimeCloseTimeout:     this.RuntimeCloseTimeout,
	}
}

// resolveExecutionShutdownPolicy 从 ConfigManager 启动快照补齐未显式声明的时间预算。
// 阶段角色：Runtime Build 成功后、BeforeRunHook/Workload 启动前冻结本次执行策略。
func (this *App) resolveExecutionShutdownPolicy(runtime Runtime) (executionShutdownPolicy, error) {
	// 第一阶段：以显式 Option/内建默认值建立不会依赖运行期配置的安全基线。
	policy := this.shutdown.executionPolicy()
	snapshot := runtime.ConfigManager().Current()

	// 第二阶段：按字段用启动快照补齐空缺；显式字段对应的低优先级非法值不参与解析。
	if !this.shutdown.WorkloadGracefulTimeoutSet {
		configured, exists, err := readShutdownDuration(snapshot, shutdownWorkloadGracePeriodKey)
		if err != nil {
			return executionShutdownPolicy{}, err
		}
		if exists {
			policy.WorkloadGracefulTimeout = configured
		}
	}
	if !this.shutdown.RuntimeCloseTimeoutSet {
		configured, exists, err := readShutdownDuration(snapshot, shutdownRuntimeCloseGracePeriodKey)
		if err != nil {
			return executionShutdownPolicy{}, err
		}
		if exists {
			policy.RuntimeCloseTimeout = configured
		}
	}
	// 第三阶段：返回本次执行的冻结策略，后续配置变化不再影响正在运行的关闭流程。
	return policy, nil
}

// readShutdownDuration 读取并校验一个可选的正时长配置，不把非法值静默降级为默认值。
// 阶段角色：ConfigManager 启动快照到有效 Shutdown Policy 的字段级转换边界。
func readShutdownDuration(snapshot config.Snapshot, key string) (time.Duration, bool, error) {
	// 第一阶段：区分配置缺失与显式声明，并取得来源元信息供失败诊断。
	value, exists := snapshot.Get(key)
	if !exists {
		return 0, false, nil
	}
	meta, _ := snapshot.Explain(key)
	version := snapshot.Version()

	// 第二阶段：只接受带单位字符串或进程内 typed duration，拒绝无单位数值产生歧义。
	var duration time.Duration
	switch typed := value.(type) {
	case time.Duration:
		duration = typed
	case string:
		parsed, err := time.ParseDuration(strings.TrimSpace(typed))
		if err != nil {
			return 0, true, errorForAppInternal(messagef(
				"op=app.shutdown_policy result=failed stage=read_config reason=duration_parse_failed key=%s snapshot_version=%d source=%s layer=%s cause=%v",
				key, version, meta.Source, meta.Layer, err,
			), err)
		}
		duration = parsed
	default:
		return 0, true, errorForAppInternal(messagef(
			"op=app.shutdown_policy result=failed stage=read_config reason=duration_type_unsupported key=%s snapshot_version=%d source=%s layer=%s value_type=%T",
			key, version, meta.Source, meta.Layer, value,
		))
	}
	// 第三阶段：非正预算无法提供可预测的 graceful 阶段，必须在 Workload 启动前失败。
	if duration <= 0 {
		return 0, true, errorForAppInternal(messagef(
			"op=app.shutdown_policy result=failed stage=read_config reason=duration_not_positive key=%s snapshot_version=%d source=%s layer=%s duration=%s",
			key, version, meta.Source, meta.Layer, duration,
		))
	}
	return duration, true, nil
}
