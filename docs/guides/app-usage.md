# App 使用指南

本文面向使用 `app` 构建命令行应用的开发者，完整说明 app 提供的能力、每项能力如何配置，以及业务代码如何取得和使用最终结果。设计边界与决策理由见 [App Design](../architecture/app-design.md)，精确退出状态机见 [App 生命周期管理](../architecture/app-lifecycle-design.md)。

app 的公共 API 不暴露 Cobra/Pflag。使用者只需要理解五类配置入口：

1. `app.Option`：配置整个应用，例如名称、Runtime、Root Runner、全局 Hooks。
2. `app.Command`：配置一个子命令，例如参数、Flags、RuntimeMode 和 Runner。
3. `app.ShutdownOption`：配置应用级信号、关闭时间预算和主动工作负载 Hooks。
4. `config.Option`、`app.Logging`、`app.Module`、`ioc.Option`：配置 Runtime 内的 config/log/IOC 能力。
5. CLI flags：为本次执行提供 bootstrap、pinned 配置覆盖和普通命令参数。

## 0. 能力总览

| 能力 | 配置入口 | 业务使用入口 | app 负责什么 |
|---|---|---|---|
| 应用身份与帮助 | `WithName/WithDescription/WithVersion` | `--help`、`--version` | 构建 root 命令并渲染稳定 help |
| Root 业务入口 | `WithRunner` | `Runner(ctx, runtime)` | 构建 Runtime 后调用 Runner |
| 子命令 | `WithCommands`、`App.AddCommand`、`Command` | `CommandRunner(ctx, runtime, command)` | 选择一个命令并执行同一生命周期 |
| 位置参数 | `Command.Use/ValidateArgs` | `CommandContext.Args()` | 在 Runtime Build 前校验并生成只读快照 |
| 业务 Flags | `WithFlagBinder`、`Command.FlagBinder` | Binder 绑定字段 | 解析、Complete、Validate，不进入配置快照 |
| 配置 Flags | `CommandFlags.Config*`、内置 `--override` | ConfigManager/ConfigValue | 转换为本次 Runtime 的 pinned 配置层 |
| 配置来源 | `WithConfig(config.Option...)` | `Runtime.ConfigManager()`、`ConfigValue[T]` | 交给 Runtime Build/Close ConfigManager |
| 日志 | `WithLogging` | `Runtime.Logger(name)` 或 IOC 注入 | 在 config 后初始化，在 IOC 后关闭/flush |
| IOC | `WithBeans/WithModules/WithIOCOptions` | `Runtime.Container()` 或字段注入 | 构建 Container 并统一释放 managed beans |
| 运行前准备 | `WithBeforeRunHook` | Hook 参数中的 Runtime/CommandContext | Runner 前按声明顺序执行并聚合错误 |
| 优雅退出 | `WithShutdown(WithShutdown*...)` | Shutdown Hook | 协调信号、Runner、Hooks 和 Runtime Close |
| 测试与嵌入 | `New/ExecuteContext/WithOutput` | 显式 context、args、buffer | 不安装全局信号，返回完整错误链 |

普通路径只需要声明 Options，不需要调用方手工 Build 或 Close Runtime：

```text
Options + args
  -> Parse / Complete / Validate
  -> Runtime Build（config -> log -> ioc）
  -> BeforeRunHooks
  -> Runner
  -> Shutdown Hooks + Runner convergence
  -> Runtime Close（ioc -> log -> config）
  -> error / exit
```

一个同时使用主要能力的装配入口如下。实际应用只声明需要的 Options，不需要为了“完整”而启用所有能力：

```go
func main() {
    flags := &rootFlags{}
    servers := newServerGroup()

    app.Main(
        app.WithName("order-service"),
        app.WithDescription("Order service"),
        app.WithVersion("v1.2.3"),

        app.WithConfig(
            config.Defaults(defaultConfig()),
            config.File(config.FilePath("application.yaml"), config.FileWatch()),
            config.Provider(nacos.New()),
        ),
        app.WithConfigBinding[ApplicationConfig]("", validateConfig),
        app.WithLogging(applogzap.New()),
        app.WithModules(&OrderModule{}),
        app.WithIOCOptions(ioc.WithCloseTimeout(5*time.Second)),

        app.WithFlagBinder(flags),
        app.WithBeforeRunHook(auditCommandStart),
        app.WithShutdown(
            app.WithShutdownGracefulTimeout(20*time.Second),
            app.WithShutdownRuntimeCloseTimeout(10*time.Second),
            app.WithShutdownHook("servers", servers.Shutdown),
        ),

        app.WithRunner(func(ctx context.Context, runtime app.Runtime) error {
            service, err := ioc.ResolveAs[*OrderService](runtime.Container())
            if err != nil {
                return err
            }
            return servers.Serve(ctx, service)
        }),
        app.WithCommands(app.Command{
            Use:                  "doctor",
            Short:                "Run local diagnostics",
            RuntimeMode:          app.RuntimeModeDisabled,
            DisableShutdownHooks: true,
            Runner:               runDoctor,
        }),
    )
}
```

这里的 `config.Provider(nacos.New())` 只声明 Nacos 能力；地址、namespace 和 Sources 可以继续通过配置文件、环境变量或 `--bootstrap` 为本次运行提供。`servers` 是调用方持有的主动工作负载 controller，不属于 Runtime 内部资源。

## 1. 选择启动入口

| 入口 | 参数 | context/args 来源 | 信号行为 | 错误处理 |
|---|---|---|---|---|
| `app.Main(options...)` | 全部 App Options | `context.Background()`、`os.Args[1:]` | 安装默认或自定义信号 | 失败写入 error output 并 `os.Exit(1)` |
| `app.Run(options...)` | 全部 App Options | `context.Background()`、`os.Args[1:]` | 与 Main 相同 | 返回 error，由 main 映射退出码 |
| `app.New(options...)` | 全部 App Options | 尚不执行 | 尚不安装 | 返回可执行 `*App` |
| `(*App).Run()` | 无参数 | `context.Background()`、`os.Args[1:]` | 安装默认或自定义信号 | 返回 error |
| `(*App).ExecuteContext(ctx, args)` | `ctx`、不含程序名的参数切片 | 调用方显式提供 | 不安装进程信号 | 返回 error，适合测试/嵌入式宿主 |
| `app-runtime.Application.Build` | Runtime Options | 调用方提供 | 不处理 | 无 CLI 时的高级入口；调用方负责 Close |

普通应用从 `app.Run` 或 `app.Main` 开始，不需要创建 `app-runtime.Application`。

入口参数语义：

1. `options` 中的 nil Option 被忽略；声明冲突通常记录为 setup error，并在实际执行时 fail-fast。
2. `ExecuteContext(nil, args)` 把 nil context 归一为 `context.Background()`。
3. `args` 不包含 `os.Args[0]`；例如执行 `orderctl inspect key` 时传入 `[]string{"inspect", "key"}`。
4. `ExecuteContext` 不接管宿主的 signal handler；宿主应把信号、服务停止或测试取消转换为传入 context。
5. `Main` 的失败路径调用 `os.Exit(1)`，因此调用 `Main` 的 goroutine 中尚未执行的 defer 不会运行；应用 Runtime 已在 `Main` 退出前由 app 完成关闭。
6. `App` 缓存命令树和 Flag 状态，不保证并发安全；每个并发执行或测试用例应创建独立 App。

## 2. 创建 Root 应用

无参数时执行业务 Runner：

```go
func main() {
    app.Main(
        app.WithName("order-service"),
        app.WithDescription("Order service"),
        app.WithConfig(
            config.Defaults(defaultConfig()),
        ),
        app.WithConfigBinding[Config](""),
        app.WithRunner(runServer),
    )
}

func runServer(ctx context.Context, runtime app.Runtime) error {
    service, err := ioc.ResolveAs[*Server](runtime.Container())
    if err != nil {
        return err
    }
    return service.Run(ctx)
}
```

默认流程：解析参数，构建 Runtime，执行 hooks 和 Runner，最后关闭 Runtime。

### 2.1 应用基础 Options

| Option | 参数 | 省略时 | 作用与约束 |
|---|---|---|---|
| `WithName(name)` | root 命令名；建议非空、稳定、适合 CLI | `app` | 决定 help 中的命令名；默认同时传播为 Runtime application name 和 bootstrap env prefix 来源 |
| `WithDescription(description)` | root help 的简短说明 | `Application built by App` | 写入 root `Short`；空字符串表示不展示简介 |
| `WithVersion(version)` | `--version` 输出的完整文本 | `dev` | 只输出文本，不构建 Runtime、不执行 Hooks/Runner |
| `WithOutput(writer)` | help、version、bootstrap schema 等框架输出目标 | Cobra 默认输出 | nil 表示不覆盖默认输出；测试通常传 `*bytes.Buffer`；不重定向业务的 `os.Stdout` |
| `WithErrorOutput(writer)` | Cobra error output，以及 `app.Main` 的最终错误输出 | Cobra 默认错误输出；Main 回退 `os.Stderr` | 不改变 `Run/ExecuteContext` 的 error 返回值 |
| `WithRunner(runner)` | `func(context.Context, app.Runtime) error` | nil | 配置 Root Runner；root 没有 Runner 但存在子命令时显示 help，否则返回 `runner_required` |
| `WithCommands(commands...)` | 零个或多个 `app.Command` | 空集合 | 按声明追加子命令；叶子命令必须提供 Runner |
| `WithFlagBinder(binder)` | root 级 `FlagBinder` | nil | 声明 root 本地业务 Flags；不自动继承给子命令 |
| `WithBeforeRunHook(hook)` | `BeforeRunHook` | 空集合 | nil 被忽略；非 nil Hook 按声明顺序执行 |
| `WithShutdown(options...)` | 一组 `ShutdownOption` | 默认信号和 timeout、空 Hooks | 聚合应用级退出配置，详见第 12 节 |
| `WithRuntime(runtimeApplication)` | 非 nil `*app-runtime.Application` | 自动创建默认蓝图 | 完整替换 Runtime 蓝图；与 `WithRuntimeOptions`/Runtime 门面 Options 互斥 |
| `WithRuntimeOptions(options...)` | app-runtime Options | 空集合 | 在默认蓝图上追加高级 Runtime 配置 |

