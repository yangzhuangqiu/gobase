# log

`log` 是业务日志门面：对业务提供稳定的 `Logger/Level/Field/LoggerFactory/Provider`，内部以标准 `log/slog` 事件模型连接具体实现。

## 设计范围与目标

### In Scope

1. 提供 Context-first 的结构化日志契约。
2. 统一 logger 名称、caller、字段与 provider 降级语义。
3. Provider 稳定持有 LoggerFactory；以 `slog.Handler` 作为 provider 作者的底层 SPI。
4. 通过 typed `ContextExtractor` 在统一边界补充调用链字段。
5. provider 缺失时提供 stderr text 降级输出。
6. 在 provider 之前统一执行字段脱敏、保留键保护与重复键归一化。

### Out of Scope

1. 不负责 provider 配置发现、文件轮转或动态更新。
2. 不提供全局 provider 注册与自动发现。
3. 不提供 Fatal、进程退出或可靠日志投递。
4. 不模拟 ThreadLocal MDC，也不按字符串 key 读取 context。

## 核心对象与职责

| 对象 | 职责 |
|---|---|
| `Logger` | 显式 level、Context-first、结构化 Field 与不修改父对象的 `With` |
| `Level` | Debug/Info/Warn/Error 跨 provider 稳定级别 |
| `Field` | 业务结构化字段；`F/Err/Group` 是最小构造入口 |
| `LoggerFactory` | 显式名称归一化、固定 `logger` 字段与降级 |
| `Provider` | 稳定提供 `LoggerFactory`，并关闭底层实现资源 |
| `ContextExtractor` | 使用 typed context key 提取白名单字段 |
| `Redactor` | 按完整字段路径返回安全输出值；实现必须并发安全 |
| `ResultOK/ResultFailed/ResultDegraded/ResultSkipped/ResultRunning` | 提供 `result` 的预定义标准字符串值 |

## 公共入口与契约

```go
provider, err := logzap.NewProvider(logzap.Config{})
if err != nil {
    return err
}
defer provider.Close()

factory := provider.Factory()
logger := factory.Get("order.service")
logger.Info(ctx, "order accepted",
    log.Op("order.accept"),
    log.Result(log.ResultOK),
    log.F("order_id", orderID),
)
```

失败事件：

```go
logger.Error(ctx, "order persistence failed",
    log.Op("order.persist"),
    log.Result(log.ResultFailed),
    log.Reason("repository_error"),
    log.Err(err),
)
```

`LoggerFactory.Get` 只接受稳定、低基数字符串名称。空白名称归一化为 `root`；request/user/order ID 必须作为 Field，不能作为 logger 名称。

## 配置模型与默认行为

根模块没有配置结构。provider 作者调用 `NewLoggerFactory(nil)`，或应用没有配置 Provider 时，使用固定降级语义：

1. stderr text 输出；
2. 最低级别 INFO；
3. 保留 source；
4. 固定 `logger` 字段；
5. 不持有外部资源。

具体 Provider 在构造期装配 typed extractor。例如 Zap Provider：

```go
provider, err := logzap.NewProvider(config,
    logzap.WithContextExtractors(func(ctx context.Context) []log.Field {
        requestID, ok := requestid.FromContext(ctx)
        if !ok {
            return nil
        }
        return []log.Field{log.F("request_id", requestID)}
    }),
)
factory := provider.Factory()
```

字段脱敏在 Provider 构造期注册，并覆盖 `With`、单次 Field、Group 与 ContextExtractor：

```go
provider, err := logzap.NewProvider(config,
    logzap.WithRedactor(log.NewKeyRedactor("password", "token", "auth.secret")),
)
```

脱敏器只处理结构化字段，不扫描 message；敏感值不得拼入 message。内建键脱敏器按叶子键或 dotted 完整路径匹配，命中标量或 Group 时均整体替换为 `[REDACTED]`。固定字段在 `Get/With` 阶段处理，单次事件与 ContextExtractor 字段在 level 启用后处理。

