# App 使用指南

## 1. 选择执行模型

| 场景 | 声明方式 | 业务合同 |
|---|---|---|
| 一次性任务 | `WithWorkloads(Runner(fn))` | `RunFunc(ctx, runtime, command)` |
| 常驻服务/Consumer/Scheduler | `WithWorkloads(workload...)` | `Workload.Run/Shutdown` |
| 被动连接池/Client | IOC Definition/Module | `Resource.Close(ctx)` |
| 子命令 | `NewCommand(use, options...)` | 节点自己的 Workloads |
| 遗留关闭回调 | `WithShutdownHook` | 高级逃生口 |

不要为子命令设计另一套 Runner。root、一级子命令和多级子命令都用 Workload。

## 2. 一次性 root 任务

```go
func main() {
    app.Main(
        app.WithName("report"),
        app.WithDescription("Generate reports"),
        app.WithWorkloads(app.Runner(func(
            ctx context.Context,
            runtime app.Runtime,
            command app.CommandContext,
        ) error {
            return generate(ctx, runtime, command.Args())
        })),
    )
}
```

`Runner` 返回固定名称 `runner` 的 Workload。函数返回表示任务真实完成；它适合迁移、校验、生成等 one-shot 工作。可取消的一次性任务必须监听传入的 `ctx`；`Runner.Shutdown` 是无副作用操作，收到关闭信号时只能依靠业务函数响应 run context 并返回。

## 3. 常驻 Workload

```go
type HTTPWorkload struct {
    server *http.Server
    done   chan struct{}
}

func (this *HTTPWorkload) Name() string { return "http" }

func (this *HTTPWorkload) Run(ctx context.Context, _ app.Runtime, _ app.CommandContext) error {
    defer close(this.done)
    err := this.server.ListenAndServe()
    if errors.Is(err, http.ErrServerClosed) {
        return nil
    }
    return err
}

func (this *HTTPWorkload) Shutdown(ctx context.Context, _ app.Runtime, _ app.CommandContext) error {
    if err := this.server.Shutdown(ctx); err != nil {
        _ = this.server.Close()
    }
    <-this.done
    return nil
}
```

```go
app.Main(
    app.WithWorkloads(httpWorkload, consumerWorkload),
)
```

任一 Run 返回都会触发整组 Shutdown。app 会等待所有 Shutdown 与所有 Run 返回，然后才关闭 Runtime/IOC Resource。

### 停止合同

1. 常驻 Workload：`Shutdown` 必须最终让 `Run` 返回。`Run` 可以直接监听自己的 run context，也可以像上例一样由 `Shutdown` 停止底层 Server；两条路径至少有一条必须真正结束运行单元。
2. 可取消的一次性 `app.Runner`：业务函数应监听传入的 context。因为适配器的 `Shutdown` 不执行额外停止动作，函数忽略取消时，框架只能继续等待它自行返回。

`Run(ctx, ...)` 与 `Shutdown(ctx, ...)` 的 context 含义不同：前者在应用开始关闭时被取消；后者携带 graceful 预算。`Shutdown` 应在预算内尝试优雅停止，预算到期后执行底层 force close，并确保 `Run` 最终返回。仅在 `ctx.Done()` 后直接从 `Shutdown` 返回、却把 `Run` 留在后台，不符合 Workload 合同。

## 4. 构建子命令树

`NewCommand` 与 root 使用同一批 `NodeOption`：

```go
app.Main(
    app.WithName("ops"),
    app.WithBeforeRunHook(rootAudit),
    app.WithCommands(
        app.NewCommand("admin",
            app.WithDescription("Administrative commands"),
            app.WithBeforeRunHook(adminAudit),
            app.WithCommands(
                app.NewCommand("status [service]",
                    app.WithDescription("Show service status"),
                    app.WithArgsValidator(requireOneArg),
                    app.WithoutRuntime(),
                    app.WithFlagBinder(&statusFlags{}),
                    app.WithWorkloads(app.Runner(runStatus)),
                ),
            ),
        ),
    ),
)
```

