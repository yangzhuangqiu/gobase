# ioc

`ioc` 是面向应用装配的轻量级依赖注入容器。它通过显式的
`Register/RegisterDefinition -> Build -> Resolve -> Close` 阶段，把对象发现、依赖写入和资源生命周期收敛到一个可测试的启动边界中。

`ioc` 追求可预测装配，不对标 Spring IoC 的完整功能集：不做包扫描、注解、AOP、作用域代理和运行期增量重建。

## 设计范围与目标

### In Scope

1. 显式注册实例，并按具体类型及 `(声明类型, 名称)` 建立索引。
2. 为结构体的 `interface`、`ptr`、`slice`、`map[string]T` 字段注入依赖。
3. 根据依赖图安排 `Initializer` 和 `Closer` 生命周期。
4. 区分容器拥有的 managed bean 与外部拥有的 borrowed bean。
5. 检测循环依赖，并为兼容场景提供显式降级开关。
6. 提供稳定错误码、结构化日志字段和运行指标。

### Out of Scope

1. 自动扫描与隐式注册。
2. 构造器注入、方法注入、工厂方法和 prototype/request 等作用域。
3. 条件装配、配置绑定、AOP 和事件总线。
4. Build 成功后的动态注册或容器重建。

## Quick Start

```go
container := ioc.New()

if err := container.Register(&OrderRepository{}); err != nil {
    return err
}
if err := container.Register(&OrderService{}); err != nil {
    return err
}
if err := container.Build(ctx); err != nil {
    return err
}

service, err := ioc.ResolveAs[*OrderService](container)
if err == nil {
    err = service.Serve(ctx)
}

shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
defer cancelShutdown()
return errors.Join(err, container.Close(shutdownCtx))
```

Build 成功后，调用方必须保证容器进入 Close。示例使用独立的 shutdown context，并聚合业务错误与关闭错误，避免 Resolve 或 Serve 失败时泄漏已初始化资源。

完整的接口声明、集合注入、生命周期和循环依赖场景见
[IOC 使用指南](../docs/guides/ioc-usage.md)。

## 核心对象与职责

| 对象 | 职责 |
|---|---|
| `Container` | 维护注册索引、状态机、依赖图和生命周期顺序 |
| `Definition` | 将模块 Bean 与默认注册策略绑定为原子装配结果 |
| `RegisterOption` | 描述单个 bean 的名称、声明类型、注入和所有权 |
| `Option` | 配置容器级日志、单 bean Close timeout 和循环依赖策略 |
| `Initializer` | 在依赖注入完成后执行资源初始化 |
| `Closer` | 在容器关闭或 Build 回滚时释放资源 |
| `MetricsSnapshot` | 暴露构建、降级、初始化和关闭指标快照 |

## 公共入口

| API | 调用阶段 | 语义 |
|---|---|---|
| `New(options...)` | 创建 | 创建处于 `created` 状态的容器 |
| `Register(bean, options...)` | Build 前 | 注册已有实例和它支持的声明类型 |
| `Define(bean, options...)` | 模块声明 | 构造隔离 options slice 的 Definition |
| `RegisterDefinition(definition)` | Build 前 | 原样注册模块提供的 Bean 及默认策略 |
| `Build(ctx)` | 启动 | 建图、注入、初始化并发布可解析状态 |
| `Resolve/ResolveAll/ResolveByName` | Build 成功后 | 按类型或类型范围内的名称读取已注册实例 |
| `Close(ctx)` | 退出 | 逆成功 Build 顺序关闭 managed bean，并发/重复调用共享首次结果 |
| `Metrics()` | 任意阶段 | 返回并发安全的只读指标快照 |

泛型辅助入口：`ResolveAs[T]`、`ResolveAllAs[T]`、`ResolveByNameAs[T]`。

## 配置模型与默认值

| 配置 | 默认值 | 说明 |
|---|---|---|
| bean 生命周期 | managed | 注入字段，并由容器调用 `Init/Close` |
| borrowed bean 注入 | 关闭 | `WithBorrowed()` 后需用 `WithInject(true)` 显式开启 |
| 循环依赖 | 拒绝 | Build 返回 `ioc.build.circular_dependency` |
| Init 返回错误 | 固定 stop | 停止后续 Init，逆序回滚成功项并由 Build 返回 error |
| Init 意外 panic | rollback + panic | 逆序回滚成功项，再继续抛出 `init_failed` panic |
| 正常 Close 失败 | 固定 continue | 继续关闭全部 bean 并在最后聚合错误；不可配置为 stop |
| 单 bean Close timeout | 不启用 | `WithCloseTimeout(duration)` 显式配置 |
| 字段依赖 | required | `ioc:"required=false"` 可声明为可选 |

Init error 处理不可配置。是否把 Build error 转成 panic、日志或进程退出码，由应用入口决定。

## 运行时语义

