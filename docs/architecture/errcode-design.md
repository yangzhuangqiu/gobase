# Errcode 架构设计

本文描述 `errcode` 的泛型错误码模型、错误链遍历和仓库治理边界。模块只提供错误载体与查询原语；具体 code 命名、日志和协议映射由使用模块负责。

## 1. 要解决的问题

普通错误文本适合诊断，却不适合作为稳定分支条件。跨层包装和 `errors.Join` 又要求上层增加阶段信息时不能丢失底层失败类别。`errcode` 用强类型 code 与标准 `Unwrap` 链分离“机器分支”和“人类消息”。

### 设计目标

1. code 可以使用调用模块自己的字符串、整数或其他 comparable 类型。
2. 包装错误保留标准 cause chain，兼容 `errors.Is/As`。
3. 目标 code 查询可以遍历单链和 `errors.Join` 多链。
4. 模块无注册表、全局状态和外部依赖。

### 非目标

1. 不集中分配全仓库 code，也不检查 code 是否重复。
2. 不提供 HTTP/RPC 状态、国际化消息或告警级别映射。
3. 不自动添加调用栈、时间、日志字段或指标。
4. 不强制使用模块遵循某一种命名格式。

## 2. 核心设计选择

### 2.1 泛型 code 而非统一字符串

`CodedError[T comparable]` 允许模块使用自有强类型 code，同时保留 `StringCode`、`IntCode` 两个便捷基础类型。不同 `T` 的 coded error 在 `errors.As` 中是不同类型，避免字符串与整数 code 意外混用。

代价是调用查询 API 时必须提供正确泛型类型；同值但不同定义类型不会匹配。

### 2.2 标准 Unwrap 而非私有原因树

`CodedError.Unwrap` 返回单个 cause，使 coded error 能与标准库包装、`errors.Is/As` 和 `errors.Join` 组合。模块不复制 cause，也不维护独立错误图。

### 2.3 `CodeOf` 与 `IsCode` 分工

`CodeOf[T]` 用于“提取遍历中第一个同类型 code”。`IsCode[T]` 用于“完整链中是否存在指定 code”。当一条链可能包含多个相同 code 类型，业务分支必须用 `IsCode`，不能假设 `CodeOf` 会返回内层或某个特定分支。

### 2.4 无全局注册表

code 由各模块定义并随模块版本演进。这样没有初始化顺序、注册冲突和中央依赖，但命名唯一性、稳定性与文档同步属于仓库治理责任。

## 3. 对象模型与依赖方向

| 对象 | 职责 |
|---|---|
| `StringCode` | 字符串 code 便捷类型 |
| `IntCode` | 整数 code 便捷类型 |
| `CodedError[T]` | 保存 `Code`、`Msg`、`Err` |
| `New[T]` | 创建无 cause 的 coded error |
| `Wrap/Wrapf[T]` | 增加 code/message 并保留 cause |
| `CodeOf[T]` | 提取首个同类型 code |
| `IsCode[T]` | 遍历错误链匹配目标 code |

推荐依赖方向：

```text
app/config/ioc/log/provider modules -> errcode -> Go standard errors
```

`errcode` 不反向依赖任何业务或基础模块。

## 4. 构建与文本契约

1. `New(code, msg)` 总是返回新的 `*CodedError[T]`，其中 cause 为 nil。
2. `Wrap/Wrapf` 在 cause 为 nil 时返回 nil，避免制造没有真实原因的包装错误。
3. 所有消息在写入时 trim；`Wrapf` 使用 `fmt.Sprintf` 后再 trim。
4. `Error()` 不输出 code：
   - 无 cause、空消息：`unknown error`；
   - 无 cause、有消息：消息；
   - 有 cause、空消息：cause 文本；
   - 有 cause、有消息：`<message>: <cause>`。
5. code 必须通过字段或查询 API 获取，不能从错误文本解析。

公开字段允许上层通过 `errors.As` 读取上下文，但对象构建后应视为只读。并发修改 `Code/Msg/Err` 没有同步保护。

## 5. 查询算法

### 5.1 CodeOf

`CodeOf[T]` 调用 `errors.As` 查找第一个 `*CodedError[T]`。标准库决定深度优先遍历顺序；在 `errors.Join` 或同类型嵌套中，结果只代表首个匹配类型，不代表最内层、最外层或最高优先级语义。

没有匹配时返回 `T` 的零值和 `false`。

### 5.2 IsCode

`IsCode[T]` 的阶段是：

```text
nil -> false
  |
errors.As(current, *CodedError[T]) and code equal -> true
  |
Unwrap() []error -> recursively inspect every branch
  |
Unwrap() error -> recursively inspect single cause
  |
false
```

