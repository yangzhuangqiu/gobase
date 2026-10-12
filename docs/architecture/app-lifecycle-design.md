# App 生命周期管理

本文定义 `app` 的稳定生命周期管理：从命令完成校验、Runtime Build、`BeforeRunHook`、Runner，到 `ShutdownHook`、Runtime Close 和进程退出，全部由一次命令事务统一编排。

普通应用继续从 `app.Main(...)` 启动，Runtime 与 Runner 沿用默认生命周期；只有需要运行前切面、主动工作负载关闭、自定义信号或时间预算时，才配置 `WithBeforeRunHook(...)` 或 `app.WithShutdown(...)`。

## 1. 设计目标与边界

App 生命周期管理必须解决三个阶段不同、所有者不同的问题：

1. 运行前准备：Runtime Build 成功后执行 `BeforeRunHook`，只完成 Runner 启动前的命令切面和可回滚准备。
2. 应用主动工作负载停止：HTTP/gRPC Server、Consumer、Scheduler、Worker 等不再产生或处理新工作，并排空存量工作。
3. Runtime 释放：app 在工作负载停止后调用 Runtime Close；Runtime 内部关闭哪些资源、采用什么顺序属于 Runtime 自身契约。

如果主动工作负载仍在运行时 Runtime 已关闭，它可能继续访问已经失效的运行能力。因此核心不变量是：

> Shutdown Hooks 已全部返回，并且 Runner 已完全退出后，app 才能关闭 Runtime。

### 1.1 In Scope

1. Runtime Build、`BeforeRunHook`、Runner、`ShutdownHook` 与 Runtime Close 的固定阶段顺序。
2. SIGINT/SIGTERM、宿主 context 和 Runner 结果的统一退出路径。
3. 应用 Shutdown Group 与 Runtime Close 的严格阶段边界。
4. root Runner 与选中子命令 `CommandRunner` 的统一生命周期。
5. 阻塞型与后台型主动工作负载的关闭协调。
6. graceful、force、第二次信号和 Kubernetes SIGKILL 的分层边界。
7. Before、Shutdown、Runner 与 Runtime Close 的错误聚合和观测语义。

### 1.2 Out of Scope

1. 在 `app` 核心依赖 Gin、Hertz、gRPC 或其他框架类型。
2. 发现、初始化、排序或释放 Runtime 内部资源。
3. 为 Shutdown Hooks 增加 priority、依赖图或隐式顺序推断。
4. 承诺 timeout 能强制终止不响应 context 的 goroutine。
5. 捕获操作系统不允许处理的 SIGKILL/SIGSTOP。

## 2. 生命周期阶段与所有权

### 2.1 固定阶段

一次选中命令只执行一条固定生命周期：

```text
Parse / Complete / Validate
  -> Runtime Build（Enabled）
  -> BeforeRunHook
  -> Runner
  -> Shutdown trigger
  -> ShutdownHook + Runner convergence
  -> Runtime Close
  -> Error/Exit
```

help、version、bootstrap schema 在 Runtime Build 前短路。Runtime Build 失败由 Runtime 构建方自行回滚；Build 成功后，无论后续阶段如何结束，app 都拥有本次 Runtime Close 责任。

### 2.2 所有权

Shutdown Hook 只管理必须在 Runtime Close 前停止的主动工作负载：

```text
Application Shutdown Group
  -> HTTP / gRPC Server
  -> Message Consumer
  -> Scheduler / Worker
  -> 其他持续产生或处理工作的入口
```

Runtime 对 app 保持不透明：

```text
app
  -> Runtime.Close(ctx)
       -> 内部资源和关闭顺序由 Runtime 自己定义
```

app 只定义两个所有权边界：Shutdown Hook 拥有其声明的主动工作负载停止责任，app 拥有在正确阶段调用一次 Runtime Close 的责任。工作负载实例来自哪里、如何装配、是否同时暴露在某个容器中，属于调用方、adapter 和 Runtime 的边界，不由本文规定。

该边界不是通用 GracefulShutdown 注册表：app 只增加一个固定的“主动工作负载停止”阶段，不提供任意资源发现、启动回调、关闭优先级或依赖排序。

## 3. `BeforeRunHook` 契约

`BeforeRunHook` 的公共契约是：

```go
type BeforeRunHook func(
    ctx context.Context,
    runtime Runtime,
    command CommandContext,
) error

func WithBeforeRunHook(hook BeforeRunHook) Option
```

