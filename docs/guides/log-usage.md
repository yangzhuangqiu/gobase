# Log 使用指南

本文面向已经决定使用 gobase 日志体系的开发者，按“能力 → 启用 → 使用 → 可观察结果”说明常规路径和高级路径。设计理由与精确并发不变量见 [Log 架构设计](../architecture/log-design.md)，端到端代码见 [quickstart-log](../../examples/quickstart-log/README.md)。

## 1. 能力总览与入口选择

日志体系由稳定业务契约和可选适配模块组成。业务代码始终只依赖 `log.Logger`；Provider、Manager、global 和 Hertz 入口只在应用装配层出现。

| 能力 | 解决的问题 | 配置或启用入口 | 默认状态 | 业务使用入口 | 不适用场景 |
|---|---|---|---|---|---|
| 结构化事件 | 统一 message、level、字段和 logger 名称 | 取得任意 `log.Logger` | 默认可用 | `Debug/Info/Warn/Error/With` | 可靠投递、业务事务 |
| 标准 fallback | Provider 缺失时仍能观察启动/关闭问题 | `log.NewLoggerFactory(nil)`；app-runtime 未配置 Logging 时自动使用 | INFO、stderr、text、source enabled | `LoggerFactory.Get` | 文件轮转、动态 level |
| Zap 输出 | JSON/text、console/file、caller 和轮转 | `logzap.NewProvider(Config, Option...)` | 需显式创建；配置零值回填推荐默认值 | `provider.Factory().Get` | ConfigManager 订阅 |
| 调用链字段 | 从 typed context key 补充白名单字段 | `WithContextExtractors` | 默认关闭 | 正常 Logger 调用自动生效 | 网络查询、字符串 key 扫描 |
| 字段脱敏 | 在统一出口替换敏感结构化字段 | `WithRedactor` | 默认关闭 | 正常 Logger 调用自动生效 | message 文本扫描 |
| 动态 level | 不重建 Logger 即可改变输出阈值 | `ApplyRuntimeConfig` 或 `app-log-zap` | 只有 level 可热更新 | 已有 Logger 自动使用新阈值 | mode/format/caller/rotate 更新 |
| Manager 装配 | 从最终 Snapshot 创建 Provider 并持续接收 level | `applogzap.New` 或 `NewProviderFromManager` | 默认 scope 为 `log.zap` | Runtime Logger 或返回的 Provider | 没有 ConfigManager 的进程 |
| 零配置全局入口 | 小工具接受全局状态时减少装配代码 | 导入 `app-log-zap/global` | package init 自动初始化 | `global.Get/Logger` | 需要策略定制、重建或并发 Close |
| Hertz 共享输出 | Hertz 与业务日志共享 level、writer 和关闭状态 | `hlogzap.NewFromProvider` | 显式启用 | `hlog.FullLogger` | 需要独立 Hertz 输出 |
| Hertz 独立输出 | Hertz 单独创建或安装 Zap Logger | `hlogzap.NewLogger/Install` | 显式启用 | Hertz `hlog` | 需要根 Field/Redactor 语义 |

### 入口选择

1. 普通 app 应用：`app.WithLogging(applogzap.New())`。
2. 没有 ConfigManager：直接创建 `logzap.Provider`。
3. 已有自建 Manager：使用 `applogzap.NewProviderFromManager`，Provider 先于 Manager 关闭。
4. 接受 package init 和全局单例的小工具：使用 `app-log-zap/global`。
5. 已有 `*logzap.Provider` 的 Hertz 服务：优先 `hlogzap.NewFromProvider`。
6. Hertz 确实需要独立后端：使用 `NewLogger`；只有需要修改全局 hlog 时才使用 `Install`。

## 2. 最小使用闭环

### 2.1 独立进程

