package app

import (
	"context"
)

// ShutdownHook 定义 Runner 收敛期间停止主动工作负载的强契约。
//
// 典型调用场景：停止 HTTP/gRPC Server、Consumer、Scheduler 或 Worker，并在 Runtime Close 前确认其完全退出。
//
// 语义：
// 1. ctx 只界定 graceful 阶段；到期后 Hook 必须执行 Force Close；
// 2. Hook 返回时，其工作负载必须已经停止访问 Runtime；
// 3. runtime 和 command 仅在本次 Hook 调用期间借用，Hook 不得调用 Runtime Close；
// 4. RuntimeModeDisabled 命令传入 nil runtime。
type ShutdownHook func(ctx context.Context, runtime Runtime, command CommandContext) error
