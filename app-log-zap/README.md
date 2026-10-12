# app-log-zap

`app-log-zap` 是 `config.Manager` 与纯 `log-zap.Provider` 之间的装配适配器。它拥有日志配置订阅、把启动配置与运行期窄配置分离，并通过 Go 结构化接口接入 `app-runtime`，但模块本身不依赖 `app-runtime`。

## 设计范围与目标

### In Scope

1. 提供与 `app-runtime.Logging` 结构化兼容的 `Build` 方法，从 `config.Manager` 读取日志配置。
2. 默认从 `log.zap` scope 构建 `log-zap.Provider`。
3. 独占 ConfigManager subscription，并只把运行期 level 更新传给纯 Provider。
4. 将 subscription 与 Zap 核心组合成一个可关闭的 `log.Provider`。
5. 在 `global` 子包提供拥有 ConfigManager、组合 Provider 与关闭事务的零配置入口。

### Out of Scope

1. 不定义日志契约、Zap 启动配置和 `RuntimeConfig` 校验；分别由 `log`、`log-zap` 负责。
2. 根包不创建或关闭 `config.Manager`；只有显式使用 `app-log-zap/global` 时由 global 拥有 Manager。
3. 不依赖 `app-runtime`，也不返回 `app-runtime.Option`。
4. 不管理 app 的 signal、shutdown deadline 或退出码。

## 核心对象与职责

| 对象 | 职责 |
|---|---|
| `Bootstrap` | 保存日志 scope，并在 Runtime Build 阶段创建 provider |
| `runtimeProvider` | 组合 Zap 核心、subscription 与幂等关闭状态 |
| `runtimeConfigBridge` | 原子衔接初次配置回放、核心创建和后续 level 更新 |
| `WithScope` | 覆盖默认 `log.zap` scope |
| `WithProviderOptions` | 转发 ContextExtractor、Redactor 等 Provider 构造期策略 |
| `New` | 构建结构化兼容 `app-runtime.Logging` 的 Bootstrap |
| `NewProviderFromManager` | 不使用 app-runtime 时直接创建组合 Provider |
| `global` | 提供 `Get/Logger/Provider/Close` 零配置入口 |

## 最小使用示例

```go
application := appruntime.New(
	appruntime.WithName("order"),
	appruntime.WithLogging(applogzap.New(
		applogzap.WithProviderOptions(
			logzap.WithRedactor(log.NewKeyRedactor("password", "token")),
		),
	)),
)

runtime, err := application.Build(ctx)
if err != nil {
	return err
}
runErr := run(ctx, runtime.Logger("order.service"))
closeErr := runtime.Close(context.Background())
return errors.Join(runErr, closeErr)
```

默认配置位置：

```yaml
log:
  zap:
    level: info
    mode: console
    format: json
```

## 公共入口与核心契约

1. `New(options...)` 返回默认 scope 为 `log.zap` 的 Bootstrap；nil option 被忽略。
2. `WithScope(scope)` 会 trim 输入；空值不覆盖当前 scope。
3. `WithProviderOptions` 按声明顺序转发构造期策略；调用方不得在 Build 并发执行期间修改 option 捕获状态。
4. `Bootstrap.Build(ctx, manager)` 返回 `log.Provider`：manager 非 nil 时创建组合 Provider，manager 为 nil 时创建使用默认配置的纯 Provider。
5. `NewProviderFromManager(ctx, manager, scope, options...)` 直接返回同时拥有 subscription 与 Zap 核心的 Provider；manager 必填，空 scope 回退 `log.zap`。
6. context 取消只停止 subscription，不提前关闭 Zap 核心；核心必须由 Provider `Close` 确定性释放。
7. 直接构建时调用方拥有返回 Provider；通过 Runtime 装配时由 Runtime 拥有并关闭它。

## 配置模型与默认值

本模块没有独立配置结构，完整复用 `logzap.Config`：

