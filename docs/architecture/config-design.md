# Config 架构设计

`config` 解决的不是“读取一个 YAML 文件”，而是在启动与运行期间，把文件、环境变量、远端中心和进程固定覆盖组织成可解释、可校验、可热更新且有明确关闭责任的配置快照。

模块入口见 [config README](../../config/README.md)，任务型用法见 [Config 使用指南](../guides/config-usage.md)，外部环境变量协议见 [Bootstrap 环境变量契约](config-bootstrap-env-contract.md)。

## 1. 问题域、目标与边界

### 1.1 原始问题

配置运行链存在两类不同信息：

1. **bootstrap declaration**：配置文件路径、Provider connector 参数和 Sources 声明，只决定配置从哪里来。
2. **runtime snapshot**：业务最终读取的 default/file/remote/pinned 合并结果。

Build 负责完成两者之间的资源转换：`Provider.Open` 根据 bootstrap declaration 建立 `Session/Sources`，成功后由 Manager 接管其生命周期。`Session/Sources` 是运行资源，不是额外配置层，也不进入业务 Snapshot。这个边界避免了循环依赖：不能先依赖业务配置连接远端，再从该远端取得同一份业务配置。

### 1.2 设计目标

1. **阶段明确**：bootstrap 只决定来源，不进入业务 Snapshot。
2. **结果可解释**：每个 dotted leaf key 都能说明 layer、priority、pinned 和 version。
3. **发布原子**：读者只观察到完整旧快照或完整新快照。
4. **失败保守**：重算或校验失败保留旧快照；subscriber 失败不伪装成发布失败。
5. **扩展显式**：provider 通过 SPI 注册，不依赖隐式 init 或厂商 SDK 内置。
6. **所有权单一**：Manager 统一持有 watcher context、subscriptions、watcher handles 和 Provider Sessions。

### 1.3 非目标

1. CLI 参数解析、命令路由和 help。
2. config/log/ioc 的跨模块生命周期编排。
3. 厂商 SDK、配置写入、管理后台或服务端存储。
4. 业务对象依赖注入和资源 Start/Stop。
5. 增量 patch 事务或 subscriber 副作用回滚。

## 2. 核心设计决策

### 2.1 Bootstrap 与 Runtime 隔离

```text
bootstrap(file/env/code)
  -> provider.Open
  -> Session + vendor Sources
  -> runtime layers(default/file/remote/pinned)
  -> validated Snapshot
```

bootstrap 的生命周期止于 Build；Session 在 Manager 生命周期内固定；Snapshot 可以多次重算发布。这个拆分阻止远端 connector 参数被当作业务值读取，也避免 refresh 时反复改变连接拓扑。只有出现“运行期必须原子切换配置中心连接”的明确需求时，才重新评估固定 Session 拓扑。

### 2.2 不可变 Snapshot 与原子发布

Manager 使用原子指针发布 `snapshotState`。每次发布前深拷贝内部 raw/flat/meta；`Raw/Flat` 返回深拷贝，`Get` 深拷贝引用值，`Explain` 按值返回元信息，因此调用方不能修改 Manager 内部状态。

代价是每次 refresh 都有全量合并与复制成本，但读取路径无需锁，也不会观察到半更新结构。只有配置体积和刷新频率使复制成为实测瓶颈时，才考虑结构共享，而不退回可变全局对象。

### 2.3 Refresh 全量重算

远端 watcher callback 的内容只作为变化信号，不直接 patch Snapshot。refresh 重新读取 file（仅启用本地 watch 时）、全部 remote sources 和 pinned 层，再执行同一合并/校验流程。

这样初次构建与热更新只有一套语义；代价是远端源较多时刷新成本随来源数量和配置体积增长。只有全量 Load 成为可测量瓶颈，且 Provider 能提供一致的版本或内容契约时，才评估增量优化。

### 2.4 发布前校验与发布后通知

Validator 表达“候选配置是否允许成为当前版本”，必须在原子 Store 前完成。Subscriber 表达“已发布配置触发什么消费动作”，必须在 Store 后读取新版本。

