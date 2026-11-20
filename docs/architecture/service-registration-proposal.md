# 服务注册能力提案（TODO Feature）

> 状态：**待开发；`web.StartupServer` 启动结果合同已实现，其余注册能力尚未实现**。
>
> 本文记录 gobase 后续服务注册 feature 的目标模型、模块边界、生命周期和验收条件。
> 文中的包名、接口和配置仅为设计候选，不是当前公共 API，也不能作为现有运行保证。

## 1. 背景与问题

常驻 Web 服务接入 Nacos 等注册中心时，需要协调四类不同生命周期：

1. Web Server 只有在真实监听并可接收请求后才能发布实例；
2. 服务实例需要在运行期保持注册，并在停止前从注册中心摘除；
3. Web Server 摘流量后仍要排空存量请求；
4. Naming Client、日志和配置等 Resource 只能在 Workload 的 `Run` 完全收敛后释放。

参考项目中的 admin/redirector 通过 Hertz `server.WithRegistry` 注入 Nacos adapter，
再由 `Hertz.Spin()` 的启动和关闭钩子完成注册、注销。这证明多个服务存在共用能力需求，
但该实现依赖 Hertz 自有信号监听和固定延迟注册，不适合直接复制到 gobase。

当前 gobase 的事实边界是：

1. `app` 统一监听 OS 信号并监督 Workload/Resource，不允许 provider 再建立一套进程级信号入口；
2. `web-hertz` 使用 `Hertz.Run()` 而不是 `Spin()`；当前会拒绝 Hertz Registry Option，
   防止出现“停止时注销、启动时未注册”的不对称生命周期；
3. `app-web-hertz.Workload()` 已建立 `Run -> Shutdown -> 等待 Run -> Resource Close` 顺序；
4. 可选 `web.StartupServer` 已提供首次启动结果；readiness、Endpoint、持续 Status 和服务注册合同尚未提供。

因此，待开发能力不能只是放开 `server.WithRegistry`，而要复用现有 startup result，并显式设计实例身份、
注册失败回滚、摘流量、Web 排空和客户端所有权。

## 2. 决策摘要

本提案采用“通用语义合同 + 厂商 adapter + Web 生命周期组合”的三层方案：

1. 候选 `service-registry` 模块定义厂商无关的 `Registrar`、`Registration` 和 `Instance`；
2. 候选 `service-registry-nacos` 模块创建并拥有 Nacos Naming Client，实现服务注册合同；
3. `app-web-hertz` 通过可选窄接口组合 Web startup result 与注册生命周期，业务仍只声明一个
   Web Workload；
4. `app` 保持现有 Workload、Resource、Shutdown Policy 和信号语义，不感知 Nacos；
5. `config-nacos` 继续只负责配置中心，不承担 Naming Service，也不共享不同所有权的 SDK Client。

候选名称使用 `service-registry`，避免与 `ioc.Registry` 淆义。正式实现前仍需进行公共命名评审。

## 3. 目标与非目标

### 3.1 In Scope

1. 启动快照中的服务身份、注册地址和 provider 配置；
2. Server 启动成功后注册，注册失败时停止 Server 并回滚；
3. 收到关闭请求后先注销/禁用实例，再排空 Web 请求；
4. Nacos 临时实例、group、cluster、weight 和 metadata；
5. Naming Client 的 IOC Resource 所有权与关闭；
6. 结构化错误、`op/result/reason` 日志和可测试的阶段顺序；
7. standalone provider 测试、Web 组合测试和进程信号集成测试。

### 3.2 Out of Scope

1. 默认启用 Nacos 或根据依赖存在性静默注册；
2. 修改 `app` 的信号监听或增加一套 Registry 专用 Shutdown Policy；
3. 把服务注册、客户端服务发现、配置中心合并为一个大接口；
4. 自动猜测生产环境可达的网卡、Pod IP、NAT 地址或公网地址；
5. 把 `Hertz.Spin()` 重新引入 `web-hertz`；
6. 首版热更新服务名、注册地址、namespace、group 或 cluster；
7. 替代 Kubernetes Service/DNS、Service Mesh 或部署平台健康检查。

## 4. 模块与所有权