`WithConfig/WithLogging/WithBeans/WithModules/WithIOCOptions/WithConfigBinding` 都是更具体的 Runtime 门面，内部等价于追加 `WithRuntimeOptions`。因此只要使用了其中任何一项，就不要再同时使用 `WithRuntime`。

### 2.2 Root Runner 的输入与返回

```go
type Runner func(ctx context.Context, runtime app.Runtime) error
```

| 参数/返回 | 语义 |
|---|---|
| `ctx` | 本次命令的 run context；信号或宿主取消会关闭 `Done()`；Runner 应协作停止主动工作 |
| `runtime` | Build 成功且尚未关闭的只读 Runtime 门面；Root Runner 中非 nil |
| `error == nil` | Runner 正常完成；app 仍执行 Shutdown Hooks，再关闭 Runtime |
| `error != nil` | 保留原始业务错误；app 不跳过 Shutdown Hooks 和 Runtime Close |
| panic | app 先执行 Shutdown Hooks、关闭 Runtime，再恢复 panic |

Root Runner 固定使用 `RuntimeModeEnabled`。如果一个入口完全不需要 config/log/IOC，应把它声明为 `RuntimeModeDisabled` 子命令；当前没有 Root RuntimeMode 开关。

## 3. 创建 Command-Only 应用

只有子命令时不需要占位 Root Runner：

```go
func main() {
    app.Main(
        app.WithName("orderctl"),
        app.WithCommands(
            app.Command{
                Use:    "create [name]",
                Short:  "Create an order",
                Runner: createOrder,
            },
            app.Command{
                Use:         "doctor",
                Short:       "Run local diagnostics",
                RuntimeMode: app.RuntimeModeDisabled,
                Runner:      runDoctor,
            },
        ),
    )
}
```

```text
orderctl              -> 展示 root help，不构建 Runtime
orderctl create demo  -> 构建 Runtime 后执行 createOrder
orderctl doctor       -> 不构建 Runtime，只执行 runDoctor
orderctl unknown      -> 返回 unknown command
```

### 3.1 `Command` 完整参数

| 字段 | 类型 | 默认值 | 作用与约束 |
|---|---|---|---|
| `Use` | `string` | 空 | 命令用法，例如 `inspect [key]`；首个词是命令名，后续文本用于 usage 展示 |
| `Short` | `string` | 空 | root 的 Available Commands 列表和命令简介 |
| `Long` | `string` | 空 | 命令详细说明；非空时 help 优先展示 Long，否则展示 Short |
| `Example` | `string` | 空 | help 的 Examples 区域；调用方负责换行和缩进 |
| `Aliases` | `[]string` | nil | 命令别名；执行别名仍得到实际命令的稳定 `CommandContext.Name/Path` |
| `ValidateArgs` | `ArgumentValidator` | nil | 在 Runtime Build 前校验位置参数；只接收参数副本，不暴露 Cobra |
| `RuntimeMode` | `RuntimeMode` | `RuntimeModeEnabled` | Enabled 构建完整 Runtime；Disabled 给 Runner/Shutdown Hook 传 nil Runtime |
| `FlagBinder` | `FlagBinder` | nil | 声明当前子命令的本地业务 Flags，并在解析后执行可选 Complete/ValidateFlags |
| `DisableShutdownHooks` | `bool` | `false` | true 只跳过应用 Shutdown Hooks；不影响 context 取消和已构建 Runtime 的 Close |
| `Runner` | `CommandRunner` | nil | 叶子命令必填；缺失时返回 `command_runner_required` |

`RuntimeMode` 只接受两个稳定值：

| 模式 | Runtime | BeforeRunHooks | Shutdown Hooks | 典型场景 |
|---|---:|---:|---:|---|
| `RuntimeModeEnabled` | 构建，Runner 非 nil | 执行 | 默认执行，Runtime 非 nil | 服务、迁移、需要配置/日志/IOC 的业务命令 |
| `RuntimeModeDisabled` | 不构建，Runner 为 nil | 跳过 | 默认执行，Runtime 为 nil | version 之外的 doctor、格式化、纯本地诊断 |

纯 CLI 子命令通常同时设置：

```go
app.Command{
    Use:                  "doctor",
    RuntimeMode:          app.RuntimeModeDisabled,
    DisableShutdownHooks: true,
    Runner:               runDoctor,
}
```

### 3.2 `CommandRunner` 与 `CommandContext`

命令 Runner 可以读取稳定参数快照：

```go
func createOrder(ctx context.Context, runtime app.Runtime, command app.CommandContext) error {
    args := command.Args()
    return create(ctx, runtime, args[0])
}
```

```go
type CommandRunner func(
    ctx context.Context,
    runtime app.Runtime,
    command app.CommandContext,
) error
```

| 输入 | 语义 |
|---|---|
| `ctx` | 与 Root Runner 相同的 run context |
| `runtime` | Enabled 非 nil；Disabled 必须按 nil 使用 |
| `command.Name()` | 当前真实命令名，例如 `create` |
| `command.Path()` | 包含 root 的完整路径，例如 `orderctl create` |
| `command.Args()` | 不包含命令名和 Flags 的位置参数副本；每次调用返回新切片 |

### 3.3 动态追加命令

除了 `WithCommands`，还可以在构建 App 后追加命令：

```go
application := app.New(app.WithName("orderctl"))
application.AddCommand(app.Command{Use: "inspect [key]", Runner: inspect})
return application.Run()
```

`AddCommand` 应在第一次 `Run/ExecuteContext` 前完成。App 会缓存已构建的命令树；执行开始后再追加不会更新已经缓存的 root。常规静态装配优先使用 `WithCommands`，插件式启动装配才使用 `AddCommand`。

## 4. Root 与 Commands 共存

服务默认启动，同时提供迁移和诊断命令：

```go
app.Main(
    app.WithName("order-service"),
    app.WithRunner(runServer),
    app.WithCommands(
        app.Command{Use: "migrate", Runner: runMigration},
        app.Command{
            Use:         "doctor",
            RuntimeMode: app.RuntimeModeDisabled,
            Runner:      runDoctor,
        },
    ),
)
```

```text
order-service          -> 执行 runServer
order-service migrate  -> 执行 runMigration
order-service --help   -> 展示 help，不构建 Runtime
```

### 4.1 Root 默认决策

| Root Runner | Commands | 执行 root 的结果 |
|---:|---:|---|
| 有 | 任意 | root 是真实业务命令，执行 Root Runner |
| 无 | 有可用命令 | root 是命令目录，展示 help 并返回 nil |
| 无 | 无 | 返回 `app.lifecycle.runner_required` |

一次进程参数只选择 root 或一个一级子命令，不会同时执行 root Runner 和 Command Runner。当前公共 `Command` 模型不支持递归声明多级子命令树。

### 4.2 一次命令的精确阶段

```text
Cobra parse / help / ValidateArgs
  -> --version short-circuit
  -> validate App setup
  -> --list-bootstrap-keys short-circuit
  -> validate Runner exists
  -> Flag Complete / ValidateFlags
  -> Runtime Input + Build（Enabled）
  -> BeforeRunHooks（Enabled）
  -> Runner
  -> Shutdown Hooks + Runner convergence
  -> Runtime Close（Enabled）
```

注意：

1. help 是 CLI 解析层终止条件；version 和 bootstrap schema 不构建 Runtime。
2. `--version` 在 App setup 校验前短路，因此即使其他 Runtime 声明暂时无效，也可以读取版本。
3. `--list-bootstrap-keys` 需要先通过 App setup 校验，因为它必须读取完整 Runtime 蓝图中的 Provider schema。
4. Runtime Build 失败由 app-runtime 回滚已取得资源；app 只对成功 Build 的 Runtime 调用一次 Close。
5. Runner 错误不会跳过 Shutdown Hooks；BeforeRunHook 失败则 Runner/Shutdown Hooks 都不启动。

## 5. 校验命令参数

`ValidateArgs` 只接收字符串快照，不泄漏 Cobra：

```go
app.Command{
    Use: "inspect [key]",
    ValidateArgs: func(args []string) error {
        if len(args) != 1 {
            return errors.New("one key is required")
        }
        return nil
    },
    Runner: inspect,
}
```

参数校验失败时不会构建 Runtime。

`ArgumentValidator` 的参数和阶段语义：

1. `args` 是位置参数副本，不包含命令名和已经解析的 Flags。
2. nil validator 表示不增加业务参数约束；app 不隐式根据 `[name]`、`<name>` 推导数量规则。
3. 返回 nil 表示允许进入命令执行；返回非 nil error 时不会执行 Flag Complete/Validate、Runtime Build、Hooks 或 Runner。
4. `-h/--help` 由 CLI 层直接终止；其他命令执行仍先经过 Cobra 参数校验，再进入 app 的 `--version`、schema 和 Runtime 短路分支。
5. 需要校验 Flag 与位置参数的组合时，把位置参数规则放在 `ValidateArgs`，Flag 组合规则放在 `FlagValidator`；Runner 中只保留依赖运行态数据的校验。

## 6. 声明业务 Flags

`FlagBinder` 把业务 Flag 声明、字段绑定与 CLI 实现隔离：