`BeforeRunHook` 在 Runtime Build 成功后、Runner 启动前执行：

1. `ctx` 是 run context；取消表示不再进入 Runner。
2. `runtime` 在 `RuntimeModeEnabled` 下非 nil，可使用其公共能力完成命令切面；Hook 只借用，不取得所有权。
3. `command` 是当前选中命令的只读快照。
4. Hook 不能启动 Server、Consumer、Scheduler 等长期主动工作负载。
5. Hook 失败时 Runner 和 Shutdown Hooks 均不执行；Hook 必须在返回错误前撤销自己创建的临时副作用。
6. 需要内部依赖排序、初始化或资源释放的准备应交给对应 Runtime 能力，不应放入 BeforeRunHook。

同阶段 BeforeRunHooks 按声明顺序执行并聚合错误；任一失败后不再进入 Runner，但已经执行的 Hook 仍必须依靠各自的失败回滚契约保持安全。

## 4. 退出触发源

所有终止原因都进入同一个、最多执行一次的 Shutdown 流程：

```text
SIGINT / SIGTERM
宿主 context 取消
Runner 正常返回
Runner 返回业务错误
Server / Consumer 等工作负载意外退出并使 Runner 返回
```

触发源只决定退出原因，不分别拥有清理逻辑：

| 触发原因 | 目标结果 |
|---|---|
| SIGINT/SIGTERM，全部阶段成功 | 正常退出 |
| 宿主 context 取消 | 完整关闭；`ExecuteContext` 返回 `context.Cause(ctx)`，由宿主决定是否忽略 |
| Runner 正常完成 | 执行 Shutdown Group 后正常关闭 Runtime |
| Runner/工作负载失败 | 保留原始错误，仍执行 Shutdown Group 和 Runtime Close |
| Shutdown Hook 失败 | 保留 hook 错误，不跳过其他 hooks 和 Runtime Close |

Runtime Build 或 before hook 失败时 Runner 尚未开始，不执行 Shutdown Hooks；Runtime Build 成功后仍必须关闭 Runtime。`BeforeRunHook` 只能完成 Runner 启动前的准备，不能启动 Server、Consumer 等长期工作负载；hook 失败前必须自行回滚它创建的临时副作用，因此跳过 Shutdown Group 才是安全的。

## 5. 入口与信号语义

| 入口 | 信号与 Shutdown 责任 |
|---|---|
| `App.Run()` | 安装所选命令的默认退出信号并等待完整 Shutdown |
| `app.Run()` | 使用相同策略，将最终错误返回 main |
| `app.Main()` | 使用相同策略，完整关闭后映射错误和退出码 |
| `ExecuteContext(ctx, args)` | 不安装进程 signal handler；宿主 context 取消仍触发 Shutdown Group |
| Runtime Builder | 不感知进程信号和 Shutdown Hooks，只负责 Runtime Build/Close |

默认信号：

1. Unix：`os.Interrupt` 与 SIGTERM；`os.Interrupt` 已表示 SIGINT，不重复注册。
2. Windows：`os.Interrupt`。
3. SIGHUP、SIGQUIT 不默认监听，分别保留给 reload 和诊断。
4. `WithShutdownSignals` 替换默认集合，不做隐式追加。
5. SIGKILL、SIGSTOP 无法监听。

第一次信号启动 Shutdown 并立即停止拦截后续同类信号，使第二次信号恢复系统默认行为：

```text
第一次信号
  -> 记录 shutdown cause
  -> 取消 run context
  -> 恢复默认信号行为
  -> 启动 Shutdown Group

第二次信号
  -> 操作系统立即终止进程
```

Shutdown 也可能先由 Runner 结果或宿主 context 触发。无论触发源是什么，只要进入 Shutdown，app 都立即停止并恢复进程信号的默认行为；因此 Shutdown 已经进行时收到的下一次配置内信号会直接终止进程，不需要再被 app 消费一次。

信号配置只属于 `Run/Main` 拥有的进程入口；`ExecuteContext` 始终由宿主管理信号，避免测试、Windows Service 或嵌入式程序被隐式修改全局行为。

## 6. Root 与子命令语义

本文中的 Runner 表示当前实际执行的 root `Runner` 或选中子命令 `CommandRunner`。一个进程执行一次命令事务，Shutdown 只作用于本次选中的命令。

