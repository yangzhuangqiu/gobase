# Config Bootstrap 环境变量契约

本文集中说明 `config` 在 bootstrap 阶段的环境变量契约：键命名规则、优先级、生效条件与失败语义。

## 1. 适用范围

1. 适用于 `config.BootstrapEnvPrefix(...)` Option 路径。
2. Provider schema/env 扩展只对通过 `config.Provider` 注册且实现对应可选接口的 Provider 生效。
3. 不实现 schema/env/validator 的自定义 Provider 不依赖 env 解析；其 Open 仍走统一 Session 生命周期。

## 2. 优先级规则

### Bootstrap 阶段优先级（用于打开 Provider Session）

合并顺序（后者覆盖前者）：
1. Provider 构造默认值（由具体 Provider 内部持有）
2. `bootstrap` in file
3. `bootstrap` in env
4. `BootstrapOverrides`

语义：`provider constructor defaults < file < env < BootstrapOverrides`。`Provider(...)` 与 `BootstrapOverrides(...)` 的相对声明顺序不改变层级；多次声明 `BootstrapOverrides` 时仍由后声明的同 key 覆盖前声明。`BootstrapOverrides` 可以由 `Input`、CLI 适配层或其他宿主产生，但 config 不感知其上层来源。

补充说明：
1. `bootstrap.configFile.watchInterval` 是“仅配置文件生效”键，不参与 env/override 合并优先级。