```go
type rootFlags struct {
    // Debug 仅控制当前进程行为，不进入 Runtime 配置快照。
    Debug bool
}

// Bind 声明配置参数与进程参数。
func (this *rootFlags) Bind(flags app.CommandFlags) {
    flags.ConfigString("service.address", "Service listen address")
    flags.Bool("debug", &this.Debug, false, "Enable debug mode")
}

// Complete 在校验前补全和归一化 Flag 值。
func (this *rootFlags) Complete() error {
    return nil
}

// ValidateFlags 在 Runtime Build 前校验 Flag 组合。
func (this *rootFlags) ValidateFlags() []error {
    return nil
}

app.Run(
    app.WithFlagBinder(&rootFlags{}),
    app.WithRunner(run),
)
```

| 类型 | 用途 | 是否进入 Runtime config |
|---|---|---:|
| `Config*` | 配置 pinned override | 是 |
| `String/Int/Bool/...` | 进程、命令行为 | 否 |

`FlagCompleter` 用于补全与归一化，`FlagValidator` 用于校验参数组合；app 通过同一个 `FlagBinder` 的可选接口自动发现这两项能力。配置快照校验使用 `app.WithConfig(config.Validate[T](...))`。

### 6.1 Binder 配置位置和作用域

| 配置位置 | 注册到哪里 | 谁能使用 | Complete/Validate 执行顺序 |
|---|---|---|---|
| `WithFlagBinder(rootBinder)` | root 本地 Business Flags | 只在 root 命令行出现；不会继承给子命令 | 每个实际业务命令执行时先处理 rootBinder |
| `Command.FlagBinder` | 当前子命令本地 Business Flags | 只在该子命令出现 | rootBinder 之后处理当前 command binder |
| app 内置 Framework Flags | root persistent Flags | root 和全部子命令继承 | 由 app 内部解析，不调用业务 Binder |

如果 root 和子命令都需要同一个业务 Flag，应由共享函数分别绑定到两个 Binder，不能假设 root 本地 Flag 会被继承。不要在同一命令上重复注册同名 Flag，否则底层 CLI 会拒绝或 panic。

`Bind` 在命令树首次构建时调用，不是在每次 Runner 前调用。Binder 应只声明 Flags，不应读取 Runtime、连接外部服务或启动 goroutine。

### 6.2 配置覆盖 Flags：`Config*`

`Config*` 只接收 `name` 和 `usage`，不绑定业务字段。只有用户显式传入的 Flag 才进入 pinned 层；未传入时，内部零值不会覆盖 default/file/remote 配置。

| 方法 | CLI 值类型 | 典型输入 | 写入 Snapshot 的 Go 值 |
|---|---|---|---|
| `ConfigString(name, usage)` | string | `--service.name=orders` | `string` |
| `ConfigInt(name, usage)` | int | `--worker.count=8` | `int` |
| `ConfigInt64(name, usage)` | int64 | `--limit.bytes=1048576` | `int64` |
| `ConfigUint(name, usage)` | uint | `--worker.batch=32` | `uint` |
| `ConfigUint64(name, usage)` | uint64 | `--feature.mask=7` | `uint64` |
| `ConfigBool(name, usage)` | bool | `--feature.enabled` 或 `--feature.enabled=false` | `bool` |
| `ConfigFloat64(name, usage)` | float64 | `--sampling.rate=0.25` | `float64` |
| `ConfigDuration(name, usage)` | Go duration | `--server.timeout=1500ms` | `time.Duration` |
| `ConfigStringArray(name, usage)` | 可重复 string | `--tag=a --tag=b` | `[]string{"a", "b"}` |

参数约束：

1. `name` 直接成为 CLI Flag 名；配置 key 推荐使用稳定 dotted path，例如 `server.http.timeout`。
2. `usage` 展示在 Business Flags help 分组，应说明单位、允许值和默认来源，不要只写字段名。
3. `ConfigDuration` 使用 Go duration 语法，例如 `500ms`、`10s`、`2m`，不是裸毫秒整数。
4. `ConfigStringArray` 保留每次出现的原始字符串，不按逗号自动拆分。
5. 同一 key 同时由 `--override` 和显式 `Config*` 提供时，`Config*` 值优先。

### 6.3 普通业务 Flags

无前缀方法把解析结果写入调用方提供的目标字段，不进入配置 Snapshot：

| 方法 | target 类型 | defaultValue 类型 | 示例 |
|---|---|---|---|
| `String` | `*string` | `string` | `flags.String("format", &this.Format, "table", "Output format: table|json")` |
| `Int` | `*int` | `int` | `flags.Int("limit", &this.Limit, 100, "Maximum result count")` |
| `Int64` | `*int64` | `int64` | `flags.Int64("offset", &this.Offset, 0, "Start offset")` |
| `Uint` | `*uint` | `uint` | `flags.Uint("retries", &this.Retries, 3, "Retry count")` |
| `Uint64` | `*uint64` | `uint64` | `flags.Uint64("revision", &this.Revision, 0, "Expected revision")` |
| `Bool` | `*bool` | `bool` | `flags.Bool("dry-run", &this.DryRun, false, "Validate without writing")` |
| `Float64` | `*float64` | `float64` | `flags.Float64("ratio", &this.Ratio, 1, "Traffic ratio")` |
| `Duration` | `*time.Duration` | `time.Duration` | `flags.Duration("timeout", &this.Timeout, 5*time.Second, "Operation timeout")` |
| `StringArray` | `*[]string` | `[]string` | `flags.StringArray("label", &this.Labels, nil, "Label key=value; repeatable")` |

`target` 必须是可写的非 nil 指针，并且其生命周期至少覆盖 App 执行。默认值会在绑定阶段写入 target，即使用户没有显式传 Flag；业务通过 Binder 结构字段读取最终值。

### 6.4 Complete 与 ValidateFlags

一个 Binder 可以按需同时实现三个接口：

```go
type FlagBinder interface { Bind(CommandFlags) }
type FlagCompleter interface { Complete() error }
type FlagValidator interface { ValidateFlags() []error }
```

固定顺序：

```text
Parse Flags
  -> root Complete
  -> root ValidateFlags
  -> command Complete
  -> command ValidateFlags
  -> Build Runtime
```

同一个 Binder 内即使 `Complete` 返回错误，app 仍调用 `ValidateFlags`，以便一次返回全部参数问题；nil error 元素被忽略，其他错误通过 `errors.Join` 聚合。存在任何非 nil 错误时不构建 Runtime。

职责划分：

| 阶段 | 应做 | 不应做 |
|---|---|---|
| `Bind` | 声明名称、类型、默认值、usage，绑定普通字段 | 读取解析结果、访问 Runtime、连接外部系统 |
| `Complete` | Trim、大小写归一、派生默认值、去重排序 | 校验配置 Snapshot、启动资源 |
| `ValidateFlags` | 校验必填、范围、互斥/依赖组合 | 访问尚未 Build 的 ConfigManager/IOC |
| Config validator | 校验 default/file/remote/pinned 合并后的 typed 配置 | 校验纯 CLI 输出格式等命令行为 |

## 7. 内置 Flags

本节是 `app` 内置 flags 的权威清单。除 `-h/--help` 由每个命令分别注册外，其余内置 flags 都是 root persistent flags，会被子命令继承。

| Flag | 值与默认值 | 作用阶段与结果 | 是否终止命令生命周期 |
|---|---|---|---|
| `-h, --help` | bool；默认 `false` | 输出当前 root/子命令 help | 是；不构建 Runtime，不执行 hooks/Runner |
| `--version` | bool；默认 `false` | 输出 `WithVersion` 设置的版本；默认 `dev` | 是；root/子命令均不构建 Runtime，不执行 hooks/Runner |
| `-c, --config` | string；默认空 | bootstrap 阶段选择本地配置文件 | 否；进入 `RuntimeModeEnabled` 的 Runtime Build |
| `--bootstrap` | `key=value`；可重复 | 写入 bootstrap overrides；`--config` 与 `--bootstrap config=...` 不一致时失败 | 否；进入 `RuntimeModeEnabled` 的 Runtime Build |
| `--bootstrap-env-prefix` | string；默认空 | 指定 bootstrap 环境变量前缀；省略时由 Runtime application name 派生 | 否；进入 `RuntimeModeEnabled` 的 Runtime Build |
| `--bootstrap-without-config-file` | bool；默认 `false` | 单次运行禁用显式配置文件和默认文件发现；不能与显式/环境变量配置文件并存 | 否；进入 `RuntimeModeEnabled` 的 Runtime Build |
| `--override` | `key=value`；可重复 | 写入最终 pinned 配置；同 key 的显式 `Config*` flag 优先 | 否；进入 `RuntimeModeEnabled` 的 Runtime Build |
| `--list-bootstrap-keys[=table\|json]` | 无值时默认 `table` | 输出 config core 与已注册 provider 的 bootstrap schema | 是；不构建 Runtime，不执行 hooks/Runner |

### 7.1 输出、继承和 help 分组

1. Framework Flags 是 root persistent Flags，root 和子命令都可在选中命令上使用。
2. `-h/--help` 是每个命令自己的本地 Flag，但展示结果中也归入 Framework Flags。
3. app 把 help 分成 `Framework Flags` 与 `Business Flags`；业务 Flags 按 `CommandFlags` 声明自动分类，无需调用方维护 annotation。
4. help 优先展示 `Command.Long`，为空时展示 `Short`；随后展示 Usage、按名称排序的子命令、两组 Flags 和 Example。
5. `WithOutput` 控制 help、version、bootstrap schema 的输出目标；`WithErrorOutput` 控制错误目标。业务 Runner 写 stdout/stderr 时仍由业务自行选择 writer。

### 7.2 `--bootstrap` 与 `--override` 表达式

两者都使用可重复的 `key=value`，但阶段不同：