应用级 Shutdown Hooks 默认被 root 和子命令继承。纯迁移、诊断等子命令可以显式关闭继承：

```go
app.Command{
    Use:                  "migrate",
    DisableShutdownHooks: true,
    Runner:               runMigration,
}
```

`DisableShutdownHooks` 只关闭应用 Shutdown Group，不关闭以下基础语义：

1. 命令 context 仍可被宿主或进程信号取消。
2. Runtime Build 成功后仍必须执行 Runtime Close。
3. `RuntimeModeDisabled` 仍表示不构建 Runtime，也不执行 Runtime hooks。
4. help、version、bootstrap schema 等 Runner 前短路分支不执行 Shutdown Hooks。

| 命令类型 | Shutdown Hooks | Runtime Close |
|---|---:|---:|
| root 常驻服务 | 默认执行 | 执行 |
| 常驻子命令 | 默认继承并执行 | 执行 |
| 显式 `DisableShutdownHooks` | 不执行 | Runtime 存在时仍执行 |
| `RuntimeModeDisabled` | 默认可执行；通常由命令显式禁用 | 无 Runtime |
| help/version/schema | 不执行 | 不构建 Runtime |

## 7. 统一 Shutdown 流程

### 7.1 固定阶段

```text
1. Parse / Complete / Validate
2. Build Runtime（RuntimeModeEnabled）
3. before hooks
4. 启动当前 Runner
5. 等待退出触发源
6. 取消 run context
7. 并行执行 Shutdown Hooks，并等待 Runner 收敛
8. Shutdown Group 与 Runner 全部完成
9. 使用独立 context 关闭 Runtime
10. 聚合错误并决定退出码
```

信号或宿主取消发生时，Shutdown Hooks 不能机械地等待 Runner 先返回。阻塞型 `Serve/Spin` 可能只有在 hook 调用 Shutdown 后才会返回，因此 app 必须协调两个并行分支：

```text
cancel(runCtx)

├─ A: Shutdown Group
│     -> 所有 hooks 并发执行
│     -> graceful timeout 后由各 hook Force Close
│     -> 等待全部 hooks 返回
│
└─ B: Runner convergence
      -> 等待当前 Runner 完全退出

A + B 完成
  -> Runtime Close
```

Runner 自己先正常返回或失败时，app 记录 Runner 结果、取消 run context，并执行相同的 Shutdown Group。整个流程由 `sync.Once` 等内部状态机保证最多启动一次。

Runner 保持在命令调用 goroutine 执行；app 使用独立触发监听分支在宿主/信号取消时并发启动 Shutdown Hooks。因此阻塞型 `Serve/Spin` 不需要先迁移到业务后台 goroutine，Hook 仍能使其返回；后台型工作负载也沿用同一状态机。

Shutdown trigger 与 Runner result 是两个独立状态：trigger 只决定何时开始关闭，Runner result 始终在收敛分支完成后单独采集并进入最终错误链。`sync.Once` 只能保护 Shutdown 启动，不能采用“第一个结果获胜”而丢弃稍后返回的 Runner 业务错误。

触发源在 Runner 获准启动前已经到达时，app 不再启动 Runner。触发源与 Runner 启动并发发生时，adapter controller 必须让 `Start` 与 `Shutdown` 线性化，禁止 Shutdown 已经封口后再发布新的工作负载。

### 7.2 三类 Context

| Context | 所有者 | 用途 | 禁止事项 |
|---|---|---|---|
| run context | app/宿主 | Runtime Build、before hooks 和 Runner；退出时取消 | 不能用于取消后的资源收敛 |
| shutdown context | app | Shutdown Group 的 graceful 截止点；到期后进入 force/wait | 不能用于 Runtime Close，也不是 hook 总执行上限 |
| runtime close context | app | 向 Runtime Close 传播独立关闭 deadline | 不能复用已取消的 run context，也不能承诺强制终止 Close |

Shutdown 与 Runtime Close 是两个串行阶段，分别拥有 deadline。一个总 timeout 容易被前一阶段耗尽，使 Runtime 没有清理机会，因此不共用 context。

### 7.3 Timeout 的边界

timeout 只能限制等待，不能终止 Go goroutine。Shutdown Hook 应在 graceful deadline 到期后执行框架支持的 Force Close，并等待工作负载真正停止后再返回。