1. 默认 scope 为 `log.zap`；使用 `log` 等其他 scope 时必须显式 `WithScope("log")`。
2. provider 默认 `level=info`、`mode=console`、`format=json`、caller 开启。
3. 初次回放使用完整 `logzap.Config` 创建核心；后续只投递 `logzap.RuntimeConfig{Level: ...}`。
4. mode、format、caller、轮转、ContextExtractor 和 Redactor 保持启动期值。
5. manager 为 nil 时使用 `logzap.Config{}` 的标准化默认值，不注册 subscription。

## 错误语义

根包稳定错误码：

- `app_log_zap.bootstrap.build_failed`
- `app_log_zap.provider.subscribe_failed`
- `app_log_zap.provider.core_build_failed`
- `app_log_zap.provider.runtime_config_apply_failed`
- `app_log_zap.provider.close_failed`

`global` 子包稳定错误码：

- `app_log_zap.global.manager_build_failed`
- `app_log_zap.global.provider_build_failed`
- `app_log_zap.global.fallback_build_failed`
- `app_log_zap.global.close_failed`

所有错误按场景包含 `op/result/stage/scope/reason` 等诊断上下文，并保留 config/log-zap cause；调用方应使用 `errcode.IsCode` 同时识别适配阶段和底层根因。

## 运行时语义

1. Logging.Build 发生在 ConfigManager 构建成功之后、IOC Build 之前。
2. 创建时先订阅并同步接收当前版本，再在通知串行区内创建核心，避免读取 Snapshot 与订阅之间丢失更新。
3. Build context 取消会停止 subscription，但已有 Logger 继续输出；Provider Close 才关闭 Zap 核心。
4. Provider Close 先取消 subscription，再关闭核心；不会越权关闭调用方传入的 Manager。
5. Runtime 正常关闭顺序是 IOC、日志 Provider、ConfigManager，保证组件关闭阶段仍可记录日志。
6. Provider Close 后旧 Logger 保持可调用但不再输出。

## 最佳实践摘要

1. 普通应用使用 `appruntime.WithLogging(applogzap.New())` 或 `app.WithLogging(applogzap.New())`；装配发生在应用入口。
2. 配置统一使用默认 `log.zap`，只有既有配置确实使用其他 scope 时才覆盖。
3. 不使用 app-runtime 时调用 `NewProviderFromManager`，并显式 Close 返回的 Provider 与调用方拥有的 Manager。
4. 业务只通过 Runtime 或 `log.Logger` 记录日志，不依赖本适配器。
5. 使用 `errcode.IsCode` 分支并保留 Runtime Build/Close 的完整错误链。
6. 需要自动链路字段或脱敏时，通过 `WithProviderOptions` 在 Build 前固定策略。

## Open Gaps / Future Improvements

1. Provider options 属于构造期静态策略；运行期不能替换 ContextExtractor 或 Redactor。
2. context 取消只停止 subscription，不中断已进入的同步 Provider 创建。
3. 当前没有独立装配指标；资源状态通过 ConfigManager subscription count 与错误码观察。

## 破坏性迁移

1. `AsRuntimeOption(options...)` 已删除，迁移为 `appruntime.WithLogging(applogzap.New(options...))`。
2. `log-zap/bootstrap.NewProviderFromManager` 已迁移为 `applogzap.NewProviderFromManager(ctx, manager, scope, options...)`。
3. `log-zap/global` 已迁移为 `app-log-zap/global`。
4. `logzap.WithWatcher` 已删除；Manager 驱动场景使用 app-log-zap，其他宿主直接调用 `Provider.ApplyRuntimeConfig`。

## File Map

1. `bootstrap.go`：scope/provider options 与结构化 Build 契约。
2. `provider.go`：subscription、配置桥接、context 取消与组合关闭。
3. `errors.go`：根包稳定错误码与 cause 包装。
4. `global/`：零配置入口、Manager/Provider 所有权与全局错误码。
5. `bootstrap_test.go`：配置回放、热更新、资源关闭和分层错误码场景。
6. `doc.go`：包级职责说明。

## 相关文档

1. [App 使用指南](../docs/guides/app-usage.md)
2. [Log 使用指南](../docs/guides/log-usage.md)
3. [Log 架构设计](../docs/architecture/log-design.md)
4. [app-runtime README](../app-runtime/README.md)
5. [log-zap README](../log-zap/README.md)
