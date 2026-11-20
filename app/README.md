# app

`app` 是进程级应用装配与命令生命周期门面。root 与任意深度子命令都是同一种“命令节点”：使用 Option 声明元数据、Flag、Bean Definitions、Module 和 Hook；执行时由框架把 root 到选中节点归并成一次 `executionPlan`。

## 设计范围与目标

范围内：

- 构建并缓存命令树，隔离 Cobra 类型。
- 构建 `app-runtime`，向业务暴露只读 `Runtime`。
- 监督一次性任务与常驻服务的 `Workload` 生命周期。
- 协调进程信号、宿主取消、Shutdown Hook 与 Runtime/Resource Close。
- 支持 root、一级和多级子命令使用相同声明模型。

范围外：

- 业务资源的创建和依赖排序，由 IOC/模块负责。
- Server/Consumer 的具体停止算法，由对应 `Workload.Shutdown` 负责。
- 配置和日志的实现细节，由 `config`、`log` 与 `app-runtime` 负责。

## 快速开始

```go
err := app.Run(
    app.WithName("tool"),
    app.WithDescription("Operations tool"),
    app.WithModules(app.Runner(func(
        ctx context.Context,
        runtime app.Runtime,
        command app.CommandContext,
    ) error {
        return runOnce(ctx, runtime, command)
    })),
    app.WithCommands(
        app.NewCommand("admin",
            app.WithCommands(
                app.NewCommand("status [service]",
                    app.WithoutRuntime(),
                    app.WithModules(app.Runner(status)),
                ),
            ),
        ),
    ),
)
```

`Runner` 是 workload-only `Module`：不注册 Bean，只贡献一个 one-shot `Workload`。函数签名为：

```go
type RunFunc func(context.Context, app.Runtime, app.CommandContext) error
```

常驻服务模块实现两阶段合同：`Register` 声明 Bean，`Workloads` 暴露主动执行单元：

```go
type Module interface {
    Name() string
    Register(ioc.Registry) error
}

type WorkloadModule interface {
    Module
    Workloads() []Workload
}

type Workload interface {
    Name() string
    Run(context.Context, Runtime, CommandContext) error
    Shutdown(context.Context, Runtime, CommandContext) error
}
```

## 核心对象与职责

| 对象 | 职责 |
|---|---|
| `App` | 进程级默认值、Runtime 蓝图、信号策略和缓存命令树 |
| `Command` | 通过 `NewCommand` 构建的命令节点声明 |
| `NodeOption` | root 与子命令共用的节点能力 |
| `CommandOption` | 只适用于子命令的能力 |
| `Module` | IOC Build 前的具名 Bean 声明单元 |
| `WorkloadModule` | 在 Module 基础上贡献主动 Workload 的两阶段合同 |
| `Workload` | 主动运行单元；Run 与 Shutdown 都必须真实收敛 |
| `Resource` | 被动资源关闭契约，通常由 IOC 统一释放 |
| `ShutdownHook` | 无法表达为 Workload/Resource 时的高级逃生口 |
| `executionPlan` | root 到选中节点的字段级归并快照 |

## 公共入口与归并规则

App 级入口：

- `WithName`、`WithVersion`、`WithOutput`、`WithErrorOutput`
- `WithRuntime`、`WithRuntimeOptions` 及 Runtime 门面选项
- `WithShutdownPolicy`
- `WithGlobalBeforeRunHook`

root/子命令共享的 `NodeOption`：

- `WithDescription`、`WithLongDescription`、`WithExample`、`WithAliases`
- `WithArgsValidator`、`WithoutRuntime`、`RequireRuntime`
- `WithFlagBinder`、`WithBeanDefinitions`、`WithModules`
- `WithBeforeRunHook`、`WithShutdownHook`
- `WithCommands`

子命令专用 `CommandOption`：

- `WithShutdownBudget`

字段级归并规则：

| 能力 | 规则 |
|---|---|
| 命令元数据、参数校验 | 当前节点局部值 |
| Runtime 需求 | `WithoutRuntime` / `RequireRuntime` 最近显式声明覆盖；子命令默认继承，root 默认需要 Runtime |
| `FlagBinder` | 只属于当前节点，不继承 |
| `BeanDefinitions` | 只注册选中节点的私有对象，不继承 |
| `Modules` | 只构建选中节点的集合，不继承；其中 `WorkloadModule` 贡献本次 Workload |
| `GlobalBeforeRunHook` | App 每次实际命令执行都按声明顺序调用 |
| `BeforeRunHook` | 只执行选中节点的集合，不继承 |
| `ShutdownHook` | 只执行选中节点的集合，不继承 |
| `ShutdownBudget` | 两个 duration 字段分别按最近显式声明覆盖 |
| `WithCommands` | 递归构建直接子节点 |

