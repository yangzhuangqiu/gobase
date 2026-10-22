# app-runtime

`app-runtime` 是与 CLI 无关的应用运行时内核：按 `config -> logging -> ioc` 构建一次 Runtime，并在失败或退出时按相反顺序收敛资源。

普通命令行应用优先使用 `app`；无 CLI 后台任务、嵌入式入口、集成测试和运行时适配模块可直接使用本模块。

## 设计范围与目标

### In Scope

1. 用可复用 `Application` 蓝图构建独立 Runtime。
2. 通过 `WithConfig` 聚合 config 模块声明，不复制配置语义。
3. 在配置首次发布后构建一个日志 Provider。
4. 通过完整 Bean Definitions 和可复用 Modules 声明 IOC 对象。
5. 统一 Build 回滚、并发幂等 Close 和跨层资源所有权。
6. 通过 `WithConfigBinding[T]` 把 `ConfigValue[T]` 作为 borrowed bean 提供给业务对象。

### Out of Scope

1. 命令、flags、help、version、signal 和退出码。
2. 包扫描、注解、隐式 Module 发现和条件自动配置。
3. Module 自有 Start/Stop；bean 依赖图是生命周期唯一事实源。
4. 重复实现 config、log、ioc 的底层 API。

## 核心对象与职责

| 对象 | 职责 |
|---|---|
| `Application` | 保存可重复 Build 的稳定能力声明 |
| `BuildInput` | 保存一次运行才确定的 `config.Input` |
| `Runtime` | 拥有 ConfigManager、日志 Provider 和 IOC Container |
| `Logging` | 基于已发布 ConfigManager 构建日志 Provider |
| `Module` | 通过最小 `ioc.Registry` 声明可复用 bean 集合 |
| `ConfigValue[T]` | 基于 config Observe 向业务提供 typed config pull 视图 |

## 最小使用示例

```go
application := appruntime.New(
    appruntime.WithName("order-service"),
    appruntime.WithConfig(
        config.File(config.FileDisabled()),
        config.Defaults(defaultConfig()),
    ),
    appruntime.WithConfigBinding[OrderConfig]("order", validateOrderConfig),
    appruntime.WithLogging(applogzap.New()),
    appruntime.WithBeanDefinitions(
        ioc.Define(&OrderRepository{}),
        ioc.Define(&OrderService{}),
    ),
)

runtime, err := application.Build(ctx)
if err != nil {
    return err
}

runErr := run(ctx, runtime)
shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
closeErr := runtime.Close(shutdownCtx)
return errors.Join(runErr, closeErr)
```

业务对象通过字段依赖使用能力：

```go
type OrderService struct {
    Config *appruntime.ConfigValue[OrderConfig]
    Logger log.Logger
    Repo   *OrderRepository
}

func (this *OrderService) Create(ctx context.Context) error {
    cfg := this.Config.Load()
    this.Logger.Info(ctx, "order creation started",
        log.F("op", "order.create"),
        log.F("result", "running"),
        log.F("timeout", cfg.Timeout),
    )
    return this.Repo.Create(ctx, cfg.Timeout)
}
```

## 公共入口与契约

| API | 场景 | 语义 |
|---|---|---|
| `New(options...)` | 创建蓝图 | 不获取文件、连接、logger 或 bean 资源 |
| `WithName` | 应用身份 | 设置 ApplicationInfo，并作为 env prefix 默认来源 |
| `WithConfig(config.Option...)` | 配置能力 | 保存 config 唯一事实源声明，每次 Build 重放 |
| `WithConfigBinding[T]` | typed IOC 配置 | 发布前校验 scope，每次 Build 创建独立 ConfigValue |
| `WithLogging(Logging)` | 日志能力 | config 后创建本次 Runtime 独占 Provider |
| `WithBeanDefinitions(...)` | 完整 IOC | 原样注册 Bean、接口、名称、注入和生命周期策略 |
| `WithModules(...)` | 模块 IOC | 按声明顺序执行具名 Module.Register |
| `WithIOCOptions(...)` | 高级 IOC | 调整循环、关闭和 logger 等底层策略 |
| `Application.Build(ctx)` | 常规构建 | 无单次覆盖的极简入口 |
| `Application.BuildWithInput(ctx, input)` | 高级构建 | 合并一次 `config.Input`，不修改 Application |
| `Runtime.Close(ctx)` | 退出 | IOC -> logging -> config，所有调用返回首次结果 |

`Application.BootstrapSchemas` 仅供 CLI 适配层在 Build 前发现 Provider schema；普通业务不需要调用。

