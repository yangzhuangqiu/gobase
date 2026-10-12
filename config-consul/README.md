# config-consul

`config-consul` 是 `config` 的 Consul Provider：它根据启动声明打开 Consul Session，并通过 `hashicorp/consul/api` 实现远端配置读取和 blocking query 监听。

## 设计范围与目标

### In Scope

1. 提供 Consul bootstrap schema、typed bootstrap 和环境变量扩展映射。
2. 校验连接器与 Sources，打开实现 `provider.Session` 的运行会话。
3. 将 Consul KV Get 和 blocking query 收敛在 Consul 包内 Client/Session 下。
4. 通过统一 Provider 契约接入 `config`、`app-runtime` 与 `app`。

### Out of Scope

1. 不负责 default/file/remote/pinned 合并、Snapshot 发布和 Subscriber 通知；这些属于 `config` 内核。
2. 不向 Consul KV 写入配置，也不管理 ACL token。
3. 不把 bootstrap 连接参数写入最终业务 Snapshot。
4. 不管理应用或 `config.Manager` 生命周期；Session 只负责本次 Open 所取得的连接资源。

## 核心对象与职责

| 对象 | 职责 |
|---|---|
| `Bootstrap` | 传给 `New` 的 typed 启动默认值，可被 file/env/Build Input 覆盖 |
| `BootstrapSource` | 描述一个 Consul KV prefix/key 及其优先级、可选性 |
| `Provider` | 解析 `bootstrap.consul`，校验字段并打开 Session |
| `Session` | 聚合 Client、Consul Sources、加载/监听和关闭语义 |
| `Client` | Consul 包内的最小 Get/Watch 厂商契约，不泄漏到 config 内核 |
| `ClientOptions` | 描述 Consul API client 的地址、作用域和阻塞查询时长 |

## 最小使用示例