```bash
order-service \
  --bootstrap nacos.address=127.0.0.1:8848 \
  --bootstrap nacos.namespace=prod \
  --override service.address=:28080 \
  --override feature.enabled=true
```

| 规则 | 结果 |
|---|---|
| 缺少 `=` | `app.cli.invalid_expression` |
| key 为空 | `app.cli.empty_key` |
| `--bootstrap bootstrap.xxx=...` | `app.cli.invalid_bootstrap_key`；key 不应重复带 bootstrap 根前缀 |
| `--bootstrap config=` | `app.cli.empty_config_file` |
| 同一 key 重复 | 后出现值覆盖前值 |
| dotted key | 转换为嵌套 map，例如 `server.http.port=8080` |

字面量转换规则同时用于 bootstrap 和 override：

| 输入 | 结果类型 |
|---|---|
| `true` / `false` | bool |
| `null` / `nil` / `~` | nil |
| `123` / `-123` | int64 |
| `01` / `-01` | 保留 string，避免丢失前导零 |
| `1.25` / `1e3` | float64 |
| `+1`、普通文本、地址 | 保留原 string |

需要强制字符串且内容看起来像 bool/数字时，优先声明对应的 `ConfigString` Flag；`--override` 当前没有额外的引号类型标记。

### 7.3 配置文件和环境变量前缀

1. `--config path` 与 `--bootstrap config=path` 都选择本次启动文件；两者同时出现时值必须一致。
2. `--bootstrap config=` 会失败；空 `--config=` 当前归一为没有有效显式路径，因此不要用它表达“禁用文件”，禁用必须使用 `--bootstrap-without-config-file`。
3. `--bootstrap-env-prefix PREFIX` 覆盖本次 Runtime 的 bootstrap 环境变量前缀，显式空值失败。
4. 未提供 prefix 时，从最终 Runtime application name 派生：转为大写并把 `-` 替换成 `_`，例如 `order-service` → `ORDER_SERVICE`。
5. `--bootstrap-without-config-file` 为 true 时给本次 Runtime 注入 `config.FileDisabled()`；它不修改应用的稳定 Config 声明。

### 7.4 Bootstrap schema 输出

`--list-bootstrap-keys` 汇总 config core schema 与 `WithConfig(config.Provider(...))` 中 Provider 暴露的 schema：

```bash
order-service --list-bootstrap-keys        # table
order-service --list-bootstrap-keys=table
order-service --list-bootstrap-keys=json
```

table 包含 key、type、required、default、provider、description；JSON 额外包含四个 bootstrap CLI contract 字段。未知 mode 返回 `app.cli.list_bootstrap_keys_invalid_mode`。该命令需要一个有效的 App/Runtime 蓝图声明，但不会建立 ConfigManager、Provider Session、Logger 或 IOC Container。

示例：

```bash
order-service --config application.yaml
order-service --bootstrap nacos.address=127.0.0.1:8848
order-service --override service.address=:8080
order-service --list-bootstrap-keys=json
order-service create --version
```

`Config*` 和无前缀的类型方法不是额外的内置 flag 前缀，而是 `CommandFlags` 的业务参数方法族。传入的 `name` 原样作为 CLI flag 名：

```go
flags.ConfigString("service.address", "Service address") // --service.address
flags.Bool("debug", &this.Debug, false, "Debug")          // --debug
```

`RuntimeModeDisabled` 命令不会构造 `app-runtime.BuildInput`，因此继承到该命令上的 config/bootstrap/override flags 只会被 CLI 解析，不会产生配置效果。`-h/--help`、`--version` 和 `--list-bootstrap-keys` 仍按表中终止语义执行。

## 8. 使用配置

app 把稳定来源声明与本次 CLI 输入交给 app-runtime，构建并持有唯一的 ConfigManager。完整流程是：

```text
稳定来源声明 + 本次 CLI 输入
  -> Runtime 构建 ConfigManager
  -> 按固定层级发布 Snapshot
  -> 业务通过 ConfigValue 或 ConfigManager 读取结果
```

业务不自行创建第二个 Manager，也不负责关闭 Runtime 持有的 Manager。

### 8.1 App Runtime 门面

app 为高频 Runtime 能力提供一组聚合 Options。它们只负责声明蓝图，不会在 `app.New` 时立即连接外部系统；真正构建发生在选中 `RuntimeModeEnabled` 命令之后。

| Option | 参数 | 可否重复 | Build 结果 |
|---|---|---:|---|
| `WithConfig(options...)` | `config.Option` 列表；nil 被 Runtime 忽略 | 是，按声明顺序追加 | 创建一个 ConfigManager |
| `WithConfigBinding[T](scope, validators...)` | typed 配置类型、dotted scope、typed validators | 是 | 每次 Build 创建并注册独立 `*ConfigValue[T]` |
| `WithLogging(logging)` | 一个实现 `app.Logging` 的日志 bootstrap | 可重复，后一个非 nil 实现覆盖前一个 | config 后创建日志 Provider/Factory |
| `WithBeans(beans...)` | managed bean 实例列表 | 是，按输入顺序注册 | 进入 IOC Build；nil/typed nil 在获取资源前失败 |
| `WithModules(modules...)` | `app.Module` 列表 | 是 | 调用 Module.Register；空名/同名/typed nil 在 Build 前失败 |
| `WithIOCOptions(options...)` | `ioc.Option` 列表 | 是，按声明顺序追加 | 创建 Container 时应用高级策略 |

这些 Options 最终都追加到默认 `app-runtime.Application`。如果需要完整替换蓝图，应只使用 `WithRuntime(customApplication)`，不能再组合上表入口。

#### 8.1.1 `Runtime` 提供的业务能力

Runner 和 Hooks 得到的 `app.Runtime` 是只读能力门面：

| 方法 | 参数 | 返回 | 典型使用位置 |
|---|---|---|---|
| `Info()` | 无 | `app-runtime.ApplicationInfo` 副本 | 启动日志、诊断输出、应用身份 |
| `ConfigManager()` | 无 | 本次 Runtime 的 `config.Manager` | Runner/诊断读取 Snapshot、Observe/Subscribe |
| `Logger(name)` | 稳定低基数组件名 | 绑定本次日志 Provider 的 `log.Logger` | Runner、Hooks、装配边界日志 |
| `Container()` | 无 | 已完成 Build 的 `ioc.Container` | Runner 解析一个业务入口、测试和诊断 |

Runtime 不暴露 `Close`。Runner、BeforeRunHook、ShutdownHook 只能在自己的调用期间借用 Runtime；关闭责任始终属于 app。

```go
app.WithRunner(func(ctx context.Context, runtime app.Runtime) error {
    info := runtime.Info()
    logger := runtime.Logger("order.bootstrap")
    snapshot := runtime.ConfigManager().Current()
    service, err := ioc.ResolveAs[*OrderService](runtime.Container())
    if err != nil {
        return err
    }
    logger.Info(ctx, "application started",
        log.F("op", "app.start"),
        log.F("result", "success"),
        log.F("application", info.Name),
        log.F("config_version", snapshot.Version()),
    )
    return service.Serve(ctx)
})
```

普通业务对象不应持有 Runtime 或 Container；应通过 IOC 字段注入 `ConfigValue[T]`、Logger 和领域依赖。

#### 8.1.2 `WithConfigBinding[T]` 参数

```go
app.WithConfigBinding[OrderConfig]("order", validateOrderConfig)
```

| 参数 | 语义 |
|---|---|
| `T` | 解码目标业务配置类型；`ConfigValue.Load` 返回 T 的浅拷贝 |
| `scope` | Trim 后的 dotted path；`"order"` 解码 `order` 子树，空字符串解码完整 Snapshot |
| `validators ...func(*T) []error` | 可选；nil validator 被忽略，其他 validator 按声明顺序全部执行并聚合错误 |

validator 在 Snapshot 发布前执行。初始配置不能解码或校验失败时 Runtime Build 失败；刷新候选失败时不替换当前 Snapshot。validator 应只检查并返回错误，对入参的修改不会写回 Snapshot。成功声明会向 IOC 注册 `*app-runtime.ConfigValue[T]`，业务 Bean 可以按类型字段注入；同一 `T+scope` 不应重复声明，否则会形成同类型同名称冲突。

`Load` 的浅拷贝意味着结构体顶层值独立，但内部 map/slice/pointer 仍可能共享引用；业务应把配置视为只读，不原地修改嵌套成员。

### 8.2 区分来源声明与运行输入

`WithConfig` 直接接收 config 模块的唯一 Option 词汇。App 常用声明如下：

| config Option | 参数与默认值 | 用途 |
|---|---|---|
| `config.Defaults(value)` | struct/map；可重复，后声明覆盖同 key | 最低优先级默认配置 |
| `config.File(fileOptions...)` | 默认启用、默认发现文件、不 watch | 聚合文件路径、禁用和监听策略 |
| `config.FilePath(paths...)` | 多个非空路径，按输入顺序追加 | 显式文件；同层后文件覆盖前文件 |
| `config.FileDisabled()` | 无参数 | 严格关闭文件读取和默认发现；不能与 path/watch 并用 |
| `config.FileWatch(interval...)` | 启用 watch；首个正 interval 生效，否则配置值/30s 回退 | 轮询本地文件变化 |
| `config.Provider(provider)` | config Provider；nil 忽略，名称必须非空且唯一 | 声明远程或自定义配置来源 |
| `config.Validate[T](validator)` | 完整 Snapshot 解码为 T；nil validator 忽略 | 首次/刷新发布前 typed 校验 |
| `config.OnRefreshError(handler)` | 后声明覆盖；回调应快速非阻塞 | 观测异步文件/Provider refresh 失败 |
| `config.PinnedValues(values)` | map，后声明覆盖 | 代码/测试固定最高层；生产 CLI 更常用 `--override/Config*` |

