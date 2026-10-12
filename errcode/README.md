# errcode

`errcode` 提供最小的泛型错误码载体和错误链查询能力，适合模块使用自有 `comparable` code 类型建立稳定失败分支。

## 设计范围与目标

### In Scope

1. 用 `CodedError[T]` 保存 code、可读消息和可选 cause。
2. 通过 `New`、`Wrap`、`Wrapf` 创建或包装 coded error。
3. 通过 `CodeOf` 提取首个同类型 code。
4. 通过 `IsCode` 匹配单链或 `errors.Join` 多分支中的目标 code。

### Out of Scope

1. 不维护全仓库错误码注册中心、HTTP 状态映射或国际化消息。
2. 不规定每个模块的 code 命名空间和值域。
3. 不自动记录日志、指标或调用栈。
4. 不替代 `errors.Is` / `errors.As` 对底层 cause 的匹配。

## 核心对象与职责

| 对象 | 职责 |
|---|---|
| `StringCode` | 常用字符串 code 类型 |
| `IntCode` | 常用整数 code 类型 |
| `CodedError[T]` | 保存 `Code`、`Msg`、`Err` 并实现 `error` / `Unwrap` |
| `CodeOf[T]` | 返回遍历中首个 `*CodedError[T]` 的 code |
| `IsCode[T]` | 判断完整单链或多链中是否存在目标 code |

## 最小使用示例

```go
type ErrorCode = errcode.StringCode

const ErrCodeOrderNotFound ErrorCode = "order.not_found"

err := errcode.Wrapf[ErrorCode](cause, ErrCodeOrderNotFound, "order_id=%s", orderID)
if errcode.IsCode[ErrorCode](err, ErrCodeOrderNotFound) {
	// 执行稳定业务分支，不解析错误文本。
}
```

## 公共入口与核心契约

1. code 类型必须满足 `comparable`；不同泛型类型互不匹配。
2. `New(code, msg)` 创建无 cause 的 coded error。
3. `Wrap/Wrapf(nil, ...)` 返回 nil；非 nil cause 通过 `Unwrap` 保留。
4. `CodeOf[T]` 使用 `errors.As` 返回遍历顺序中的首个同类型 code；它不保证返回调用方期望的内层 code。
5. `IsCode[T]` 在首个同类型 code 不匹配时继续遍历单链和 `errors.Join` 分支，适合做目标 code 判断。
6. `CodedError` 字段公开且可变；构建后应把 error 视为只读对象。

## 默认行为与错误文本

1. `Msg` 会 trim；空消息不会自动包含 code。
2. 无 cause 且消息为空时，`Error()` 返回 `unknown error`。
3. 有 cause 且消息为空时，`Error()` 返回 cause 文本。
4. 消息和 cause 同时存在时，文本为 `<msg>: <cause>`。
5. code 是分支契约，消息只用于人类阅读和诊断；不能通过文本反推 code。

## 错误语义

1. 使用 `IsCode` 做已知目标匹配，使用 `CodeOf` 做“提取首个 code”场景。
2. 使用 `errors.Is/As` 检查 coded error 下的原始 cause。
3. `errors.Join` 中可能同时存在多个 code；单次 `CodeOf` 只能返回其中一个。
4. 模块包装错误时应保留 cause，不要用新错误覆盖可分支的下层语义。

## 运行时语义

1. 本模块不持有全局状态、资源或后台任务。
2. 查询只遍历当前错误链，不缓存结果。
3. 对正常的无环错误链可并发读取；并发修改公开字段不安全，也不属于支持场景。
4. `IsCode` 没有错误链环检测；自定义循环 `Unwrap` 链可能导致无限递归。

## 最佳实践摘要

1. 每个模块定义自己的强类型 code 别名和常量，避免散落裸字符串。
2. code 保持稳定、低基数；动态 ID 和阶段信息放入消息。
3. 上层增加阶段语义时继续 Wrap cause，分支时使用 `IsCode`。
4. 对外边界分别处理用户消息、日志上下文和稳定 code，不让一个字段承担全部职责。

## Open Gaps / Future Improvements

1. 尚无错误链环检测。
2. `CodeOf` 只能返回首个 code，没有返回全部匹配 code 的 API。
3. 空消息不包含 code，单独打印 error 时可能缺少分类信息。
4. 字段公开可变，类型本身不提供不可变或并发修改保护。

## File Map

1. `errors.go`：code 类型、`CodedError`、构建和查询 API。
2. `errors_test.go`：单链、同类型嵌套和 `errors.Join` 匹配。

## 相关文档

1. [Errcode 架构设计](../docs/architecture/errcode-design.md)
2. [测试规范](../docs/testing/testing.md)