如果 Force Close 仍不能让工作负载停止，app 不得假装 Shutdown 成功并继续关闭 Runtime。此时进程继续等待第二次信号或平台 SIGKILL，避免活跃 goroutine 访问已关闭依赖。

`WithShutdownRuntimeCloseTimeout` 同样是协作式 deadline：app 只把独立 context 传给 Runtime Close，不检查或控制 Runtime 内部资源。Runtime 实现可能超过该时间；app 不通过“另起 goroutine、超时后直接返回”伪造关闭完成，真正的进程硬上限仍由后续信号或部署平台提供。

## 8. `WithShutdown` 配置模型

所有应用级退出配置收敛到一个 app Option。下面的 `workloads` 是调用方或 adapter 持有的工作负载 controller；它从哪里创建、如何装配不属于 app 生命周期契约。`workloads.Shutdown` 必须提供完整 graceful→force→wait 语义，不能直接替换为 `http.Server.Shutdown` 等原始框架方法：

```go
workloads := newWorkloadController()

application := app.New(
    app.WithRunner(func(ctx context.Context, runtime app.Runtime) error {
        return workloads.Run(ctx, runtime)
    }),
    app.WithShutdown(
        app.WithShutdownSignals(os.Interrupt, syscall.SIGTERM),
        app.WithShutdownGracefulTimeout(15*time.Second),
        app.WithShutdownRuntimeCloseTimeout(10*time.Second),
        app.WithShutdownHook("servers", workloads.Shutdown),
    ),
)
```

公共形态：

```go
type ShutdownHook func(
    ctx context.Context,
    runtime Runtime,
    command CommandContext,
) error

type ShutdownOption interface {
    // 私有实现，调用方只能通过 WithShutdown* 构造函数创建。
}

func WithShutdown(options ...ShutdownOption) Option
func WithShutdownSignals(signals ...os.Signal) ShutdownOption
func WithShutdownGracefulTimeout(timeout time.Duration) ShutdownOption
func WithShutdownRuntimeCloseTimeout(timeout time.Duration) ShutdownOption
func WithShutdownHook(name string, hook ShutdownHook) ShutdownOption
```

默认行为：

| 配置 | 默认值 |
|---|---|
| 机制 | `Run/Main` 默认启用；`ExecuteContext` 只响应宿主 context |
| Signals | Unix 为 Interrupt+SIGTERM；Windows 为 Interrupt |
| Graceful timeout | 15 秒 |
| Runtime Close timeout | 10 秒 |
| Hooks | 空集合 |
| 子命令 | 继承全局 hooks，可通过 `DisableShutdownHooks` 关闭 |

`15s + 10s` 只是框架提供的起始默认值，不是对部署总退出预算的承诺。生产环境仍必须根据流量摘除时间、工作负载停止时间和 Runtime 关闭时间显式计算退出预算。

Option 组合规则：

1. `WithShutdown` 可以省略，省略时仍使用默认信号和 timeout。
2. `WithShutdownSignals`、两个 timeout 属于单值声明，重复配置属于声明错误，不采用“最后一个覆盖”。
3. `WithShutdownHook` 可重复声明并组成 Shutdown Group；hook name 必须非空且全局唯一。
4. nil hook、空信号集合、非正 timeout 在执行前 fail-fast。
5. hooks 并发执行，声明顺序只用于稳定诊断输出，不代表关闭顺序。
6. 有依赖顺序的多个动作必须封装成一个 hook，不增加 priority API。

hook name 进入 app 错误的稳定 `name` 字段；业务日志可以把它映射为 `component`，不能使用请求 ID 等高基数值。

`ShutdownHook` 的三个输入具有不同且稳定的用途：

1. `ctx` 是独立 shutdown context，只界定 graceful 阶段，不复用已取消的 run context。
2. `runtime` 在 Runtime Close 前始终存活，Hook 可以使用其公共能力定位或停止已经构建的工作负载；Hook 只借用 Runtime，不能调用 Runtime Close，也不能把关闭阶段变成新的装配阶段。
3. `command` 是当前选中命令的只读快照，用于稳定观测和极少量命令级分支，不应替代 controller 的实际启动状态。
4. `RuntimeModeEnabled` 时 runtime 必须非 nil；`RuntimeModeDisabled` 时传入 nil，应用级 Hook 必须 nil-safe，或由该命令显式 `DisableShutdownHooks`。

