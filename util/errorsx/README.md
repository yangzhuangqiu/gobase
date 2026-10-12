# errorsx

`errorsx` 是 `util` 子包共享的错误语义载体，用 `op/kind/detail/cause` 分离排障上下文、稳定分支和底层原因。

## 设计范围与目标

### In Scope

1. 定义 namespaced `Kind` 和统一 `Error`。
2. 创建或包装带稳定 kind 的错误。
3. 通过 `IsKind` 遍历普通单链和 `errors.Join` 多分支，通过 `KindOf`、`OpOf` 提取首个元数据。
4. 通过 `Unwrap` 兼容标准 `errors.Is/As`。

### Out of Scope

1. 不提供全局 kind 注册中心、HTTP 映射或国际化消息。
2. 不自动记录日志、指标或调用栈。
3. 不校验 op/kind 命名，也不阻止公开字段被修改。
4. 当前不提供错误链环检测或全部 Kind 收集 API。

## 核心对象与职责

| 对象 | 职责 |
|---|---|
| `Kind` | 调用方可采取动作的稳定语义键，推荐 `<package>.<reason>` |
| `Error` | 保存内部阶段 `Op`、稳定原因 `Kind`、诊断 `Detail` 和可选 cause |
| `New/Newf` | 创建无 cause 的错误 |
| `Wrap/Wrapf` | 保留 cause 并增加当前操作语义 |
| `IsKind` | 在普通单链或 `errors.Join` 错误树中判断目标 kind |
| `KindOf/OpOf` | 提取首个匹配 `errorsx.Error` 的字段 |

## 最小使用示例

```go
value, err := convx.ToInt(input)
if err != nil {
	if errorsx.IsKind(err, convx.KindUnsupportedType) {
		return fallback
	}
	return err
}
```

## 公共入口与核心契约

1. `New/Newf` 始终创建 `*Error`；`Newf` 将格式化结果写入 Detail。
2. `Wrap/Wrapf(nil, ...)` 返回 nil；非 nil cause 通过 `Unwrap` 保留。
3. `Error()` 将空 Op 显示为 `unknown`、空 Kind 显示为 `unknown.unknown`，但不会改写对象字段。
4. 文本固定包含 `op=<...> result=failed kind=<...>`，随后追加非空 detail 和 cause。
5. `KindOf/OpOf` 使用 `errors.As`，只返回遍历中的首个 `*Error`。
6. 对象字段公开且可变；构建后应按只读错误对象使用。
7. typed nil `*Error` 的 `Error()` 返回稳定 unknown 文本，不会 panic。

## Op 与 Kind 最佳实践

1. `Kind` 回答“调用方接下来做什么”，例如 `invalid_argument`、`authentication_failed`、`output_limit_exceeded`。
2. `Op` 回答“失败发生在哪个入口或阶段”，例如 `filex.WriteString`、`aesx.validateNonce`。
3. `Detail` 保存动态上下文和内部步骤，例如 `stage=open path=...`，不得用于程序分支。
4. `Kind` 不使用 `new_reader`、`close_writer`、`write_bytes_error` 等实现步骤；重构内部流程不应迫使调用方修改分支。
5. OS、context 和业务 cause 通过 `errors.Is/As` 判断，不为每个底层类型复制一套 Kind。

## 错误链与 Join 边界

`IsKind` 先尝试用 `errors.As` 快速命中目标 Kind；未命中时根据错误节点形态继续遍历：

1. 实现 `Unwrap() []error`：逐个递归检查 `errors.Join` 的全部兄弟分支。
2. 实现 `Unwrap() error`：沿单个 cause 继续检查。
3. 没有可展开 cause：返回 false。

因此 Join 分支顺序不会改变目标 Kind 的匹配结果，标准包装后的嵌套 Join 也可命中。`KindOf/OpOf` 的语义不同：它们使用 `errors.As`，只返回遍历中的首个 `*Error`。

## 运行时语义

1. 本包没有共享状态、资源或后台任务。
2. Error 查询按错误链长度同步执行，不缓存结果。
3. 无环且只读的 Error 可并发读取；并发修改公开字段不安全。
4. 自定义循环错误链没有环检测，调用方不得构造循环 `Unwrap`。

## 最佳实践摘要

1. 子包在 `kinds.go` 导出稳定、调用方可行动的 Kind 常量，调用方不硬编码字符串。
2. Op 使用 `package.func` 或 `package.type.method`，Detail 保存 stage、path、len、attempt 等动态上下文。
3. 敏感值不进入 Detail 或 cause 文本。
4. 分支使用 Kind，底层系统错误使用 `errors.Is/As`。

## Open Gaps / Future Improvements

1. 尚无错误链环检测或返回全部 Kind 的 API。
2. 公开字段没有不可变保护。
3. 没有 kind 命名和重复检测工具。

## File Map

1. `errors.go`：Kind、Error、构建和查询 API。
2. `errors_test.go`：文本、包装和查询测试。

## 相关文档

1. [util README](../README.md)
2. [Util 使用指南](../../docs/guides/util-usage.md)
