# App Runtime 使用指南

本文按“声明能力、提供运行输入、业务消费能力、关闭 Runtime”的顺序说明 app-runtime。设计理由与失败矩阵见 [App Runtime 架构设计](../architecture/app-runtime-design.md)。

## 1. 什么时候直接使用

无 CLI 后台任务、嵌入式入口、集成测试，以及需要组合 Config Provider、日志实现和 IOC Module 的宿主可以直接使用。普通命令行应用使用 `app.Run/app.Main`，不要重复编写生命周期代码。

## 2. 常规路径

```go
application := appruntime.New(
    appruntime.WithName("order-service"),
    appruntime.WithConfig(
        config.Defaults(defaultConfig()),
        config.File(config.FilePath("application.yaml")),
        config.Provider(nacos.New()),
        config.Validate[ApplicationConfig](validateConfig),
    ),
    appruntime.WithConfigBinding[OrderConfig]("order", validateOrder),
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
return errors.Join(runErr, runtime.Close(shutdownCtx))
```

调用方只需要识别六组声明：Name、Config、ConfigBinding、Logging、Bean Definitions、Modules。底层策略才使用 `WithIOCOptions`。

## 3. 配置能力

app-runtime 聚合 `config.Option`，仅额外提供属于 IOC 装配边界的 ConfigBinding：

```go
appruntime.WithConfig(
    config.Defaults(defaultConfig()),
    config.File(
        config.FilePath("application.yaml"),
        config.FileWatch(5*time.Second),
    ),
    config.Provider(nacos.New()),
    config.Validate[ApplicationConfig](validateConfig),
    config.OnRefreshError(reportRefreshError),
)
```

| Option | 作用 |
|---|---|
| `config.Defaults` | 最低优先级代码默认值 |
| `config.File` | 显式本地配置文件；空值恢复默认发现 |
| `config.File(FileOption...)` | 统一声明文件路径、启用状态和轮询 refresh |
| `config.Provider` | 逐项注册 Nacos、Consul 或自定义 Provider |
| `config.Validate[T]` | 发布前校验完整 typed 快照 |
| `config.SnapshotValidators` | 高级原始 Snapshot 校验 |
| `config.OnRefreshError` | 观察异步 refresh 失败 |

配置优先级仍是 `defaults < file < remote source priority < pinned`。

### 3.1 Typed config

```go
appruntime.WithConfigBinding[OrderConfig]("order", func(value *OrderConfig) []error {
    if value.Timeout <= 0 {
        return []error{errors.New("order.timeout must be positive")}
    }
    return nil
})
```

业务注入并读取：

```go
type OrderService struct {
    Config *appruntime.ConfigValue[OrderConfig]
}

func (this *OrderService) Handle(ctx context.Context) error {
    cfg := this.Config.Load()
    return handleWithTimeout(ctx, cfg.Timeout)
}
```

Build 首次发布前完成解码和校验，返回前 ConfigValue 已取得初始值。refresh 解码或校验失败时拒绝候选；发布成功后 `Load` 通过 Observe pull 最新版本。返回值是浅拷贝，业务不得修改其中共享 map/slice/pointer。

原始 `Runtime.ConfigManager()` 保留给 Explain、诊断和动态 key，不是常规业务读取入口。

## 4. 单次 Build 输入

普通调用使用 `Build(ctx)`。CLI、测试或嵌入式宿主才使用：

```go
runtime, err := application.BuildWithInput(ctx, appruntime.BuildInput{
    Config: config.Input{
        BootstrapOverrides: map[string]any{
            "nacos.address": "127.0.0.1:8848",
        },
        PinnedValues: map[string]any{
            "order.concurrency": 16,
        },
        EnvPrefix:  "ORDER_SERVICE",
        File: []config.FileOption{
            config.FileDisabled(),
        },
    },
})
```

应用顺序是 `Name 派生 env prefix < Application WithConfig options < BuildInput.Config`。BuildInput 只影响本次 Runtime，并只覆盖显式提供的 File path/watch/disabled 字段；`FileDisabled()` 是单向收紧。

## 5. 日志能力

常规使用由适配模块提供实现：

```go
appruntime.WithLogging(applogzap.New())
```

level、format、输出模式等行为继续来自统一 ConfigManager。业务可调用 `runtime.Logger("order.service")`，也可以注入 `log.Logger` 或 `log.LoggerFactory`。