| 模块 | 候选职责 | 明确不负责 |
|---|---|---|
| `app` | 进程信号、Workload 监督、Shutdown Policy、Resource Close | Nacos、服务身份、Web startup/readiness |
| `web` | 保持最小 `Server` 生命周期合同；按需增加独立窄接口 | 厂商注册协议、配置加载 |
| `web-hertz` | 已发布 `WaitStarted` 启动结果；后续按需提供 Endpoint；维持 Hertz 单周期运行状态机 | 创建 Naming Client、监听 OS 信号 |
| `app-web-hertz` | 把 Server、Registrar 和 Registration 组合成一个 Workload 生命周期 | 持有厂商凭据、实现 Nacos SDK |
| `service-registry` | `Registrar`、`Registration`、`Instance` 等稳定语义 | Nacos SDK、IOC、Web provider |
| `service-registry-nacos` | 配置解析、Naming Client、Nacos 注册/注销 adapter | 配置中心 Source、Web Server、信号监听 |
| Application | 显式选择 provider，声明服务身份和可达地址 | 手写注册/注销时序 |

资源所有权：

```text
Application
  -> 声明 service-registry-nacos Module
  -> IOC 构造并拥有 Naming Client Resource
  -> app-web-hertz 借用 Registrar
  -> Registration 只拥有当前实例的注册句柄
  -> Workload 收敛后 IOC 关闭 Naming Client
```

`Registrar` 和 Naming Client 不能由 Middleware、Config 值对象或临时 goroutine 隐式创建。

## 5. 候选语义合同

接口形状在实现前可以调整，但必须保留以下语义：注册动作可取消、成功后产生唯一注销句柄、
注销幂等且不依赖调用者重新拼装实例身份。

```go
type Registrar interface {
    Register(ctx context.Context, instance Instance) (Registration, error)
}

type Registration interface {
    Deregister(ctx context.Context) error
}
```

候选 `Instance` 至少包含：

| 字段 | 语义 |
|---|---|
| `ServiceName` | 必填；稳定的逻辑服务名，不由框架隐式拼接 |
| `AdvertiseAddress` | 必填；消费者可访问的地址，不能直接发布 wildcard 地址 |
| `Group` | 可选；provider 默认值必须文档化 |
| `Cluster` | 可选；provider 默认值必须文档化 |
| `Weight` | 可选；零值和合法区间必须明确 |
| `Metadata` | 可选；必须复制并限制敏感信息、大小和 key/value 合法性 |
| `Ephemeral` | 可选；Nacos 首版建议默认临时实例 |

Web startup result 已通过不扩张现有 `web.Server` 的独立可选合同实现：

```go
type StartupServer interface {
    Server
    WaitStarted(ctx context.Context) error
}
```

已实现语义：

1. `WaitStarted` 在 Hertz 确认 running 后返回 nil；官方 transport 同时确认 listener 已存在，启动成功前失败返回稳定 startup result；
2. 多个等待者共享缓存结果，单个 caller context 结束不取消 Server、不污染共享结果；
3. 启动成功是历史启动屏障，不表达 readiness 或持续健康。

仍待设计的 Endpoint 语义：

1. Endpoint 必须使用独立窄接口，不能擅自把 `0.0.0.0`、`::` 转换成生产注册地址；
2. Application 显式 `AdvertiseAddress` 优先于监听地址推导；
3. startup result/endpoint 是服务注册的具体跨 provider 需求，但不得把 Nacos 类型加入根 `web.Server`。

## 6. 启动与关闭流程

### 6.1 正常启动

```text
Runtime Build
  -> IOC 初始化 Naming Client Resource
  -> IOC 构造 Hertz Server
  -> app 启动 app-web-hertz Workload
  -> Hertz 开始监听
  -> WaitStarted 返回启动成功
  -> Registrar.Register
  -> 发布 registration result=registered
  -> Workload 持续等待 Server 运行结果
```

注册只能发生在 WaitStarted 成功之后。不得使用固定 `sleep` 代替启动结果合同，也不得在 IOC `Init` 中注册
尚未运行的 Server。

### 6.2 注册失败

```text
WaitStarted success
  -> Register failed
  -> 记录结构化错误
  -> 请求 Hertz Shutdown
  -> 等待 Server Run 返回
  -> Workload 返回注册失败
  -> app 关闭 Runtime Resource
```

显式启用注册后，默认采用 fail-fast。首版不提供 best-effort，以免进程存活但服务实际不可发现。
若未来确有离线任务或特殊部署需求，应以高级策略显式声明，而不是改变默认值。

### 6.3 优雅关闭

```text
OS signal / 任一 Workload 返回
  -> app 取消运行 context
  -> app-web-hertz Workload.Shutdown
  -> Registration.Deregister（先摘流量）
  -> 可选等待注册中心/客户端缓存传播
  -> Server.Shutdown（停止接收；按 handlerCompletion 合同处理存量请求）
  -> 等待 Server.Run 完全返回
  -> IOC 逆序关闭 Naming Client 等 Resource
```