`BootstrapEnvPrefix`、`BootstrapOverrides` 通常由 app 从应用名和内置 Flags 自动构造；普通 App 不需要在代码中重复声明，除非编写嵌入式适配层。

| 来源 | 稳定声明 | 本次运行输入 | 进入最终 Snapshot |
|---|---|---|---:|
| default | `config.Defaults` | 无 | 是，最低优先级 |
| file | `config.File` 或默认文件发现 | `--config`、`--bootstrap config=...` | 是；文件内 `bootstrap` 块除外 |
| remote | `config.Provider` 注册 Provider 类型 | Provider 构造参数、bootstrap file/env/CLI | 是 |
| pinned | `config.PinnedValues` | `--override`、显式 `Config*` Flag | 是，最高优先级 |

Provider 是稳定能力声明；连接参数和 Sources 是 bootstrap。bootstrap 只决定“从哪里加载”，不会进入最终业务 Snapshot。Provider 对应的 bootstrap 未声明时，`Open` 返回 nil Session，表示该 Provider 本次未启用。

### 8.3 从多种来源构建一份配置

下面的应用同时声明 default、两个有序文件、两个 Nacos Sources 和一个业务配置 Flag：

```go
type configFlags struct{}

// Bind 声明本次进程允许通过 CLI 固定覆盖的业务配置键。
func (this *configFlags) Bind(flags app.CommandFlags) {
    flags.ConfigInt("worker.concurrency", "Worker concurrency")
}

app.Main(
    app.WithName("order-service"),
    app.WithConfig(
        config.Defaults(defaultConfig()),
        config.File(
            config.FilePath("application.yaml", "application-prod.yaml"),
            config.FileWatch(),
        ),
        config.Provider(nacos.New(nacos.Bootstrap{
            Address: "127.0.0.1:8848",
            Sources: []nacos.BootstrapSource{
                {GroupID: "DEFAULT_GROUP", DataID: "common.yaml", Priority: 10},
                {GroupID: "DEFAULT_GROUP", DataID: "orders.yaml", Priority: 20},
            },
        })),
    ),
    app.WithFlagBinder(&configFlags{}),
    app.WithConfigBinding[ApplicationConfig]("", validateConfig),
    app.WithBeans(&OrderService{}),
    app.WithRunner(runServer),
)
```

假设各来源包含这些值：

| 来源层 | 关键输入值 |
|---|---|
| default | `service.name=orders`、`service.address=:8080`、`worker.concurrency=4`、`feature.enable_cache=false` |
| `application.yaml` | `service.name=orders-file`、`service.address=:18080`、`worker.concurrency=8` |
| `application-prod.yaml` | `service.name=orders-prod` |
| Nacos `common.yaml`，priority=10 | `worker.concurrency=16`、`feature.enable_cache=true` |
| Nacos `orders.yaml`，priority=20 | `service.name=orders-remote`、`service.address=:19090` |

本次启动再提供两个 pinned 覆盖；显式 `Config*` Flag 对同 key 的优先级高于 `--override`：

```bash
order-service --override service.address=:28080 --override worker.concurrency=24 --worker.concurrency=32
```

### 8.4 理解层级和最终结果

上述来源按以下顺序合并：

```text
default(100)
  < application.yaml(200)
  < application-prod.yaml(200)
  < Nacos common.yaml(310)
  < Nacos orders.yaml(320)
  < CLI pinned(10000)
```

同为 file 层时，后声明文件覆盖先声明文件；remote 的实际层级是 `300 + source.priority`。相同 remote priority 先按 Provider 名保持稳定顺序，同一 Provider 内后声明 Source 覆盖先声明 Source。最终结果为：

| 配置键 | 最终值 | 最终来源 |
|---|---:|---|
| `service.name` | `orders-remote` | Nacos `orders.yaml`，priority=20 |
| `service.address` | `:28080` | `--override` pinned |
| `worker.concurrency` | `32` | 显式 `ConfigInt` Flag pinned |
| `feature.enable_cache` | `true` | Nacos `common.yaml`，priority=10 |

文件和 remote 都按 map 深度合并；标量和 slice 整体替换。更完整的同层覆盖、optional source 和刷新规则见 [Config 使用指南](config-usage.md)。

### 8.5 在业务中获取配置结果

普通业务 Bean 优先注入 `ConfigValue[T]`。每次 `Load` 都读取 ConfigManager 最新一次合法发布的 typed 配置；候选配置未通过 `validateConfig` 时不会替换当前值：

```go
type OrderService struct {
    Config *appruntime.ConfigValue[ApplicationConfig]
    Logger log.Logger
}

// Serve 从 typed 配置视图读取最新合法配置并启动服务。
func (this *OrderService) Serve(ctx context.Context) error {
    current := this.Config.Load()
    return serve(ctx, current, this.Logger)
}

// runServer 只在装配边界解析业务入口，不把 Container 传入业务层。
func runServer(ctx context.Context, runtime app.Runtime) error {
    service, err := ioc.ResolveAs[*OrderService](runtime.Container())
    if err != nil {
        return err
    }
    return service.Serve(ctx)
}
```

Runner、诊断命令或需要多个 key 严格来自同一版本时，可以读取不可变 Snapshot：

```go
snapshot := runtime.ConfigManager().Current()

var current ApplicationConfig
if err := snapshot.Decode("", &current); err != nil {
    return err
}

if meta, ok := snapshot.Explain("service.address"); ok {
    runtime.Logger("config").Info(
        ctx,
        "config value explained",
        log.F("key", "service.address"),
        log.F("source", meta.Source),
        log.F("layer", meta.Layer),
        log.F("priority", meta.Priority),
        log.F("pinned", meta.Pinned),
    )
}
```

读取入口按场景选择：

| 场景 | 推荐入口 |
|---|---|
| 普通业务 Bean 持续读取最新 typed 配置 | `app.WithConfigBinding[T]` + `ConfigValue[T].Load()` |
| 多个 key 必须来自同一个发布版本 | `runtime.ConfigManager().Current()` + `Snapshot.Decode/Get/getters` |
| IOC 外主动读取最新 typed scope | `config.Observe[T](...).Current()` |
| 配置发布后触发缓存切换等副作用 | `config.Subscribe[T](...)` |
| 排查最终值来自哪个来源层 | `Snapshot.Explain` |

### 8.6 文件选择、bootstrap 与生命周期边界

未显式声明 `FilePath` 且不传 `--config` 时，按顺序发现 `application.yaml`、`application.yml`、`config/application.yaml`、`config/application.yml`，只选择首个存在文件；全部不存在时允许无文件运行。

代码显式声明的 `FilePath` 列表优先于 CLI/bootstrap 文件路径。如果希望部署时通过 `--config` 选择单个文件，就不要同时在代码中固定 `FilePath`。`--bootstrap-without-config-file` 是严格禁用，不能与任何显式文件路径或 file watch 同时使用。

密码、token 等敏感 bootstrap 不得写入日志；`Snapshot.Explain` 只返回来源元信息，不返回凭据。ConfigManager 由 Runtime 创建和关闭，Runner、hooks 和业务 Bean 都不负责调用 `Close`。完整 Provider、refresh、Observe 和 Subscribe 语义见 [Config 使用指南](config-usage.md)。

## 9. 使用日志

常规应用通过 Runtime Option 选择实现，通过配置文件设置行为，通过 Runtime 获取 logger：

```go
app.Run(
	app.WithLogging(applogzap.New()),
	app.WithRunner(runServer),
)
```

`WithLogging(logging app.Logging)` 的参数是日志 Runtime 适配器，不是 `log.Logger` 实例。适配器契约为：在 ConfigManager 首次发布后调用 `Build(ctx, manager)`，返回本次 Runtime 独占的 `log.Provider`。Provider 成功交付后由 Runtime 在 IOC 关闭之后负责 Close/flush。

声明规则：

1. 省略 `WithLogging` 时 Runtime 使用无外部 Provider 的基础 LoggerFactory，业务仍可取得 logger，但没有具体输出实现保证。
2. 多次声明时，后一个非 nil Logging 覆盖前一个。
3. nil Logging 被忽略；interface 中的 typed nil 会在 Runtime Build 获取资源前失败。
4. Logging Build 失败时不进入 IOC Build，Runtime 回滚已经创建的 ConfigManager。
5. 业务不保存或关闭 Provider；只获取 `runtime.Logger(name)` 或注入 `log.Logger`。

`app-log-zap` 不依赖 app-runtime；`Bootstrap.Build` 通过 Go 结构化接口满足 `Logging` 契约。旧 `applogzap.AsRuntimeOption` 已删除。

`app-log-zap` 默认读取 `log.zap` scope：

```yaml
log:
  zap:
    level: info
    mode: console
    format: json
```

```go
func runServer(ctx context.Context, runtime app.Runtime) error {
    logger := runtime.Logger("order.service")
    logger.Info(ctx, "order service running",
        log.F("op", "order.serve"),
        log.F("result", "running"),
    )
    return serve(ctx, logger)
}
```

`Runtime.Logger(name)` 参数应是稳定、低基数、能够定位代码组件的名称，例如 `order.service`、`order.repository`。它不是日志 level、category 配置路径，也不应用于 request ID/user ID；动态请求信息应作为字段或 context 内容传入。

1. logger 名称使用稳定组件名，不使用请求 ID、用户 ID 等高基数值。
2. 请求字段通过显式 `log.Field` 或类型化 `ContextExtractor` 注入；不得记录 token、密码或完整凭据。
3. 运行期配置刷新当前只动态应用 level；输出目标、格式和轮转配置需要重启生效。
4. 既有配置使用 `log.*` 时，显式传入 `applogzap.WithScope("log")`；新应用优先保留默认 `log.zap`。
5. Runtime 负责 provider 初始化、flush 和 Close，业务不自行关闭 logger/provider。

