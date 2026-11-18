package app

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/ioc"
)

// shutdownPolicyDeadlineResource 记录 Runtime Close 传给 IOC Resource 的剩余预算。
type shutdownPolicyDeadlineResource struct {
	// remaining 接收 Close 调用时 context deadline 的剩余时长；无 deadline 时写入零值。
	remaining chan<- time.Duration
}

// Close 发布本次 Resource 实际收到的 Runtime Close 预算。
func (this *shutdownPolicyDeadlineResource) Close(ctx context.Context) error {
	deadline, exists := ctx.Deadline()
	if !exists {
		this.remaining <- 0
		return nil
	}
	this.remaining <- time.Until(deadline)
	return nil
}

// TestShutdownPolicyConfigPrecedence 验证显式 Policy、启动快照和内建默认值按字段归并。
//
// 测试方法：构建包含非法 workload 配置和合法 runtime 配置的 Runtime，同时显式声明 workload 预算；
// 断言显式字段屏蔽低优先级非法值，未显式字段采用 ConfigManager 启动快照。
func TestShutdownPolicyConfigPrecedence(t *testing.T) {
	// 第一阶段：同时布置显式字段、非法低优先级配置和合法配置字段。
	application := New(
		WithConfig(
			config.File(config.FileDisabled()),
			config.Defaults(map[string]any{
				"app": map[string]any{
					"shutdown": map[string]any{
						"workloadGracePeriod":     "invalid-but-overridden",
						"runtimeCloseGracePeriod": "4s",
					},
				},
			}),
		),
		WithShutdownPolicy(ShutdownPolicy{WorkloadGracefulTimeout: 2 * time.Second}),
	)
	runtime, err := application.runtimeApplication.Build(context.Background())
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}
	defer func() { _ = runtime.Close(context.Background()) }()

	// 第二阶段：解析冻结策略，逐字段验证显式 > 启动快照 > 内建默认值。
	policy, err := application.resolveExecutionShutdownPolicy(runtime, shutdownBudgetDeclaration{})
	if err != nil {
		t.Fatalf("resolve shutdown policy: %v", err)
	}
	if policy.WorkloadGracefulTimeout != 2*time.Second {
		t.Fatalf("explicit workload policy must win, got=%s", policy.WorkloadGracefulTimeout)
	}
	if policy.RuntimeCloseTimeout != 4*time.Second {
		t.Fatalf("runtime close period must come from config snapshot, got=%s", policy.RuntimeCloseTimeout)
	}
}

// TestCommandShutdownBudgetPrecedence 验证命令预算按字段优先于 App Policy 与配置快照。
//
// 测试方法：配置两个非法低优先级值，App 显式声明 Runtime Close，命令只覆盖 Workload；
// 解析后断言命令字段和 App 字段分别胜出，未参与归并的非法配置不会导致失败。
func TestCommandShutdownBudgetPrecedence(t *testing.T) {
	application := New(
		WithConfig(
			config.File(config.FileDisabled()),
			config.Defaults(map[string]any{
				"app": map[string]any{
					"shutdown": map[string]any{
						"workloadGracePeriod":     "invalid-but-overridden",
						"runtimeCloseGracePeriod": "also-overridden",
					},
				},
			}),
		),
		WithShutdownPolicy(ShutdownPolicy{RuntimeCloseTimeout: 5 * time.Second}),
	)
	runtime, err := application.runtimeApplication.Build(context.Background())
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}
	defer func() { _ = runtime.Close(context.Background()) }()

	budget := shutdownBudgetDeclaration{
		WorkloadGracefulTimeout:    3 * time.Second,
		WorkloadGracefulTimeoutSet: true,
	}
	policy, err := application.resolveExecutionShutdownPolicy(runtime, budget)
	if err != nil {
		t.Fatalf("resolve command shutdown budget: %v", err)
	}
	if policy.WorkloadGracefulTimeout != 3*time.Second || policy.RuntimeCloseTimeout != 5*time.Second {
		t.Fatalf("unexpected command policy: %+v", policy)
	}
}

