# config-nacos

`config-nacos` 是 `config` 的 Nacos Provider：它根据启动声明打开 Nacos Session，并通过 `nacos-sdk-go/v2 v2.3.5` 实现远端配置的读取与监听。

## 设计范围与目标

### In Scope

1. 提供 Nacos bootstrap schema、typed bootstrap 和环境变量扩展映射。
2. 校验连接器与 Sources，打开实现 `provider.Session` 的运行会话。
3. 适配 Nacos SDK 的 `GetConfig`、`ListenConfig` 和取消监听能力。
4. 通过统一 Provider 契约接入 `config`、`app-runtime` 与 `app`。

### Out of Scope

1. 不负责 default/file/remote/pinned 合并、Snapshot 发布和 Subscriber 通知；这些属于 `config` 内核。
2. 不向 Nacos 写入或发布配置。
3. 不把 bootstrap 连接参数写入最终业务 Snapshot。
4. 不管理应用或 `config.Manager` 生命周期；Session 只负责本次 Open 所取得的连接资源。

## 核心对象与职责

| 对象 | 职责 |
|---|---|
| `Bootstrap` | 传给 `New` 的 typed 启动默认值，可被 file/env/Build Input 覆盖 |
| `Server` | 描述单个 Nacos 节点的 host、HTTP/gRPC 端口、协议与上下文路径 |
| `BootstrapSource` | 描述一个 `groupId/dataId` 远端源及其优先级、可选性 |
| `Provider` | 解析 `bootstrap.nacos`，校验字段并打开 Session |
| `Session` | 聚合 Client、Nacos Sources、加载/监听和关闭语义 |
| `Client` | Nacos 包内的最小 Get/Watch 厂商契约，不泄漏到 config 内核 |
| `ClientOptions` | 描述官方 SDK 的连接参数和本地目录 |

## 最小使用示例

```go
func run(ctx context.Context) (err error) {
	manager, err := config.New(
		config.Provider(nacos.New(nacos.Bootstrap{
			Address: "127.0.0.1:8848",
			Sources: []nacos.BootstrapSource{
				{GroupID: "DEFAULT_GROUP", DataID: "application.yaml"},
			},
		})),
	).Build(ctx)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, manager.Close())
	}()

	var service ServiceConfig
	return manager.Current().Decode("service", &service)
}
```