1. managed bean 默认参与字段注入和 `Init/Close`。
2. borrowed bean 可注册、可被其他 bean 注入、可解析，但默认不修改自身字段，也不调用自身 `Init/Close`。
3. Build 先完成依赖图和循环检测，再注入字段，最后按依赖优先顺序执行 Init。
4. Build 失败会始终尝试逆序关闭全部已成功初始化的 bean、聚合回滚错误，并把容器恢复为 `created`；字段写入不是内存事务，不会自动恢复旧值。
5. Close 使用成功 Build 顺序的逆序；首个调用执行关闭，并发调用等待同一事务，重复调用返回首次结果。
6. waiter 的 context 只取消自身等待，不改变首个 Close 使用的执行 context 和共享结果。
7. 循环依赖兼容模式按注册顺序降级，无法保证依赖完成初始化后才初始化消费者。

完整机制与设计理由见 [IOC 架构设计](../docs/architecture/ioc-design.md)。

## 错误语义

1. 错误码按 `ioc.register.*`、`ioc.build.*`、`ioc.resolve.*`、`ioc.close.*` 分类。
2. 循环依赖错误包含可检索的 `A -> B -> A` 链路。
3. closing 阶段的业务操作返回 `ioc.container.closed/reason=close_running`；waiter 取消返回 `ioc.close.failed/reason=wait_canceled`。
4. 关键错误和日志包含 `op/result/stage/reason`。
5. 按错误码分支时使用 `errcode.IsCode`，它会检查包装链和聚合错误；`errors.As` 用于提取特定错误类型及上下文，不替代任意内层错误码匹配。
6. 不解析错误文本；文本字段只用于日志检索和人工诊断。

## 最佳实践

1. 每个应用运行单元维护一个容器，在启动阶段一次性完成 Register/RegisterDefinition 和 Build。
2. 模块拥有默认 Bean 装配策略时返回 Definition；Composition Root 只选择模块并调用 RegisterDefinition。
3. 应用入口必须处理 Build error；需要 fail-fast 时在最外层显式 panic 或退出。
4. 需要字段注入的 bean 注册为非 nil struct 指针；值类型 bean 的字段不可写，会被容器跳过。
5. 每个 bean 只使用一个稳定 name；多个声明类型通过重复 `WithDeclare[T]()` 累积，全部指向同一个实例。
6. 默认名称去掉指针、包路径和泛型参数；名称只需在相同声明类型下唯一，不同泛型闭包类型可以共享原始类型名称。
7. 多实现的单值字段名与目标 bean name 对齐；同一声明类型下默认名称冲突时使用面向角色的 `WithName`。
8. 外部拥有的连接、Manager、Logger 等对象注册为 borrowed，避免重复初始化和释放。
9. 优先消除循环依赖，只为历史对象图开启降级，并监控降级指标。
10. `Closer` 必须响应 context 并保持幂等；panic 会携带 stack 转为聚合错误，但仍属于应修复的编程异常。
11. timeout 只能停止容器等待，不能强制终止忽略 context 的业务 goroutine。

完整的场景选择、反例和评审清单见 [IOC 使用指南：最佳实践](../docs/guides/ioc-usage.md#14-最佳实践)。

## 文档

1. [IOC 使用指南](../docs/guides/ioc-usage.md)：按任务说明注册、注入、生命周期、测试和排障。
2. [IOC 架构设计](../docs/architecture/ioc-design.md)：问题、设计推导、实现机制和精确运行语义。
3. [App Runtime 使用指南](../docs/guides/app-runtime-usage.md)：IOC 由应用 Runtime 托管时的使用方式。
4. [quickstart-ioc](../examples/quickstart-ioc/README.md)：可运行的 strict、allow-circular 和 prod 示例。

## Migration Notes

`WithManagedLifecycle(bool)` 已删除，统一改为显式的 borrowed 语义：

1. `WithManagedLifecycle(false)` 替换为 `WithBorrowed()`。
2. `WithManagedLifecycle(true)` 直接删除；managed 是默认值。
3. borrowed bean 如需字段注入，仍显式追加 `WithInject(true)`。
4. `WithCloseFailurePolicy(...)` 和 `CloseFailurePolicy*` 已删除；移除调用即可，Close 固定继续并聚合全部错误。
5. 重复 Close 现在返回首次关闭结果，不再用后续 nil 覆盖首次失败。

## Open Gaps / Future Improvements

1. 当前只支持字段注入，尚未提供构造器或工厂注册模型。
2. 缺少条件装配和显式 lazy bean；所有注册结构在 Build 时一次性确定。
3. 循环依赖降级是兼容策略，不提供代理或 two-phase initialization 语义。
4. Close timeout 无法终止忽略 context 的 `Closer`。

## File Map

1. `container.go`：公共容器契约、索引和状态机。
2. `definition.go`：模块 Bean 与默认注册策略的原子装配对象。
3. `options.go`：bean 注册选项。
4. `container_option.go`：日志、Close timeout 和循环策略。
5. `model.go`：共享 Close、指标与内部 bean 模型。
6. `inject.go`：字段规则、候选选择与依赖收集。
7. `lifecycle.go`：建图、拓扑、Init、回滚和 Close。
8. `generic.go`：泛型解析辅助。
9. `errors.go`：稳定错误码与错误构造。
10. `definition_test.go`：Definition 声明发布、选项隔离和非法输入用例。
11. `usecase_test.go`：状态、注入、所有权、循环、Init 回滚和共享 Close 事务用例。
12. `generic_injection_test.go`：真实泛型的默认名称、注入、解析和冲突测试。
13. `benchmark_test.go`：装配、注入和关闭性能基准。
