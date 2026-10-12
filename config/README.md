# config

`config` 是与 CLI、IOC 和具体配置中心 SDK 无关的配置内核：声明配置来源，构建不可变 Snapshot，并在文件或远端变化时校验、发布和通知。

## 设计范围与目标

### In Scope

1. 用声明式 `Option` 组合 default、file、remote、pinned 配置层。
2. 解析 bootstrap 后由 Provider 打开独立 Session，并加载中立 Source。
3. 发布可解释、带版本的只读 Snapshot。
4. 文件与远端 watcher 触发全量重算和发布前校验。
5. 通过 `Observe` pull 最新状态，通过 `Subscribe` push 已发布事件。
6. 通过包级泛型函数提供 typed Observe/Subscribe 适配。
7. 明确 Manager 对 watcher/subscriber 的关闭所有权。

### Out of Scope

1. CLI flags、应用 Runtime、IOC bean 注册和日志实现。
2. 具体 Nacos/Consul SDK 连接参数和重试算法。
3. 大型业务资源、规则包和多文件事务数据集。

## 核心对象

| 对象 | 职责 |
|---|---|
| `Option` | 声明稳定配置来源和发布策略 |
| `Input` | 描述一次构建的 bootstrap/pinned/env/file 策略输入 |
| `Builder` | 固化声明并构建 Manager；由 `New(options...)` 创建 |
| `Manager` | 发布 Snapshot、创建 pull Observation、管理 push Subscription、关闭 watcher |
| `Snapshot` | 某个版本的不可变配置和来源元信息 |
| `Status` | Manager 某一时刻的生命周期、健康、刷新、通知和资源状态快照 |
| `Provider` | 根据 bootstrap 打开一次独立 Session；返回 nil 表示本次未启用 |
| `Session` | 聚合连接、Source 加载能力和关闭所有权；可选实现 `WatchableSession` |
| `Source` | 只向内核暴露名称、优先级和可选性；厂商定位字段由具体 Provider 持有 |
| `Observation` | 绑定 scope、每次从最新 Snapshot 解码的非泛型 pull 视图 |
| `Subscriber/Subscription` | push 消费声明及其取消/完成生命周期 |

## 最小使用

```go
func loadService(ctx context.Context) (cfg ServiceConfig, err error) {
	builder := config.New(
		config.File(config.FileDisabled()),
		config.Defaults(defaultConfig()),
		config.Validate[ApplicationConfig](validateConfig),
	)

	manager, err := builder.Build(ctx)
	if err != nil {
		return cfg, err
	}
	defer func() {
		err = errors.Join(err, manager.Close())
	}()

	service, err := config.Observe[ServiceConfig](manager, "service")
	if err != nil {
		return cfg, err
	}
	return service.Current()
}
```

需要配置变化副作用时使用 `config.Subscribe[T]`。IOC typed binding 由 app-runtime 基于 Observe 提供，不属于 config 内核。

## 公共入口与契约

| API | 语义 |
|---|---|
| `New(options...)` | 推荐声明入口，不获取资源 |
| `Defaults` | 最低优先级默认层 |
| `File(FileOption...)` | 统一声明有序本地文件、禁用状态和监听策略 |
| `BootstrapEnvPrefix/Overrides` | CLI/宿主提供的启动阶段动态覆盖 |
| `PinnedValues` | 最终固定覆盖 |
| `Provider` | 注册一个远端 Provider；不同名称可各声明一次 |
| `Validate[T]` | 完整 Snapshot typed 校验 |
| `SnapshotValidators` | 高级原始 Snapshot 校验 |
| `OnRefreshError` | 异步 refresh 失败观测 |
| `Builder.Build` | 创建 Manager、首次快照和 watchers |
| `Snapshot.Raw/Flat/Get/getters` | 读取同一个不可变版本的结构、dotted 视图和标量值 |
| `Snapshot.Decode/Explain` | 在同一个不可变版本内完成解码和来源解释 |
| `Manager.Observe` | 创建绑定 scope 的 pull 视图 |
| `Manager.Status` | 读取 Manager 当前不可变运行状态快照 |
| `Manager.Subscribe` | 注册自带 scope 的 push Subscriber |
| `Observe[T]/Subscribe[T]` | 非泛型 Manager 能力的包级泛型适配 |