因此两类失败含义不同：validator 失败拒绝发布并保留旧版本；subscriber 失败返回或上报错误，但新 Snapshot 已经生效，不能回滚。需要发布原子性的业务约束应进入 validator，而不是依赖 subscriber 副作用回滚。

### 2.5 Session 资源所有权与 Source 定位

Provider 只负责一次 `Open`，成功后立即把 Session 所有权移交给构建流程。Session 同时提供 Sources、Load 和 Close，使“连接在哪里、谁关闭”成为一个对象的完整契约；可选 `WatchableSession` 只表达监听能力。Manager 不 join 已开始的 Load，因此 Session.Close 必须幂等并可与 Load 并发。

Source 只通过 `Info` 向内核暴露 Name/Priority/Optional。Nacos 的 groupId/dataId、Consul 的 prefix/key 保留在各自 Source 实现中，避免 core 为每个厂商维护伪中立定位字段。已有 Client 由具体 Provider 的 `NewWithClient` 适配，借用还是托管也由对应 Session 明确表达。

## 3. 对象模型与依赖方向

| 对象 | 生命周期 | 状态职责 |
|---|---|---|
| `Builder` | Build 前 | 收集 defaults、file、bootstrap、providers、validators 和 watch 策略 |
| `runtimePlan` | 单次 Build 后固定 | 有序 file paths/layers、remote Sessions/Sources、pinned 和本地轮询间隔 |
| `Manager` | Build 成功到 Close | 当前 Snapshot、pull Observation、subscriptions、watcher cancel/closers、Sessions |
| `Snapshot` | 单次发布版本 | immutable raw/flat/meta/version/updatedAt，以及版本内 Decode/Explain |
| `Status` | 单次状态读取 | immutable lifecycle/health/snapshot/refresh/notification/resources 事实快照 |
| `Observation` | Manager 生命周期内 | 固定 scope 的无资源 pull 视图 |
| `Subscriber` | Subscribe 到 Cancel/Close | 自带 scope 的同步 push 消费方 |
| `provider.Provider` | Builder 扩展 | bootstrap schema/env/validation 与 Session Open |
| `provider.Session` | Manager 生命周期 | Sources、Load 和 Close；可选实现 WatchableSession |
| `provider.Source` | Session 生命周期 | 向内核暴露稳定中立元信息，厂商定位由实现私有持有 |

高层运行结构：

```text
┌────────────────────────────── Build ──────────────────────────────┐
│ ┌──────────────────── Bootstrap inputs ────────────────────────┐ │
│ │ File bootstrap │ Environment │ BootstrapOverrides           │ │
│ └──────────────────────────┬───────────────────────────────────┘ │
│                            v                                     │
│                      ┌─────────┐  Provider.Open  ┌─────────────┐ │
│                      │ Builder │ ───────────────> │ Sessions /  │ │
│                      │         │                  │ Sources     │ │
│                      └────┬────┘                  └──────┬──────┘ │
│                           └──────────┬───────────────────┘        │
│                                      v                            │
│ runtimePlan = files + Sessions/Sources + pinned + watch interval │
└──────────────────────────────────────┬────────────────────────────┘
                                       v
┌────────────────────────── Manager runtime ────────────────────────┐
│ Session.Watch / file poll ── change signal ──> Rebuild           │
│                                                   ^               │
│ Defaults / files / Session.Load / pinned ─────────┘               │
│                                                   │               │
│                                             Merge + Validate      │
│                                                   │               │
│                                             atomic publish        │
│                                                   v               │
│                  ┌────────────────┬────────────────┬────────────┐ │
│                  v                v                v            │ │
│            ┌──────────┐     ┌───────────┐     ┌────────┐       │ │
│            │ Snapshot │     │ Subscribe │     │ Status │       │ │
│            │ Current/ │     │ published │     │ runtime│       │ │
│            │ Observe  │     │ event     │     │ facts  │       │ │
│            └──────────┘     └───────────┘     └────────┘       │ │
└──────────────────────────────────────────────────────────────────┘
```

图中的 bootstrap 只决定配置来源和连接拓扑，不进入业务 Snapshot；watch payload 只作为变化信号，仍回到同一条全量重算与校验路径。