```go
func run(ctx context.Context) (err error) {
    provider, err := logzap.NewProvider(logzap.Config{})
    if err != nil {
        return err
    }
    defer func() {
        err = errors.Join(err, provider.Close())
    }()

    logger := provider.Factory().Get("order.worker")
    logger.Info(ctx, "worker started",
        log.Op("worker.start"),
        log.Result(log.ResultOK),
        log.F("queue", "orders"),
    )
    return work(ctx, logger)
}
```

零值 Config 会标准化为 INFO、console、JSON、caller enabled。成功后 stdout 中应出现包含 `logger=order.worker`、`op=worker.start`、`result=ok` 和 `queue=orders` 的事件。Provider 创建方必须保存业务错误并聚合 Close error。

### 2.2 app 应用

```go
func main() {
    app.Main(
        app.WithConfig(config.Defaults(map[string]any{
            "log": map[string]any{
                "zap": logzap.DefaultConfig(),
            },
        })),
        app.WithLogging(applogzap.New()),
        app.WithRunner(func(ctx context.Context, runtime app.Runtime) error {
            logger := runtime.Logger("order.service")
            logger.Info(ctx, "service started", log.Op("service.start"), log.Result(log.ResultOK))
            return serve(ctx, logger)
        }),
    )
}
```

app 负责 ConfigManager、日志 Provider 和业务运行事务；Runner 不关闭 Logger、Provider 或 Manager。构建失败、业务失败和关闭失败最终由 app 聚合并映射为进程结果。完整命令、Config Flag 和应用关闭语义见 [App 使用指南](app-usage.md)。

## 3. 记录可检索的结构化事件

### 3.1 message、level 与 Field

message 用于人类阅读，应稳定、简短，不拼接动态 ID 或敏感值；机器检索维度使用 Field。

```go
logger.Info(ctx, "request completed",
    log.F("request_id", requestID),
    log.F("latency", 25*time.Millisecond),
    log.Group("http",
        log.F("method", method),
        log.F("status", status),
    ),
)
```

| 输入 | 使用规则 | 可观察结果 |
|---|---|---|
| Debug | 开发和诊断；昂贵值先调用 `Enabled` | 默认 INFO 阈值下不输出 |
| Info | 正常状态转换 | 默认输出 |
| Warn | 降级、重试、可恢复异常 | 输出 WARN |
| Error | 当前操作失败 | 只记录，不 panic、不退出，也不代替返回 error |
| `F(key, value)` | key 会 trim；空 key 忽略 | 常用 Go 类型保留对应编码，其余使用 Any |
| `Err(err)` | nil 被忽略 | 固定字段名 `error` |
| `Group(key, fields...)` | 复制字段结构，不深复制引用值 | 输出嵌套对象 |
| `With(fields...)` | 固化组件级字段，不修改父 Logger | 派生 Logger 后续事件自动携带 |

引用类型 Value 在记录期间不得并发修改。根契约不提供 Fatal 或格式化镜像方法；确需文本格式化时由调用方显式使用 `fmt.Sprintf`，但仍应把检索维度单独写入 Field。

昂贵 Debug 字段应避免无效计算：

```go
if logger.Enabled(ctx, log.LevelDebug) {
    logger.Debug(ctx, "snapshot calculated", log.F("snapshot", buildSnapshot()))
}
```

### 3.2 op、result、reason 与 error

关键流程至少提供稳定 `op/result`，失败或分支事件增加 `reason`；`err.Error()` 不是稳定 reason。

```go
logger.Warn(ctx, "remote config unavailable",
    log.Op("config.refresh"),
    log.Result(log.ResultDegraded),
    log.Reason("remote_timeout"),
    log.Err(err),
)
```

| Result | 语义 |
|---|---|
| `ResultOK` | 正常完成 |
| `ResultFailed` | 操作未完成 |
| `ResultDegraded` | 通过降级继续 |
| `ResultSkipped` | 因前置条件跳过 |
| `ResultRunning` | 已开始、尚未结束 |

扩展 result/reason 必须保持稳定低基数；错误细节放入 `Err(err)`，不能直接成为聚合维度。

