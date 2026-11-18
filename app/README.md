# app

`app` 是不泄漏 Cobra 的命令行应用框架。它负责命令、flags、help/version 和执行编排，并通过 app-runtime 自动构建/关闭 config、logging、ioc Runtime。

## 设计范围与目标

### In Scope

1. root Workload、一次性 Runner 包装器、子命令和只有子命令的命令目录。
2. 公共 Command/Flags 模型与 Cobra 隔离。
3. 配置覆盖 flags、bootstrap flags 和普通业务 flags。
4. `--version`、help、bootstrap schema 的 Runtime 前短路。
5. Runtime Build -> BeforeRunHook -> Workload 收敛 -> Resource Close 的应用生命周期。
6. `Run/Main` 的默认退出信号与 `ExecuteContext` 的宿主取消协调。
7. Name/Config/Logging/Bean Definitions/Modules 的常规 app-runtime 门面。

### Out of Scope

1. config、log、ioc 的底层实现和完整高级 API。
2. HTTP、gRPC、Consumer 等具体协议实现；app 只定义统一 Workload 语义。
3. 把 Runtime/Container 传播到普通业务层。

## 最小使用

```go
func main() {
    app.Main(
        app.WithName("order-service"),
        app.WithConfig(
            config.Defaults(defaultConfig()),
        ),
        app.WithConfigBinding[OrderConfig]("order", validateOrder),
        app.WithLogging(applogzap.New()),
        app.WithBeanDefinitions(
            ioc.Define(&OrderRepository{}),
            ioc.Define(&OrderService{}),
        ),
        app.WithWorkloads(app.Runner(func(ctx context.Context, runtime app.Runtime) error {
            service, err := ioc.ResolveAs[*OrderService](runtime.Container())
            if err != nil {
                return err
            }
            return service.Run(ctx)
        })),
    )
}
```

业务对象通过字段依赖 `*appruntime.ConfigValue[T]`、Logger 和领域对象，不需要依赖 app.Runtime。

## 核心入口

| API | 作用 |
|---|---|
| `Run/Main` | 创建并执行应用；Main 负责错误输出和退出码 |
| `WithName/Description/Version` | 命令身份和帮助输出 |
| `WithConfig(config.Option...)` | 聚合配置能力，不复制 config API |
| `WithConfigBinding[T]` | 声明 app-runtime typed IOC 配置视图 |
| `WithLogging` | 选择日志实现 |
| `WithBeanDefinitions` | 注册携带接口、名称和生命周期策略的完整 bean definitions |
| `WithModules` | 注册可复用 Modules |
| `WithIOCOptions` | 高级 IOC 策略 |
| `Runner` | 把一次性 root 函数包装为固定名称 `runner` 的 Workload |
| `WithWorkloads` | 声明一个或多个由 app 监督的主动工作负载 |
| `WithCommands` | 子命令声明 |
| `WithFlagBinder` | root 业务 flags |
| `WithBeforeRunHook` | Runtime Build 后、Workload 前的可回滚准备 |
| `WithShutdownPolicy` | 统一声明信号、Workload graceful 与 Runtime Close 预算 |
| `WithShutdownHook` | 顶层高级关闭逃生口 |
| `WithRuntime` | 完全接管 app-runtime 蓝图的高级入口 |

## Root 默认策略

1. 有 Root Workload：root 是真实业务命令；一次性任务由 `Runner` 包装成 Workload。
2. 没有 Workload、有 Commands：root 是命令目录，执行时展示 help。
3. Workload、Commands 都没有：执行 root 返回 runner_required。

## 配置、日志和 IOC

配置声明：

```go
app.WithConfig(
    config.Defaults(defaultConfig()),
    config.File(config.FilePath("application.yaml")),
    config.Provider(nacos.New()),
    config.Validate[ApplicationConfig](validateConfig),
)
app.WithConfigBinding[OrderConfig]("order", validateOrder)
```

日志声明：

```go
app.WithLogging(applogzap.New())
```

IOC 声明：

```go
app.WithBeanDefinitions(
    ioc.Define(&Repository{}),
    ioc.Define(&Service{}),
)
app.WithModules(database.Module(), order.Module())
```

完整配置值继续来自统一文件/env/CLI。`--config` 选择文件，`--bootstrap` 覆盖 Provider 启动输入，Config flags/`--override` 进入 pinned 层。

## Runtime 语义

```text
parse/complete/validate flags
  -> Build Runtime
  -> BeforeRunHook
  -> Workload.Run（一次性函数由 Runner 包装）
  -> Workload.Shutdown + wait all Run（高级 ShutdownHook 可并发加入）
  -> Close Runtime / IOC Resource
```