真正需要对所有 Runtime 命令生效的高级 Bean，可显式使用
`WithRuntimeOptions(appruntime.WithBeanDefinitions(...))` 进入稳定蓝图；普通业务对象应保持节点局部。

## 配置与默认值

有效关闭预算的优先级是：

```text
选中命令路径的 WithShutdownBudget
  > App 显式 WithShutdownPolicy
  > ConfigManager 启动快照
  > 框架内建默认值
```

配置键：

- `app.shutdown.workloadGracePeriod`，默认 `15s`
- `app.shutdown.runtimeCloseGracePeriod`，默认 `10s`

Signals 必须在 Runtime Build 前安装，因此只接受 `WithShutdownPolicy` 的代码声明，不从 ConfigManager 读取。

## 运行时语义

选中命令的固定流程：

```text
解析 flags
  -> 当前节点 FlagBinder Complete/Validate
  -> 收集并校验选中 Module 的 Workloads
  -> 注入选中 Bean Definitions / Modules 并构建 Runtime（需要 Runtime 时）
  -> App GlobalBeforeRunHooks
  -> 选中节点 BeforeRunHooks
  -> 选中节点 Workloads Run
  -> 取消 run context
  -> Workloads Shutdown + 高级 Shutdown Hooks
  -> 等待全部 Workload Run 真正返回
  -> Runtime/IOC Resource Close
```

任一 Workload Run 返回、宿主 context 取消或首次进程信号都会触发关闭。graceful context 到期只表示实现应进入 force close，不允许 app 提前释放仍被 Workload 使用的 Runtime/Resource。

命令树和 Flag 声明只构建一次；顺序复用同一 App 时，每次只替换本次 `executionSession`。`FlagBinder.Bind` 每个节点只调用一次，Complete/Validate 仅在该节点被选中时按执行次数调用。session 不存放在 `context.Value`。

默认 root 需要 Runtime，子命令自动继承最近祖先。`WithoutRuntime()` 用于纯本地命令：不构建 Runtime，GlobalBeforeRunHook、选中节点 BeforeRunHook、Workload 和 ShutdownHook 均收到 nil Runtime。`RequireRuntime()` 仅用于在禁用 Runtime 的子树中恢复默认能力。
选中节点同时声明 `WithoutRuntime()` 与 `WithBeanDefinitions(...)` 属于语义冲突并会 fail-fast，避免私有对象图被静默忽略。

## 错误语义

- 选中的叶子命令没有 Workload：`app.lifecycle.workload_required`。
- Option 输入和归并冲突：`app.internal`，消息包含 `op/result/stage/reason`。
- 各阶段错误保留原始错误链，并追加稳定 stage。
- Workload、宿主 cause、Shutdown Hook 与 Runtime Close 错误会在安全收敛后聚合返回。

## 最佳实践

1. 一次性任务使用 `WithModules(Runner(fn))`。
2. 当前命令私有对象使用 `WithBeanDefinitions`；它与 Modules 一样不继承。
3. Server、Consumer、Scheduler 由 `WorkloadModule` 同时声明 Bean 与主动 Workload。
4. 数据库、连接池、Client 实现 Resource 并交给 IOC，不放进 Shutdown Hook。
5. Shutdown Hook 仅处理遗留全局动作或跨系统补偿。
6. 子命令用 `NewCommand` + Option 构建；分组节点可以没有 Workload，执行时展示 help。
7. Binder 的 `Bind` 只做声明和字段绑定，不依赖每次执行副作用。
8. 默认不声明 Runtime 选项；纯本地命令使用 `WithoutRuntime()`，仅在禁用子树的例外节点使用 `RequireRuntime()`。
9. 审计、权限校验等跨命令策略使用 GlobalBeforeRunHook；命令私有准备使用 BeforeRunHook，且只依赖选中节点对象图。

## File Map

| 文件 | 责任 |
|---|---|
| `app.go` | App 构建、执行 session 和命令树缓存 |
| `command.go` | Command/CommandOption 节点模型 |
| `option.go` / `command_option.go` / `node_option.go` | 三类公共 Option 语义合同 |
| `options.go` | Option 实现与共享节点选项 |
| `command_builder.go` | 递归 Cobra 命令树与 Flag 声明 |
| `execution_plan.go` | root→leaf 字段级归并器 |
| `command_execution.go` | root/subcommand 统一执行路径 |
| `before_run_hook.go` | App 全局与命令局部运行前 Hook 合同 |
| `workload.go` / `workload_module.go` / `workload_execution.go` | Workload、两阶段 Module 契约与监督器 |
| `runner.go` | workload-only one-shot Module 适配器 |
| `shutdown_policy.go` / `shutdown_policy_config.go` | 关闭策略与配置优先级 |
| `shutdown_execution.go` | 关闭触发、主动工作收敛与错误聚合 |
| `execution_session.go` | 一次执行独占的信号控制状态 |
