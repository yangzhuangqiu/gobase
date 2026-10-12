# log-zap

`log-zap` 是 `gobase/log` 的 Zap provider。它向业务稳定提供 `LoggerFactory`，内部以标准 `slog.Handler` 连接 Zap 编码、输出、轮转与动态 level，并负责资源关闭。

## 设计范围与目标

### In Scope

1. JSON/text 编码与 console/file 输出。
2. lumberjack 文件轮转。
3. `slog.Handler` 的 level、Attr、Group、error 与 source 映射。
4. 通过窄 `RuntimeConfig` 显式应用动态 level。
5. Sync 与 writer Close。
6. Provider 构造期注入 ContextExtractor 与 Redactor。

### Out of Scope

1. 不创建或缓存业务 Logger。
2. 不解析 owner/name。
3. 不读取字符串 context key。
4. 不依赖或订阅 `config.Manager`，不拥有配置来源生命周期。
5. 不提供 Fatal 或进程退出。

## 核心对象

| 对象 | 职责 |
|---|---|
| `Provider` | 持有稳定 LoggerFactory、runtime、AtomicLevel 与 writer 生命周期 |
| `handler` | 实现 `slog.Handler` 并映射 Record 到 Zap core |
| `Config` | 创建输出核心所需的完整启动配置 |
| `RuntimeConfig` | 只包含无需重建输出资源即可应用的运行期字段 |

## 公共入口

```go
provider, err := logzap.NewProvider(logzap.Config{
    Level:  "info",
    Mode:   logzap.ModeConsole,
    Format: logzap.FormatJSON,
})
if err != nil {
    return err
}
defer provider.Close()

logger := provider.Factory().Get("order.service")
logger.Info(ctx, "service started",
    log.Op("service.start"),
    log.Result(log.ResultOK),
)
```

## 配置模型与默认值

| 字段 | 默认值 | 约束 |
|---|---|---|
| `level` | `info` | debug/info/notice/warn/warning/error；`trace` 是 Debug 兼容别名 |
| `mode` | `console` | console/file |
| `format` | `json` | json/text |
| `disableCaller` | `false` | source 由根 Logger Record.PC 提供 |
| `rotate.maxSize` | `10` | MB |
| `rotate.maxBackups` | `50` | 历史文件数 |
| `rotate.maxAge` | `3` | 天 |
| `rotate.compress` | `false` | 压缩历史文件 |
| `rotate.localTime` | `false` | 使用本地轮转时间 |

file 模式必须提供 `rotate.filename`。caller skip、context keys 和 name resolver 已从配置删除。

## Handler 语义

1. `trace`/`debug` 配置均选择 Zap Debug；Info 映射 Info，Warn 映射 Warn，Error 及以上映射 Error。
2. `WithAttrs/WithGroup` 返回不可变 Handler。
3. 同层同名 Group 在写出前合并，避免 JSON 重复键覆盖。
4. error 使用 `zap.NamedError`；嵌套 Group 使用 ObjectMarshaler。
5. `Record.PC` 映射为 Zap caller；`disableCaller=true` 时省略。
6. Provider 关闭后 Handler disabled，Handle 返回 nil。
7. 写入通过 `Core.Check` 后再转换属性并调用 `CheckedEntry.Write`，保留 Zap sampling/core hook 语义。

`Provider.Factory()` 在构造成功后始终返回同一非 nil 工厂。Provider 关闭后，关闭前取得的 Factory/Logger 仍可安全调用，但不会继续输出。

## 动态配置

```go
err := provider.ApplyRuntimeConfig(logzap.RuntimeConfig{Level: "debug"})
```

`ApplyRuntimeConfig` 只更新 level，并立即影响已有 Logger。空 level 表示保持当前值；非法 level 返回 `log.level.invalid` 且不污染现有状态。mode、format、caller 与 rotate 仍需重建 Provider。Provider 已关闭时调用返回 nil，不会重新激活核心。

`Provider.Core()` 为框架适配模块提供稳定的借用 Zap Core：每次写入读取当前 Provider 快照，共享动态 level、encoder、writer 和关闭状态。该 Core 的 `Sync` 是 no-op，不转移底层资源所有权；普通业务仍应只依赖 `Provider.Factory()`。

配置订阅、变化过滤和生命周期取消由宿主负责；使用 `config.Manager` 时推荐通过 `applogzap.NewProviderFromManager` 装配。

脱敏策略在构造期固定：

```go
provider, err := logzap.NewProvider(cfg,
    logzap.WithRedactor(log.NewKeyRedactor("password", "token", "auth.secret")),
)
```

## 错误语义

稳定错误码：

- `log.runtime.internal`
- `log.mode.invalid`
- `log.format.invalid`
- `log.file.filename_required`
- `log.level.invalid`

构建失败不返回半初始化 Provider；Close 使用 `errors.Join` 保留 Sync 与 writer close 错误。

## 运行时与资源所有权

1. Provider 创建方负责 Close。
2. 所有业务 Logger 停止写入后再 Close。
3. 宿主调用 `ApplyRuntimeConfig` 后，共享 AtomicLevel 立即影响已有 Logger。
4. Provider 拥有 Factory；Factory/Handler 不单独关闭资源。
5. console 输出不持有 writer closer；file 输出由 Provider 关闭 lumberjack。

## Open Gaps

1. 单次写入错误不会反馈到业务 Logger。
2. 纯 Provider 并发 Close 不缓存第一次错误结果；需要组合生命周期时由宿主包装幂等关闭。
3. 运行期仅支持 level 热更新。
4. 配置订阅与全局入口不属于本模块；分别由 `app-log-zap` 与 `app-log-zap/global` 提供。

## 破坏性迁移

1. `RuntimeConfigWatcher` 与 `WithWatcher` 已删除。自有配置来源的宿主改为调用 `ApplyRuntimeConfig`；ConfigManager 场景改用 `app-log-zap`。
2. `log-zap/bootstrap` 已删除，入口迁移到 `applogzap.NewProviderFromManager(ctx, manager, scope, options...)`。
3. `log-zap/global` 已迁移到 `app-log-zap/global`。

## File Map

- `handler.go`：slog Handler、Attr/Group/caller 映射。
- `core.go`：供框架适配层借用的动态 Zap Core。
- `provider.go`：runtime、显式动态 level 与 Close。
- `config.go`：默认值、标准化和校验。
- `provider_options.go`：ContextExtractor 与 Redactor option。
- `zap_builder.go`：encoder、writer 与轮转。
- `errors.go`：稳定配置错误码。