完整字段、Provider 配置、动态级别和文件轮转语义见 [Log 使用指南](log-usage.md)。

## 10. 使用 IOC

应用私有对象直接使用 `WithBeans`；跨应用复用的装配使用 Module。调用方不手工创建、Build 或 Close Container：

| 配置入口 | 参数 | 声明阶段行为 | 失败语义 |
|---|---|---|---|
| `WithBeans(beans...)` | 已构造的 bean 实例 | 按输入顺序保存，Runtime Build 时注册为 managed bean | nil/typed nil 在获取 Runtime 资源前 fail-fast；注册/注入/Init 失败触发 Build 回滚 |
| `WithModules(modules...)` | 实现 `Name/Register` 的 Module | 按声明顺序调用 Register | nil Module 忽略；typed nil、空名称、同名 Module 失败 |
| `WithIOCOptions(options...)` | `ioc.Option` | 创建 Container 时，在默认 IOC logger 后按顺序应用 | 由具体 IOC Option 定义；面向 close policy/timeout/logger 等高级策略 |

```go
type OrderModule struct{}

func (this *OrderModule) Name() string {
    return "order.service"
}

func (this *OrderModule) Register(registry ioc.Registry) error {
    if err := registry.Register(&OrderRepository{}); err != nil {
        return err
    }
    return registry.Register(&OrderService{})
}

type OrderService struct {
    Repository *OrderRepository
    Config     *appruntime.ConfigValue[OrderConfig]
    Logger     log.Logger
}
```

`Module` 参数契约：

```go
type Module interface {
    Name() string
    Register(registry ioc.Registry) error
}
```

1. `Name()` 必须返回非空稳定名称，用于重复检测和错误定位，不能使用运行期随机值。
2. `Register` 只接收最小 `ioc.Registry`，只能声明 bean，不能 Resolve、Build 或 Close Container。
3. Register 返回错误时停止后续 Module，Runtime Build 回滚 config/log/已取得的 IOC 资源。
4. 连接创建放在 bean 的 `Init(ctx)`，释放放在 `Close(ctx)`；Module 本身不是第二套生命周期。

```go
app.Run(
    app.WithConfigBinding[OrderConfig]("order", validateOrder),
    app.WithModules(&OrderModule{}),
    app.WithRunner(func(ctx context.Context, runtime app.Runtime) error {
        service, err := ioc.ResolveAs[*OrderService](runtime.Container())
        if err != nil {
            return err
        }
        return service.Serve(ctx)
    }),
)
```

运行规则：

1. Module 只声明 bean，不建立连接、启动 goroutine，也拿不到 Resolve/Build/Close 能力。
2. ConfigManager、Logger、LoggerFactory、ConfigValues 等基础对象以 borrowed bean 注册，可以直接字段注入。
3. `runtime.Container()` 只停留在 Runner、测试和诊断等装配边界；业务对象通过字段表达依赖，不把 Container 当 Service Locator。
4. 资源初始化和释放放在 bean 的 `ioc.Initializer/Closer`，Runtime 统一 Build 和 Close。
5. Close timeout、循环依赖等高级策略通过 `app.WithIOCOptions(...)` 配置。

Runtime 在业务 Modules 前注册以下 borrowed 基础对象：

| 能力 | IOC 声明/名称 | 所有权 |
|---|---|---|
| `*app-runtime.Runtime` | `Runtime` | app-runtime；IOC 不 Close |
| `*app-runtime.ApplicationInfo` | `ApplicationInfo` | app-runtime |
| `config.Manager` | 接口声明，名称 `ConfigManager` | app-runtime |
| 默认 `log.Logger` | 接口声明，名称 `Logger` | 日志 Provider/Runtime |
| `log.LoggerFactory` | 接口声明，名称 `LoggerFactory` | 日志 Provider/Runtime |
| `*ConfigValue[T]` | 具体类型，稳定生成名称 | app-runtime |

这些对象以 borrowed 方式注册，不由 IOC 执行 Init/Close；Runtime 会在 IOC 之后关闭 logging/config。业务 Bean 可以注入它们，但不取得释放责任。

完整声明、名称、集合、生命周期和循环依赖规则见 [IOC 使用指南](ioc-usage.md)。

## 11. 使用 Hooks

`BeforeRunHook` 只用于 Runtime Build 成功后、Runner 启动前的准备：

```go
type BeforeRunHook func(
    ctx context.Context,
    runtime app.Runtime,
    command app.CommandContext,
) error
```

```go
app.WithBeforeRunHook(func(ctx context.Context, runtime app.Runtime, command app.CommandContext) error {
    runtime.Logger("audit").Info(ctx, "command running",
        log.F("op", "command"),
        log.F("result", "running"),
        log.F("command", command.Path()),
    )
    return nil
})
```

```text
Runtime Build -> BeforeRunHooks -> Runner -> Shutdown Hooks + Runner convergence -> Runtime Close
```

1. Hooks 按声明顺序执行并聚合错误；任一错误都使 Runner 不启动。
2. Hook 不能启动 Server、Consumer、Scheduler 等长期主动工作负载。
3. Hook 失败前必须撤销自己创建的临时副作用；app 仍关闭已经构建的 Runtime。
4. `RuntimeModeDisabled` 不构建 Runtime，也不执行 BeforeRunHooks。
5. 停止主动工作负载使用下一节的 Shutdown Hook；关闭 Runtime 内部资源交给 Runtime Close。

参数和返回：

| 参数/返回 | 语义 |
|---|---|
| `ctx` | run context；如果此时已取消，Hook 应尽快返回取消原因或完成最小回滚 |
| `runtime` | 已 Build 且存活的 Runtime；BeforeRunHook 只在 RuntimeModeEnabled 下执行，因此非 nil |
| `command` | 当前 root 或子命令的稳定快照；root 也会提供 Name/Path/Args |
| nil error | 当前准备完成，继续执行后续 BeforeRunHooks |
| non-nil error | 继续执行同阶段其他 BeforeRunHooks 以收集错误，但最终不启动 Runner/Shutdown Hooks |
| panic | Runtime 仍通过 defer 关闭，panic 继续向上传播 |

`WithBeforeRunHook(nil)` 被忽略；多次调用按声明顺序追加。Hook 适合启动审计、tracing span 准备或不产生长期工作负载的可回滚检查，不适合建立必须由 Shutdown Hook 才能停止的 Server/Consumer。

## 12. 优雅关闭常驻服务

`app.Run/Main` 默认监听退出信号，并把信号、宿主 context 取消和 Runner 结果收敛到同一条 Shutdown 流程：

```text
退出触发
  -> cancel(run context)
  -> Shutdown Hooks 与 Runner 并行收敛
  -> Runtime Close
  -> Error/Exit
```

核心不变量：

> Shutdown Hooks 已全部返回，并且 Runner 已完全退出后，app 才关闭 Runtime。

本文使用 `Server` 统称 HTTP、gRPC、管理端口等常驻请求入口。app 只要求 Server 在 Runtime Close 前停止，不规定 Server 来自哪里、Runtime 内部如何装配或关闭资源；同一个 Server 必须只有一个生命周期所有者。

下文的 Shutdown/Force Close 表示通用生命周期动作：HTTP 通常对应 `Shutdown/Close`，gRPC 通常对应 `GracefulStop/Stop`。

公共配置形态：

```go
app.WithShutdown(
    app.WithShutdownSignals(os.Interrupt, syscall.SIGTERM),
    app.WithShutdownGracefulTimeout(20*time.Second),
    app.WithShutdownRuntimeCloseTimeout(10*time.Second),
    app.WithShutdownHook("http", shutdownHTTP),
    app.WithShutdownHook("grpc", shutdownGRPC),
)
```

| 入口 | 参数 | 默认/组合规则 | 无效输入 |
|---|---|---|---|
| `WithShutdown(options...)` | `ShutdownOption` 列表 | 可调用一次或多次，内部声明累积 | nil option 记录 setup error |
| `WithShutdownSignals(signals...)` | 一个或多个 `os.Signal` | 替换平台默认集合；整个 App 只能声明一次 | 空集合、nil signal、重复声明失败 |
| `WithShutdownGracefulTimeout(d)` | 正 `time.Duration` | 默认 15s；所有 Hooks 共享同一个 graceful deadline；只能声明一次 | `d <= 0`、重复声明失败 |
| `WithShutdownRuntimeCloseTimeout(d)` | 正 `time.Duration` | 默认 10s；独立于 Hook context；只能声明一次 | `d <= 0`、重复声明失败 |
| `WithShutdownHook(name, hook)` | 稳定名称、`ShutdownHook` | 可重复注册不同名称；执行时并发 | Trim 后空名、重复名、nil hook 失败 |

重复单值声明不会采用“最后一个覆盖”，而是在执行前 fail-fast，避免装配顺序悄悄改变退出策略。Hook name 进入错误中的 `name` 字段，应使用 `http`、`grpc`、`consumer.orders` 等稳定低基数名称。

### 12.1 声明 Shutdown Group

Runner 与 Shutdown Hook 应共享同一个并发安全 controller。Runner 负责运行工作负载，Hook 负责停止并等待同一工作负载：

```go
func main() {
    servers := newServerController()
    app.Main(
        app.WithName("order-service"),
        app.WithRunner(func(ctx context.Context, runtime app.Runtime) error {
            handler, err := ioc.ResolveAs[*HTTPHandler](runtime.Container())
            if err != nil {
                return err
            }
            return servers.Serve(ctx, handler)
        }),
        app.WithShutdown(
            app.WithShutdownGracefulTimeout(20*time.Second),
            app.WithShutdownRuntimeCloseTimeout(10*time.Second),
            app.WithShutdownHook("servers", servers.Shutdown),
        ),
    )
}
```