```go
func run(ctx context.Context) (err error) {
	manager, err := config.New(
		config.Provider(consul.New(consul.Bootstrap{
			Address: "127.0.0.1:8500",
			Sources: []consul.BootstrapSource{
				{Prefix: "services/order", Key: "application.yaml"},
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

普通应用可以使用 `app.WithConfig(config.Provider(consul.New()))`，由 Runtime 管理 `config.Manager`。完整场景见 [App 使用指南](../docs/guides/app-usage.md#8-使用配置)。

## 公共入口与核心契约

1. `New()` 返回无共享运行状态的 Provider；`New(Bootstrap)` 同时携带 Provider 本地默认值，但不建立连接。
2. 构造默认值会省略未声明的零值字段；file/env/Build Input 的同名字段优先，不完整 Source 在 Build 建连前 fail-fast。
3. `Provider.ValidateBootstrap()` 校验 Consul 块的结构、字段和可解析类型，不创建 API client；连接器/Sources 配对和 client 创建在 `Open()` 阶段校验。
4. 未声明 Consul 时，`Open()` 返回 nil Session；声明有效时返回包含 Client 和 Sources 的独立 Session。
5. `NewClient(options)` 创建 Consul 包内 Client；`NewWithClient(client, sources...)` 接入已有连接，Client 保持 borrowed，并与 `bootstrap.consul` 输入互斥。

## 配置模型与默认值

### Connector

| 字段 | 默认/规则 | 说明 |
|---|---|---|
| `address` | 无；启用 ClientOptions 路径时必填 | 可使用 `host:port` 或带 scheme/path 的 URL |
| `scheme` | 地址内 scheme，最终回退 `http` | 显式字段优先于 URL scheme |
| `datacenter` | 空 | 交给 Consul agent/server 选择默认数据中心 |
| `namespace` | 空 | 可选 Consul Enterprise namespace |
| `token` | 空 | ACL token，属于敏感字段 |
| `waitTimeMs` | `30000` | `0` 使用默认值；正数转换为 blocking query wait time，超过 time.Duration 可表达范围时拒绝；没有业务 min/max 限制 |

`address` 是 schema、typed bootstrap、map/YAML 和 env 的唯一规范字段。`BootstrapSchema.DefaultValue` 仅用于可发现性说明，实际默认在 API client 创建阶段生效。

### Sources

1. `prefix` 和 `key` 必填；二者 trim `/` 后拼为 Consul KV path。
2. `name` 默认 `<prefix>/<key>`，只用于日志、Explain 和层名称。
3. `priority` 默认 `0`，由 config 内核校验为 `0..1000`；数值越大，remote layer 越晚合并。
4. `optional` 默认 `false`。首次读取或 watcher 启动失败时，optional Source 可跳过；内容已读取但无法解析时仍然失败。
5. 只声明 connector 或只声明 Sources 都会返回 `cfg.consul.binding.invalid`。
6. `New(Bootstrap)` 与 YAML/map 声明都会对缺少 `prefix/key` 的 Source fail-fast。

### 环境变量

schema 自动映射标量和完整列表，例如 `APP_CONSUL_ADDRESS`、`APP_CONSUL_WAIT_TIME_MS`、`APP_CONSUL_SOURCES`；Sources 也可使用更便于逐项注入的 `APP_CONSUL_SOURCE_<N>_PREFIX/KEY/NAME/PRIORITY/OPTIONAL` 扩展协议。完整键表见 [Bootstrap 环境变量契约](../docs/architecture/config-bootstrap-env-contract.md)。

## 错误语义

| 错误码 | 含义 |
|---|---|
| `cfg.consul.internal` | 无法归入更细类别的内部错误 |
| `cfg.consul.bootstrap.invalid` | Consul bootstrap 块、字段或结构无效 |
| `cfg.consul.binding.invalid` | connector 与 Sources 组合无效 |
| `cfg.consul.source.invalid` | Source 字段、结构或值无效 |
| `cfg.consul.value.invalid_type` | 数字、布尔等 bootstrap 值无法转换 |
| `cfg.consul.sdk.invalid_input` | API 地址、KV path、缺失 key 或回调参数无效 |
| `cfg.consul.sdk.client_create_failed` | Consul API client 创建失败 |

provider error 会被 config Builder 按 Open、load 或 watch 阶段继续包装；调用方应使用 `errcode.IsCode` 检查错误链，不解析错误文本。Token 不得进入错误、日志或 bootstrap dump。

## 运行时语义

1. `Provider` 本身无连接和后台任务；`Open` 成功时创建独立 Session 和 Consul API client。
2. `GetConfig` 使用调用 context；KV 不存在时返回 `cfg.consul.sdk.invalid_input`。
3. `WatchConfig` 为每个 Source 启动 blocking query goroutine；首次成功查询只建立基线，不触发变更回调。
4. 后续只有 Consul index 变化且内容不同才回调；查询失败时固定等待 `500ms` 后重试，错误不会直接上报给 config refresh handler。
5. watcher closer 通过取消 context 幂等停止监听，但不等待 goroutine 完成；失败退避使用普通 sleep，取消最多延后到当前 `500ms` 退避结束后被观察。
6. Provider 创建的 API client 由 Session 持有；Manager 在停止 watcher 后关闭 Session，再由 Session 关闭空闲 HTTP 连接。`NewWithClient` 注入的 borrowed Client 不会被关闭。
7. Consul 变更回调只触发 config 内核全量重算，回调 payload 不直接 patch Snapshot。

## 最佳实践摘要

1. 普通应用与独立 config 场景都注册 `New()`；只有已持有 Client/Sources 时才使用 `NewWithClient`，并在 Manager.Close 后自行关闭 Client。
2. connector 参数放 bootstrap，业务配置放 Consul KV value，不混入 `bootstrap` 根块。
3. 生产环境显式设置 datacenter/namespace、受控 token 来源和 Sources。
4. 按服务容量定义并校验 blocking query 的业务范围；Provider 只提供转换溢出保护。
5. 为所有热更新路径配置 Snapshot validator 与 refresh error handler，并额外监控 Consul 可用性。

## Open Gaps / Future Improvements

1. watch 查询错误只做固定 `500ms` 退避，没有指数退避、抖动、错误回调或重试指标。
2. watcher Close 不等待 goroutine 完成；退避 sleep 也不能被 context 立即打断。
3. `waitTimeMs` 缺少业务 min/max 限制，scheme 缺少 provider 侧枚举校验。

## File Map

1. `bootstrap_schema.go`：typed bootstrap 与 Source 模型。
2. `bootstrap_codec.go`：bootstrap map 解码、typed defaults 编码和内部 binding 转换。
3. `bootstrap_adapter.go`：indexed Source 环境变量扩展。
4. `provider.go`：schema、校验、Session 建立和 Source 解析。
5. `client.go`：Consul 包内 Client 契约。
6. `session.go`：Provider Session、加载/监听和资源所有权。
7. `sdk_client.go`：Consul API client、KV path、blocking query 和 watcher closer。
8. `errors.go`：稳定错误码和错误包装。

## 相关文档

1. [config 模块 README](../config/README.md)
2. [Config 使用指南](../docs/guides/config-usage.md)
3. [Config 架构设计](../docs/architecture/config-design.md)
4. [Bootstrap 环境变量契约](../docs/architecture/config-bootstrap-env-contract.md)
5. [config-nacos README](../config-nacos/README.md)