选中 `ops admin status payment` 时：

- `CommandContext.Name()` 是 `status`；
- `CommandContext.Path()` 是 `ops admin status`；
- `CommandContext.Args()` 是 `[]string{"payment"}`；
- 只执行 status 节点的 Workload；
- Runtime 需求取路径上最近的显式声明；
- 需要 Runtime 时 BeforeRunHook 按 root→admin→status 顺序执行。

分组节点可以只有 `WithCommands` 而没有 Workload，直接执行时显示该节点 help。叶子节点缺少 Workload 返回 `app.lifecycle.workload_required`。

## 5. Option 分类

### App Option

| Option | 作用 |
|---|---|
| `WithName` | root 名称和默认 Runtime 名称 |
| `WithVersion` | `--version` 文本 |
| `WithOutput` / `WithErrorOutput` | CLI 输出目标 |
| `WithRuntime` / `WithRuntimeOptions` | Runtime 蓝图 |
| `WithConfig` / `WithLogging` / `WithModules` | 高频 Runtime 门面 |
| `WithShutdownPolicy` | 进程信号和 App 默认关闭预算 |

### NodeOption（root 与子命令共用）

| Option | 归并语义 |
|---|---|
| `WithDescription` / `WithLongDescription` / `WithExample` / `WithAliases` | 当前节点局部 |
| `WithArgsValidator` | 当前节点局部 |
| `WithoutRuntime` / `RequireRuntime` | 最近显式声明覆盖；默认需要 Runtime |
| `WithFlagBinder` | 当前节点局部，不继承 |
| `WithWorkloads` | 当前节点局部，不继承 |
| `WithBeforeRunHook` | root→leaf 追加 |
| `WithShutdownHook` | root→leaf 追加 |
| `WithCommands` | 递归添加直接子节点 |

### CommandOption

| Option | 作用 |
|---|---|
| `WithShutdownBudget` | 两个 duration 字段分别覆盖祖先/App 默认 |
| `WithoutInheritedShutdownHooks` | 清空祖先高级 Hook；本节点新 Hook 仍保留 |

## 6. FlagBinder

```go
type serveFlags struct {
    Address string
    Debug   bool
}

func (this *serveFlags) Bind(flags app.CommandFlags) {
    flags.String("address", &this.Address, ":8080", "Listen address")
    flags.Bool("debug", &this.Debug, false, "Enable debug")
}

func (this *serveFlags) Complete() error { return nil }
func (this *serveFlags) ValidateFlags() []error { return nil }
```

生命周期：

1. 命令树首次构建时，每个节点的 `Bind` 调用一次。
2. 每次执行仅对选中节点调用 `Complete`、`ValidateFlags`。
3. root 的业务 Binder 不参与子命令执行；框架 config/bootstrap/version flags 单独作为 persistent flags 继承。

因此 `Bind` 应是纯声明式的，不应依赖“每次执行都会调用”的副作用。

## 7. Runtime 依赖

默认 root 需要 Runtime，子命令未声明时自动继承最近祖先。普通命令不需要声明任何 Runtime Option。

纯本地诊断、离线转换或修复 Runtime 启动条件的命令使用 `WithoutRuntime()`：不构建 Runtime、不执行 BeforeRunHook，Workload/ShutdownHook 收到 nil Runtime。在整个禁用子树中，只有少数命令重新依赖 config/log/ioc 时，才使用高级覆盖入口 `RequireRuntime()`：

```go
app.NewCommand("admin",
    app.WithoutRuntime(),
    app.WithCommands(
        app.NewCommand("local-check",
            app.WithWorkloads(app.Runner(runLocalCheck)),
        ),
        app.NewCommand("repair",
            app.RequireRuntime(),
            app.WithWorkloads(app.Runner(runRepair)),
        ),
    ),
)
```