依赖方向：

```text
app/app-runtime -> config -> config/provider
config-nacos/config-consul -> config/provider
```

config 不依赖 app、app-runtime、IOC 或具体 provider 模块。

## 4. 声明式 Option、Builder 与默认值

`config.New(options...)` 是唯一公共声明入口，每个 Option 应用到本次独立 Builder；Builder 只公开 `Build`，没有 `WithXxx` 双轨入口，也不承诺并发 Build 安全。

| 声明 | 合并语义 |
|---|---|
| `Defaults` | 后声明覆盖整个 defaults 对象 |
| `File(FileOption...)` | 在唯一稳定声明中归一化有序 paths/disabled/watch；重复声明、重复路径或禁用冲突时 fail-fast |
| `BootstrapOverrides` | 深度合并，后声明同 key 覆盖；主要面向 Build Input/CLI/宿主 |
| `PinnedValues` | 深度合并，后声明同 key 覆盖 |
| `Provider` | 逐项按 `Provider.Name()` 保存；名称为空或重复声明记录输入错误，不读取 Provider 内部构造状态 |
| `Validate/SnapshotValidators` | 忽略 nil validator，按声明顺序追加 |
| `OnRefreshError` | 后声明覆盖；nil 表示异步失败静默 |

默认值：

1. 无 defaults、Provider、validators 和 refresh handler。
2. 配置文件读取启用并执行默认发现。
3. 本地文件 watch 关闭。
4. nil Build context 归一为 `context.Background()`。

## 5. Bootstrap 与文件解析

### 5.1 文件路径状态

```text
if FileDisabled():
  explicit paths/env/override path or watch -> cfg.file.disabled_conflict
  otherwise -> no file
else:
  explicit FilePath list > override config > env config > first default discovery match
```

显式路径列表按声明顺序逐个校验并转为绝对路径；任一路径不存在、指向目录或标准化后重复都会失败。没有显式路径时，默认候选只选择首个存在文件；候选全部不存在时使用空 file layers 继续 Build。

### 5.2 Bootstrap 合并

配置文件被选定后，Builder 提取并移除 `bootstrap` 根块：

```text
provider constructor defaults < file[0] bootstrap < ... < file[n] bootstrap < schema env + provider env extension < Build Input/CLI overrides
```

Provider 构造默认值由具体 Provider 在 `ValidateBootstrap/Open` 内部与 Builder 传入值合并，Builder 输入保持更高优先级。config 内核不提供构造 patch SPI，也不读取 Provider 私有配置。

strict 校验只统一检查 root：允许 `config` 和已注册 provider 名；未知 root 返回 `cfg.bootstrap.unknown_key`，可能带 `did_you_mean`。provider 叶子语义由可选 `BootstrapValidator` 负责。

`configFile.watchInterval` 是仅配置文件生效的特殊 bootstrap 值，在 provider strict 合并前移除。完整外部协议由独立 Contract 文档维护。

### 5.3 本地 watch interval

选择顺序：

1. `File(FileWatch(interval))` 的首个正值，直接使用。
2. 按顺序合并后的文件 `bootstrap.configFile.watchInterval`，支持 `ms/s/min`，无单位按秒，合法范围 `[1s,60min]`。
3. 默认 `30s`。

文件内值缺失、解析失败或越界时降级为默认值，不让 Build 失败。没有最终配置文件时不启动轮询。

## 6. Provider Session 与配置合并

### 6.1 Session 建立

1. Provider 按名称排序执行，保证跨 map 遍历稳定。
2. strict bootstrap 阶段只调用实现 `BootstrapValidator` 的 Provider。
3. `Provider.Open` 返回 nil Session 表示本次未启用；返回 error 时构建失败并关闭同时返回的 Session。
4. 非 nil Session 必须至少包含一个 Source；nil Source、空名称、同 Session 重名都会 fail-fast。
5. Builder 在 Open 阶段捕获一次 `SourceInfo`，并校验 priority `0..1000`，运行期不再重复读取元信息。
6. 只实现 Session 表示 pull-only；实现 `WatchableSession` 才会启动远端 watcher。
7. Provider 按名称排序，Source 按 priority 稳定排序；构建回滚和 Manager Close 均逆序关闭 Sessions。

