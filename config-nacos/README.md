# config-nacos

`config-nacos` 是 `config` 的 Nacos Provider：它根据启动声明打开 Nacos Session，并通过 `nacos-sdk-go` 实现远端配置的读取与监听。

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

| 字段 | 默认/规则 | 说明 |
|---|---|---|
| `address` | 无；启用 SDK 路径时必填 | 支持逗号分隔多地址；缺少端口时使用 `8848` |
| `namespace` | 空 | 交给 Nacos SDK/服务端选择默认命名空间，通常为 `public` |
| `username/password` | 空 | 可选认证信息，属于敏感字段 |
| `timeoutMs` | `10000` | `0` 使用默认值；正数直接传给 SDK，当前没有范围校验 |
| `scheme` | 地址内 scheme，最终回退 `http` | 显式字段优先于地址中的 scheme |
| `contextPath` | `/nacos` | 非空值缺少 `/` 时自动补齐 |
| `logDir` | `<系统临时目录>/nacos/log` | Nacos SDK 自有日志目录 |
| `cacheDir` | `<系统临时目录>/nacos/cache` | Nacos SDK 自有缓存目录 |
| `logLevel` | `warn` | 非空值直接交给 SDK |

`BootstrapSchema.DefaultValue` 用于可发现性说明，不会由 schema mapper 自动写入 bootstrap。上述默认值在 SDK client 创建阶段生效。

### Sources

1. `groupId` 和 `dataId` 必填；生成的 Source 名称固定为 `<groupId>/<dataId>`。
2. `priority` 默认 `0`，由 config 内核校验为 `0..1000`；数值越大，remote layer 越晚合并。
3. `optional` 默认 `false`。首次读取或 watcher 启动失败时，optional Source 可跳过；内容已读取但无法解析时仍然失败。
4. 只声明 connector 或只声明 Sources 都会返回 `cfg.nacos.binding.invalid`。
5. `New(Bootstrap)` 与 YAML/map 声明都会对缺少 `groupId/dataId` 的 Source fail-fast。

### 环境变量

schema 自动映射标量和完整列表，例如 `APP_NACOS_ADDRESS`、`APP_NACOS_TIMEOUT_MS`、`APP_NACOS_SOURCES`；Sources 也可使用更便于逐项注入的 `APP_NACOS_SOURCE_<N>_*` 扩展协议。完整键表见 [Bootstrap 环境变量契约](../docs/architecture/config-bootstrap-env-contract.md)。

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
5. `config.Manager.Close` 先取消 watcher context、关闭 watcher closer，再关闭 Session；当前 Nacos SDK Client 没有 client-level Close，因此 Session.Close 只保留统一所有权边界。
6. Nacos 变更回调只触发 config 内核全量重算，回调 payload 不直接 patch Snapshot。

## 最佳实践摘要

1. 普通应用与独立 config 场景都注册 `New()`；只有已持有 Client/Sources 时才使用 `NewWithClient`，并在 Manager.Close 后自行关闭 Client。
2. connector 参数放 bootstrap，业务配置放 Nacos Source 内容，不混入 `bootstrap` 根块。
3. 生产环境显式设置 namespace、认证来源和 Sources；不要依赖未知服务端默认值。
4. 密码只从受控环境或密钥系统注入，不写入示例、日志和诊断快照。
5. 为所有热更新路径配置 Snapshot validator 与 refresh error handler。

## Open Gaps / Future Improvements

1. 当前 Nacos SDK 接口没有 client-level Close，因此 Session 无法主动释放整个底层 SDK client。
2. 显式关闭 Nacos watcher 不会主动结束等待 context 的辅助 goroutine；直接使用 Client 时仍需取消 context。
3. timeout、scheme 和 SDK log level 缺少 provider 侧范围或枚举校验。
4. provider 没有内建连接、监听重试或回调失败指标。

## File Map

1. `bootstrap_schema.go`：typed bootstrap 与 Source 模型。
2. `bootstrap_codec.go`：bootstrap map 解码、typed defaults 编码和内部 binding 转换。
3. `bootstrap_adapter.go`：indexed Source 环境变量扩展。
4. `provider.go`：schema、校验、Session 建立和 Source 解析。
5. `client.go`：Nacos 包内 Client 契约。
6. `session.go`：Provider Session、加载/监听和资源所有权。
7. `sdk_client.go`：Nacos SDK client、地址解析与监听 closer。
8. `errors.go`：稳定错误码和错误包装。

## 相关文档

1. [config 模块 README](../config/README.md)
2. [Config 使用指南](../docs/guides/config-usage.md)
3. [Config 架构设计](../docs/architecture/config-design.md)
4. [Bootstrap 环境变量契约](../docs/architecture/config-bootstrap-env-contract.md)
5. [config-consul README](../config-consul/README.md)