先执行 `errors.As` 可以快速命中常见外层 code；code 不等时仍继续遍历，因此同一泛型类型的内外层 code 和 Join 多分支都能匹配。

## 6. 多层错误语义

一条错误链可以同时保留多个层级的 code：

```text
app_runtime.build.failed
  -> cfg.snapshot.remote_load_failed
     -> cfg.nacos.sdk.invalid_input
```

上层 code 表达当前抽象层的处置类别，下层 code 表达根因。调用方可以分别用 `IsCode` 判断重试、降级或用户提示，不需要牺牲任一层语义。

`errors.Join` 表示多个并列失败，不定义 code 优先级。需要选择主要错误时由调用边界显式制定策略，而不是依赖 `CodeOf` 的遍历顺序。

## 7. 并发与资源边界

1. 模块没有共享状态、锁、goroutine 或资源所有权。
2. 构建与查询只分配小型错误对象或遍历现有链。
3. 无环、只读错误链可以并发查询。
4. 自定义错误若形成循环 `Unwrap`，`IsCode` 没有 visited set，会无限递归；标准 Go 错误链应保持无环。

## 8. 边界与失败矩阵

| 场景 | 结果 |
|---|---|
| `Wrap(nil, ...)` | 返回 nil |
| 泛型 code 类型错误 | `CodeOf` 返回 false，`IsCode` 返回 false |
| 同类型嵌套 code | `CodeOf` 返回首个；`IsCode` 可匹配任意目标 |
| `errors.Join` 多 code | `IsCode` 遍历所有分支 |
| 空消息且无 cause | 文本为 `unknown error`，code 仍保存在字段中 |
| cause 实现 `errors.Is/As` | 通过 `Unwrap` 保留标准匹配 |
| 循环 Unwrap | 当前没有终止保证 |

## 9. 仓库治理边界

仓库推荐 dotted lowercase code，例如 `<module>.<domain>.<reason>`，动态 ID 放消息而不是 code。每个模块在自己的 `errors.go` 维护 code 常量和构造辅助，测试用 `IsCode` 验证分支语义。

`scripts/check-error-semantic.ps1` 是仓库级文本门禁，不是 `errcode` 运行时能力。当前默认只扫描 `app`、`config`、`config-nacos`、`config-consul`、`examples` 中生产 Go 文件的 `fmt.Errorf` / `errors.New`；它不覆盖全部模块，也不能证明错误链语义正确。

## 10. 边界不变量

1. code 类型必须 comparable。
2. 包装非 nil cause 后，`errors.Is/As` 仍可到达 cause。
3. `Wrap/Wrapf(nil, ...)` 不产生错误。
4. `CodeOf` 只承诺首个同类型 code，不承诺优先级。
5. `IsCode` 是目标 code 全链判断入口。
6. 错误文本不是 code 的序列化协议。

## 11. 备选方案与取舍

### 统一字符串 code

API 更简单，但整数协议或模块自定义枚举需要额外转换，也更容易跨域误用。当前泛型方案保留类型隔离。

### 中央 code 注册表

可以检查冲突和生成文档，但会引入全局状态、初始化顺序和跨模块发布耦合。当前由模块自治，仓库工具做静态治理。

### 只使用 sentinel error

`errors.Is` 很适合固定身份错误，但难以同时携带稳定分类、动态消息和多层 code。两者可以组合：coded error 包装 sentinel cause。

## 12. 性能边界

1. 构建 coded error 是常量级分配。
2. `CodeOf` 和 `IsCode` 成本与错误链节点数相关。
3. `IsCode` 不缓存遍历结果；极深或大量 Join 分支会线性增加查询成本。
4. 循环链会破坏终止性，调用方不得构造循环 `Unwrap`。

## 13. Open Gaps / Future Improvements

1. `IsCode` 尚无错误链环检测。
2. 尚无返回全部 code 或显式遍历访问者的 API。
3. code 不进入默认错误文本，单独日志输出可能缺少分类字段。
4. 仓库文本门禁未覆盖所有模块，也没有 code 重复/命名静态检查。
5. 是否引入生成器或注册表仍属于未来治理选择，当前实现没有相关能力。

## 14. File Map

1. `errcode/errors.go`：code 类型、`CodedError`、构建和查询算法。
2. `errcode/errors_test.go`：基础构建、泛型隔离、嵌套和 Join 测试。
3. `scripts/check-error-semantic.ps1`：有限范围的仓库文本门禁。

## 相关文档

1. [errcode README](../../errcode/README.md)
2. [测试规范](../testing/testing.md)