### 6.2 Runtime 层级

| Layer | Priority | 来源标识 |
|---|---:|---|
| default | 100 | `default` |
| ordered files | 200 | `file:<absolute-path>` |
| remote source | `300 + source.priority` | `<provider>:<source.Name>` |
| pinned | 10000 | `pinned`，`Pinned=true` |

文件层使用相同 priority 并保留声明顺序，因此后文件稳定覆盖前文件。remote source 先按 priority 稳定排序；相同 priority 保留 provider name 顺序和同一 provider 内 source 声明顺序。所有层再按 priority 稳定合并，map 递归覆盖，标量和 slice 整体覆盖。

`ValueMeta` 记录最终 dotted leaf key 的来源。Snapshot 版本在发布时补入 Meta。

### 6.3 Optional source

1. optional source 的 `Session.Load` 失败会跳过该层。
2. optional source 的 `WatchableSession.Watch` 启动失败会跳过该 watcher。
3. 已成功读取但 YAML 解析失败仍让构建/refresh 失败。
4. required source 的读取或 watcher 启动失败会失败收口。

### 6.4 Nacos 与 Consul 适配边界

两个官方 provider 都实现 `Provider`、`BootstrapEnvLoader` 和 `BootstrapValidator`。validator 负责不建连的 bootstrap 解析与声明校验；connector/Sources 运行配对、Source 完整性和 SDK/API client 创建发生在后续 `Open` 阶段。

| 行为 | config-nacos | config-consul |
|---|---|---|
| 连接器 | Nacos SDK options；支持逗号分隔多地址 | Consul API options；地址可携带 scheme/path prefix |
| Source 映射 | Nacos Source 私有持有 `groupId/dataId` | Consul Source 私有持有 `prefix/key`，trim `/` 后映射为 KV path |
| Watch | SDK `ListenConfig`，context 取消时注销 | 每 Source 一个 blocking query goroutine |
| 首次 Watch 事件 | 由 Nacos SDK 决定 | 首次成功查询只建立基线，不回调 |
| Watch 错误 | SDK 内部语义 | 固定 `500ms` 退避并重试，不向 Manager 报告 |
| Watch Close | SDK `CancelListenConfig`，缓存首次结果 | 取消 context，返回 nil，不等待 goroutine |

Provider 创建的 Client 封装进 Session；Manager 在 Close 时先取消 watcher、关闭 watcher closer，再逆序关闭 Sessions，不等待已开始的 Load。官方 `NewWithClient` 把注入 Client 视为 borrowed，Provider 自建 Client 则由 Session 负责释放。

Nacos watcher closer 与自动取消 goroutine 共享同一幂等 Close；如果外部直接调用 closer Close 而不取消 context，等待 context 的辅助 goroutine仍会存活。Consul closer 只发出取消信号，查询 goroutine 或 `500ms` 失败退避不会被 join。

两个 provider 的变更 payload 都不直接参与 Snapshot 更新。config 的 watcher callback 忽略 payload 并调用全量 `refresh`，重新读取所有已声明远端 Sources，再执行 YAML 解析、合并和 validator。

## 7. Build 事务与失败路径

### 7.1 主流程

```text
validate Builder input/schema
  -> resolve ordered files + merged bootstrap
  -> open and validate remote Sessions/Sources
  -> create Manager and rebuild closure
  -> build + validate initial Snapshot(version=1)
  -> detach cancellation/deadline and create Manager-owned watcher context
  -> start remote watchers
  -> start local file poller
  -> return Manager
```

Build 只在首个 Snapshot 安装且全部 required watchers 启动成功后返回 Manager。

### 7.2 失败矩阵