关闭不变量：

1. 注销发生在 Server 终止监听之前；
2. Naming Client 在注销和 Server `Run` 收敛前保持可用；
3. `WorkloadGracefulTimeout` 约束注销与 Server graceful 阶段，首版不新增普通用户必须理解的 timeout；
4. 即使 graceful context 超时，Workload 仍必须让 `Run` 真实结束，才能释放其借用的 Resource；
5. 重复 Shutdown、context 取消和 Server 自然退出不能导致重复注销破坏最终结果。
6. 默认 `bestEffort` 只保证有界尽力关闭；需要等全部已放行 Handler 后再允许 Resource Close 时，显式选择 `required`。

注册中心传播等待是高级能力。首版可默认 `0`，只有验证 Nacos 客户端缓存或网关行为确实需要时，
再提供有上限的 `DrainDelay`；它必须计入同一 graceful budget。

## 7. 声明与配置草案

普通使用者的候选装配方式：

```go
app.New(
    app.WithModules(
        appwebhertz.NewModule(),
        nacosregistry.NewModule(),
    ),
)
```

`appwebhertz.NewModule()` 自动贡献 Web Workload。用户不应再声明第二个“注册 Workload”，否则
app 并发 Shutdown 无法表达“先注销、再停止 Web”的内部顺序。注册协调应隐藏在同一个 Web Workload 中。

候选启动快照：

```yaml
serviceRegistration:
  serviceName: ssg-short-link.admin
  advertiseAddress: ${POD_IP}:8080
  weight: 10
  metadata:
    version: v1

  nacos:
    servers:
      - host: nacos
        port: 8848
    namespace: production
    group: DEFAULT_GROUP
    cluster: DEFAULT
```

配置语义：

1. 不声明 Nacos Module：注册能力不存在，Web 行为与当前版本一致；
2. 显式声明 Nacos Module：表示启用意图，缺失必填配置必须 fail-fast；
3. 配置只在 Runtime Build 读取一次，不热更新实例身份；
4. credentials 可来自 bootstrap、环境变量或 ConfigManager 快照，但不得进入错误和日志；
5. `config-nacos` 的 Config Client 与本 feature 的 Naming Client 分别拥有和关闭；未来如需复用，
   只能抽取无客户端所有权的连接参数 VO，不能共享隐式全局 Client；
6. 服务名必须显式声明。可以在更高层提供命名策略 helper，但不能把某个项目的拼接规则设为框架默认；
7. 容器环境推荐显式注入 Pod IP/可达地址；只监听 `:8080` 不足以确定可注册地址。

## 8. 错误与可观测性

候选稳定阶段：

| 阶段 | 典型 reason | 结果 |
|---|---|---|
| `load_config` | `scope_not_found`、`config_invalid` | Runtime Build 失败 |
| `create_client` | `nacos_client_create_failed` | IOC Build 回滚 |
| `wait_started` | `server_start_failed`、`context_done` | 不注册，收敛 Workload |
| `register` | `register_failed` | 停止 Server，返回失败 |
| `deregister` | `deregister_failed`、`context_done` | 继续停止 Server并聚合错误 |
| `close_client` | `client_close_failed` | Runtime Close 返回聚合错误 |

关键日志必须包含 `op/result/reason/stage/service/group/cluster/address`，失败时保留底层 cause。
禁止记录 password、token、完整 credentials、敏感 metadata 或 SDK 原始配置快照。

建议指标属于后续 adapter，可包括注册状态、注册/注销耗时和失败次数；首版不能为了指标而依赖
Prometheus 或修改 `app` 核心。

## 9. 不采用的方案

### 9.1 直接放开 Hertz `WithRegistry`

不采用。当前 `web-hertz` 使用 `Run()`，Hertz 注册启动钩子只由 `Spin()` 安装，直接放开会形成
不完整生命周期。

### 9.2 改回 `Hertz.Spin()`

不采用。`Spin()` 自己等待 OS 信号，会和 `app` 的进程信号、Shutdown Policy 和多 Workload
监督形成两套控制面。

### 9.3 把注册器作为独立 Workload

不采用。app 会并发请求多个 Workload Shutdown，独立注册 Workload 无法保证注销早于 Web Shutdown，
也会增加普通用户的声明负担。

