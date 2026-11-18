package app

import (
	"context"
	"strings"
)

// ShutdownHook 定义无法建模为 Workload/Resource 时使用的高级关闭逃生口。
//
// 典型调用场景：遗留全局回调、跨系统补偿或第三方 Lease 释放；常规 Server/Consumer 应实现 Workload。
//
// 语义：
// 1. ctx 只界定 graceful 阶段；到期后 Hook 必须执行 Force Close；
// 2. Hook 返回时，其工作负载必须已经停止访问 Runtime；
// 3. runtime 和 command 仅在本次 Hook 调用期间借用，Hook 不得调用 Runtime Close；
// 4. RuntimeModeDisabled 命令传入 nil runtime。
type ShutdownHook func(ctx context.Context, runtime Runtime, command CommandContext) error

// WithShutdownHook 注册无法建模为 Workload/Resource 的高级关闭逃生口。
//
// 参数：
// - name：应用内非空且唯一的稳定组件名称；
// - hook：满足 graceful→force→wait 返回强契约的关闭函数。
//
// 说明：常规 Server/Consumer/Scheduler 应优先声明 Workload；常规资源应实现 Resource。
func WithShutdownHook(name string, hook ShutdownHook) Option {
	return func(app *App) {
		name = strings.TrimSpace(name)
		if name == "" {
			app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_hook reason=name_empty")))
			return
		}
		if hook == nil {
			app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_hook reason=hook_is_nil name=%s", name)))
			return
		}
		if _, exists := app.shutdown.HookNames[name]; exists {
			app.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_hook reason=name_duplicate name=%s", name)))
			return
		}
		app.shutdown.HookNames[name] = struct{}{}
		app.shutdown.Hooks = append(app.shutdown.Hooks, namedShutdownHook{Name: name, Hook: hook})
	}
}