| 失败点 | 已取得状态 | 可观察结果 |
|---|---|---|
| Builder 输入/schema/bootstrap/file/Session | 无 Manager 资源 | 返回对应 `cfg.*` error |
| 初次 layer load/merge | Manager 未发布、无 watcher | `Build` 返回 snapshot/file/provider error |
| 初次 validator | 候选 Snapshot 未 Store | `Build` 返回 `cfg.manager.snapshot_validate_failed` |
| required remote watcher 启动 | 可能已有早先 watcher handles | 调用 Manager.Close 后返回 `cfg.watch.failed` |
| local watcher stat | 可能已有 remote watcher handles | 调用 Manager.Close 后返回 `cfg.watch.failed` |
| Provider.Open/Session.Sources/Source.Info/Session.Load/Watch panic | 可能已有 Session、watcher 或旧 Snapshot | 转为 Provider SPI error；Build 阶段清理已取得资源，refresh 阶段拒绝候选；错误只保留 payload type |
| Provider.Name/bootstrap 能力方法 panic | 尚未进入或正处于声明/bootstrap 阶段 | 不隔离，panic 继续传播；扩展必须保证这些方法 panic-free |

watcher 启动失败时会通过 Manager.Close 回滚已取得 handles 和 Sessions；启动错误与补偿 Close 错误使用 `errors.Join` 一并返回。

`WatchableSession.Watch` panic 会转换为 error，构建流程随后关闭已启动的 watcher 和 Sessions。`Provider.Name`、`BootstrapSchema`、`LoadBootstrapFromEnv`、`ValidateBootstrap` 仍没有统一 panic 隔离，扩展必须遵守“可预期失败返回 error”的契约。

## 8. Refresh 状态机与通知

### 8.1 发布流程

```text
watch signal
  -> lock reloadMu
  -> closed precheck
  -> full rebuild
  -> prepare next version
  -> run validators
  -> diff old/new
  -> lock manager state
  -> closed publish precheck
  -> atomic Store + capture subscribers
  -> notify matching subscribers
```

即使 diff 为空，成功 Store 的 Snapshot 版本仍递增；diff 为空时不通知 subscriber。

### 8.2 失败边界

1. rebuild、publish precheck 或 validator 失败发生在 Store 前，旧 Snapshot 保持不变。
2. context 取消、Manager Close 或 subscriber 错误若发生在 Store 后，新 Snapshot 保留，通知阶段返回错误。
3. subscriber panic 转为 `cfg.subscriber.panic`；其余 subscribers 继续执行，错误聚合。
4. watcher 路径只有配置了 refresh handler 才向调用方上报 refresh error。
5. handler panic 被 recover，并通过标准库 logger 输出 `stage=callback_panic`。
6. Provider.Open、Session.Sources/Load/Watch 和 Source.Info panic 会在 Builder 边界转为 Provider SPI error；声明/bootstrap 能力方法 panic 仍会继续传播。

### 8.3 Subscribe 注册

Subscriber 必须非 nil，并通过独立递增 registration ID 标识。Subscribe 在返回前同步回放当前 Snapshot；每个 registration 以独立通知锁保证初次回放先于后续版本。首次回放失败会注销并返回错误。

Subscription 只提供幂等 Cancel 和 Done 信号。Subscriber 错误同步进入 Subscribe/refresh 错误链，因此 Subscription 不保存第二份 Err 状态。

Observe 表达无独立资源的 pull 状态读取，Subscribe 表达带生命周期的 push 发布事件，二者语义互斥且入口唯一。

### 8.4 IOC typed binding 边界

config 内核不创建或注册 IOC typed Value。app-runtime 通过 SnapshotValidator 完成 scope 发布前校验，并基于 Observe 创建每个 Runtime 独立的 ConfigValue；该 pull 视图不参与 Subscriber 通知链。

## 9. Context、并发与资源所有权

1. Manager 的 Snapshot 读取由 atomic pointer 支撑；refresh 由 `reloadMu` 全局串行。
2. Build context 只控制启动事务；Build 成功后通过 `context.WithoutCancel` 保留 values、切断父级 deadline/cancel，再创建由 Manager 独占的 watcher context。
3. Close 先在锁内标记 closed、清空 subscriptions/handles/resources，再取消 context、结束 Done，最后依次关闭 watcher handles、逆序关闭 Sessions 并聚合错误；不 join 在途 rebuild，后者完成后会在发布点因 closed 被拒绝。
4. Close 幂等但不提供 join；closed 标记在 watcher handles 实际关闭前设置，并发后续调用直接返回 nil，不等待首次关闭完成。
5. 首次调用后的错误不会缓存，后续 Close 不能取得首次结果。
6. Close 与 refresh 在发布锁上形成稳定顺序：先 Close 则候选不发布，先 Store 则 Close 不回滚已发布 Snapshot。
7. Close 后 Current、Snapshot.Decode/Explain 和 Observation 仍可读取最后版本；Subscribe 和内部 refresh 返回 closed error。
8. Manager.Close 没有 context/timeout；远端 watcher Closer 必须自行保证可收敛。