### 9.4 把 Nacos Naming 加入 `config-nacos`

不采用。配置中心和服务注册拥有不同客户端、接口、失败语义和资源生命周期；合并会制造隐式共享
和关闭顺序歧义。

### 9.5 自动选择本机 IP

不作为生产默认。多网卡、容器、NAT 和 Service Mesh 环境中，本机 IP 不等于消费者可达地址。

## 10. 实施阶段

### Phase 0：合同验证

- [ ] 确认至少一个真实 Nacos consumer 依赖 Naming Service，而不是 Kubernetes DNS；
- [ ] 验证目标 Nacos SDK 的临时实例、心跳、注销和 Client Close 行为；
- [ ] 确定固定端口、预绑定 listener 与 `:0` 场景的 endpoint 语义；
- [ ] 确定公共包名，避免与 `ioc.Registry` 淆义。

### Phase 1：通用合同与 Nacos adapter

- [ ] 新增 `service-registry` 最小合同、错误语义和 fake；
- [ ] 新增 `service-registry-nacos` Module、typed config 和 Naming Client Resource；
- [ ] 覆盖配置校验、注册、幂等注销、context、SDK 错误和 Close 测试；
- [ ] 不接入 Web，不修改 app 信号模型。

### Phase 2：Web startup 与组合生命周期

- [x] 为 `web-hertz` 增加独立 `web.StartupServer.WaitStarted` 窄合同，不修改根 `web.Server`；
- [ ] 按真实注册需求增加独立 Endpoint 窄合同；
- [ ] 让 `app-web-hertz.Workload()` 可选借用 Registrar；
- [ ] 实现启动成功后注册、注册失败回滚、先注销再排空；
- [ ] 保持未声明 Registrar 时零行为变化；
- [ ] 删除 `web-hertz` 对 Registry 的能力拒绝只能发生在新合同完整替代后，不能提前放开。

### Phase 3：证据与文档

- [ ] fake Registrar 验证严格阶段顺序；
- [ ] 真实 listener 验证启动成功之前没有注册；
- [ ] 注册失败验证 Server 和 Runtime 完整回滚；
- [ ] OS 信号集成测试验证注销、排空、Run 收敛和 Resource Close；
- [ ] 超时测试验证不会提前销毁 Naming Client；
- [ ] 可选 Nacos integration test 验证临时实例、心跳和注销；
- [ ] 补齐模块 README、Guide、Architecture、示例和测试清单。

## 11. Feature 验收标准

该 feature 只有同时满足以下条件才能视为完成：

1. 未启用注册时，现有 Web 构造、运行、关闭和配置行为完全不变；
2. 显式启用后，实例只在 Server 启动成功后可见；
3. 注册失败不会留下仍在运行但不可发现的 Web Server；
4. 关闭时可以用测试证明 `Deregister < Server Shutdown < Run returned < Client Close`；
5. Shutdown context 超时不会导致 Naming Client 在 Workload 仍使用时被提前关闭；
6. 不存在第二套 OS 信号监听；
7. 业务不需要手写注册、注销、goroutine 或 ShutdownHook；
8. 错误可按阶段和稳定 code 分支，日志不泄露凭据；
9. Nacos adapter 可替换，通用合同不依赖 Nacos SDK 类型；
10. 文档明确说明 Kubernetes DNS 等不需要该 feature 的场景。

## 12. XY 风险与启用判断

服务注册不是所有部署的默认答案。如果服务调用方已经只通过 Kubernetes Service/DNS 或
Service Mesh 发现实例，再注册到 Nacos 会形成两套事实源，增加摘流量和故障排查复杂度。

开始实现前必须回答：

1. 哪些 consumer 实际查询 Nacos Naming？
2. Nacos 是唯一发现源，还是与 Kubernetes DNS 并存？
3. 注册地址由 Pod IP、Host IP、Service 地址还是网关地址提供？
4. 客户端缓存传播需要多长摘流量窗口？
5. 注册中心不可用时，服务应 fail-fast 还是允许离线运行？

本提案的默认答案是：显式启用、单一事实源、地址显式、注册失败 fail-fast、关闭先注销。
只有真实业务约束证明不适用时，才增加高级覆盖策略。

## 13. 相关文档

1. [App 生命周期管理](app-lifecycle-design.md)
2. [web-hertz 架构设计](web-hertz-design.md)
3. [app-web-hertz 架构设计](app-web-hertz-design.md)
4. [App Runtime 架构设计](app-runtime-design.md)
5. [Config 架构设计](config-design.md)
