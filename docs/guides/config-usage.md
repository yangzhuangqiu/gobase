# Config 使用指南

本文面向已经决定使用 `config` 的开发者，按任务说明文件、远端 provider、读取、热更新、关闭和排障。设计理由与精确状态语义见 [Config 架构设计](../architecture/config-design.md)。

## 1. 选择使用入口

| 场景 | 推荐入口 |
|---|---|
| 普通 CLI 应用 | `app.Run/app.Main` 与 app 的 Runtime 门面 |
| 需要统一 config/log/ioc 生命周期 | `app-runtime.Application` |
| 无 CLI 的独立配置管理、provider 开发或专项测试 | 直接使用 `config.New(options...)` |

业务方法如果只需要配置，不应自行创建第二个 Manager；从装配层接收 `config.Manager`、业务配置对象或只读 Snapshot。由 app/app-runtime 托管 ConfigManager 时，请从 [App 使用指南](app-usage.md#8-使用配置) 开始；以下内容面向直接使用 config 的场景。

## 2. 从来源声明到最终配置

先把一次完整配置过程理解为四步：声明来源、按固定层级合并、Build 发布 Snapshot、从 Snapshot 或 Observer 读取结果。

下面的例子同时使用 default、两个有序文件、两个 Nacos Sources 和 pinned。假设各来源提供这些值：

| 来源层 | 声明/定位 | 关键输入值 |
|---|---|---|
| default | `config.Defaults` | `service.name=orders`、`service.address=:8080`、`worker.concurrency=4`、`feature.enable_cache=false` |
| file[0] | `application.yaml` | `service.name=orders-file`、`service.address=:18080`、`worker.concurrency=8` |
| file[1] | `application-prod.yaml` | `service.name=orders-prod` |
| remote | Nacos `common.yaml`，priority=10 | `service.name=orders-common`、`worker.concurrency=16`、`feature.enable_cache=true` |
| remote | Nacos `orders.yaml`，priority=20 | `service.name=orders-remote`、`service.address=:19090` |
| pinned | `config.PinnedValues` | `service.address=:28080` |

使用一个 Builder 声明全部来源：

```go
manager, err := config.New(
    config.Defaults(map[string]any{
        "service": map[string]any{"name": "orders", "address": ":8080"},
        "worker": map[string]any{"concurrency": 4},
        "feature": map[string]any{"enable_cache": false},
    }),
    config.File(config.FilePath(
        "application.yaml",
        "application-prod.yaml",
    )),
    config.Provider(nacos.New(nacos.Bootstrap{
        Address: "127.0.0.1:8848",
        Sources: []nacos.BootstrapSource{
            {GroupID: "DEFAULT_GROUP", DataID: "common.yaml", Priority: 10},
            {GroupID: "DEFAULT_GROUP", DataID: "orders.yaml", Priority: 20},
        },
    })),
    config.PinnedValues(map[string]any{
        "service.address": ":28080",
    }),
).Build(ctx)
if err != nil {
    return err
}
```

合并顺序固定为：

```text
default < file[0] < file[1] < remote(priority=10) < remote(priority=20) < pinned
```

因此首次发布的最终结果是：

| 配置键 | 最终值 | 最终来源 |
|---|---|---|
| `service.name` | `orders-remote` | Nacos `orders.yaml`，priority=20 |
| `service.address` | `:28080` | pinned |
| `worker.concurrency` | `16` | Nacos `common.yaml`，priority=10 |
| `feature.enable_cache` | `true` | Nacos `common.yaml`，priority=10 |

从同一个不可变版本读取整份配置并验证来源：

```go
snapshot := manager.Current()

var application ApplicationConfig
if err := snapshot.Decode("", &application); err != nil {
    return err
}

meta, ok := snapshot.Explain("service.address")
// ok=true，meta.Source=pinned，meta.Version=snapshot.Version()
```

长期持有一个 scope，并在每次调用时获取最新 typed 配置块：

```go
service, err := config.Observe[ServiceConfig](manager, "service")
if err != nil {
    return err
}

currentService, err := service.Current()
```

读取入口按场景选择：

| 需求 | 使用方式 |
|---|---|
| 多个 key 必须来自同一发布版本 | `snapshot := manager.Current()`，随后使用 `Decode/Get/getters/Explain` |
| 多次主动读取某个最新 typed 配置块 | `config.Observe[T](...).Current()` |
| 配置发布后执行副作用 | `config.Subscribe[T](...)` |

Manager 的正式关闭必须保留错误，按下一节处理。

## 3. 构建并关闭 Manager

```go
manager, err := config.New(
    config.Defaults(defaultConfig()),
).Build(ctx)
if err != nil {
    return err
}

runErr := run(ctx, manager)
closeErr := manager.Close()
return errors.Join(runErr, closeErr)
```

所有权规则：

1. `Build` 成功后，调用方拥有 Manager，并负责调用一次正式 `Close`。
2. `Close` 幂等，会取消 watcher context、清空 subscriptions，并依次关闭 watcher 句柄和 Provider Sessions；不等待已开始的 Session.Load 返回。
3. 首次调用聚合 watcher/resource Close 错误；结果不会缓存，后续或并发调用看到 closed 后直接返回 nil，不会等待首次关闭完成。
4. 不能用无条件 `defer manager.Close()` 丢弃关闭结果。
5. Build context 只约束启动事务；Build 成功后 watcher 保留其 context values，但取消、deadline 不再向运行期传播。
6. Manager 是 watcher context 的唯一取消方；停止运行期监听必须调用 Manager Close。

## 4. 理解配置层和 bootstrap

### 4.1 Runtime Snapshot 优先级

```text
default(100)
  < file(200)
  < remote(300 + source.priority, priority=0..1000)
  < pinned(10000)
```

同一 priority 的 remote 层先按 provider 名稳定排序；同一 provider 内保留 source 声明顺序，后声明 source 覆盖先声明 source。`config.PinnedValues` 是最终固定覆盖，不会因 refresh 改变。

### 4.2 Bootstrap 优先级

bootstrap 只决定配置文件、provider 连接和 Sources，不进入最终业务 Snapshot：

```text
provider constructor defaults < file[0] bootstrap < ... < file[n] bootstrap < bootstrap env < BootstrapOverrides
```

推荐业务装配把不随环境变化的 typed 默认值直接交给具体 Provider 构造器；file/env/动态 CLI 或宿主输入均可覆盖同名默认值。`config.Provider(...)` 与 `config.BootstrapOverrides(...)` 的 Option 声明顺序不影响该优先级。完整 env 命名和类型协议见 [Bootstrap 环境变量契约](../architecture/config-bootstrap-env-contract.md)。

以同一个 `nacos.address` 为例，四层可以同时声明：

```yaml
# application.yaml
bootstrap:
  nacos:
    address: "file:8848"
```

```text
APP_NACOS_ADDRESS=env:8848
```

```go
manager, err := config.New(
    config.File(config.FilePath("application.yaml")),
    config.BootstrapEnvPrefix("APP"),
    config.Provider(nacos.New(nacos.Bootstrap{
        Address: "code:8848",
        Sources: []nacos.BootstrapSource{
            {GroupID: "DEFAULT_GROUP", DataID: "orders.yaml"},
        },
    })),
    config.BootstrapOverrides(map[string]any{
        "nacos.address": "input:8848",
    }),
).Build(ctx)
```

最终地址是 `input:8848`；移除 `BootstrapOverrides` 后是 `env:8848`，再移除环境变量后是 `file:8848`，最后才回退到构造默认值 `code:8848`。未被外部层覆盖的 `Sources` 继续使用构造默认值。

### 4.3 稳定声明与运行输入

| 信息 | 放置位置 |
|---|---|
| default、Provider 类型、validator、watch 策略 | Builder 声明 |
| provider connector 参数 | bootstrap file/env、Provider 构造参数或动态 override |
| 已持有的 Nacos/Consul Client 与 Sources | `config.Provider(nacos.NewWithClient(...))` / `consul.NewWithClient(...)` |
| 最终强制业务值 | `config.PinnedValues` |

Build 开始后不要修改传入的 map、provider、Client 或 Sources。Builder 没有冻结 API，其整体并发安全取决于这些扩展对象。

## 5. 使用配置文件和本地监听

### 5.1 默认发现与显式路径

未指定路径时，依次发现：

1. `application.yaml`
2. `application.yml`
3. `config/application.yaml`
4. `config/application.yml`

没有候选文件时允许继续构建。默认发现只选择首个存在的候选，不自动合并全部候选。显式 `config.File(config.FilePath(paths...))` 会逐个加载全部路径；任一路径不存在、重复或指向目录时 Build 失败。

文件路径选择顺序是：

```text
explicit FilePath list > bootstrap override config > env config > first default discovery match
```

`config.File(config.FileDisabled())` 是严格禁用：如果同时声明显式路径、watch、env 或 override 文件路径，会返回 `cfg.file.disabled_conflict`，而不是静默忽略。

稳定配置只声明一个 `config.File(...)`；路径、禁用状态和 watch 必须放在同一组 File Options 中。重复稳定声明返回 `cfg.file.invalid`。单次 `config.Input.File` 不计作重复声明，并只覆盖显式提供的 path/watch/disabled 字段。

多个显式文件按声明顺序形成独立 file layers：前文件优先级低，后文件覆盖前文件；map 深度合并，标量和 slice 整体替换。`Explain` 的 layer 包含最终来源文件路径。

### 5.2 开启本地文件 watch

```go
manager, err := config.New(
    config.File(
        config.FilePath("application.yaml", "application-prod.yaml"),
        config.FileWatch(5*time.Second),
    ),
    config.OnRefreshError(reportRefreshError),
).Build(ctx)
```

规则：

1. 默认不开启文件 watch；启用后采用文件 size/mtime 轮询。
2. 没有最终配置文件时，`config.FileWatch` 是 no-op；存在多个文件时监听全部路径。
3. 显式传入的首个正 interval 直接生效；未传或小于等于 0 时读取文件中的 `bootstrap.configFile.watchInterval`。
4. 文件值支持 `ms/s/min`，无单位按秒，范围 `[1s,60min]`；缺失或非法回退 `30s`。
5. 任一文件变化都会按原顺序重读全部 runtime file layers；bootstrap、Provider Session 和文件路径在 Build 后保持不变。
6. refresh 在 Snapshot 发布前失败时保留旧快照和扫描基线，下一轮会重试同一文件版本；Snapshot 已发布但 Subscriber 失败时确认扫描基线，不重复读取文件。

## 6. 注册远端 Provider

### 6.1 从 bootstrap 打开 Session

```go
func buildRemoteManager(
    ctx context.Context,
    remoteProvider provider.Provider,
    bootstrapOverrides map[string]any,
) (config.Manager, error) {
    return config.New(
        config.Provider(remoteProvider),
        config.BootstrapOverrides(bootstrapOverrides),
    ).Build(ctx)
}
```

`config.Provider` 以 `Provider.Name()` 为 key；名称为空或重复声明会在 Build 获取资源前 fail-fast。Provider 应遵循：未配置时 `Open` 返回 nil Session，配置非法时返回 error，配置有效时返回至少包含一个 Source 的独立 Session。具体 Provider 可在构造器中接收 typed bootstrap；`bootstrapOverrides` 仅供动态宿主输入覆盖。

### 6.2 接入已有连接或自定义 Provider

```go
manager, err := config.New(
    config.Provider(nacos.NewWithClient(client,
        nacos.BootstrapSource{GroupID: "order", DataID: "application.yaml", Priority: 20},
    )),
).Build(ctx)
```

官方 Provider 已为“调用方持有 Client”提供 `NewWithClient`，不需要在 core 暴露固定 Plan。传入 Client 是 borrowed：Session.Close 不关闭它，调用方仍负责其整体生命周期。

自定义配置中心实现三个中立契约：`Provider.Open` 建立一次 Session；`Session.Sources/Load/Close` 聚合连接和释放；厂商 Source 通过 `Info` 只暴露名称、优先级和可选性。支持 push/长轮询时再实现 `WatchableSession.Watch`；pull-only Session 无需伪造 watcher。Manager.Close 不 join 在途 Load，因此 Session.Close 必须幂等并可与 Load 并发；Load 应尽力响应 context，底层不支持取消时由 Provider 请求超时收敛。group/data、prefix/key 等定位字段应保留在自定义 Source 内部。

### 6.3 Source 失败语义

1. `Priority` 必须处于 `0..1000`，越界返回 `cfg.provider.session_invalid`。
2. required source 首次读取或 watcher 启动失败会让 Build 失败。
3. optional source 首次读取或 watcher 启动失败会跳过；已读取内容无法解析时仍然失败。
4. watcher 回调只作为变化信号，refresh 会重新读取全部 file/remote/pinned 层。
5. provider schema 同时定义 bootstrap 可发现性和默认 env 映射；`list/map` 可通过完整 YAML/JSON 文本注入，indexed Sources 等附加协议由可选 `BootstrapEnvLoader` 补充。
6. `Provider.Open`、`Session.Sources/Load/Watch` 和 `Source.Info` panic 会转为 Provider SPI error；Build 阶段清理已取得资源，refresh 阶段拒绝候选。`Provider.Name` 与 bootstrap schema/env/validation 方法仍必须 panic-free。

### 6.4 选择具体 Provider

`config` 只定义 `Provider -> Session -> Sources` 扩展契约，不定义具体远端系统的 bootstrap schema、定位方式、监听算法或 SDK 生命周期。选择实现后，按对应模块文档完成注册和配置：

| Provider | 模块入口 | 说明 |
|---|---|---|
| Nacos | [config-nacos README](../../config-nacos/README.md) | bootstrap 字段、Source 定位、SDK listener 与错误语义 |
| Consul | [config-consul README](../../config-consul/README.md) | bootstrap 字段、KV 定位、blocking query 与错误语义 |

无论使用哪种实现，Manager 只依据统一 Session 管理 Source 读取、watcher closer、快照合并与刷新发布。凭据等敏感 bootstrap 输入不得写入日志、错误、Explain 或完整配置 dump。

## 7. 读取和解释配置

### 7.1 一次性解码

```go
snapshot := manager.Current()
var service ServiceConfig
if err := snapshot.Decode("service", &service); err != nil {
    return err
}
```

scope 使用 dotted path，空字符串表示整份配置。输出必须是非 nil 指针；非空 scope 不存在时返回 `cfg.decode.scope_not_found`，且不改写目标。

### 7.2 Snapshot 读取

```go
snapshot := manager.Current()
address := snapshot.String("service.address", ":8080")
workers := snapshot.Int("worker.concurrency", 4)
```

`Raw/Flat` 返回深拷贝，`Get` 深拷贝引用值，来源元信息通过 `Explain` 按值返回。`String` 只接受 string/[]byte；`Int/Bool/Duration` 只做有限转换，类型不匹配、解析失败或溢出时返回默认值。typed getter 不应替代完整结构体校验。

### 7.3 排查来源

```go
snapshot := manager.Current()
meta, ok := snapshot.Explain("service.address")
if ok {
    logger.Info(ctx, "config value explained",
        log.F("layer", meta.Layer),
        log.F("priority", meta.Priority),
        log.F("pinned", meta.Pinned),
        log.F("version", meta.Version),
    )
}
```

`Explain` 是“为什么最终是这个值”的首选排障入口。

## 8. 校验、refresh 与运行状态

### 8.1 校验和观察 refresh 失败

```go
manager, err := config.New(
    config.SnapshotValidators(func(snapshot config.Snapshot) error {
        if snapshot.Int("worker.concurrency", 0) <= 0 {
            return errors.New("worker.concurrency must be positive")
        }
        return nil
    }),
    config.OnRefreshError(func(err error) {
        logger.Error(ctx, "config refresh failed",
            log.F("op", "config.refresh"),
            log.F("result", "failed"),
            log.F("reason", "validation_failed"),
            log.Err(err),
        )
    }),
).Build(ctx)
```

1. validators 按声明顺序执行，首个 error 停止校验。
2. 首次校验失败：Build 失败，不返回 Manager。
3. refresh 校验失败：拒绝候选快照，旧版本和 bindings 保持不变。
4. validator panic 会转为 `cfg.manager.snapshot_validate_failed` 错误。
5. 未设置 handler 时，watcher 触发的异步 refresh 失败不会主动通知调用方。
6. handler 应快速、非阻塞且不 panic；panic 会被隔离并写入标准日志。

### 8.2 使用 Status 诊断 Manager

```go
status := manager.Status()
if status.State() == config.ManagerStateClosed {
    logger.Warn(ctx, "config manager closed")
}
if status.Available() {
    logger.Info(ctx, "config snapshot available",
        log.F("version", status.Snapshot().Version),
        log.F("health", status.Health().Level),
        log.F("refresh_failures", status.Refresh().ConsecutiveFailures),
    )
}
```

`Status` 是调用时刻的不可变快照，适合健康检查和运行诊断：

1. `State()` 是 Manager 生命周期；`Health()` 是根据生命周期、Snapshot 和最近刷新结果派生的健康状态。
2. `Available()` 只回答“是否仍有合法 Snapshot 可读”。Close 后最后版本继续可读，因此 `State()==closed` 时仍可能 `Available()==true`，但 Health 为 unavailable。
3. refresh 发布成功、Subscriber 通知失败时，`Refresh().LastResult` 仍是 succeeded，失败记录在 `Notification()`，已经发布的 Snapshot 不回滚。
4. 刷新失败且旧 Snapshot 可用时 Health 为 degraded；后续刷新成功后恢复 healthy、连续失败次数归零，最近历史失败仍保留用于诊断。
5. `Resources()` 分别报告有效 Subscription、watcher 和 Provider Session 数量；Status 不包含配置内容或完整错误文本。

## 9. 使用 Observe 和 Subscribe

### 9.1 Observe：pull 最新状态

```go
builder := config.New(config.Defaults(defaultConfig()))
manager, err := builder.Build(ctx)
if err != nil {
    return err
}

worker, err := config.Observe[WorkerConfig](manager, "worker")
if err != nil {
    return err
}
cfg, err := worker.Current()
```

`Observation` 不注册回调或 goroutine；`Decode` 每次从最新 Snapshot 解码绑定 scope。泛型 `Observer.Current` 直接返回最新的 typed 配置块对象。

config 内核的 typed 状态读取统一使用 Observe；IOC 注入由 app-runtime 的 ConfigBinding 适配。

### 9.2 Subscribe：push 发布事件

```go
subscription, err := config.Subscribe[WorkerConfig](manager, "worker", func(update config.TypedUpdate[WorkerConfig]) error {
    if update.Initial {
        return worker.Start(update.Value)
    }
    return worker.Reload(update.Value)
})
if err != nil {
    return err
}
defer subscription.Cancel()
```

Subscribe 在返回前同步回放当前 Snapshot，`Initial=true`；后续只在 scope 变化时推送。Update 只包含 Snapshot、scope 内 Changes 和 Initial；每个 Subscriber 获得独立的 Change Old/New 引用值副本。

Subscription 只提供幂等 Cancel 和 Done。初次回调错误由 Subscribe 返回，后续错误进入 refresh 错误链和 `OnRefreshError`；错误不在 Subscription 中重复保存。

### 9.3 非泛型 Subscriber

Subscriber 自带 `Scope()` 和 `OnUpdate(Update) error`。Manager 在注册阶段固化 scope，并以独立 registration ID 管理取消；同一订阅的回放和后续版本严格串行。

运行语义：

1. refresh 先发布新 Snapshot，再同步通知命中 scope 的 subscribers。
2. subscriber panic 会转成 `cfg.subscriber.panic`，其余 subscribers 继续执行。
3. subscriber 错误不会回滚已经发布的 Snapshot。
4. 慢 subscriber 会阻塞当前 refresh 和 watcher 回调；不要执行无界网络调用。
5. Manager 不提供旧式 listener 兼容入口；pull 使用 Observe，push 使用 Subscribe。

## 10. Context、并发与关闭

1. Snapshot 通过原子指针发布；`Decode/Explain/getters` 都作用于同一个不可变版本。
2. refresh 全局串行，避免版本并发跳跃；版本从 1 单调递增。
3. Close 与 refresh 在发布锁上形成顺序：先 Close 则候选不发布，先 Store 则 Close 不回滚已发布版本；Close 不等待已经开始的 rebuild/Load。
4. 并发 Close 不形成 join：后续调用可能在首次调用仍关闭 watcher 时提前返回 nil。
5. Close 后仍可通过 Current/Observation 读取最后 Snapshot；新 Subscribe 和内部 refresh 返回 closed 错误。
6. Manager Close 不接收 context，也没有超时机制；provider watcher 的 `io.Closer` 必须自行避免无限阻塞。
7. 同一个 Builder 不承诺并发 Build；需要独立实例时创建独立 Builder 和输入对象。

## 11. 测试配置

单元测试优先禁用文件发现，避免工作目录污染：

```go
manager, err := config.New(
    config.File(config.FileDisabled()),
    config.Defaults(testConfig()),
).Build(context.Background())
if err != nil {
    t.Fatal(err)
}
t.Cleanup(func() {
    if closeErr := manager.Close(); closeErr != nil {
        t.Errorf("close manager: %v", closeErr)
    }
})
```

建议覆盖：优先级、Explain、缺失 scope、validator 拒绝、optional/required source、subscriber 初次回放与取消、Close 后不再发布、watcher/resource Close 错误。

## 12. 最佳实践

1. 一个进程只保留一个 Manager 所有者，由最外层装配边界统一 Build 和 Close。
2. 生产环境显式决定是否允许配置文件，不依赖未知工作目录中的默认发现。
3. provider connector 参数放 bootstrap，业务值放 runtime layers，不混用 `bootstrap` 根块。
4. 多 remote sources 显式设置 priority，并在运维文档中记录覆盖关系。
5. pinned 只用于必须稳定覆盖的输入；长期业务配置应回归正式来源。
6. 所有热更新路径设置 validator 和 `config.OnRefreshError`。
7. typed 状态读取使用 Observe；app-runtime IOC 使用 `ConfigValue[T]` 包装同一 pull 能力。
8. Subscriber 只做快速内存切换；耗时副作用使用业务自有队列，并定义顺序、失败和回滚语义。
9. Build 和 Close 的错误都必须保留，日志记录完整 error chain 与 `op/result/stage/reason`。
10. 对自定义 Provider、Session、Source 和 bootstrap 能力增加契约测试；运行期 SPI panic 会转为 error，Provider.Name 与 bootstrap 能力方法仍必须 panic-free。

## 13. 常见问题

| 现象 | 原因 | 处理 |
|---|---|---|
| 本地误读到仓库配置 | 默认文件发现命中工作目录文件 | 测试/嵌入场景使用 `config.File(config.FileDisabled())` |
| `disabled_conflict` | 禁用文件后仍声明显式、env 或 override 路径 | 选择严格禁用或移除冲突路径 |
| provider 注册后未连接 | bootstrap 未声明，Open 返回 nil Session | 检查 file/env/typed bootstrap |
| 具体 Provider 的 bootstrap 或监听异常 | schema、远端定位和重试语义由实现模块定义 | 查阅对应 Provider README，并确认 Open 返回了非 nil Session |
| 已有 Client 不应由框架关闭 | `NewWithClient` 使用 borrowed 语义 | 由创建 Client 的调用方负责最终关闭 |
| refresh 失败但业务值未变 | rebuild 或 validator 拒绝候选快照 | 查看 refresh handler，修复来源后再次触发变化 |
| Subscriber 报错但 Current 已变化 | Subscriber 在发布后执行 | 将发布前约束放入 SnapshotValidator |
| 修改文件 bootstrap 未生效 | Build 后只重读 runtime file layer | 重建 Manager 才能更改 Provider Session |
| Close 后还能读到值 | 最后 Snapshot 保留为只读视图 | 这是当前契约；Close 只停止更新和监听 |
| 第二个 Close 提前返回 | closed 标记在 watcher 关闭前设置，后续调用不等待 | 只让一个所有者执行正式 Close，并等待其结果 |
| 文件 refresh 持续失败 | rebuild、解析或 validator 在发布前持续失败 | 查看 refresh handler；修复原因后 watcher 会在下一轮自动重试同一文件版本 |

## 14. 可运行示例与相关文档

1. [quickstart-config](../../examples/quickstart-config/README.md)：本地、远端模拟、pinned、refresh 和 validator。
2. [config 模块入口](../../config/README.md)：范围、默认值和公共入口。
3. [Config 架构设计](../architecture/config-design.md)：bootstrap/runtime 边界、状态机与失败矩阵。
4. [Bootstrap 环境变量契约](../architecture/config-bootstrap-env-contract.md)：外部 env 协议。
5. [App Runtime 使用指南](app-runtime-usage.md)：统一 Runtime 所有权路径。
6. [config-nacos README](../../config-nacos/README.md)：Nacos 配置、错误和 SDK 生命周期。
7. [config-consul README](../../config-consul/README.md)：Consul KV 配置、blocking query 和生命周期。