`Manager.Status()` 返回不可变时点快照：`State()` 表达生命周期，`Available()` 仅表达合法 Snapshot 是否可读，`Health()` 由生命周期、Snapshot 和最近刷新结果派生。refresh 发布与 Subscriber 通知分别记录在 `Refresh()` 和 `Notification()`，避免发布后通知失败污染刷新健康；`Resources()` 反映当前 Subscription、watcher 和 Provider Session 数量。Close 后允许 `State()==closed`、`Available()==true` 与 Health unavailable 同时成立。

Manager 不内建强制 Close timeout，因为无法安全终止不合作的 `io.Closer`；如果业务需要关闭时限，应由 Provider 的请求超时与 Closer 收敛契约保证。只有引入可等待、可取消的资源协议后，才重新评估 Manager 级 deadline。

## 10. Snapshot 读取契约

`Snapshot` 是封口只读接口，包外不能实现。`Raw/Flat` 返回深拷贝，`Get` 对引用值深拷贝，来源元信息通过 `Explain(key)` 按值返回；新代码按需使用 Get/Decode/Explain。

typed getter 当前行为：

1. `String`：只接受 string/[]byte。
2. `Int`：只接受可无损转换为 int 的整数或十进制字符串，并执行溢出保护。
3. `Bool`：只接受 bool 或可解析布尔字符串。
4. `Duration`：只接受 time.Duration 或可解析 duration 字符串，拒绝无单位数值。

`Snapshot.Decode`、`Observation.Decode` 和泛型 Observe/Subscribe 使用 YAML 语义。非空 scope 不存在时返回 `cfg.decode.scope_not_found` 且不改写目标；输出必须是非 nil 指针。

## 11. 错误与可观测性

稳定错误码按职责分组：

1. `cfg.file.*`：文件解析、加载、禁用冲突。
2. `cfg.bootstrap.*`：结构、unknown key、类型和 provider bootstrap 校验。
3. `cfg.provider.*`：Provider SPI、声明去重与 Session/Source 校验。
4. `cfg.snapshot.*`：default/remote layer 构建与解析。
5. `cfg.manager.*`：校验、refresh、subscribe 和 watcher/Session Close。
6. `cfg.subscriber.*`、`cfg.observe.*`、`cfg.decode.*`、`cfg.out.*`：消费端契约错误。

关键 Build/refresh/close 错误携带 `op/result/stage/reason`，远端路径按场景补充 provider/source/key；底层解析错误不保证同时具备全部字段。`Explain` 提供值来源诊断，Snapshot version/updatedAt 与 `Status` 提供运行事实；config 当前没有内建 metrics/tracing。

## 12. 边界不变量

1. bootstrap 不进入业务 Snapshot。
2. remote priority 不得越过 pinned。
3. Build 成功时首个 Snapshot 和 required watchers 均已就绪。
4. Snapshot 发布是原子的，已返回版本不会被后续 refresh 修改。
5. validator 失败发生在发布前，subscriber 失败发生在发布后。
6. refresh 全量重算，watcher payload 不直接 patch 当前 Snapshot。
7. Manager 是 watcher context、handles、Sessions 和 subscriptions 的唯一关闭入口。
8. Close 后不会再发布新 Snapshot，但最后版本保持可读。
9. validator、subscriber、refresh error handler、运行期 Provider SPI 和资源 Close 具有显式 panic 隔离；Provider 声明/bootstrap 能力方法 panic 保持传播。

## 13. 容量与性能边界