## 配置模型与默认值

1. Application name 默认 `app`。
2. 未声明 config Option 时，config 保持默认文件发现。
3. `WithConfig` 可以多次调用，Options 按 Application 声明顺序追加。
4. Build 时先应用由 Name 派生的 env prefix，再应用稳定 Options，最后应用 `BuildInput.Config`；因此单次输入优先。
5. `config.File(config.FileDisabled())` 是单向收紧；单次输入不能重新开启已禁用的文件能力。
6. 未声明 Logging 时使用 log 标准回退，不产生需关闭 Provider。
7. 未声明 Bean Definitions/Modules 时仍创建 IOC，并注册 Runtime 基础对象。

## 错误语义

| ErrorCode | 语义 |
|---|---|
| `app_runtime.build.failed` | config、logging、bean/module 注册或 IOC Build 失败 |
| `app_runtime.close.failed` | IOC、logging、config 关闭失败或关闭扩展 panic |
| `app_runtime.module.invalid` | Module 名称/回调无效或名称重复 |
| `app_runtime.bean.invalid` | WithBeanDefinitions 收到 nil 或 typed nil Bean |
| `app_runtime.logging.invalid` | WithLogging 收到 typed nil 实现 |

Build 正常失败返回 error 并逆序回滚。ConfigManager 成功取得后的意外 Build panic 会在回滚后继续 panic；Close 链 panic 转为 `close.failed`，确保并发等待者不会永久阻塞。

## 运行时语义

1. Build：config -> typed ConfigValues -> logging -> IOC -> Bean Definitions -> Modules -> IOC Build。
2. Close：IOC -> logging -> config；每层失败后仍继续下一层。
3. Bean Definitions/Modules 只声明对象，连接和 goroutine 由 bean `Initializer/Closer` 管理。
4. Module 只获得 `ioc.Registry`，不能提前 Resolve、Build 或 Close Container。
5. Runtime、ApplicationInfo、ConfigManager、Logger、LoggerFactory、ConfigValues 都是 borrowed bean。
6. `ConfigValue[T]` 通过 Observe pull 最新合法 Snapshot；业务把 Load 结果视为不可变。
7. Runtime Close 使用 `sync.Once`，后续调用等待并返回首次关闭结果。

## 最佳实践

1. 普通路径使用 `Build(ctx)`，只有 CLI/测试/宿主适配才使用 `BuildWithInput`。
2. 配置来源和发布规则写成 `config.Option`；只有 IOC typed binding 使用 `WithConfigBinding[T]`。
3. 新代码使用 `WithBeanDefinitions`；基础设施模块优先提供 Definition 工厂，避免 composition root 重复 `WithDeclare`。
4. 业务对象依赖 ConfigValue、Logger 和领域接口，不依赖整个 Runtime 或 Container。
5. Module.Register 只注册对象，不读取网络、不启动任务、不 Resolve。
6. 第一次 Close 使用覆盖完整关闭窗口的 context；后续 context 不会替换它。

## Open Gaps / Future Improvements

1. Close context 不能约束 contextless 的日志 Provider 和 ConfigManager Close。
2. 首次 Close 失败后不提供分层重试。
3. `ConfigValue[T].Load` 返回浅拷贝，包含 map/slice/pointer 的 T 仍需遵守不可变约束。
4. Application 的并发 Build 能力仍取决于 Provider、Logging 和 Module 实现。
5. Runtime 阶段事件和 actuator 风格端点由独立观察模块提供。

## File Map

1. `application.go`：Application 蓝图、Module/Bean/Definition 声明校验和 schema 适配。
2. `config_binding.go`：typed ConfigValue、scope validator 与 IOC binding 声明。
3. `options.go`：Name/Config/Logging/Bean Definitions/Modules/IOC 聚合入口。
4. `model.go`：BuildInput 与 ApplicationInfo。
5. `logging.go`：日志 Provider 构建契约。
6. `module.go`：最小 Registry 模块契约。
7. `runtime.go`：Build、基础 bean、回滚和并发 Close。
8. `errors.go`：稳定错误码。
9. `runtime_test.go`：顺序、回滚、typed config、单次输入和 Close 边界。

## 文档与示例

1. [App Runtime 使用指南](../docs/guides/app-runtime-usage.md)
2. [App Runtime 架构设计](../docs/architecture/app-runtime-design.md)
3. [App 使用指南](../docs/guides/app-usage.md)
4. [Config 使用指南](../docs/guides/config-usage.md)
5. [quickstart-app](../examples/quickstart-app/README.md)