公共装配只有 `New(options...)` 一条路径；Builder 不暴露 `WithXxx` 双轨声明方法。

稳定配置只允许一个 `File(...)` 声明；重复声明返回 `cfg.file.invalid`。`FilePath` 可一次传入或多次追加多个路径，按声明顺序覆盖；`Input.File` 是单次 Build 覆盖层，不计作重复稳定声明。

## 配置优先级

```text
default(100)
  < ordered files(200, declaration order)
  < remote(300 + source.priority, priority=0..1000)
  < pinned(10000)
```

bootstrap 只决定文件路径、Provider 连接和 Sources，不进入最终业务 Snapshot：

```text
provider constructor defaults < file[0] bootstrap < ... < file[n] bootstrap < env bootstrap < Build Input/CLI overrides
```

具体 Provider 的 typed 构造参数是其本地默认值，不由 config 内核读取；file/env/Build Input 的同名字段均可覆盖它。因此 `Provider(...)` 与 `BootstrapOverrides(...)` 的 Option 声明顺序不会改变上述优先级。

迁移说明：`provider.BootstrapPatchProvider` 已删除。自定义 Provider 应在 `ValidateBootstrap/Open` 内部合并自己的 typed 默认值与传入的完整 bootstrap map，并保持传入值优先。

## Observe 与 Subscribe 语义

1. `Observation` 不注册回调、不启动 goroutine；`Decode` 每次从最新 Snapshot 解码绑定 scope。
2. `Observe[T]` 在创建时验证当前 scope；`Current` 每次返回最新的 typed 配置块对象。
3. `Subscribe` 在返回前同步回放当前版本，后续按版本和注册顺序串行通知。
4. `Update` 只包含 Snapshot、scope 内 Changes 和 Initial 标记；Change 的 Old/New 按订阅深拷贝。
5. `Subscription` 只提供 Cancel/Done；回调错误同步进入 Subscribe 或 refresh 错误链，不保存第二份错误状态。
6. Subscriber 失败发生在 Snapshot 发布后，不回滚当前版本。

## Status 语义

1. `Manager.Status()` 返回调用时刻的不可变状态快照，后续运行变化不会修改已返回对象。
2. `State()` 返回 Manager 生命周期阶段：`running/closing/closed`；首个 Snapshot 发布前为零值。
3. `Available()` 只表示存在可读取的有效 Snapshot，不等同于 Manager 健康或仍在运行；Close 后最后 Snapshot 仍可读，因此可能同时出现 `State()==closed` 与 `Available()==true`。
4. `Health()` 表达当前派生健康；刷新失败但旧 Snapshot 可用时为 `degraded`，后续刷新成功恢复为 `healthy`。
5. `Refresh()` 与 `Notification()` 分别记录刷新发布和 Subscriber 通知事实，通知失败不会把已经成功的刷新改成失败。
6. `Resources()` 返回当前 Subscription、watcher 和 Provider Session 数量，适合健康检查与关闭排障。

## 与 app-runtime 的边界

1. config 只提供 Snapshot、Observe 和 Subscribe，不感知 IOC bean 注册。
2. app-runtime 的 `WithConfigBinding[T]` 负责 scope 发布前校验和 typed bean 装配。
3. app-runtime `ConfigValue[T]` 通过 Observe pull 最新版本，不占用 Manager Subscription。

## 错误语义

错误使用 `cfg.*` 稳定 code，并保留 `op/result/stage/source/key`：