继承所需的未声明/需要/禁用三态完全位于框架内部；调用方只表达“这个节点不需要 Runtime”或“在禁用子树中重新需要 Runtime”。

## 8. Shutdown Policy 与命令 Budget

```go
app.New(
    app.WithShutdownPolicy(app.ShutdownPolicy{
        Signals:                 []os.Signal{os.Interrupt, syscall.SIGTERM},
        WorkloadGracefulTimeout: 20 * time.Second,
        RuntimeCloseTimeout:     8 * time.Second,
    }),
    app.WithCommands(
        app.NewCommand("import",
            app.WithShutdownBudget(app.ShutdownBudget{
                WorkloadGracefulTimeout: 2 * time.Minute,
            }),
            app.WithWorkloads(importWorkload),
        ),
    ),
)
```

duration 的字段级优先级：

```text
命令路径 WithShutdownBudget
  > 显式 WithShutdownPolicy
  > ConfigManager 启动快照
  > 内建默认值（15s / 10s）
```

配置：

```yaml
app:
  shutdown:
    workloadGracePeriod: 15s
    runtimeCloseGracePeriod: 10s
```

Signals 不从配置读取，因为它必须在 Runtime Build 前安装。

## 9. Shutdown Hook

```go
app.WithShutdownHook("legacy.lease", func(
    ctx context.Context,
    runtime app.Runtime,
    command app.CommandContext,
) error {
    return releaseLegacyLease(ctx)
})
```

普通 Server/Consumer 不要使用 Hook，应实现 Workload。普通 Client/连接池不要使用 Hook，应交给 IOC Resource。

某个子命令不应执行祖先高级 Hook 时：

```go
app.NewCommand("doctor",
    app.WithoutRuntime(),
    app.WithoutInheritedShutdownHooks(),
    app.WithWorkloads(app.Runner(runDoctor)),
)
```

`WithoutInheritedShutdownHooks` 不影响 Workload Shutdown 或 Runtime Close。

## 10. Web Server 集成

`app-web-hertz` 已提供推荐适配器：

```go
app.Main(
    app.WithModules(appwebhertz.NewModule()),
    app.WithWorkloads(appwebhertz.Workload()),
)
```

其流程是：从 IOC 解析唯一 `web.Server`，Workload Run 委托 `Server.Run`，Shutdown 委托 `Server.Shutdown`，app 等待 Run 返回，最后 IOC Close Server Resource。

## 11. 顺序复用同一个 App

同一 App 支持顺序 `ExecuteContext`：

- Cobra 命令树和所有 Flag 声明只构建一次；
- 每次执行只绑定本次 `executionSession`；
- session 不进入业务 context value；
- Binder Complete/Validate、Workload、Hook 按执行次数调用。

同一 App 不保证并发执行安全。需要并发隔离时构建不同 App。

## 12. 常见问题

| 现象 | 原因 | 处理 |
|---|---|---|
| `workload_required` | 选中叶子没有 Workload | 使用 `WithWorkloads` 或 `Runner` |
| 子命令没执行 root Workload | Workload 是节点局部能力 | 在该子命令声明自己的 Workload |
| root Binder 未校验子命令 | Binder 不继承 | 把共用规则提取为普通函数或给各节点声明 Binder |
| `WithoutRuntime` 命令收到 nil Runtime | 这是声明合同 | 不访问 Runtime，或在该节点使用 `RequireRuntime()` |
| Runtime Close 一直等待 | Workload/Closer 尚未真实结束 | 实现 context 感知的 graceful→force→wait |
| 配置预算未生效 | 代码显式 Policy/Budget 优先 | 检查字段级优先级和启动快照来源 |

## 13. 相关文档

- [App 命令节点设计](../architecture/app-design.md)
- [App 生命周期与优雅关闭设计](../architecture/app-lifecycle-design.md)
- [app 包 README](../../app/README.md)
