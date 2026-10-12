package main

import (
	"context"
	"fmt"
	"sync"

	baseapp "github.com/yangzhuangqiu/gobase/app"
	webhertzobserve "github.com/yangzhuangqiu/gobase/app-observe-webhertz"
	"github.com/yangzhuangqiu/gobase/ioc"
	baseweb "github.com/yangzhuangqiu/gobase/web"
)

// workloadAssembly 在 BeforeRun、Runner 和 Shutdown Hook 之间发布同一个监督器实例。
type workloadAssembly struct {
	// mu 保护一次性 Prepare 与后续并发 Run/Shutdown 读取。
	mu sync.Mutex
	// controller 只在全部配置、IOC 依赖和诊断路由构造成功后发布。
	controller *workloadController
}

// Prepare 在 Runner 前解析业务 Server、诊断配置和 Metrics Handler，但不启动 listener 或 goroutine。
func (this *workloadAssembly) Prepare(_ context.Context, runtime baseapp.Runtime, _ baseapp.CommandContext) error {
	if this == nil || runtime == nil {
		return fmt.Errorf("op=quickstart.workloads result=failed stage=prepare reason=assembly_and_runtime_required")
	}
	this.mu.Lock()
	defer this.mu.Unlock()
	if this.controller != nil {
		return fmt.Errorf("op=quickstart.workloads result=failed stage=prepare reason=already_prepared")
	}

	// 第一阶段：读取应用私有诊断配置，并在任何 listener 获取前完成全部校验。
	manager := runtime.ConfigManager()
	if manager == nil || manager.Current() == nil {
		return fmt.Errorf("op=quickstart.workloads result=failed stage=prepare reason=config_snapshot_required")
	}
	var config diagnosticsConfig
	if err := manager.Current().Decode(diagnosticsConfigScope, &config); err != nil {
		return fmt.Errorf("op=quickstart.workloads result=failed stage=prepare reason=diagnostics_config_decode_failed: %w", err)
	}
	resolved, err := resolveDiagnosticsConfig(config)
	if err != nil {
		return err
	}

	// 第二阶段：解析已完成 IOC Build 的业务 Server；Metrics Handler 仅在对应路由启用时需要。
	business, err := ioc.ResolveAs[baseweb.Server](runtime.Container())
	if err != nil {
		return fmt.Errorf("op=quickstart.workloads result=failed stage=prepare reason=business_server_resolve_failed: %w", err)
	}
	declared := []namedWorkload{{name: "business-web", workload: business}}
	if resolved.Enabled {
		var metrics webhertzobserve.PrometheusHandler
		if resolved.MetricsEnabled {
			metrics, err = ioc.ResolveAs[webhertzobserve.PrometheusHandler](runtime.Container())
			if err != nil {
				return fmt.Errorf("op=quickstart.workloads result=failed stage=prepare reason=prometheus_handler_resolve_failed: %w", err)
			}
		}
		diagnostics, buildErr := newDiagnosticsServer(resolved, metrics)
		if buildErr != nil {
			return buildErr
		}
		declared = append(declared, namedWorkload{name: "diagnostics-http", workload: diagnostics})
	}

	// 第三阶段：原子发布同一个 Controller，后续 Runner 与 Shutdown Hook 不再重复 Resolve 或构造。
	controller, err := newWorkloadController(declared...)
	if err != nil {
		return err
	}
	this.controller = controller
	return nil
}

// Run 把已准备的监督器接入 app Runner。
func (this *workloadAssembly) Run(ctx context.Context) error {
	controller, err := this.loadController("run")
	if err != nil {
		return err
	}
	return controller.Run(ctx)
}

// Shutdown 把同一个监督器接入 app Shutdown Hook，保证 Runtime Close 前成员已停止。
func (this *workloadAssembly) Shutdown(ctx context.Context, _ baseapp.Runtime, _ baseapp.CommandContext) error {
	controller, err := this.loadController("shutdown")
	if err != nil {
		return err
	}
	return controller.Shutdown(ctx)
}

// loadController 返回 Prepare 完整发布的实例，拒绝阶段错序。
func (this *workloadAssembly) loadController(stage string) (*workloadController, error) {
	if this == nil {
		return nil, fmt.Errorf("op=quickstart.workloads result=failed stage=%s reason=assembly_required", stage)
	}
	this.mu.Lock()
	defer this.mu.Unlock()
	if this.controller == nil {
		return nil, fmt.Errorf("op=quickstart.workloads result=failed stage=%s reason=controller_not_prepared", stage)
	}
	return this.controller, nil
}