`servers.Shutdown(ctx, runtime, command)` 必须提供完整的 graceful→必要时 force→wait 语义。不能把原始 `http.Server.Shutdown` 直接注册为 Hook，因为它没有表达 graceful 失败后的 Force Close，也未必等待业务侧 Serve goroutine 完全退出。

Hook 的强契约：

1. 返回时，其管理的工作负载必须已经停止访问 Runtime。
2. shutdown context 的 deadline 只结束 graceful 阶段；到期后 Hook 必须 Force Close 并继续等待工作负载退出。
3. 返回 error 表示发生过关闭失败或降级，不表示工作负载仍在运行。
4. Hook 必须幂等，并与 Runner 的 Start/Serve 线性化，不能在 Hook 返回后才发布新工作负载。
5. Hook 只能借用传入 Runtime，不能调用 Runtime Close。

`ShutdownHook` 参数：

```go
type ShutdownHook func(
    ctx context.Context,
    runtime app.Runtime,
    command app.CommandContext,
) error
```

| 参数/返回 | 语义 |
|---|---|
| `ctx` | 从已取消 run context 去除取消状态后创建的独立 graceful context；默认 15s deadline |
| `runtime` | Enabled 命令非 nil；Disabled 命令 nil；Hook 只能借用，不能 Close |
| `command` | 本次实际执行命令的稳定快照，可用于日志和少量命令分支 |
| nil error | 工作负载已停止，且没有需要上报的 graceful/force 错误 |
| non-nil error | 工作负载已经停止，但关闭发生错误或降级；app 聚合错误后仍 Close Runtime |
| panic | 转换为 `app.internal` shutdown_hook 错误，不跳过其他 Hooks 和 Runtime Close |

多个互相独立的 Hooks 并发执行；声明顺序不表示关闭顺序。有依赖的 HTTP/gRPC/Consumer 应封装为一个 Group，由 Group 内部显式编排。

Runner 保持在命令调用 goroutine：阻塞型 `Serve/Spin` 可直接在 Runner 中运行，由并发 Shutdown Hook 使其返回；后台型 Server 由 controller 等待 Serve 结果。两种写法不需要不同的 app 配置。

### 12.2 信号、宿主与返回错误

| 入口/触发 | 行为 |
|---|---|
| `Run/Main` + SIGINT/SIGTERM | 完整 Shutdown；其他阶段成功时返回 nil |
| `ExecuteContext` + 宿主取消 | 完整 Shutdown；返回 `context.Cause(ctx)` |
| Runner 正常返回 | 执行 Shutdown Group 后正常关闭 Runtime |
| Runner 返回业务错误 | 保留业务错误，仍执行 Hooks 和 Runtime Close |
| Shutdown Hook 失败 | 保留 Hook 错误，不跳过其他 Hooks 和 Runtime Close |

Unix 默认监听 `os.Interrupt` 和 SIGTERM，Windows 默认监听 `os.Interrupt`。第一次信号启动 Shutdown 并立即恢复默认信号行为；第二次同类信号由操作系统直接终止进程。自定义宿主不要调用 `Run`，而应通过 `ExecuteContext(ctx, args)` 自己管理信号和取消来源。

自定义进程信号使用替换语义：

```go
app.WithShutdown(
    app.WithShutdownSignals(os.Interrupt, syscall.SIGTERM),
)
```

### 12.3 配置默认值和子命令

| 配置 | 默认值 |
|---|---|
| `WithShutdownSignals` | Unix：Interrupt+SIGTERM；Windows：Interrupt |
| `WithShutdownGracefulTimeout` | 15 秒，共享给全部 Shutdown Hooks |
| `WithShutdownRuntimeCloseTimeout` | 10 秒，独立于 graceful context |
| Shutdown Hooks | 空集合 |
| 子命令 | 继承应用 Hooks |

单值配置重复声明、空信号集合、非正 timeout、nil Hook、空或重复 Hook 名称都会在执行前 fail-fast。两个 timeout 都是协作式 deadline，不能强制停止忽略 context 的 goroutine。

时间预算不是一个总 timeout：

```text
Shutdown Hooks graceful deadline（默认 15s）
  -> Hook 内必要的 Force Close + 最终 wait（可能超过 deadline）
  -> Runtime Close 独立 deadline（默认 10s）
```

Hook context 到期只通知 adapter 从 graceful 切换到 force。app 不在后台遗留 Hook goroutine 并继续关闭 Runtime，否则工作负载可能访问已经关闭的依赖；因此 Force Close 后仍无法退出时，进程等待第二次信号或平台 SIGKILL。Runtime Close timeout 同样只向下传播 context，不能强制终止不响应 context 的 Closer。

不需要应用 Shutdown Group 的子命令显式关闭继承：

```go
app.Command{
    Use:                  "migrate",
    DisableShutdownHooks: true,
    Runner:               runMigration,
}
```

该字段只跳过 Shutdown Hooks；Runtime Build 成功时仍执行 Runtime Close。`RuntimeModeDisabled` 默认仍执行 Shutdown Hooks 并传入 nil Runtime，通常应同时设置 `DisableShutdownHooks: true`。

### 12.4 Server adapter 的职责

接入 Gin、Hertz、gRPC 或其他服务框架时，适配层统一负责：

1. readiness 与 draining 状态；
2. 停止接受新的 HTTP 请求或 gRPC RPC；
3. 等待存量请求、RPC 和 Stream 完成；
4. graceful shutdown 超时后的强制 Close；
5. 等待 `Serve/Spin` 完全退出；
6. 把意外运行错误返回 Runner。

