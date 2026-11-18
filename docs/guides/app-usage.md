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

`Runner` 返回固定名称 `runner` 的 Workload。函数返回表示任务真实完成；它适合迁移、校验、生成等 one-shot 工作。

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
                    app.WithRuntimeMode(app.RuntimeModeDisabled),
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
- RuntimeMode 取路径上最近的显式值；
- Enabled 时 BeforeRunHook 按 root→admin→status 顺序执行。

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
| `WithRuntimeMode` | 最近显式声明覆盖 |
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

## 7. RuntimeMode

| 模式 | Runtime | BeforeRunHook | Workload/ShutdownHook 的 Runtime |
|---|---|---|---|
| `RuntimeModeEnabled` | Build | root→leaf 执行 | 非 nil |
| `RuntimeModeDisabled` | 不 Build | 全部跳过 | nil |

root 默认 Enabled；子命令未声明时自动继承最近祖先。调用方只需要选择 Enabled 或 Disabled。纯本地诊断或格式化命令通常显式 Disabled。

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
    app.WithRuntimeMode(app.RuntimeModeDisabled),
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
| Disabled 命令收到 nil Runtime | 这是模式合同 | 不访问 Runtime，或改用 Enabled |
| Runtime Close 一直等待 | Workload/Closer 尚未真实结束 | 实现 context 感知的 graceful→force→wait |
| 配置预算未生效 | 代码显式 Policy/Budget 优先 | 检查字段级优先级和启动快照来源 |

## 13. 相关文档

- [App 命令节点设计](../architecture/app-design.md)
- [App 生命周期与优雅关闭设计](../architecture/app-lifecycle-design.md)
- [app 包 README](../../app/README.md)