Runner 与 Hook 必须定位或持有同一个并发安全的 controller。controller 的 `Start` 与 `Shutdown` 必须线性化：`Start` 先成功时由 Shutdown 停止并等待；Shutdown 先发生时必须封口，后续 `Start` 返回错误且不得发布工作负载。工作负载尚未启动时，Hook 只有在完成该封口后才能返回，不能只是观察空状态后无操作返回。

## 9. Shutdown Hook 强契约

Shutdown Hook 的返回语义是公共接口契约，不是可选的最佳实践：

> hook 返回时，它管理的主动工作负载必须已经停止访问 Runtime；即使 graceful shutdown 失败，也必须完成 Force Close 并确认工作负载停止后才能返回。

具体要求：

1. hook 必须幂等；app 每次命令事务只调用一次，但 adapter 可能同时收到框架内部关闭请求。
2. controller 的 Start/Shutdown 必须线性化；Shutdown 封口后不能再产生新工作负载。
3. Hook 定位的 controller 必须与 Runner 启动的实例相同；定位只用于取得已构建工作负载，不能在关闭阶段创建新工作负载。
4. shutdown context 的 deadline 是 graceful 阶段的截止点，不是 hook 可以提前返回的总截止点；deadline 到期后必须切换到 Force Close。
5. hook 返回 nil 表示 graceful/force 关闭均完成且无需要报告的错误。
6. hook 返回 error 表示关闭发生过降级或失败，但不表示工作负载仍在运行。
7. hook 不能调用 Runtime Close，也不能关闭不属于该 Hook 的资源。
8. hook 返回前必须等待 `Serve/Spin/Consume/Run` goroutine 完全退出。
9. 多个 hooks 不得依赖执行顺序；共享状态必须自行保证并发安全。

Go 类型系统无法证明 goroutine 已停止，因此这是“语义上的硬契约、实现上的测试约束”。框架应通过以下方式把约束落到实现：

1. 官方 adapter 为每个框架封装 graceful→force→wait 全流程。
2. adapter 测试断言 hook 返回前 Serve/Spin 已退出。
3. app 测试断言 Runtime Close 不早于全部 hooks 与 Runner 完成。
4. adapter 的关键日志包含 `op=app.shutdown`、`result`、`stage`、`component` 和失败 `reason`。

“优先 graceful、超时 force、最终 wait”的实现方式属于最佳实践；“返回后不得再访问 Runtime”本身属于必须遵守的接口语义。

## 10. Before 与 Shutdown 为什么是两个契约

`BeforeRunHook` 与 `ShutdownHook` 不合并成一个通用类型，也不增加 `WithLifecycle(...)`：

| 契约 | 输入 | 返回语义 |
|---|---|---|
| `BeforeRunHook` | run context、Runtime、CommandContext | 准备完成；失败时 Runner 不启动，临时副作用已回滚 |
| `ShutdownHook` | shutdown context、Runtime、CommandContext | 工作负载已经停止且不再访问 Runtime；Runtime 只作为存活中的借用能力 |

两者的阶段、资源所有权和失败后果不同；复用同一个函数类型只会隐藏关键契约。前置准备与退出收敛也不是必须成对出现的对称 API。