app 核心只提供固定的 Shutdown Group，不增加 Gin/Hertz/gRPC 类型，也不发现或管理 IOC 内部资源。多个 Server 可由 adapter 内的 `ServerGroup` 统一管理，再作为一个 Shutdown Hook 参与应用退出。完整契约见 [App 生命周期管理](../architecture/app-lifecycle-design.md#8-withshutdown-配置模型)。

### 12.5 Kubernetes 配合

Pod 开始终止后，相关 EndpointSlice 会进入 terminating 且 ready=false，普通负载均衡器不应继续选择它；但控制面、Ingress、Service Mesh、云负载均衡器和既有连接之间不存在瞬时断流保证。因此 Server adapter 应先进入 draining，等待路由传播，再停止监听并排空请求。

推荐部署约束：

1. readiness 在 draining 时失败，liveness 继续成功；
2. `preStop` 可调用幂等的 prepare-shutdown endpoint，提前设置 draining；
3. prepare、SIGTERM 和重复 Shutdown 共享首次 draining 时间，不能重复等待完整 drain delay；
4. `terminationGracePeriodSeconds` 必须覆盖 preStop/drain delay、Server shutdown、Runtime Close 和安全余量；
5. 全部 Server 完全停止前，不能关闭 Runtime。

Kubernetes 终止语义见 [Pod 与 Endpoint 终止流程](https://kubernetes.io/docs/tutorials/services/pods-and-endpoint-termination-flow/) 和 [Container Lifecycle Hooks](https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/)。

### 12.6 评审清单

1. 进程入口是否使用 `Run/Main`，嵌入式宿主是否使用 `ExecuteContext`？
2. 第一次信号是否开始优雅退出，第二次信号是否恢复默认强制终止能力？
3. Shutdown Hook 返回时是否保证全部工作负载已经停止访问 Runtime？
4. graceful shutdown 失败后是否执行 Force Close，并等待 Serve/Spin goroutine 结束？
5. 每个 Server 是否只有一个明确的生命周期所有者？
6. 多个有顺序依赖的关闭动作是否封装在同一 Group 内？
7. readiness、drain delay 与 Pod termination grace period 是否经过真实环境验证？

## 13. 高级 Runtime 配置

Config 使用 config.Options；IOC 底层策略使用显式高级入口：

```go
app.Run(
	app.WithConfig(config.Provider(provider)),
	app.WithIOCOptions(ioc.WithCloseTimeout(5*time.Second)),
    app.WithRunner(runServer),
)
```

完全控制蓝图时使用 `WithRuntime`：

```go
runtimeApplication := appruntime.New(
	appruntime.WithName("order-service"),
	appruntime.WithConfig(config.Provider(provider)),
)

application := app.New(
    app.WithName("orderctl"),
    app.WithRuntime(runtimeApplication),
    app.WithRunner(runServer),
)
```

`WithRuntime` 与 `WithRuntimeOptions` 互斥。同时声明会在执行前 fail-fast。

完整选择规则：

| 需求 | 使用入口 | 不要同时使用 |
|---|---|---|
| 普通应用配置 config/log/IOC | `WithConfig/WithLogging/WithBeans/WithModules/WithIOCOptions` | `WithRuntime` |
| app 没有门面的 app-runtime Option | `WithRuntimeOptions(option...)` | `WithRuntime` |
| 外部已经拥有完整 `*app-runtime.Application` 蓝图 | `WithRuntime(application)` | 任何 Runtime 门面或 `WithRuntimeOptions` |

参数与身份语义：

1. `WithRuntime(nil)` 记录 setup error，不会静默回退默认蓝图。
2. `WithRuntimeOptions` 可以多次调用；Options 按声明顺序追加，nil app-runtime Option 最终由 app-runtime 忽略。
3. 默认蓝图先应用 `app.WithName` 作为 Runtime 名称，再应用调用方 Runtime Options；显式 `app-runtime.WithName` 因此可以覆盖传播值。
4. `WithRuntime(custom)` 完全尊重 custom Application 的名称和声明；`app.WithName` 此时只配置 CLI root 名。
5. `app-runtime.Application` 是构建蓝图，不是已经 Build 的 Runtime；每个 Enabled 命令执行才创建本次资源。
6. App 本身面向一次命令事务。不要并发复用 App；需要多个独立运行实例时分别创建 App/Application。

## 14. 测试应用

```go
func TestStatusCommand(t *testing.T) {
    var output bytes.Buffer
    application := app.New(
        app.WithOutput(&output),
        app.WithCommands(app.Command{
            Use:         "status",
            RuntimeMode: app.RuntimeModeDisabled,
            Runner: func(context.Context, app.Runtime, app.CommandContext) error {
                _, _ = output.WriteString("ready")
                return nil
            },
        }),
    )

    err := application.ExecuteContext(context.Background(), []string{"status"})
    if err != nil {
        t.Fatal(err)
    }
}
```

使用 `WithOutput/WithErrorOutput` 控制 help、version 和错误输出，不获取底层 Cobra Command。

`App` 持有可变的命令与 flag 解析状态，不保证并发安全。并发测试或需要状态隔离的用例应各自调用 `app.New`，不要共享同一个实例。

### 14.1 测试输入与输出

| 测试项 | 配置方式 |
|---|---|
| CLI 参数 | `ExecuteContext(ctx, []string{"status", "--format=json"})`，不包含程序名 |
| help/version/schema 输出 | `WithOutput(&bytes.Buffer{})` |
| Main/CLI 错误输出 | `WithErrorOutput(&bytes.Buffer{})`；单元测试通常不直接调用会 `os.Exit` 的失败 Main |
| 宿主取消 | `context.WithCancelCause` 后调用 `ExecuteContext` |
| Runtime 关闭 | 注册可观察 `ioc.Closer` 或 config/log 测试 Provider |
| Shutdown Hook | 使用 channel/controller 断言 Hook 返回前 Runner 已收敛 |

测试 `--version` 的 Runtime 前短路：

```go
func TestVersionDoesNotRunBusiness(t *testing.T) {
    var output bytes.Buffer
    runnerCalled := false
    application := app.New(
        app.WithVersion("v1.2.3"),
        app.WithOutput(&output),
        app.WithRunner(func(context.Context, app.Runtime) error {
            runnerCalled = true
            return nil
        }),
    )

    err := application.ExecuteContext(context.Background(), []string{"--version"})
    if err != nil || runnerCalled || strings.TrimSpace(output.String()) != "v1.2.3" {
        t.Fatalf("unexpected version result: err=%v called=%t output=%q", err, runnerCalled, output.String())
    }
}
```

测试宿主取消时应使用 `errors.Is` 检查 cause，而不是比较最终错误文本，因为 Runner、Hook 和 Runtime Close 错误可能通过 `errors.Join` 同时存在。

## 15. 处理错误

1. `app.Run` 与 `ExecuteContext` 返回 error，由调用方决定日志、退出码和重试策略；`app.Main` 只负责把错误写入 error output 并以状态码 1 退出。
2. app 自身解析失败可按 `app.cli.*` 分支；缺少 Runner 可按 `app.lifecycle.*` 分支；构建声明冲突或未知内部失败使用 `app.internal`。
3. `ArgumentValidator` 返回的业务错误在 Runtime Build 前直接返回；hooks、Runner 与 Runtime 错误保留在最终 error chain 中。
4. app 不主动记录业务日志。需要 `op/result/reason` 的业务观测时，应在 Runner/hooks 中通过 `app.Runtime.Logger` 输出，并避免记录敏感参数。

### 15.1 稳定错误码

| 常量 | code | 典型原因 |
|---|---|---|
| `ErrCodeAppInternal` | `app.internal` | nil App、Option 声明冲突、Shutdown Hook panic、无法归类的内部阶段失败 |
| `ErrCodeAppRunnerRequired` | `app.lifecycle.runner_required` | root 既没有 Runner，也没有可展示子命令 |
| `ErrCodeAppCommandRunnerRequired` | `app.lifecycle.command_runner_required` | 选中的叶子 Command 没有 Runner |
| `ErrCodeAppCLIInvalidExpression` | `app.cli.invalid_expression` | bootstrap/override 不是 `key=value` |
| `ErrCodeAppCLIEmptyKey` | `app.cli.empty_key` | `=value` |
| `ErrCodeAppCLIInvalidBootstrapKey` | `app.cli.invalid_bootstrap_key` | bootstrap key 错误携带 `bootstrap.` 根前缀 |
| `ErrCodeAppCLIEmptyConfigFile` | `app.cli.empty_config_file` | `--bootstrap config=` |
| `ErrCodeAppCLIEmptyBootstrapEnvPrefix` | `app.cli.empty_bootstrap_env_prefix` | 显式空 env prefix |
| `ErrCodeAppCLIFlagReadFailed` | `app.cli.flag_read_failed` | 内部无法按声明类型读取 Flag |
| `ErrCodeAppCLIFlagLookupFailed` | `app.cli.flag_lookup_failed` | 已标记 Flag 在读取阶段不存在 |
| `ErrCodeAppCLIFlagTypeUnsupported` | `app.cli.flag_type_unsupported` | 配置 Flag 使用不支持的底层类型 |
| `ErrCodeAppCLIConflict` | `app.cli.conflict` | 两种入口对同一单值 bootstrap 参数给出不同值 |
| `ErrCodeAppListBootstrapKeysInvalidMode` | `app.cli.list_bootstrap_keys_invalid_mode` | mode 不是 table/json |

通过完整错误链分支：

```go
if err := application.ExecuteContext(ctx, args); err != nil {
    if code, ok := errcode.CodeOf[app.ErrorCode](err); ok {
        switch code {
        case app.ErrCodeAppCLIInvalidExpression, app.ErrCodeAppCLIEmptyKey:
            return printUsageError(err)
        }
    }
    return err
}
```

下层 `config.*`、`app_runtime.*`、`ioc.*` 错误码会保留在 error chain 中，app 只追加 `stage` 上下文，不应通过字符串截取判断根因。

### 15.2 聚合和取消语义

1. Complete 与 ValidateFlags 的全部非 nil 错误聚合返回。
2. BeforeRunHook 失败时聚合本阶段错误和 Runtime Close 错误；不运行 Runner/Shutdown Hooks。
3. Runner、宿主 cause、全部 Shutdown Hook 和 Runtime Close 错误可以同时存在，使用 `errors.Is/As` 检查每个 cause。
4. `Run/Main` 仅由首次进程信号触发且其他阶段成功时返回 nil；第二次信号由操作系统直接终止。
5. `ExecuteContext` 因宿主 context 取消时完成 Shutdown，并返回 `context.Cause(ctx)`；Runner 单纯返回的协作式 `context.Canceled` 不重复计入错误。
6. Runner 返回 `errors.Join(context.Canceled, businessErr)` 时 businessErr 不会被吞掉。
7. Shutdown Hook 返回 error 的前提仍是工作负载已经停止；error 不能作为“还在后台运行”的许可。

## 16. 常见问题

| 现象 | 原因 | 处理 |
|---|---|---|
| `runner_required` | 没有 Root Runner，也没有可用 Commands | 声明 `WithRunner` 或 `WithCommands` |
| `command_runner_required` | 叶子 Command 没有 Runner | 设置 `Command.Runner` |
| Runtime 与 RuntimeOptions 冲突 | 同时使用 `WithRuntime` 和 `WithRuntimeOptions` | 选择完整替换或增量配置之一 |
| doctor 执行了 Runtime | 未声明 `RuntimeModeDisabled` | 为纯 CLI 命令设置该模式 |
| doctor 的 Shutdown Hook 收到 nil Runtime | Disabled 只禁用 Runtime，不自动禁用应用 Hooks | Hook 保持 nil-safe，或设置 `DisableShutdownHooks: true` |
| CLI 类型进入业务代码 | 直接依赖 Cobra/Pflag | 使用 `CommandContext`、`ArgumentValidator`、`CommandFlags` |
| 子命令识别不到 root 业务 Flag | `WithFlagBinder` 注册的是 root 本地 Flag，不是 persistent 业务 Flag | 在子命令的 `Command.FlagBinder` 中声明该 Flag |
| Config Flag 未传却覆盖了配置 | 把命令参数误声明为 Config*，或代码声明了 `config.PinnedValues` | Config* 仅显式传值才生效；检查其他 pinned 来源和 key |
| `--override code=01` 得到字符串 | 前导零整数为避免信息丢失会保留 string | 使用 ConfigInt，或把配置字段定义为 string |
| 取消 context 后仍等待关闭 | Shutdown Hook 强契约要求 force 后继续等待；timeout 不能终止 goroutine | 检查 adapter 的 Force Close 与 wait 实现，必要时用第二次信号或平台 SIGKILL 硬终止 |
| Pod 退出时仍有迟到请求 | Endpoint/Ingress/LB 状态传播和既有连接不是瞬时消失 | 先设置 draining，等待传播，再 Shutdown；全部 Server 停止后才允许 Runner 返回 |
| Runtime 关闭后仍有请求访问运行能力 | Shutdown Hook 在 Serve/Spin 完全退出前返回 | Force Close 后继续等待工作负载完全退出，再从 Hook 返回 |

## 17. 可运行示例

1. [quickstart-app](../../examples/quickstart-app/README.md)：root、commands、flags、Modules、provider。
2. [admin](../../examples/admin/main.go)：`app.Main` 与运维命令。
3. [quickstart-log](../../examples/quickstart-log/README.md)：app 与日志 Runtime 集成。

## 18. 相关文档

1. [app 模块入口](../../app/README.md)
2. [app-runtime 使用指南](app-runtime-usage.md)
3. [Config 使用指南](config-usage.md)
4. [Log 使用指南](log-usage.md)
5. [IOC 使用指南](ioc-usage.md)
6. [App Design](../architecture/app-design.md)
7. [App 生命周期管理](../architecture/app-lifecycle-design.md)
8. [Architecture Overview](../architecture/architecture-overview.md)