Runtime Snapshot 的 `default/file/remote/pinned` 合并优先级不属于环境变量协议，统一见 [Config 架构设计：Runtime 层级](config-design.md#62-runtime-层级)。

## 3. 命名规则

### 3.1 前缀规范化（`BootstrapEnvPrefix`）

1. 转大写；
2. `-` 和 `.` 替换为 `_`；
3. 自动补尾部 `_`。
4. 空白前缀保持为空，此时直接读取 `CONFIG`、`NACOS_*`、`CONSUL_*` 等无前缀键。

示例：
1. `app` -> `APP_`
2. `my-app` -> `MY_APP_`
3. `my.app_` -> `MY_APP_`

### 3.2 Key 到 Env 的默认映射规则（schema 自动映射）

1. 读取 `BootstrapSchema` 中的 `Key`（dotted key）。
2. 将每段 key 转为 SNAKE_CASE。
3. 段间以 `_` 连接，再拼接前缀。

示例：
1. `config` -> `APP_CONFIG`
2. `nacos.timeoutMs` -> `APP_NACOS_TIMEOUT_MS`
3. `consul.waitTimeMs` -> `APP_CONSUL_WAIT_TIME_MS`
4. `mock.provider.apiURL` -> `APP_MOCK_PROVIDER_API_URL`

说明：
1. 不含 `[]` 的 `list/map` schema key 会自动映射，并把环境变量值按 YAML/JSON 文本解析；例如 `nacos.sources` 对应 `APP_NACOS_SOURCES`。
2. 含 `[]` 的 schema key（如 `nacos.sources[].groupId`）不会自动映射；Provider 可以通过 `BootstrapEnvLoader` 提供更便于部署系统注入的 indexed 协议。
3. 完整列表变量与 indexed 变量同时存在时，Provider env 扩展后执行，因此 indexed Sources 覆盖完整列表。
4. 环境变量读取后会 trim；未设置或 trim 后为空的值视为“未提供”，不能通过 env 表达显式空字符串。

## 4. 环境变量契约表

### 4.1 Core（内核）

| Env Key | 写入 bootstrap key | 类型 | 说明 |
| --- | --- | --- | --- |
| `APP_CONFIG` | `config` | `string` | 未声明显式 `FilePath` 列表时，指定单个启动配置文件路径 |

### 4.2 Nacos（schema 自动映射）

| Env Key | 写入 bootstrap key | 类型 |
| --- | --- | --- |
| `APP_NACOS_ADDRESS` | `nacos.address` | `string` |
| `APP_NACOS_NAMESPACE` | `nacos.namespace` | `string` |
| `APP_NACOS_USERNAME` | `nacos.username` | `string` |
| `APP_NACOS_PASSWORD` | `nacos.password` | `string` |
| `APP_NACOS_TIMEOUT_MS` | `nacos.timeoutMs` | `uint64` |
| `APP_NACOS_SCHEME` | `nacos.scheme` | `string` |
| `APP_NACOS_CONTEXT_PATH` | `nacos.contextPath` | `string` |
| `APP_NACOS_LOG_DIR` | `nacos.logDir` | `string` |
| `APP_NACOS_CACHE_DIR` | `nacos.cacheDir` | `string` |
| `APP_NACOS_LOG_LEVEL` | `nacos.logLevel` | `string` |
| `APP_NACOS_SOURCES` | `nacos.sources` | YAML/JSON `list` |

### 4.3 Nacos（`BootstrapEnvLoader` 扩展）

| Env Key | 写入 bootstrap key | 类型 |
| --- | --- | --- |
| `APP_NACOS_SOURCE_<N>_GROUP_ID` | `nacos.sources[N].groupId` | `string` |
| `APP_NACOS_SOURCE_<N>_DATA_ID` | `nacos.sources[N].dataId` | `string` |
| `APP_NACOS_SOURCE_<N>_PRIORITY` | `nacos.sources[N].priority` | `int` |
| `APP_NACOS_SOURCE_<N>_OPTIONAL` | `nacos.sources[N].optional` | `bool` |

### 4.4 Consul（schema 自动映射）

| Env Key | 写入 bootstrap key | 类型 |
| --- | --- | --- |
| `APP_CONSUL_ADDRESS` | `consul.address` | `string` |
| `APP_CONSUL_SCHEME` | `consul.scheme` | `string` |
| `APP_CONSUL_DATACENTER` | `consul.datacenter` | `string` |
| `APP_CONSUL_NAMESPACE` | `consul.namespace` | `string` |
| `APP_CONSUL_TOKEN` | `consul.token` | `string` |
| `APP_CONSUL_WAIT_TIME_MS` | `consul.waitTimeMs` | `uint64` |
| `APP_CONSUL_SOURCES` | `consul.sources` | YAML/JSON `list` |

### 4.5 Consul（`BootstrapEnvLoader` 扩展）

| Env Key | 写入 bootstrap key | 类型 |
| --- | --- | --- |
| `APP_CONSUL_SOURCE_<N>_NAME` | `consul.sources[N].name` | `string` |
| `APP_CONSUL_SOURCE_<N>_PREFIX` | `consul.sources[N].prefix` | `string` |
| `APP_CONSUL_SOURCE_<N>_KEY` | `consul.sources[N].key` | `string` |
| `APP_CONSUL_SOURCE_<N>_PRIORITY` | `consul.sources[N].priority` | `int` |
| `APP_CONSUL_SOURCE_<N>_OPTIONAL` | `consul.sources[N].optional` | `bool` |

Indexed Source 规则：

1. `<N>` 的扫描范围是 `0..63`，每个 provider 最多读取 64 个 Source 索引。
2. 第一个有效索引之前允许空洞；找到首个非空 Source 后，遇到第一个完全为空的索引即停止继续扫描。
3. 因此生产配置应从 `0` 开始连续编号，避免后续 Source 被提前截断。
4. 单个索引只提供部分字段时仍会进入 bootstrap，随后由 provider 对 Nacos 的 `groupId/dataId` 或 Consul 的 `prefix/key` 执行 fail-fast 校验。

### 4.6 仅配置文件生效（不映射 env）

| bootstrap key | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `configFile.watchInterval` | duration（ms/s/min；无单位按秒） | `30s` | 仅在启用 `File(FileWatch())` 且未显式传 interval 时生效；合法区间 `[1s,60min]`，非法值回退默认 |

## 5. 类型规范硬约束

`BootstrapKey.Type` 必须属于以下 canonical 集合：

1. `string`
2. `bool`
3. `int`
4. `int64`
5. `uint`
6. `uint64`
7. `float32`
8. `float64`
9. `list`
10. `map`

实现建议：
1. `BootstrapKey.Type` 使用 `config/provider.BootstrapSchemaType`（枚举底层为 string）。
2. provider 侧声明 `BootstrapSchema` 时，优先使用 `config/provider` 暴露的常量：
   - `BootstrapSchemaTypeString`
   - `BootstrapSchemaTypeBool`
   - `BootstrapSchemaTypeInt`
   - `BootstrapSchemaTypeInt64`
   - `BootstrapSchemaTypeUint`
   - `BootstrapSchemaTypeUint64`
   - `BootstrapSchemaTypeFloat32`
   - `BootstrapSchemaTypeFloat64`
   - `BootstrapSchemaTypeList`
   - `BootstrapSchemaTypeMap`

`BootstrapKey.DefaultValue` 只用于 schema 发现、CLI/help 和文档说明，config 内核不会把该文本自动写入 bootstrap。运行默认值必须由 Provider 构造声明或 Provider 自身实现提供。

可接受别名（会自动归一化）：
1. `boolean` -> `bool`
2. `array` -> `list`
3. `object` -> `map`
4. `float` -> `float64`

非法类型会在 `Build` 早期失败（`stage=validate_bootstrap_schema`），避免错误延迟到运行期。

## 6. 生效条件与常见误解

1. 仅设置环境变量，不注册 provider，不会生效。
2. 注册的 provider 在 Build 阶段总会收到一次 `Open`，入参是 Builder 已合并的完整外部 bootstrap map。
3. 官方 Provider 只有在 connector 与 Sources 成对完整时才打开 Session；完全没有构造声明和外部 bootstrap 时返回 nil Session，表示本次未启用。
4. `NewWithClient` 使用构造阶段注入的 borrowed Client 与 Sources，不读取同名 Provider bootstrap；两者同时存在时 fail-fast，避免静默覆盖。
5. `configFile.watchInterval` 只能放在配置文件 `bootstrap` 块中；env/override 不支持该键。

## 7. 失败与安全语义

1. 未知 bootstrap root 返回 `cfg.bootstrap.unknown_key`；接近已知 root 时可能包含 `did_you_mean`。
2. 非法 schema 类型在 Build 早期返回 `cfg.bootstrap.invalid_type`；env 文本解析失败时保留原字符串，由 Provider validator/Open 给出字段语义错误。
3. Provider bootstrap 校验失败返回 `cfg.bootstrap.provider_invalid`；未实现 `BootstrapValidator` 的 Provider 不执行该预检。
4. `config.File(config.FileDisabled())` 与 path/watch 或 env/override 中的 `config` 路径冲突时返回 `cfg.file.disabled_conflict`。
5. `configFile.watchInterval` 缺失、解析失败或越界时降级为 `30s`，不会导致 Build 失败。
6. password、token 等敏感环境变量不得写入日志、错误文本或完整 bootstrap dump；诊断只记录 key、provider、stage 和 reason。