1. `cfg.file.*`：文件解析、加载和禁用冲突。
2. `cfg.bootstrap.*`：bootstrap schema、类型和 provider 输入。
3. `cfg.provider.*`：名称、重复和 Session/Source 完整性。
4. `cfg.decode.*`：typed validator/binding 解码。
5. `cfg.decode.scope_not_found`：请求的非空 scope 不存在。
6. `cfg.manager.snapshot_validate_failed`：首次或 refresh 发布前校验失败。
7. `cfg.manager.close_resource_failed`：Provider Session 释放失败。
8. `cfg.watch.*`：watcher 建立或异步 refresh 失败。

## 运行时语义

1. Snapshot 通过原子指针发布，读取无锁；返回视图按契约复制。
2. refresh 全局串行并全量重算，不直接 patch watcher payload。
3. validators 在发布前执行，subscribers 在发布后同步执行。
4. required source 失败阻断 Build/refresh；optional 拉取或监听失败可跳过。
5. Close 取消 watcher context、结束 subscriptions、关闭 watcher handles 并逆序关闭 Sessions；不等待已开始的 Load，但关闭后的候选不会发布，最后 Snapshot 仍可读。
6. Build context 只约束启动事务；Build 成功后 watcher 保留其 context values，但只由 Manager Close 取消。
7. 只实现 `Session` 表示 pull-only；实现 `WatchableSession` 后才会为 Sources 建立 watcher。
8. 本地文件 refresh 在 Snapshot 发布前失败时保留扫描基线，并按轮询周期重试同一文件版本；发布后的 Subscriber 错误不触发文件重读。

## 最佳实践

1. 常规装配使用 `New(options...)`，不要在 app-runtime 重复包装 config 选项。
2. 生产应用显式决定文件路径或禁用策略，不依赖未知工作目录。
3. 多 remote sources 显式设置 priority，并避免无意覆盖相同根 scope。
4. 所有热更新场景配置 validator 和 `OnRefreshError`。
5. typed 状态读取使用 `Observe[T]`；IOC 装配使用 app-runtime ConfigBinding；副作用使用 `Subscribe[T]`。
6. Subscriber 只做快速内存切换，耗时副作用进入业务队列。
7. 自定义配置中心把连接和释放放在 Session，把 group/data、prefix/key 等定位信息放在自己的 Source；Session.Close 必须幂等并可与在途 Load 并发，不要向内核复制厂商字段。

## Open Gaps / Future Improvements

1. 本地文件监听只有轮询。
2. refresh 会重新读取全部 remote sources，来源很多时成本线性增长。
3. Provider Session 在 Build 后固定，连接参数变化需要重建 Manager。
4. Manager Close 没有 context/deadline。

## File Map

1. `options.go`：声明式 Options、Input 和 schema 发现。
2. `builder.go`：Build、Session/Source 校验、watcher 和全量重算。
3. `manager.go`：Snapshot 发布、订阅注册表、并发和 Close。
4. `observation.go`：pull Observation 与包级泛型 Observe。
5. `subscriber.go`、`subscription.go`：push 事件、泛型 Subscribe 与生命周期。
6. `status.go`：Manager 状态 VO。
7. `snapshot.go`：不可变 Snapshot、Decode/Explain 和 typed getters。
8. `provider/provider.go`：Provider 及可选 bootstrap 能力。
9. `provider/session.go`：Session 与可选 WatchableSession 契约。
10. `provider/source.go`：中立 Source 与 SourceInfo 契约。
11. `provider/model.go`：bootstrap schema 元模型。
12. `errors.go`：稳定错误码。
13. `options_test.go`、`runtime_api_test.go`：Option、Observe、Subscribe 和 refresh 行为。

## 相关文档

1. [Config 使用指南](../docs/guides/config-usage.md)
2. [Config 架构设计](../docs/architecture/config-design.md)
3. [Bootstrap 环境变量契约](../docs/architecture/config-bootstrap-env-contract.md)