普通应用可以使用 `app.WithConfig(config.Provider(nacos.New()))`，由 Runtime 管理 `config.Manager`。完整场景见 [App 使用指南](../docs/guides/app-usage.md#8-使用配置)。

## 公共入口与核心契约

1. `New()` 返回无共享运行状态的 Provider；`New(Bootstrap)` 同时携带 Provider 本地默认值，但不建立连接。
2. 构造默认值会省略未声明的零值字段；file/env/Build Input 的同名字段优先，不完整 Source 在 Build 建连前 fail-fast。
3. `Provider.ValidateBootstrap()` 校验 Nacos 块的结构、字段和可解析类型，不创建 SDK client；连接器/Sources 配对和 SDK 建连在 `Open()` 阶段校验。
4. 未声明 Nacos 时，`Open()` 返回 nil Session；声明有效时返回包含 Client 和 Sources 的独立 Session。
5. `NewClient(options)` 创建 Nacos 包内 Client；`NewWithClient(client, sources...)` 接入已有连接，Client 保持 borrowed，并与 `bootstrap.nacos` 输入互斥。

## 配置模型与默认值

### Connector

| 字段 | 定位 | 默认/规则 | 说明 |
|---|---|---|---|
| `address` | 简单连接配置 | `servers` 为空时使用 | 支持逗号分隔标准节点；默认使用 `http`、`/nacos` 和每个 HTTP 端口 `+1000` 的 gRPC 端口 |
| `servers` | 结构化连接配置 | 空 | 非空时整体覆盖 `address`；用于非标准部署或节点连接参数不同的集群 |
| `namespace` | 连接配置 | 空 | 交给 Nacos SDK/服务端选择默认命名空间，通常为 `public` |
| `username/password` | 连接配置 | 空 | 可选认证信息，属于敏感字段 |
| `timeoutMs` | 常规配置 | `10000` | SDK 请求超时，单位毫秒；`0` 使用默认值，正数直接传给 SDK |
| `logDir` | 高级 SDK 配置 | `<系统临时目录>/nacos/log` | SDK 自有日志目录；进程级 first-client-wins |
| `cacheDir` | 高级 SDK 配置 | `<系统临时目录>/nacos/cache` | SDK 快照缓存目录；默认路径不按集群隔离 |
| `disableUseSnapshot` | 失败策略 | `false` | `false` 时远程读取失败可回退本地快照；`true` 时直接返回错误 |
| `logLevel` | 高级 SDK 配置 | `warn` | 仅允许 `debug/info/warn/error`；进程级 first-client-wins |

`BootstrapSchema.DefaultValue` 用于可发现性说明，不会由 schema mapper 自动写入 bootstrap。上述默认值在 SDK client 创建阶段生效。

`servers[]` 中 `host` 必填；`port`、`scheme`、`contextPath` 默认分别为 `8848`、`http`、`/nacos`。`grpcPort` 为 `0` 或省略时由 SDK 按该节点 `port + 1000` 推导。`scheme` 仅允许小写 `http/https`，两个端口都必须落在 `1..65535`，派生后的 gRPC 端口也必须有效。

`logDir` 与 `logLevel` 最终初始化 Nacos SDK 的全局 logger。SDK 只接受进程内第一个 Client 的 logger 配置，后续 Client 即使声明不同值也不会重新初始化，因此这两个字段不是可靠的 per-client 隔离配置。

`disableUseSnapshot=false` 保留 SDK 默认的本地快照降级能力：远端读取失败时可能从 `cacheDir` 读取旧值；设为 `true` 后远程读取失败会直接返回错误。该字段只控制失败后的快照读取，不阻止 SDK 将成功获取的配置写入缓存。SDK 缓存键只包含 `dataId/group/namespace`，不包含集群地址；多个应用或集群共用目录且 Source 定位相同时存在读到错误快照的风险。单进程多集群或同一宿主运行多个应用时，应显式配置互不共享的 `cacheDir`。

### Sources

1. `groupId` 和 `dataId` 必填；生成的 Source 名称固定为 `<groupId>/<dataId>`。
2. `priority` 默认 `0`，由 config 内核校验为 `0..1000`；数值越大，remote layer 越晚合并。
3. `optional` 默认 `false`。首次读取或 watcher 启动失败时，optional Source 可跳过；内容已读取但无法解析时仍然失败。
4. 只声明 connector 或只声明 Sources 都会返回 `cfg.nacos.binding.invalid`。
5. `New(Bootstrap)` 与 YAML/map 声明都会对缺少 `groupId/dataId` 的 Source fail-fast。

### 环境变量

schema 自动映射标量和完整列表，例如 `APP_NACOS_ADDRESS`、`APP_NACOS_SERVERS`、`APP_NACOS_TIMEOUT_MS`、`APP_NACOS_DISABLE_USE_SNAPSHOT`、`APP_NACOS_SOURCES`；Sources 也可使用更便于逐项注入的 `APP_NACOS_SOURCE_<N>_*` 扩展协议。完整键表见 [Bootstrap 环境变量契约](../docs/architecture/config-bootstrap-env-contract.md)。

### 真实集成测试

`integration_test.go` 使用 `CONFIG_IT_NACOS_*` 环境变量连接真实 Nacos。`ADDR`、`GROUP` 和 `DATA_ID` 必填；namespace、账号密码、OpenAPI 基址和 token 按环境选填。显式 token 优先；token 为空且 username/password 完整时，测试通过 `/v1/auth/users/login` 自动获取仅用于当前进程的临时 token。

其中 `CONFIG_IT_NACOS_SCHEME/CONTEXT_PATH` 是集成测试构造 SDK `servers[]` 和管理 OpenAPI URL 的测试夹具字段，不是 `bootstrap.nacos` 的顶层配置项。

测试写入前会读取并保存目标 `group/dataId`：原配置存在时在 cleanup 阶段恢复原文，原配置不存在时删除测试创建项。备份失败时不会开始写入，恢复失败会令测试失败。即使具备自动恢复能力，仍应使用测试专用 Source，避免进程被强制终止时来不及执行 cleanup。

## 错误语义

| 错误码 | 含义 |
|---|---|
| `cfg.nacos.internal` | 无法归入更细类别的内部错误 |
| `cfg.nacos.bootstrap.invalid` | Nacos bootstrap 块、字段或结构无效 |
| `cfg.nacos.binding.invalid` | connector 与 Sources 组合无效 |
| `cfg.nacos.source.invalid` | Source 字段、结构或值无效 |
| `cfg.nacos.value.invalid_type` | 数字、布尔等 bootstrap 值无法转换 |
| `cfg.nacos.sdk.invalid_input` | SDK 地址、端口或回调参数无效 |
| `cfg.nacos.sdk.client_create_failed` | Nacos SDK client 创建失败 |

provider error 会被 config Builder 按 Open、load 或 watch 阶段继续包装；调用方应使用 `errcode.IsCode` 检查错误链，不解析错误文本。认证信息不得进入错误、日志或 bootstrap dump。

## 运行时语义

1. `Provider` 本身无连接和后台任务；`Open` 成功时创建独立 Session 和 SDK client。
2. `GetConfig` 只在调用 SDK 前检查 context；当前 SDK 方法不接收该 context，调用开始后的取消不能中断这次适配器调用。
3. `WatchConfig` 注册 SDK listener，并返回缓存首次取消结果的幂等 closer。
4. watcher context 取消会自动调用 closer；直接使用 Client 时，即使已经显式 Close，也应取消传入的 context，使辅助 goroutine退出。
5. `config.Manager.Close` 先取消 watcher context、关闭 watcher closer，再关闭 Session；owned Client 会调用 v2 SDK `CloseClient` 释放 gRPC 连接，borrowed Client 仍由调用方关闭。
6. Nacos 变更回调只触发 config 内核全量重算，回调 payload 不直接 patch Snapshot。

## 最佳实践摘要

1. 普通应用与独立 config 场景都注册 `New()`；只有已持有 Client/Sources 时才使用 `NewWithClient`，并在 Manager.Close 后自行关闭 Client。
2. connector 参数放 bootstrap，业务配置放 Nacos Source 内容，不混入 `bootstrap` 根块。
3. 生产环境显式设置 namespace、认证来源和 Sources；不要依赖未知服务端默认值。
4. 密码只从受控环境或密钥系统注入，不写入示例、日志和诊断快照。
5. 为所有热更新路径配置 Snapshot validator 与 refresh error handler。
6. 多应用或多集群不得共享 `cacheDir`；`logDir/logLevel` 按进程级 first-client-wins 规划。

## Open Gaps / Future Improvements

1. 显式关闭 Nacos watcher 不会主动结束等待 context 的辅助 goroutine；直接使用 Client 时仍需取消 context。
2. timeout 尚未设置 provider 侧最大值校验。
3. provider 没有内建连接、监听重试或回调失败指标。

## File Map

1. `model.go`：跨 bootstrap/SDK 的结构化 Server VO。
2. `bootstrap_schema.go`：typed bootstrap 与 Source 模型。
3. `bootstrap_codec.go`：bootstrap map 解码、typed defaults 编码和内部 binding 转换。
4. `bootstrap_server.go`：结构化 Server 列表的严格解析与编码。
5. `bootstrap_adapter.go`：indexed Source 环境变量扩展。
6. `provider.go`：schema、校验、Session 建立和 Source 解析。
7. `client.go`：Nacos 包内 Client 契约。
8. `session.go`：Provider Session、加载/监听和资源所有权。
9. `sdk_client.go`：Nacos SDK client、地址解析与监听 closer。
10. `errors.go`：稳定错误码和错误包装。
11. `integration_test.go`：真实 Nacos 发布、读取、监听、关闭及环境恢复验证。

## 相关文档

1. [config 模块 README](../config/README.md)
2. [Config 使用指南](../docs/guides/config-usage.md)
3. [Config 架构设计](../docs/architecture/config-design.md)
4. [Bootstrap 环境变量契约](../docs/architecture/config-bootstrap-env-contract.md)
5. [config-consul README](../config-consul/README.md)
