# App Runtime 架构设计

## 1. 是什么：解决什么问题

`app-runtime` 解决的是应用基础设施的构建事务和所有权，而不是重新包装 config、log、ioc 的全部 API。

一个应用通常需要：

1. 先得到配置快照，才能构建依赖配置的日志实现。
2. 日志可用后创建 IOC，让容器和业务生命周期进入统一观测。
3. 只有全部阶段成功才能向调用方发布可运行实例。
4. 任一失败都要按已取得资源的相反顺序回滚。
5. 正常退出要保证业务 bean 先停、日志再 flush、配置 watcher 最后关闭。

因此 Runtime 固定事务为：

```text
Build: config -> typed ConfigValues -> logging -> IOC -> beans/modules -> IOC Build
Close: IOC -> logging -> config
```

## 2. 为什么：能力聚合而不是 API 镜像

旧的平铺入口把 config 的 defaults、file、provider、validator、refresh handler 分别复制到 app-runtime。调用方既要理解 config 原始 API，又要判断是否应该使用 Runtime 包装 API。

当前选择：

```go
appruntime.WithConfig(
    config.Defaults(...),
    config.File(...),
    config.Provider(...),
    config.Validate[T](...),
)
appruntime.WithConfigBinding[T](...)
```

边界是：

```text
app-runtime 拥有能力组合与资源所有权
config 拥有配置来源、优先级、校验和发布语义
Logging 实现拥有日志 Provider 构建
ioc 拥有 bean 图、注入和生命周期
业务 Module 拥有 bean 声明
```

这避免 app-runtime 演变为三个底层模块的 API 镜像。

## 3. 为什么：稳定声明与单次输入分离

`Application` 是可重复使用的蓝图，保存 Name、Config Options、Logging、Bean Definitions、Modules 和 IOC Options。

`BuildInput` 只保存一次运行才确定的 `config.Input`。普通路径调用 `Build(ctx)`；CLI、测试和嵌入式宿主调用 `BuildWithInput(ctx, input)`。

应用顺序：

```text
Name 派生 env prefix
  < Application Config Options
  < BuildInput.Config Options
```

单次输入不会写回 Application。`FileDisabled()` 是安全收紧语义，后声明只能禁用，不能重新开放。

## 4. 为什么：Bean Definitions 与 Modules 分层

要求每个小应用都定义具名模块会增加仪式成本，因此提供两条路径：

1. `WithBeanDefinitions`：应用私有对象及其完整接口、名称和生命周期策略。
2. `WithModules`：跨应用复用、需要稳定错误归因的 bean 集合。

Module 不是 AutoConfiguration。它没有扫描、条件匹配和隐式发现，只是显式模块声明。

Module 只获得：

```go
type Registry interface {
    Register(bean any, options ...RegisterOption) error
}
```

不暴露 Resolve、Build、Metrics 和 Close，原因是 Register 阶段只允许声明。资源获取放在 bean Init，依赖和关闭顺序由 bean 图决定。

如果 Module 自己 Start/Stop，Runtime 会同时拥有模块顺序和 bean 图两套生命周期事实，失败回滚无法可靠合并。

## 5. 为什么：typed config binding 属于 app-runtime

config 内核只负责 Snapshot、Observe 和 Subscribe，不应感知 IOC。`WithConfigBinding[T]` 在每次 Runtime Build 中创建独立 `*ConfigValue[T]`：

```text
candidate Snapshot
  -> decode scope into new T
  -> validate T
  -> publish Snapshot
  -> ConfigValue.Load pull Observer latest version
```

关键不变量：

1. 首次解码或校验失败时 Build 失败，不返回 Runtime。
2. refresh 失败时旧 Snapshot 保持不变，ConfigValue 继续读取最后合法值。
3. Build 返回前 ConfigValue 已完成首次 pull。
4. ConfigValue 是 Runtime 注册到 IOC 的 borrowed bean，不注册 Config Subscriber。
5. `Load` 返回 T 浅拷贝；业务必须把嵌套引用视为不可变。

ConfigManager 仍作为高级 bean 保留，用于 Explain、诊断和动态 key。

## 6. 对象模型

| 对象 | 阶段职责 |
|---|---|
| `Application` | 保存稳定声明和声明错误 |
| `BuildInput` | 提供单次 config 输入 |
| `Runtime` | 持有并关闭本次资源 |
| `Logging` | config 后构建日志 Provider |
| `Module` | 通过 Registry 注册可复用 beans |
| `ConfigValue[T]` | 提供 IOC typed config pull 读取 |

依赖方向：

```text
app -> app-runtime -> config + log + ioc
app-log-zap -> config + log + log-zap
config-nacos/config-consul -> config/provider
```

config、log、ioc、app-log-zap 不依赖 app-runtime；应用入口通过结构化 `Logging` 接口同时装配 app-runtime 与具体日志适配器。

## 7. Build 事务

### 7.1 主流程

```text
validate Application declarations
  -> create Config Builder from Config Options + Input
  -> build ConfigManager and initial Snapshot
  -> initialize typed ConfigValues
  -> Logging.Build
  -> create IOC Container
  -> register borrowed base beans and ConfigValues
  -> register Bean Definitions
  -> execute Modules
  -> Container.Build
  -> publish Runtime
```