停止 Server、Consumer、Scheduler 使用 Shutdown Hook；关闭 Runtime 内部资源交给 Runtime Close；命令私有结果处理或临时状态收尾放在 Runner 的显式代码或 `defer` 中。完整 before 语义见 [`BeforeRunHook` 契约](#3-beforerunhook-契约)，完整 shutdown 返回不变量见 [Shutdown Hook 强契约](#9-shutdown-hook-强契约)。

## 11. Server 与其他工作负载

`Server` 是 HTTP、gRPC、管理端口等持续接收请求的上位概念。Shutdown/Force Close 是生命周期动作，不要求具体框架方法同名：

| 框架 | graceful | force |
|---|---|---|
| `net/http` | `Shutdown(ctx)` | `Close()` |
| gRPC | `GracefulStop()` | `Stop()` |
| Hertz | `Shutdown(ctx)` 或受控 `Spin()` | `Close()` |

阻塞型 Server：Runner 调用阻塞的 Serve/Spin，Shutdown Hook 负责使阻塞调用返回；app 同时等待 hook 和 Runner。

后台型 Server：Runner 等待 run context 或 Serve 结果；Shutdown Hook 停止 Server 并等待后台 goroutine，Runner 随后收敛。

多个 HTTP/gRPC Server、Consumer 或 Scheduler 可以由 adapter 内的 Group 组成一个 hook。Group 内部可以并发关闭成员，但必须在返回前确认全部成员停止。

Server 通常由 Runner/adapter 持有或定位。它是否同时暴露在 Runtime 内部、内部依赖如何管理，不属于 app 生命周期契约；本文只要求同一个主动工作负载不能被多个生命周期所有者重复关闭。

## 12. Error 与 Observability

错误策略：

1. Runner、各 Shutdown Hook 与 Runtime Close 错误通过 `errors.Join` 保留原始 cause。
2. Shutdown trigger 与 Runner result 分别记录；无论谁先到达，都必须等待并保留 Runner 的最终结果。
3. 一个 hook 失败不跳过其他 hooks。
4. Shutdown Hook 错误不跳过 Runtime Close，前提是 hook 已遵守“返回时工作负载已停止”的契约。
5. 仅由 SIGINT/SIGTERM 导致的 cancellation，且所有阶段成功时，`Run/Main` 返回 nil。
6. `ExecuteContext` 因宿主 context 结束且其他阶段成功时返回 `context.Cause(ctx)`；调用方可以显式忽略预期 cancellation。
7. Runner 真实业务错误不能因错误链中同时存在 `context.Canceled` 而被吞掉。
8. 第二次信号或 SIGKILL 立即终止，不承诺 defer、日志 flush 或资源协议完成。

app 把错误包装为可检索的稳定阶段：

```text
op=app.command result=failed stage=shutdown_trigger reason=...
op=app.command result=failed stage=shutdown_hook reason=hook_failed name=http
op=app.command result=failed stage=close_runtime reason=...
```

app 核心不主动输出生命周期日志。业务如需记录 shutdown started/success，应在 controller/Hook 内使用 Runtime Logger，并避免 token、凭据、完整连接串或其他敏感配置。

## 13. Kubernetes 配合

Pod 进入终止状态后，Endpoint、Ingress、Service Mesh、云负载均衡器、连接池和既有连接之间不存在瞬时断流保证。Server adapter 可以把以下动作封装为一个 Shutdown Hook：

```text
draining=true
  -> readiness 失败，liveness 继续成功
  -> 等待路由传播
  -> 停止接受新请求/RPC
  -> 排空存量请求、RPC 和 Stream
  -> graceful timeout 后 Force Close
  -> 等待 Serve/Spin 完全退出
```

`preStop`、SIGTERM 和重复 Shutdown 应共享首次 draining 时间，避免重复等待完整 drain delay。

部署预算：

```text
terminationGracePeriodSeconds
  >= preStop / route propagation delay
   + Graceful timeout
   + Force Close convergence allowance
   + Runtime Close timeout
   + safety margin
```

Force Close convergence 不是 app 可以强制兑现的时间上限；预算假设 adapter 能在该余量内完成封口和等待。若工作负载或 Runtime Close 不响应，平台最终仍以 SIGKILL 作为硬边界。

Kubernetes 行为依据见 [Pod 与 Endpoint 终止流程](https://kubernetes.io/docs/tutorials/services/pods-and-endpoint-termination-flow/) 和 [Container Lifecycle Hooks](https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/)。

## 14. Open Gaps / Future Improvements

1. app 核心不提供 HTTP、gRPC、Hertz 等具体 adapter；业务当前需要自行实现 graceful→force→wait controller。
2. 尚无内建 lifecycle metrics/tracing，Hook 和 Runner 通过 Runtime Logger 完成业务观测。
3. Kubernetes readiness、preStop 与路由传播窗口仍需按部署环境提供独立 adapter 和端到端验证。

## 15. File Map

1. `app/before_run_hook.go`：BeforeRunHook 公共契约与顺序执行逻辑。
2. `app/shutdown_hook.go`：ShutdownHook 公共契约。
3. `app/shutdown_option.go`：WithShutdown 聚合入口与声明校验。
4. `app/shutdown.go`：默认时间预算和归一化 Shutdown 配置模型。
5. `app/shutdown_execution.go`：Runner、触发源、Shutdown Hooks 的单次执行协调器。
6. `app/process_signal.go`：进程信号默认值、订阅和首次触发后的恢复边界。
7. `app/command_execution.go`：命令事务与 Runtime Close 阶段。
8. `app/shutdown_execution_test.go`、`app/command_lifecycle_test.go`：信号、宿主取消、子命令继承和关闭顺序验证。