1. Snapshot 读取为原子指针读取；Raw/Flat 会按数据量复制，Get 对引用值复制。
2. refresh 成本约为文件读取 + 全部 remote Session.Load + YAML 解析 + 全量 merge/flatten/diff。
3. subscribers 在 refresh goroutine 中按注册顺序同步执行，最慢 subscriber 决定通知尾延迟。
4. 本地文件 watch 每个 Manager 使用一个 ticker goroutine 扫描全部路径；任一状态变化都会全量重读有序文件集合。发布前失败不推进扫描基线并在下一轮重试，发布后的 Subscriber 错误会推进基线；远端 goroutine 模型由 provider Client 决定。
5. typed `Observer.Current` 每次从最新 Snapshot 解码；app-runtime ConfigValue 负责 IOC 包装，并在解码异常时保留最后一次成功值。

## 14. Open Gaps / Future Improvements

1. provider 叶子 key typo 缺少 core 统一提示与字段路径协议。
2. subscriber 没有异步队列、重试、隔离或背压策略。
3. 本地文件监听只有轮询，没有原生文件事件后端。
4. bootstrap/Session 不能在线重建与原子切换。
5. Manager Close 无 context、deadline 和阻塞观测。
6. 缺少 config 自有的 refresh duration、累计失败次数和 subscriber latency 指标；`Status.Refresh().ConsecutiveFailures` 只表达当前连续失败状态，不是 metrics 导出。
7. Provider.Name 和 bootstrap schema/env/validation 能力缺少统一 panic 隔离，扩展仍需保证这些声明阶段方法 panic-free。
8. 并发 Close 不等待首次关闭完成，也不共享首次关闭错误。
9. Nacos SDK 没有 client-level Close；其 Session.Close 只能维持统一边界，无法主动释放整个底层 SDK client。
10. Consul watch 使用固定退避且没有错误回调、抖动或重试指标；Nacos SDK 监听失败观测依赖 SDK 自身。

## 15. File Map

1. `config/options.go`：声明式 Options、Input 和 bootstrap schema 发现。
2. `config/builder.go`：Builder、Build 事务、Sessions/Sources、watchers 与 refresh rebuild。
3. `config/manager.go`：Snapshot 发布、订阅注册表、并发和 Close。
4. `config/observation.go`：pull Observation 与泛型 Observe。
5. `config/subscriber.go`、`config/subscription.go`：push 事件、泛型 Subscribe 与生命周期。
6. `config/status.go`：Manager 状态 VO。
7. `config/snapshot.go`：封口 Snapshot、Decode/Explain 与 typed getter。
8. `config/snapshot_validator.go`：发布前 validator 契约。
9. `config/merge_layer.go`：稳定优先级合并。
10. `config/model.go`：SourceKind、bootstrap 常量和 ValueMeta/Change。
11. `config/bootstrap_schema.go`：core bootstrap schema。
12. `config/yaml_file.go`：YAML 文件读取。
13. `config/errors.go`：config 错误码。
14. `config/provider/provider.go`：Provider、BootstrapSchemaProvider、BootstrapEnvLoader、BootstrapValidator。
15. `config/provider/session.go`：Session 与 WatchableSession 契约。
16. `config/provider/source.go`：中立 Source 与 SourceInfo 契约。
17. `config/provider/model.go`：BootstrapKey。
18. `config/provider/bootstrap_env_mapper.go`：schema 到 env 的映射。
19. `config-nacos/provider.go`、`config-nacos/session.go`：Nacos schema、Session 与 Source 解析。
20. `config-nacos/sdk_client.go`：Nacos SDK Get/Watch 适配。
21. `config-consul/provider.go`、`config-consul/session.go`：Consul schema、Session 与 Source 解析。
22. `config-consul/sdk_client.go`：Consul KV Get 与 blocking query 适配。
23. `config/options_test.go`：Option 与文件声明行为。
24. `config/builder_test.go`：bootstrap、优先级、watch 和失败路径。
25. `config/manager_test.go`：刷新、关闭和并发边界。
26. `config/runtime_api_test.go`：Observe、Subscribe、Status 与 Snapshot 版本内能力。
27. `config/usecase_test.go`：公共使用场景。