### 3.3 名称、fallback 与字段冲突

```go
logger := provider.Factory().Get("order.service")
```

名称 trim 后为空会归一化为 `root`，并形成固定字段 `logger=<name>`。request/user/order ID 必须作为 Field；Factory 不缓存 Logger，同名只保证相同行为，不保证实例身份。

常见字段冲突结果：

1. 来源优先级为 Factory/Get/With → 单次事件 → ContextExtractor。
2. 同层标量 first-wins；同名 Group 合并后对子字段继续 first-wins。
3. 顶层 `ts/level/msg/caller/func/name/stacktrace` 是 envelope 保留键，业务同名字段被忽略。

精确字段不变量见 [Log 架构设计](../architecture/log-design.md#4-根事件契约与字段不变量)。

## 4. 启用调用链字段和脱敏

这些能力属于 Provider 构造期稳定策略，只能通过代码 Option 启用，不能从 YAML 热更新。

| 入口 | 默认/零值 | 声明规则 | 生效阶段 | 失败边界 |
|---|---|---|---|---|
| `WithContextExtractors(...)` | 无 extractor；nil 项忽略 | 多次声明追加，按注册顺序执行 | 目标 level 已启用后、每条事件写出前 | 自定义函数必须并发安全且不能 panic |
| `WithRedactor(redactor)` | nil 表示不脱敏 | 多次声明后者覆盖前者；传 nil 可清除前值 | 固定字段在 With/Get 阶段，事件/context 字段在写出阶段 | 自定义实现必须并发安全且不能 panic |

### 4.1 ContextExtractor

```go
provider, err := logzap.NewProvider(cfg,
    logzap.WithContextExtractors(func(ctx context.Context) []log.Field {
        requestID, ok := requestid.FromContext(ctx) // typed key
        if !ok {
            return nil
        }
        return []log.Field{log.F("request_id", requestID)}
    }),
)
```

Extractor 必须快速、无网络访问，只读取 typed context key，不返回 token、凭据或完整请求载荷。禁用级别不会执行 extractor；多个 extractor 的同名标量仍遵循 first-wins。

验证时分别记录一条启用和禁用级别事件：启用事件应包含 `request_id`，禁用事件不得执行提取函数。

### 4.2 Redactor

```go
redactor := log.NewKeyRedactor("password", "token", "auth.secret")
provider, err := logzap.NewProvider(cfg, logzap.WithRedactor(redactor))
```

内建键脱敏器忽略大小写和首尾空白，支持叶子键与 dotted path。字段或 Group 命中后替换为固定 `[REDACTED]`；未命中的 Group 递归处理子字段。它覆盖 With、单次 Field、Group 和 ContextExtractor，但不扫描 message。

自定义策略可使用 `log.RedactorFunc`。返回 nil 表示保留字段但输出 null；实现不得保存、记录或回传原始敏感值。Redactor 是防御边界，不是主动记录秘密的许可，敏感值尤其不能拼入 message。

验证时应断言输出包含 `[REDACTED]`，且原始秘密在完整输出中不存在。

## 5. 配置 Zap 输出能力

`log-zap` 只负责 Zap 核心、编码、输出、轮转、动态 level 和 Close，不订阅 ConfigManager。

### 5.1 配置生效契约

| 配置 | 默认/零值语义 | 启用条件与约束 | 生效阶段 | 热更新 | 失败结果 |
|---|---|---|---|---|---|
| `level` | 空值 → info | trace/debug/info/notice/warn/warning/error | Build；也可进入 RuntimeConfig | 是 | `log.level.invalid`，旧运行值不变 |
| `mode` | 空值 → console | console/file | Build | 否 | `log.mode.invalid` |
| `format` | 空值 → json | json/text | Build | 否 | `log.format.invalid` |
| `disableCaller` | false | true 时不输出 caller | Build | 否 | 无独立错误 |
| `rotate.filename` | 空 | mode=file 时必填 | Build | 否 | `log.file.filename_required` |
| `rotate.maxSize` | `<=0` → 10 MB | file 模式使用 | Build | 否 | 回退默认值 |
| `rotate.maxBackups` | `<=0` → 50 | file 模式使用 | Build | 否 | 回退默认值 |
| `rotate.maxAge` | `<=0` → 3 天 | file 模式使用 | Build | 否 | 回退默认值 |
| `rotate.compress` | false | file 模式使用 | Build | 否 | 无独立错误 |
| `rotate.localTime` | false，使用 UTC | true 使用本地轮转时间 | Build | 否 | 无独立错误 |

`DefaultConfig()` 返回完整推荐值；`ResolveConfig(input)` 只做默认合并、归一化和校验，不创建 Provider。console 默认写 stdout；根 fallback 写 stderr，两者不要混淆。

### 5.2 文件输出

```yaml
log:
  zap:
    level: info
    mode: file
    format: json
    disableCaller: false
    rotate:
      filename: ./logs/application.log
      maxSize: 100
      maxBackups: 20
      maxAge: 14
      compress: true
      localTime: true
```

Provider 创建成功后才拥有文件 writer。调用方停止业务写入后执行 Close；Close 会禁用旧 Logger、Sync 并关闭 writer，使用 `errors.Join` 保留 Sync 和 writer Close 错误。纯 Provider 不缓存首次 Close error，因此创建方必须保存第一次结果。

验证文件模式时，应在 Close 成功后读取文件，并确认 JSON/text 格式、caller、logger 名称和关键字段符合预期。

### 5.3 编码结果

| 输入 | Zap 输出 |
|---|---|
| time | ISO-8601 |
| level | 大写 |
| duration | 字符串 |
| error | NamedError |
| Group | 嵌套对象 |
| caller | 根 Logger 捕获业务 PC；`disableCaller=false` 时输出 |
| 其他对象 | `zap.Any` |

本模块当前没有 sampling、stacktrace 或自定义 encoder Option。

## 6. 动态调整 level

### 6.1 宿主显式推送

```go
logger := provider.Factory().Get("worker")
logger.Debug(ctx, "hidden before update")

if err := provider.ApplyRuntimeConfig(logzap.RuntimeConfig{Level: "debug"}); err != nil {
    return err
}
logger.Debug(ctx, "visible after update")
```

同一个 Logger 在更新前后无需重新获取。默认 INFO 下第一条 Debug 不输出，更新成功后第二条输出。

| 输入或状态 | 结果 |
|---|---|
| level 空白 | nil，保持当前 level |
| 合法 level | 原子更新，已有 Logger/Core 立即生效 |
| 非法 level | `log.level.invalid`，旧 level 保留 |
| Provider 已关闭 | nil，忽略迟到更新，不重新激活 |
| mode/format/caller/rotate 变化 | 不属于 RuntimeConfig，必须重建 Provider |

配置来源、调用时机和错误处理由宿主负责。需要 ConfigManager 时不要手写 watcher，使用 app-log-zap。

### 6.2 由 app-log-zap 消费 ConfigManager

```go
app.WithLogging(applogzap.New(
    applogzap.WithScope("log.zap"),
    applogzap.WithProviderOptions(
        logzap.WithContextExtractors(extractTrace),
        logzap.WithRedactor(log.NewKeyRedactor("token")),
    ),
))
```

`WithScope` 默认是 `log.zap`，空白值不覆盖默认；配置位于其他 scope 时才显式调整。`WithProviderOptions` 按声明顺序追加，每次 Build 转发给 Zap Provider。

app-log-zap 先订阅并同步接收完整初始 Config，再创建核心；后续 Snapshot 只应用 level。ConfigManager 的 Defaults/File/Remote/Pinned 优先级和 Config Flag 由 Config/App 模块负责，详见 [Config 使用指南](config-usage.md) 与 [App 使用指南](app-usage.md)。

| 事件 | Subscription | Zap 核心 | Manager |
|---|---|---|---|
| Build 成功 | active | active | 原创建方持有 |
| Build 核心失败 | Cancel | 未交付 | 不关闭 |
| context 取消 | Cancel | 继续可写 | 不关闭 |
| Provider Close | Cancel | Sync + Close | 不关闭 |
| app Runtime Close | 先关闭 Provider | 关闭 | 随后由 Runtime 关闭 |

非法动态 level 会进入 ConfigManager subscriber 错误链；Manager 新 Snapshot 已发布，但 Provider 保留旧 level，后续合法版本仍可恢复。组合 Provider 使用 `sync.Once`，并发或重复 Close 返回首次稳定结果。

### 6.3 自有 Manager

```go
func runOwnedManager(ctx context.Context) (err error) {
    manager, err := config.New(
        config.Defaults(map[string]any{
            "log": map[string]any{"zap": logzap.DefaultConfig()},
        }),
        config.File(config.FileDisabled()),
    ).Build(ctx)
    if err != nil {
        return err
    }

    provider, err := applogzap.NewProviderFromManager(ctx, manager, "log.zap")
    if err != nil {
        return errors.Join(err, manager.Close())
    }

    runErr := work(ctx, provider.Factory().Get("batch.worker"))
    providerErr := provider.Close()
    managerErr := manager.Close()
    return errors.Join(runErr, providerErr, managerErr)
}
```

调用方拥有 Manager 和返回 Provider，必须按 Provider → Manager 顺序关闭。传入 context 取消只停止 Subscription，不提前关闭日志核心；即使 context 已取消，仍要显式 Close Provider。

## 7. 选择 global 或 Hertz 适配

### 7.1 app-log-zap/global

导入 global 会在 package init 创建 ConfigManager 和组合 Provider。它适合接受全局单例和文件 watch 副作用的小工具，不是普通服务的首选。

```go
func runGlobal(ctx context.Context) (err error) {
    defer func() {
        err = errors.Join(err, logglobal.Close())
    }()

    logger := logglobal.Get("tool.import")
    logger.Info(ctx, "import started", log.Op("import.run"), log.Result(log.ResultRunning))
    return runImport(ctx, logger)
}
```

global 初始化失败时先用标准库日志输出 degraded，再尝试默认纯 Zap Provider；仍不可用时 `Get/Logger` 返回根 fallback。Close 按 Provider → Manager 执行并缓存首次结果，关闭后 Get 返回 fallback，不重新初始化。

限制：没有构造参数，不能注入 ContextExtractor/Redactor，全进程单实例；业务停止使用全局 Logger 后才能 Close，不保证 Get/Logger/Provider 与 Close 并发安全。

### 7.2 Hertz 共享 Provider

已有 `*logzap.Provider` 时优先复用：

```go
hertzLogger, err := hlogzap.NewFromProvider(provider)
if err != nil {
    return err
}
hlog.SetLogger(hertzLogger)

// runHertz 返回时 Host 已停止，外层随后关闭 provider。
return runHertz(ctx)
```

共享路径具有以下结果：

1. Hertz 与业务 Logger 写入同一 encoder/writer，并跟随同一关闭状态。
2. Hertz `SetLevel` 更新共享 Provider，已有业务 Logger 同时生效。
3. `SetOutput` 是 no-op，Hertz 不能绕过 Provider 替换 writer。
4. 适配器借用 Core，不执行 Sync/Close；必须先停止 Hertz Host，再关闭 Provider。
5. Hertz 调用不经过根 ContextExtractor、Redactor 和字段 schema，敏感参数需要在框架边界单独审查。
6. Trace 映射 Debug，Notice 映射 Warn；Fatal/Fatalf 保留 Zap 终止进程语义。

`NewFromProvider(nil)` 返回 `hlog_zap.provider.invalid`。

### 7.3 Hertz 独立输出

确实需要独立配置和 writer 时使用 `NewLogger`；需要安装到全局 hlog 时使用 `Install`：

```go
func runStandaloneHertz(ctx context.Context) error {
    logger, err := hlogzap.Install(hlogzap.Config{
        Level:  "info",
        Mode:   hlogzap.ModeConsole,
        Format: hlogzap.FormatJSON,
    })
    if err != nil {
        return err
    }

    runErr := runHertz(ctx)
    syncErr := logger.Sync()
    return errors.Join(runErr, syncErr)
}
```

`Install` 必须在服务并发启动前调用，不保存或恢复旧全局 Logger。独立路径复用 log-zap 的 Config 默认值和校验，但没有 RuntimeConfig 或 Manager 订阅。文件模式当前不暴露 lumberjack closer，无法提供完整释放合同；生产文件输出优先使用拥有明确 Close 的共享 Provider 路径。

独立构建使用 log-zap 配置错误码；缺失共享 Provider 使用 `hlog_zap.provider.invalid`。

## 8. 生命周期与错误处理

### 8.1 资源所有权

| 资源 | 创建方 | 关闭方 | 关闭后结果 |
|---|---|---|---|
| Logger/Factory | Provider | 不单独关闭 | Provider 关闭后可调用但不输出 |
| 纯 log-zap Provider | 独立宿主 | 同一宿主 | Sync、关闭 writer、忽略动态更新 |
| app-log-zap 组合 Provider | Runtime 或调用方 | Runtime 或调用方 | 取消 Subscription，再关闭核心 |
| ConfigManager | Runtime/global/宿主 | 原创建方 | watcher/session/subscription 结束 |
| global | package init | `global.Close` | Get 回退根 Factory |
| Hertz shared Logger | `NewFromProvider` | 不单独关闭 | 跟随 Provider 关闭状态 |
| Hertz standalone Logger | `NewLogger/Install` | 调用方至少 Sync | 不自动恢复全局 hlog；file closer 未暴露 |

普通应用的关闭顺序是：

```text
停止新请求/任务
  -> 关闭业务和 IOC
  -> 关闭日志 Provider
  -> 关闭 ConfigManager
```

业务错误、Provider Close error 和 Manager Close error 不能互相覆盖，统一使用 `errors.Join` 或等价聚合策略。

### 8.2 按错误语义处理

| 错误码或来源 | 常见原因 | 调用方动作 | 仍然有效的状态 |
|---|---|---|---|
| `log.mode.invalid` | mode 不是 console/file | 修正启动配置后重建 | 无 Provider |
| `log.format.invalid` | format 不是 json/text | 修正启动配置后重建 | 无 Provider |
| `log.file.filename_required` | file 未配置 filename | 补充文件路径后重建 | 无 Provider |
| `log.level.invalid` | 启动或动态 level 非法 | 拒绝新值；动态场景继续使用旧 level | 既有 Provider 可继续工作 |
| `app_log_zap.provider.subscribe_failed` | Manager 关闭、scope 解码失败 | 检查 Manager 状态和 scope | Manager 仍由原创建方持有 |
| `app_log_zap.provider.core_build_failed` | 初始 Zap 配置非法 | 修正 Snapshot 后重建 | 已取得 Subscription 已取消 |
| `app_log_zap.provider.runtime_config_apply_failed` | 新 level 非法 | 修正并发布后续版本 | Snapshot 已发布，Provider 保留旧 level |
| `app_log_zap.provider.close_failed` | Sync/writer Close 失败 | 保留错误并进入应用关闭结果 | 组合 Provider 已终止 |
| `hlog_zap.provider.invalid` | `NewFromProvider(nil)` | 传入有效 `*logzap.Provider` | 未修改 hlog 全局状态 |
| global init 降级日志 | Manager/Provider 构建失败 | 检查标准错误输出和配置文件 | 可能已使用纯 Provider 或根 fallback |

外层 app-log-zap 错误保留下层 config/log-zap cause；使用 `errcode.IsCode` 可以同时识别适配阶段和根因。

## 9. 验证与排障

### 9.1 可运行验证

从仓库根目录运行：

```powershell
./scripts/go.ps1 -C examples run ./quickstart-log

$env:QUICKSTART_LOG_MODE="plain-full"
./scripts/go.ps1 -C examples run ./quickstart-log --config ./quickstart-log/application.yaml --emit-debug=true --dynamic-level=debug
```

验证重点：

1. 默认输出包含 INFO，不包含更新前的 Debug。
2. 动态 level 更新后，同一个 Logger 能输出 Debug。
3. 结构化输出包含稳定 logger、op/result 和业务字段。
4. file 模式在 Close 后可读取完整事件。
5. 启用 Redactor 后，完整输出中不存在原始秘密。

业务单元测试可以使用最小 capture Logger 断言 message、op/result/reason 和关键 Field；Provider 测试应使用临时文件并处理 Close error，不能只断言内存对象。

### 9.2 常见问题

| 现象 | 检查与处理 |
|---|---|
| Debug 不输出 | 默认 INFO；检查启动 level、RuntimeConfig 和 Manager scope |
| mode/format 热更新不生效 | 只有 level 可热更新；重建 Provider/Runtime |
| app 启动失败 | 同时检查 app-log-zap 外层 code 与 config/log-zap cause |
| 输出流与预期不同 | 根 fallback 写 stderr；Zap console 写 stdout |
| context 字段缺失 | Extractor 只在 level 启用后执行；确认 typed key 和注册顺序 |
| 敏感值泄露 | Redactor 不扫描 message；检查叶子键、dotted path 和框架 Core 直写路径 |
| 字段消失 | 检查空 key、envelope 保留键和 first-wins 冲突 |
| 文件没有完整内容 | 检查 level/filename，并确认 Close/Sync error 已处理 |
| context 取消后仍能写 | 正常：app-log-zap context 只取消 Subscription，核心由 Close 释放 |
| global Close 后仍返回 Logger | 正常：返回根 fallback，不重新初始化 global |
| Hertz 与业务 level 不一致 | 独立 NewLogger/Install 不共享；需要一致时使用 NewFromProvider |
| Hertz file 无法完整 Close | 独立 API 只暴露 Sync；改用共享 Provider 或接受当前缺口 |

## 10. 最佳实践

1. 业务只依赖 `log.Logger`，Provider 和适配模块留在 composition root。
2. message 保持稳定；动态 ID、耗时、状态和错误进入 Field。
3. 关键事件使用 op/result，失败或分支事件增加 reason 和 error。
4. logger name 使用稳定组件名，不放 request/user/order ID。
5. 昂贵 Debug 字段先调用 Enabled。
6. ContextExtractor 只读取 typed key，不访问网络，不返回凭据。
7. Redactor 作为防御边界；秘密不能进入 message，框架 Core 直写需单独审查。
8. 只有 level 可热更新；输出拓扑变化通过重建 Provider 生效。
9. 先停止日志生产者，再关闭 Provider，最后关闭配置来源。
10. 关闭错误必须进入最终应用结果，不能用无返回值 defer 丢弃。
11. Hertz 已有 Provider 时优先共享；独立 Install 只用于明确需要独立后端的场景。
12. 普通服务使用显式 app-log-zap；global 只用于接受全局状态的小工具。

## 11. 相关文档

1. [log README](../../log/README.md)
2. [log-zap README](../../log-zap/README.md)
3. [app-log-zap README](../../app-log-zap/README.md)
4. [hlog-zap README](../../hlog-zap/README.md)
5. [Log 架构设计](../architecture/log-design.md)
6. [Config 使用指南](config-usage.md)
7. [App 使用指南](app-usage.md)
8. [quickstart-log](../../examples/quickstart-log/README.md)