基础 borrowed beans：

| Bean | 说明 |
|---|---|
| `*Runtime` | composition root/诊断 |
| `*ApplicationInfo` | 稳定应用身份 |
| `config.Manager` | 高级配置能力 |
| `log.Logger` | 默认 logger |
| `log.LoggerFactory` | owner logger 工厂 |
| `*ConfigValue[T]` | typed config |

borrowed beans 不由 IOC 注入字段或调用生命周期，避免 Runtime 与 IOC 双重关闭同一资源。

### 7.2 失败矩阵

| 失败点 | 已取得资源 | 回滚 |
|---|---|---|
| Application 声明 | 无 | 直接返回 module/bean error |
| Config Build | config 内部临时资源 | config 自行回滚 |
| Logging Build | ConfigManager | config |
| 基础/ConfigValue/Bean/Module 注册 | config、logging、IOC(created) | IOC -> logging -> config |
| IOC graph/inject/Init | config、logging、IOC | IOC bean rollback -> logging -> config |

正常 error 使用 `app_runtime.build.failed` 包装并保留底层 code。ConfigManager 成功取得后的意外 panic 先回滚，再以 build error 包装后继续 panic。

## 8. Close 事务

`Runtime.Close` 使用 `sync.Once + closeDone + closeErr`：

1. 首个调用者执行 IOC -> logging -> config。
2. 每层失败仍继续下一层，最终 `errors.Join`。
3. 并发调用者等待 `closeDone`，读取同一个首次结果。
4. Close 链 panic 在边界转为 `app_runtime.close.failed`。
5. `closeDone` 使用 defer 保证关闭，扩展 panic 不会让后续调用永久阻塞。

首次 context 和结果不可替换。context 只直接约束 IOC Close；日志 Provider 和 ConfigManager 的 Close 当前不接收 context。

## 9. API 合并语义

| API | 合并规则 |
|---|---|
| `WithName` | trim 后非空值覆盖 |
| `WithConfig` | 按声明顺序追加 config.Options |
| `WithLogging` | 后声明非 nil 实现覆盖 |
| `WithBeanDefinitions` | 按声明顺序原样注册；Bean nil/typed nil fail-fast |
| `WithModules` | 按声明顺序追加；name 非空且唯一 |
| `WithIOCOptions` | 按声明顺序追加，调用方可覆盖默认 IOC logger |

Provider 名称、bootstrap schema、snapshot validator 等语义只由 config 处理，app-runtime 不再维护第二份 Provider 注册表。

## 10. 错误与观测

| ErrorCode | 产生阶段 |
|---|---|
| `app_runtime.build.failed` | config/logging/注册/IOC 构建 |
| `app_runtime.close.failed` | IOC/logging/config 关闭或 panic |
| `app_runtime.module.invalid` | Module 名称、回调、重复声明 |
| `app_runtime.bean.invalid` | nil/typed nil 简单 bean |
| `app_runtime.logging.invalid` | typed nil Logging 声明 |

关键错误包含 `op/result/stage/reason`。Config refresh 失败由 `config.OnRefreshError` 观察，不伪装成 Runtime Build 失败。

## 11. 边界不变量

1. app-runtime 不依赖 app/Cobra。
2. Runtime 只有完整 Build 成功后才发布。
3. Runtime 是 config、logging、IOC 的唯一关闭入口。
4. Config 规则只在 config 模块定义。
5. Module 只注册 bean，不定义生命周期。
6. bean 图决定 Init/Close 顺序。
7. ConfigValue 只读取已经通过 scoped validator 的 Snapshot。
8. Close 并发调用返回同一首次结果，扩展 panic 不阻塞等待者。

## 12. 备选方案与取舍

### 平铺复制 Config API

调用短，但形成两套同义入口和文档。当前选择 `WithConfig(config.Option...)`。

### 巨大 Runtime Config 结构体

零值无法区分未设置/禁用，Provider、validator、binding 的追加覆盖难组合。当前使用声明式 Options。

### Module 获得完整 Container/Runtime

灵活但允许 Register 阶段 Resolve、Build、Close 和副作用。当前只暴露 Registry。

### 热更新直接修改共享结构体

读取简单但存在数据竞争和部分更新。当前使用不可变 Snapshot + Observe pull ConfigValue。

## 13. Open Gaps

1. contextless 日志/config Close 没有硬 deadline。
2. Close 首次失败后没有分层重试。
3. ConfigValue 的嵌套引用依赖不可变约定。
4. Module 没有条件声明；未来应由独立 starter/auto-configuration 层解决。
5. Application 并发 Build 仍依赖扩展实现可重入。

## 14. File Map

1. `app-runtime/application.go`：蓝图、Module/Bean 校验和 schema 适配。
2. `app-runtime/config_binding.go`：ConfigValue、scoped validator 与 IOC binding。
3. `app-runtime/options.go`：能力聚合 API。
4. `app-runtime/model.go`：BuildInput 和 ApplicationInfo。
5. `app-runtime/logging.go`：Logging 契约。
6. `app-runtime/module.go`：Module 契约。
7. `app-runtime/runtime.go`：Build/Close 事务。
8. `app-runtime/errors.go`：错误码。
9. `config/options.go`：Config 声明式 Options/Input。
10. `ioc/registry.go`：最小注册契约。