// TestShutdownPolicyConfigFallsBackPerField 验证缺失配置字段继续使用内建默认值。
//
// 测试方法：启动快照只声明 workloadGracePeriod，解析后分别断言配置值与默认 Runtime Close 值。
func TestShutdownPolicyConfigFallsBackPerField(t *testing.T) {
	// 第一阶段：只在启动快照中配置 Workload 预算，保留 Runtime Close 字段空缺。
	application := New(WithConfig(
		config.File(config.FileDisabled()),
		config.Defaults(map[string]any{
			"app": map[string]any{
				"shutdown": map[string]any{"workloadGracePeriod": 3 * time.Second},
			},
		}),
	))
	runtime, err := application.runtimeApplication.Build(context.Background())
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}
	defer func() { _ = runtime.Close(context.Background()) }()

	// 第二阶段：解析冻结策略，验证已配置字段与默认字段彼此独立归并。
	policy, err := application.resolveExecutionShutdownPolicy(runtime, shutdownBudgetDeclaration{})
	if err != nil {
		t.Fatalf("resolve shutdown policy: %v", err)
	}
	if policy.WorkloadGracefulTimeout != 3*time.Second {
		t.Fatalf("unexpected configured workload period: %s", policy.WorkloadGracefulTimeout)
	}
	if policy.RuntimeCloseTimeout != defaultShutdownRuntimeCloseTimeout {
		t.Fatalf("missing runtime period must use default, got=%s", policy.RuntimeCloseTimeout)
	}
}

// TestInvalidShutdownPolicyConfigStopsBeforeWorkload 验证非法启动快照不会启动主动工作负载。
//
// 测试方法：配置非正 runtimeCloseGracePeriod 后执行应用，断言返回稳定解析阶段且 Workload.Run 未调用。
func TestInvalidShutdownPolicyConfigStopsBeforeWorkload(t *testing.T) {
	// 第一阶段：布置非法的非正预算，并用探针记录主动工作负载是否被启动。
	var runCalled atomic.Bool
	application := New(
		WithConfig(
			config.File(config.FileDisabled()),
			config.Defaults(map[string]any{
				"app": map[string]any{
					"shutdown": map[string]any{"runtimeCloseGracePeriod": "0s"},
				},
			}),
		),
		WithWorkloads(&testWorkload{
			name: "probe",
			run: func(context.Context, Runtime, CommandContext) error {
				runCalled.Store(true)
				return nil
			},
			shutdown: func(context.Context, Runtime, CommandContext) error { return nil },
		}),
	)
	// 第二阶段：执行应用，验证解析阶段失败且不越过 Workload 启动边界。
	err := application.ExecuteContext(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "stage=resolve_shutdown_policy") ||
		!strings.Contains(err.Error(), "reason=duration_not_positive") {
		t.Fatalf("expected shutdown policy config error, got=%v", err)
	}
	if runCalled.Load() {
		t.Fatal("workload must not start when shutdown policy config is invalid")
	}
}

// TestConfiguredShutdownPolicyReachesWorkloadAndResource 验证冻结后的配置预算进入两个实际关闭阶段。
//
// 测试方法：配置远大于默认值的两个 grace period，让 Workload 立即结束并记录 Shutdown context，
// 同时由 IOC Resource 记录 Close context，断言二者收到的都是 ConfigManager 启动快照预算。
func TestConfiguredShutdownPolicyReachesWorkloadAndResource(t *testing.T) {
	// 第一阶段：让 Workload 与 Resource 分别记录实际收到的关闭 context deadline。
	workloadRemaining := make(chan time.Duration, 1)
	resourceRemaining := make(chan time.Duration, 1)
	application := New(
		WithConfig(
			config.File(config.FileDisabled()),
			config.Defaults(map[string]any{
				"app": map[string]any{
					"shutdown": map[string]any{
						"workloadGracePeriod":     "30m",
						"runtimeCloseGracePeriod": "40m",
					},
				},
			}),
		),
		WithBeanDefinitions(ioc.Define(&shutdownPolicyDeadlineResource{remaining: resourceRemaining})),
		WithWorkloads(&testWorkload{
			name: "probe",
			run:  func(context.Context, Runtime, CommandContext) error { return nil },
			shutdown: func(ctx context.Context, _ Runtime, _ CommandContext) error {
				deadline, exists := ctx.Deadline()
				if !exists {
					workloadRemaining <- 0
					return nil
				}
				workloadRemaining <- time.Until(deadline)
				return nil
			},
		}),
	)
	// 第二阶段：执行完整生命周期，使两个关闭阶段消费同一启动快照冻结出的对应预算。
	if err := application.ExecuteContext(context.Background(), nil); err != nil {
		t.Fatalf("execute application: %v", err)
	}
	// 第三阶段：用宽松边界抵消调度耗时，仅验证预算来源和阶段传递语义。
	if remaining := <-workloadRemaining; remaining < 29*time.Minute || remaining > 30*time.Minute {
		t.Fatalf("workload did not receive configured grace period: %s", remaining)
	}
	if remaining := <-resourceRemaining; remaining < 39*time.Minute || remaining > 40*time.Minute {
		t.Fatalf("resource did not receive configured runtime close period: %s", remaining)
	}
}