`Workload` 是主动运行单元，任一 `Run` 返回都会触发全组 `Shutdown`；app 必须等每个 `Shutdown` 和 `Run` 都返回后，才允许 IOC 逆序关闭实现 `Resource`（即 `ioc.Closer`）的被动资源。`ShutdownHook` 仅用于无法建模为 Workload/Resource 的遗留或跨系统关闭动作。

`Run/Main` 默认监听 Unix 的 Interrupt+SIGTERM、Windows 的 Interrupt，并把实际信号身份保留在执行 context 的取消 cause 中；`ExecuteContext` 不安装进程信号，只响应宿主 context。默认 `ShutdownPolicy` 为 Workload/高级 Hook 共享 15 秒 graceful deadline，Runtime Close 使用独立 10 秒协作式 deadline。

两个时间预算可由 ConfigManager 启动快照按字段补齐；Signals 始终由代码声明。优先级固定为“显式 `WithShutdownPolicy` > ConfigManager > 内建默认”，并在 Workload 启动前冻结，不订阅热更新：

```yaml
app:
  shutdown:
    workloadGracePeriod: 20s
    runtimeCloseGracePeriod: 10s
```

`RuntimeModeDisabled` 子命令不构建 Runtime，也不执行 BeforeRunHooks；应用 Shutdown Hooks 默认仍执行并收到 nil Runtime，可通过 `Command.DisableShutdownHooks` 关闭。`--version` 和 bootstrap schema 输出在任何 Runtime 构建前短路。

## 最佳实践

1. 常规应用只通过 app 配置能力，不手工 Build/Close Runtime。
2. 配置选项使用 config 模块词汇；不要在 app 创建同义包装。
3. 对象使用 Bean Definitions；基础设施模块提供 Definition 工厂，可复用对象集合使用 Modules。
4. Workload/Runner 包装函数是 composition root；普通业务对象只依赖字段注入。
5. 业务 flags 与配置覆盖 flags 分开声明。
6. 不需要配置/log/IOC 的命令使用 `RuntimeModeDisabled`。
7. HTTP、gRPC、Consumer 等主动组件优先实现 `Workload`；连接池、Client 等被动资源交给 IOC 并实现 `Resource`。
8. 常规代码不要注册 `ShutdownHook`；只有无法迁移的外部回调或跨系统补偿才使用该高级逃生口。

## Open Gaps / Future Improvements

1. 子命令不支持无限递归树模型。
2. bootstrap schema 仍通过 Runtime Application 适配接口发现。
3. app 尚未提供内建 lifecycle metrics/tracing；当前通过稳定 stage 错误与业务 Hook 观测。

## File Map

1. `app.go`、`launcher.go`：App 实例与包级执行入口。
2. `options.go`、`runtime_facade.go`：App Option 与只读 Runtime 能力聚合门面。
3. `command.go`、`runner.go`：Command/RuntimeMode 模型与一次性 Runner 包装器。
4. `command_builder.go`、`command_execution.go`：私有 Cobra 命令树构建与命令事务。
5. `command_context.go`、`command_flags.go`：公共命令上下文与 Flags 能力。
6. `flag_binder.go`、`flag_completer.go`、`flag_validator.go`、`flag_metadata.go`：业务 Flag 声明、规则与内部分类。
7. `runtime_input.go`、`runtime_input_bootstrap.go`、`runtime_input_pinned.go`、`runtime_input_override.go`：CLI 到 `BuildInput.Config` 的分阶段转换。
8. `workload.go`、`workload_execution.go`、`resource.go`：主动工作负载监督与被动资源语义。
9. `shutdown_policy.go`、`shutdown_policy_config.go`、`shutdown_hook.go`、`shutdown.go`、`shutdown_execution.go`：统一关闭策略、启动快照归并与高级 Hook 协调。
10. `process_signal.go`、`execution_session.go`、`execution_binding.go`：进程信号身份、订阅恢复、单次执行状态与缓存命令绑定。
11. `command_bootstrap_schema.go`、`help.go`：schema 与 help 输出。
12. `errors.go`：稳定错误码与生命周期 stage 包装。

## 相关文档

1. [App 使用指南](../docs/guides/app-usage.md)
2. [App 架构设计](../docs/architecture/app-design.md)
3. [App 生命周期管理](../docs/architecture/app-lifecycle-design.md)
4. [App Runtime 使用指南](../docs/guides/app-runtime-usage.md)
5. [quickstart-app](../examples/quickstart-app/README.md)
