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
// 4. WithoutRuntime 命令传入 nil runtime。
type ShutdownHook func(ctx context.Context, runtime Runtime, command CommandContext) error

// WithShutdownHook 注册无法建模为 Workload/Resource 的高级关闭逃生口。
//
// 参数：
// - name：当前节点内非空且唯一，并且在有效继承路径内不与祖先重复的稳定组件名称；
// - hook：满足 graceful→force→wait 返回强契约的关闭函数。
//
// 说明：常规 Server/Consumer/Scheduler 应优先声明 Workload；常规资源应实现 Resource。
func WithShutdownHook(name string, hook ShutdownHook) NodeOption {
	return nodeOptionFunc(func(command *commandConfiguration) {
		name = strings.TrimSpace(name)
		if name == "" {
			command.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_hook reason=name_empty")))
			return
		}
		if hook == nil {
			command.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_hook reason=hook_is_nil name=%s", name)))
			return
		}
		if _, exists := command.ShutdownHookNames[name]; exists {
			command.recordInputError(errorForAppInternal(messagef("op=app.option result=failed stage=with_shutdown_hook reason=name_duplicate name=%s", name)))
			return
		}
		command.ShutdownHookNames[name] = struct{}{}
		command.ShutdownHooks = append(command.ShutdownHooks, namedShutdownHook{Name: name, Hook: hook})
	})
}

// WithoutInheritedShutdownHooks 清空当前子命令继承的祖先高级 Hook。
// 说明：本节点通过 WithShutdownHook 声明的 Hook 仍会执行；Runtime Close 不受影响。
func WithoutInheritedShutdownHooks() CommandOption {
	return commandOptionFunc(func(command *commandConfiguration) {
		command.DisableInheritedShutdownHooks = true
	})
}