## 错误语义

1. Logger 方法不返回写入错误；provider 构建和 Close 是错误边界。
2. `Err(nil)` 返回零值 Field并被忽略。
3. Logger 不 panic、不退出进程；业务失败必须返回 error 给生命周期层。
4. Provider Close 错误由其创建方保留并参与应用关闭结果。

## 运行时语义

1. Logger 先执行 `Enabled`，禁用事件不转换 Field。
2. 根实现通过 `slog.Record.PC` 保存业务调用位置。
3. `With` 立即固化字段结构，并返回不修改父对象的派生 Logger；引用类型 `Field.Value` 不深复制。
4. factory 不缓存 Logger；同一名称只保证相同行为，不保证实例身份。
5. ContextExtractor 仅对已启用事件执行；调用方负责 typed key 与敏感信息边界。
6. Provider 拥有稳定 factory；重复 `Factory()` 返回同一实例，`Close` 后既有 factory/Logger 保持可调用但不再输出。
7. 顶层 `ts/level/msg/caller/func/name/stacktrace` 是系统 envelope 保留键，业务同名字段被忽略；同层标量重复键保留最早值。
8. 同层字段来源优先级为 `Factory/With > 单次事件 > ContextExtractor`；同名 Group 按声明顺序合并后对子字段应用同一 first-wins 规则。

## 最佳实践

1. message 使用稳定、简短的人类可读描述；检索维度使用 Field。
2. 关键事件使用 `Op/Result`，失败事件增加稳定 `Reason` 与 `Err(err)`；优先使用标准 result 常量，扩展值必须由跨模块 schema 明确定义。
3. 高成本字段先调用 `Enabled`。
4. 固定组件字段使用 `With`，请求字段使用单次 Field 或 ContextExtractor。
5. 不记录 token、密码、完整凭据或未经审查的请求载荷。

需要生成纯文本 message 时，由调用方显式使用 `fmt.Sprintf`：

```go
logger.Info(ctx, fmt.Sprintf("worker %s started", workerName))
```

业务变量需要检索时仍应使用 Field。Debug 等高成本格式化必须先调用 `Enabled`，根 Logger 不增加 `Debugf/Infof/Warnf/Errorf`。

## 破坏性迁移

本版本删除旧格式化契约：

1. `Debugf/Infof/Warnf/Errorf` 改为 `Debug/Info/Warn/Error(ctx, msg, fields...)`；
2. `Ctx*` 合并为统一 Context-first 方法；
3. `Auto/Fatal` 删除；
4. `Get(any)` 改为 `Get(string)`；
5. logger context 存取 helper 删除，改用显式注入或 typed ContextExtractor；
6. Provider 从 `GetLogger` 改为稳定的 `Factory()`；`slog.Handler` 仅保留为 provider 实现 SPI。

## Open Gaps / Future Improvements

1. 尚无编译期字段 schema 检查器，运行期只保护 envelope 与重复键；标准 result 值依赖常量和代码规范。
2. 高基数 logger 名称只能通过规范和测试约束。
3. Logger 接口是长期兼容合同，新增方法需严格控制。

## File Map

1. `logger.go`：业务 Logger 合同。
2. `level.go`：跨 provider 级别。
3. `field.go`：Field、Group 与 slog Attr 转换。
4. `factory.go`：命名、Handler 降级与 ContextExtractor SPI option。
5. `slog_logger.go`：私有 Logger 实现与 caller 构造。
6. `context_handler.go`：统一 context 字段装饰器。
7. `fallback_handler.go`：标准 text 降级 Handler。
8. `provider.go`：Provider 业务入口与生命周期边界。
9. `semantic_field.go`：op/result/reason 与标准 result 字符串常量。
10. `redactor.go`、`redacting_handler.go`：脱敏契约与统一处理边界。
11. `schema_handler.go`：顶层 envelope 与重复键规则。