自定义实现需要实现：

```go
Build(ctx context.Context, manager config.Manager) (log.Provider, error)
```

每次 Build 应返回本次 Runtime 独占的 Provider；失败前由实现清理未交付半成品。

## 6. Bean Definitions 与 Modules

简单应用直接声明：

```go
appruntime.WithBeanDefinitions(
	ioc.Define(&OrderRepository{}),
	ioc.Define(&OrderService{}, ioc.WithDeclare[OrderApplicationService]()),
)
```

跨应用复用的模块使用：

```go
func OrderModule() appruntime.Module {
    return appruntime.NewModule("order", func(registry ioc.Registry) error {
        if err := registry.Register(&OrderRepository{}); err != nil {
            return err
        }
        return registry.Register(&OrderService{})
    })
}
```

Module name 非空且唯一。Register 只声明 bean，不打开连接、启动 goroutine 或读取网络；它只获得 `ioc.Registry`，不能 Resolve/Build/Close。资源获取与释放放在 bean 的 `ioc.Initializer/Closer`。

`WithBeanDefinitions` 原样保留名称、接口声明、注入和生命周期所有权。

## 7. IOC 高级策略

```go
appruntime.WithIOCOptions(
    ioc.WithCloseTimeout(5*time.Second),
    ioc.WithAllowCircularDependency(false),
)
```

常规业务不需要该入口。循环依赖默认拒绝；Close timeout 是协作式等待边界。

## 8. Runtime 能力

```go
info := runtime.Info()
manager := runtime.ConfigManager()
logger := runtime.Logger("order.service")
container := runtime.Container()
```

Runtime 注册 `*Runtime`、`*ApplicationInfo`、`config.Manager`、`log.Logger`、`log.LoggerFactory` 和所有 `*ConfigValue[T]` 为 borrowed bean。业务对象优先依赖 ConfigValue、LoggerFactory 和领域接口，不依赖整个 Runtime 或 Container。

## 9. Build 与关闭

```text
Build: config -> typed ConfigValues -> logging -> IOC -> Bean Definitions -> Modules -> IOC Build
Close: IOC -> logging -> config
```

Build 只有全部阶段成功才返回 Runtime。失败按已取得资源逆序回滚，并聚合原始错误和清理错误。

`Runtime.Close` 使用 `sync.Once`：首个调用执行真实关闭，其他调用等待并返回首次结果。日志或 ConfigManager Close 意外 panic 会转为 `app_runtime.close.failed`，不会让后续调用永久等待。

## 10. 测试

```go
application := appruntime.New(
    appruntime.WithConfig(
        config.File(config.FileDisabled()),
        config.Defaults(testConfig()),
    ),
    appruntime.WithConfigBinding[TestConfig](""),
    appruntime.WithBeanDefinitions(ioc.Define(&TestService{})),
)

runtime, err := application.Build(context.Background())
if err != nil {
    t.Fatal(err)
}
t.Cleanup(func() {
    if closeErr := runtime.Close(context.Background()); closeErr != nil {
        t.Errorf("close runtime: %v", closeErr)
    }
})
```

建议覆盖 Config 校验拒绝、ConfigValue refresh、Module 注册失败回滚、bean Init 失败回滚、Logging 失败、Close error/panic 和重复 Build。

## 11. 常见问题

| 现象 | 原因 | 处理 |
|---|---|---|
| `Build(ctx)` 读取到工作目录文件 | config 默认发现生效 | 测试声明 `config.File(config.FileDisabled())` |
| ConfigValue 未注入 | 未声明 `WithConfigBinding[T]` 或 T 不一致 | 对齐 scope 和完整泛型类型 |
| refresh 后 ConfigValue 不变 | 解码/validator 拒绝候选 | 查看 `config.OnRefreshError` |
| Module 中无法 Resolve | Registry 刻意只允许声明 | 通过字段依赖，Build 后在 composition root Resolve |
| 后续 Close 仍返回首次错误 | Close 使用 sync.Once | 第一次 Close 使用正式 shutdown context |

## 12. 相关文档

1. [app-runtime README](../../app-runtime/README.md)
2. [App Runtime 架构设计](../architecture/app-runtime-design.md)
3. [Config 使用指南](config-usage.md)
4. [IOC 使用指南](ioc-usage.md)
5. [App 使用指南](app-usage.md)
