# App 生命周期与优雅关闭设计

## 1. 概念模型

App 生命周期只使用三个一等概念：

- `Workload`：主动运行单元，例如 HTTP Server、Consumer、Scheduler 或 one-shot Runner。
- `Resource`：被动依赖，例如连接池、Client、文件或租约，通常由 IOC 按依赖逆序关闭。
- `Shutdown Policy`：主动工作收敛预算和 Runtime/Resource Close 预算。

`ShutdownHook` 保留为高级逃生口，只用于无法自然表达为 Workload/Resource 的遗留全局动作或跨系统补偿。

## 2. 固定关闭不变量

完整生命周期：

```text
CLI parse / selected FlagBinder rules
  -> Runtime Build
  -> root-to-leaf BeforeRunHooks
  -> selected-node Workloads Run
  -> shutdown trigger
  -> cancel run context
  -> Workloads Shutdown + advanced ShutdownHooks
  -> wait every Shutdown and Run to return
  -> Runtime Close / IOC Resource Close
  -> return aggregated result
```

必须保持：

1. Runtime Build 失败时不运行 Workload/Shutdown Hook。
2. BeforeRunHook 失败时仍关闭已构建 Runtime，但不启动主动工作。
3. Workload 一旦启动，Runtime Close 必须等待其 Run 真正返回。
4. graceful context 到期不允许框架遗弃仍运行的 goroutine 后提前销毁 Resource。
5. Runtime Close 使用独立 context，不复用已经取消的 run context。
6. Shutdown Hook 与 Workload Shutdown 并发，彼此错误隔离并最终聚合。

## 3. Workload 合同

```go
type Workload interface {
    Name() string
    Run(ctx context.Context, runtime Runtime, command CommandContext) error
    Shutdown(ctx context.Context, runtime Runtime, command CommandContext) error
}
```

Run：

- 可以阻塞到服务终止，也可以完成一次性工作后返回。
- 任一 Run 返回会触发整组关闭。
- 必须响应 run context，或能由 Shutdown 使其退出。

Shutdown：

- ctx 表示 graceful 阶段预算。
- 实现必须在 ctx 到期后执行自身可用的 force close，并等待 Run 真实结束。
- 返回后不能继续访问 Runtime/IOC Resource。

监督器并发启动同一节点声明的所有 Workload，并发调用 Shutdown，最后等待每个 Run 的完成点。

## 4. Resource 与 IOC

Resource 是 Runtime 所有的被动资源。业务通过 Runtime 借用资源，不拥有 Runtime Close 权限。

IOC Close timeout 是传给 Closer 的协作式预算，不是“超时后后台继续关闭”的许可。Closer 收到 context 后应：

1. 尝试 graceful close；
2. context 到期时切换到 force close；
3. 等待内部操作真正结束；
4. 再返回。

这样才能保证依赖它的资源不会在其仍运行时被提前销毁。

## 5. 触发源与信号

关闭触发源：

- 任一 Workload Run 返回；
- `ExecuteContext` 的宿主 context 取消；
- `Run` 收到首个配置内进程信号。

`Run` 为每次执行创建独占 `executionSession`，保存信号恢复函数、协调阶段和首个具体信号。session 通过私有绑定槽传给缓存命令树，不存放到 `context.Value`。

首次进程信号：

1. 记录类型化 signal cause；
2. 取消本次执行 context；
3. Shutdown 协调器启动后立即恢复系统默认信号行为；
4. 第二次信号由操作系统直接处理，可强制终止卡住的进程。

仅由进程信号触发且完整收敛的关闭返回成功；Hook/Workload/Close 的真实错误仍返回，并保留 signal 诊断信息。

## 6. 命令节点继承

关闭动作属于命令计划：

- Workload 只取选中节点，不继承 root 或父命令。
- Shutdown Hook 默认 root→leaf 追加。
- `WithoutInheritedShutdownHooks()` 清空到当前节点为止的祖先 Hook；当前节点随后声明的 Hook 仍执行。
- Workload graceful 和 Runtime close 两个预算分别按最近命令声明覆盖。
- `WithoutRuntime()` 时不构建 Runtime，Hook/Workload 收到 nil Runtime；`RequireRuntime()` 可在子树中恢复。

这使 root、一级和多级子命令使用同一条生命周期路径，不存在独立的子命令 Runner 关闭协议。

## 7. Shutdown Policy

```go
type ShutdownPolicy struct {
    Signals                 []os.Signal
    WorkloadGracefulTimeout time.Duration
    RuntimeCloseTimeout     time.Duration
}

type ShutdownBudget struct {
    WorkloadGracefulTimeout time.Duration
    RuntimeCloseTimeout     time.Duration
}
```

默认值：

- Workload/Hook graceful：15s。
- Runtime/Resource Close：10s。
- Signals：平台默认 SIGINT/SIGTERM 集合。

duration 字段优先级：

```text
WithShutdownBudget on selected path
  > explicit WithShutdownPolicy
  > ConfigManager startup snapshot
  > built-in defaults
```

配置键为 `app.shutdown.workloadGracePeriod` 和 `app.shutdown.runtimeCloseGracePeriod`。配置仅在 Runtime Build 后读取一次并冻结；`WithoutRuntime()` 命令没有 ConfigManager，因此只使用代码显式值和内建默认值。

## 8. Shutdown Hook 强契约

```go
type ShutdownHook func(context.Context, Runtime, CommandContext) error
```

Hook 必须遵守：

1. ctx 到期后自行 force close；
2. 返回时不再访问 Runtime；
3. 不调用 Runtime Close；
4. `WithoutRuntime()` 命令下能处理 nil Runtime；
5. name 稳定、非空，并在有效继承路径内唯一。

常规 Server 不应使用 Hook。以 HTTP Server 为例：`Run` 调用 `Serve`，`Shutdown` 先调用 `Shutdown(ctx)`，超时时调用 `Close()`，并在返回前等待 `Serve` 退出。

## 9. 错误聚合

| 阶段 | 语义 |
|---|---|
| Flag/Runtime/Before | fail-fast；Runtime 已构建则仍 Close |
| Workload Run | 触发关闭，保留首个业务结果 |
| 非触发 Workload | 纯协作取消被忽略，真实业务错误保留 |
| Shutdown | Workload 和 Hook 全部分支都执行并聚合 |
| Runtime Close | 与前述错误 Join，不覆盖原始错误 |
| panic | 先完成关闭屏障，再恢复触发路径 panic；其他分支 panic 转成错误 |

## 10. File Map

| 文件 | 责任 |
|---|---|
| `app/workload.go` | 主动工作负载合同 |
| `app/workload_execution.go` | 并发 Run、Shutdown 与真实收敛屏障 |
| `app/resource.go` | 被动资源合同 |
| `app/shutdown_hook.go` | 高级逃生口与继承清空选项 |
| `app/shutdown_policy.go` | App Policy 与 Command Budget |
| `app/shutdown_policy_config.go` | ConfigManager 快照归并 |
| `app/shutdown_execution.go` | 触发源、取消和关闭动作协调 |
| `app/execution_session.go` | 每次执行的信号控制状态 |
| `app/process_signal_integration_unix_test.go` | 真实 SIGINT/SIGTERM 与二次信号证据 |
