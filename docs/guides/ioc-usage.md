# IOC 使用指南

本文面向直接使用 `ioc` 完成应用装配的开发者，按实际任务说明注册、注入、生命周期、测试和排障。设计推导与精确运行机制见 [IOC 架构设计](../architecture/ioc-design.md)。

## 1. 选择使用入口

| 场景 | 推荐入口 |
|---|---|
| 使用 `app` 构建命令行或服务应用 | 简单对象使用 `app.WithBeans`，可复用装配使用 `app.WithModules` |
| 自定义应用 Runtime | 使用 `app-runtime.Application` 托管 IOC |
| 独立库、测试工具或自定义启动流程 | 直接使用 `ioc.New` |

普通 app 用户通常不需要自己创建或关闭 Container，请从 [App 使用指南](app-usage.md#10-使用-ioc) 开始。以下内容面向生命周期不由 app-runtime 托管、需要直接使用 IOC 的场景。

## 2. 完成最小装配

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

Build 成功后，无论 Resolve 或业务运行是否成功，都必须进入 Close。关闭应使用独立的 shutdown context，业务错误与 Close 错误都需要向上返回；`errors.Join(nil, nil)` 会返回 nil。

固定阶段：

```text
Register/RegisterDefinition -> Build(ctx) -> Resolve -> Close(ctx)
```

约束：

1. Build 成功前不能 Resolve。
2. Build 成功后不能继续 Register。
3. Close 后不能 Register、Build 或 Resolve。
4. 重复 Build（已成功）和重复 Close 都是幂等操作。

Init 返回 error 时，Build 固定停止后续初始化、逆序回滚成功项并返回 `ioc.build.init_failed`。需要 panic 的进程入口应在 Build 返回后显式处理。

## 3. 注入结构体字段

```go
type OrderService struct {
    // Repository 是必填订单仓储依赖，必须是可导出字段。
    Repository *OrderRepository

    // Metrics 是可选能力；没有候选时保持零值。
    Metrics MetricsReporter `ioc:"required=false"`

    // Name 是业务值，不参与依赖注入。
    Name string `ioc:"-"`
}
```

支持的字段：

1. `interface`
2. `ptr`
3. `[]interface` 或 `[]*Struct`
4. `map[string]interface` 或 `map[string]*Struct`

未导出字段和其他值类型会被跳过。默认依赖必填；如果缺失是合法状态，必须使用 `ioc:"required=false"` 明确表达。

需要修改自身字段的 bean 必须注册为非 nil struct 指针：

```go
// 推荐：OrderService 的导出依赖字段可写。
container.Register(&OrderService{})

// 不推荐：值类型字段不可写，当前实现会静默跳过字段注入。
container.Register(OrderService{})
```

值类型 bean 只用于按值解析且明确不需要字段注入时，可以使用 `ioc.WithInject(false)` 表达意图。borrowed bean 即使设置 `WithInject(true)`，也必须满足同样的可写指针约束。

## 4. 按接口注册

bean 默认只按具体类型声明。业务按接口依赖时，注册处需要增加声明：

```go
type Repository interface {
    Save(ctx context.Context, order Order) error
}

if err := container.Register(
    &MysqlRepository{},
    ioc.WithDeclare[Repository](),
); err != nil {
    return err
}
```

`WithDeclare[T]()` 使用泛型参数表达声明类型。Register 会验证实例确实满足 T；声明不创建副本，同一个实例同时出现在具体类型和声明类型索引中。

同一个 bean 可以重复声明多个契约：

```go
if err := container.Register(
    &MysqlRepository{},
    ioc.WithDeclare[Repository](),
    ioc.WithDeclare[HealthChecker](),
); err != nil {
    return err
}
```

多个声明会累积去重，全部指向同一个 bean definition，因此字段注入、Init 和 Close 仍只执行一次。

模块拥有固定装配策略时，在模块的 `ioc.gen.go` 中输出 Definition，让应用入口只负责选择模块：

```go
// package order
func NewIOCService() ioc.Definition {
    return ioc.Define(
        &Service{},
        ioc.WithDeclare[OrderService](),
    )
}

// Composition Root
if err := container.RegisterDefinition(order.NewIOCService()); err != nil {
    return err
}
```

`Define` 会复制 options slice；`RegisterDefinition` 完整复用 `Register` 的状态、声明、冲突、指标和日志语义，不形成第二套注册规则。简单对象仍可直接调用 `Register`。

## 5. 处理多个实现

注册多个接口实现时，可以为需要稳定业务角色的 bean 设置名称：

```go
if err := container.Register(
    &MysqlRepository{},
    ioc.WithName("Primary"),
    ioc.WithDeclare[Repository](),
); err != nil {
    return err
}
if err := container.Register(
    &RedisRepository{},
    ioc.WithName("Cache"),
    ioc.WithDeclare[Repository](),
); err != nil {
    return err
}
```

单值字段存在多个候选时，字段名用于分流：

```go
type OrderService struct {
    // Primary 命中同名 bean。
    Primary Repository
}
```

规则是在字段声明类型的候选集合内进行大小写敏感的名称精确匹配。具体类型默认名称已经与字段名一致时不需要 `WithName`；字段名无法唯一匹配时，Build 返回 `ioc.build.dependency_ambiguous`。

运行期主动解析多实现时使用名称：

```go
repo, err := ioc.ResolveByNameAs[Repository](container, "Primary")
```

`ResolveByNameAs` 使用泛型声明类型与名称共同定位 bean；相同名称可以存在于不同声明类型中。`ResolveAs[Repository]` 不会根据变量名分流，存在多个候选时返回 `ioc.resolve.ambiguous`。

## 6. 注入集合

```go
type Dispatcher struct {
    Handlers []Handler
    Named    map[string]Handler
}
```

1. slice 按注册顺序注入全部候选。
2. map 固定使用 bean 在当前 value 声明类型范围内的最终名称作为 key。
3. 注册阶段保证 `(valueType, name)` 唯一，因此 map 不会静默覆盖同名候选。
4. 集合字段默认也是必填；允许空集合时设置 `ioc:"required=false"`。

主动读取全部实现：

```go
handlers, err := ioc.ResolveAllAs[Handler](container)
```

与字段注入不同，`ResolveAllAs` 没有候选时返回空切片，不报 dependency not found。

## 7. 管理初始化和关闭

需要运行资源的 bean 实现生命周期接口：

```go
type Server struct {
    Repository Repository
}

// Init 在依赖注入完成后启动服务资源。
func (this *Server) Init(ctx context.Context) error {
    return this.start(ctx)
}

// Close 在容器退出阶段释放服务资源。
func (this *Server) Close(ctx context.Context) error {
    return this.stop(ctx)
}
```

正常模式下：

```text
Repository.Init -> Server.Init -> Server.Close -> Repository.Close
```

关键约束：

1. Register 只装配实例，不应提前获取必须回滚的资源。
2. 资源获取放入 Init，释放放入 Close。
3. Init 必须在返回 nil 前达到可用状态。
4. Close 必须响应 context、保持幂等且不得 panic；当前容器不会恢复 Closer panic。

## 8. 区分 Managed 与 Borrowed

生命周期交由容器管理的对象使用默认配置：

```go
if err := container.Register(&OrderService{}); err != nil {
    return err
}
```

由外部创建、初始化和关闭，只需要暴露给业务 bean 的对象声明为 borrowed：

```go
if err := container.Register(
    externalManager,
    ioc.WithName("ConfigManager"),
    ioc.WithDeclare[config.Manager](),
    ioc.WithBorrowed(),
); err != nil {
    return err
}
```

borrowed 默认行为：

| 能力 | 是否启用 |
|---|---:|
| 注册和按名/按类型解析 | 是 |
| 被其他 bean 注入 | 是 |
| 修改自身字段 | 否 |
| 调用自身 Init/Close | 否 |

如果 borrowed 对象确实需要容器补充字段依赖，显式授权：

```go
if err := container.Register(
    externalObject,
    ioc.WithBorrowed(),
    ioc.WithInject(true),
); err != nil {
    return err
}
```

`WithInject(true)` 只开启字段注入，不把生命周期所有权交给容器。它与 `WithBorrowed()` 的 Option 顺序不影响最终语义。

## 9. 处理循环依赖

默认拒绝：

```text
A -> B -> A
```

Build 返回 `ioc.build.circular_dependency`，错误中包含循环链路。首选处理方式：

1. 抽取双方共同依赖。
2. 将一个方向改为事件或回调。
3. 缩小接口，重新建立单向依赖。
4. 把资源启动协调提升到更上层 orchestrator。

只有兼容历史引用环时才开启：

```go
container := ioc.New(
    ioc.WithAllowCircularDependency(true),
)
```

兼容模式先完成全部字段注入，再按注册顺序 Init。它不能保证 A.Init 执行时 B 已经 Init，因此不适合相互依赖连接、监听端口或后台任务就绪的对象。

## 10. 处理生命周期失败

```go
container := ioc.New(
    ioc.WithCloseTimeout(5*time.Second),
)
```

### Init error

Init error 使用固定原子语义，不提供策略 Option：

1. 第一个 Init 返回 error 后立即停止，不执行后续 Initializer。
2. 逆序 Close 此前成功 Init 的 bean。
3. 回滚错误与原始 Init error 聚合。
4. Build 返回 `ioc.build.init_failed`，容器恢复为 `created`。
5. 失败 Initializer 本身不进入回滚清单，必须在返回 error 前清理自己的半初始化资源。

需要收集多个配置错误时，应在 config validator 阶段完成，而不是继续执行具有资源副作用的 Init。

### Init panic

Initializer 意外 panic 时，容器会先逆序回滚此前成功项，再继续 panic。panic payload 被归一化为可检索的 `ioc.build.init_failed` error；这属于编程异常，不是可配置的启动失败策略。

### Close

Close 固定逆序遍历全部 managed Closer，并使用 `errors.Join` 聚合所有错误，不提供首错停止策略。Build 失败回滚和正常 Close 使用相同的“继续并聚合”资源收敛原则。

并发调用共享一次关闭事务：

1. 首个调用者成为 executor，其 context 控制真实 Closer 执行。
2. 关闭期间的并发调用者成为 waiter，只用自己的 context 控制等待。
3. waiter 取消返回 `ioc.close.failed/reason=wait_canceled`，不取消 executor，也不改变共享结果。
4. executor 完成后发布 `closed` 和首次结果；正常 waiter 与后续重复 Close 返回同一个结果。
5. `closing` 期间 Register、Build 和 Resolve 返回 `ioc.container.closed/reason=close_running`。

`WithCloseTimeout` 针对每个 bean 独立计时。超时只能停止容器等待；如果 Closer 忽略 context，它的 goroutine 不能被容器强制终止。Closer panic 会在执行 goroutine 内恢复，携带 stack 转换为 `ioc.close.failed`，然后继续关闭剩余资源；这保证收敛完整性，但 panic 仍是必须修复的编程异常。

## 11. 在 Build 失败后处理容器

Build 返回 error 或 panic 被恢复后，容器回到 `created`：

1. 已成功 Init 的 bean 已按逆序回滚。
2. 可以继续 Register，并再次 Build。
3. 已注入字段不会恢复为 Build 前的旧值。
4. 没有成功 Build 的 bean 不进入正常 Close 清单。

因此重试只适合装配结构修正或幂等 Init。涉及不可重复外部副作用时，优先丢弃失败容器并创建新实例。

## 12. 使用日志和指标

```go
container := ioc.New(
    ioc.WithLogger(logger),
)
```

日志重点字段是 `op/result/stage/reason`。不要通过文本判断错误类型，使用稳定 ErrorCode：

```go
if errcode.IsCode[ioc.ErrorCode](err, ioc.ErrCodeIOCBuildCircular) {
    // 记录依赖图问题并阻止应用发布。
}
```

指标快照：

```go
metrics := container.Metrics()
```

循环兼容模式重点关注：

1. `BuildCircularRejectedTotal`
2. `BuildCircularDegradedTotal`
3. `LastCircularDegrade.Chain`
4. `LastCircularDegrade.Order`

关闭阶段重点关注：

1. `CloseFailedTotal`：失败的 bean Close 次数。
2. `CloseTimeoutTotal`：单 bean timeout 次数。
3. `CloseJoinedTotal`：加入正在执行事务的并发 Close 次数。
4. `CloseWaitCanceledTotal`：waiter 独立取消次数。
5. `ClosePanicTotal`：被恢复并转换为 error 的 Closer panic 次数。

## 13. 测试装配

每个测试使用独立容器，避免状态和实例字段在用例间共享：

```go
// TestOrderServiceAssembly 验证仓储会在服务之前初始化，且服务可以被解析。
// 测试方法：注册带 trace 的仓储和服务，Build 后断言顺序与解析结果，最后 Close。
func TestOrderServiceAssembly(t *testing.T) {
    container := ioc.New()
    // register test beans ...
}
```

建议覆盖：

1. 必填依赖缺失和可选依赖缺失。
2. 多实现按字段名分流和主动 Resolve 歧义。
3. Init 与 Close 的依赖顺序。
4. Init 失败回滚和 Close 错误聚合。
5. borrowed 字段不被修改、Init/Close 不被调用。
6. borrowed + `WithInject(true)` 的显式注入。
7. 循环依赖默认拒绝和兼容降级。
8. 并发 Close 只执行一次并共享首次结果。
9. waiter context 取消不影响 executor。
10. Closer panic 转为聚合错误后仍继续关闭剩余资源。

## 14. 最佳实践

本节是 IOC 使用建议的权威入口。README 只保留高频摘要；设计原因和实现边界见 [IOC 架构设计](../architecture/ioc-design.md)。

### 14.1 明确容器边界

一个应用运行单元维护一个 Container，在启动阶段一次性完成注册和 Build，在退出阶段统一 Close。

推荐：

```text
应用入口
  -> 创建 Container
  -> 注册基础对象和业务对象
  -> Build
  -> 运行
  -> Close
```

避免：

1. 为每个业务包创建独立容器，导致跨容器依赖和关闭顺序失去统一事实源。
2. 把 Container 放入全局变量，让测试和多个应用实例共享状态。
3. Build 成功后尝试动态注册；当前容器不是运行期插件注册中心。

### 14.2 让 Container 停留在装配边界

Container 应由 `main` 或 Runtime 持有；Module 只获得 `ioc.Registry`。业务对象通过字段获得明确依赖，不应在业务方法中随时调用 Resolve。

```go
// 推荐：依赖在 Build 阶段确定。
type OrderService struct {
    Repository Repository
}
```

避免把 Container 注入业务对象并作为 Service Locator：

```go
// 不推荐：依赖隐藏在运行期 Resolve 调用中。
type OrderService struct {
    Container ioc.Container
}
```

这样可以让依赖图、循环检测和测试替身保持可见。

### 14.3 Register 只声明对象，不启动资源

对象可以在 Register 前完成纯内存构造，但连接建立、端口监听和后台任务启动应放入 `Initializer.Init`。

原因：

1. Build 能统一传播初始化错误。
2. 已成功 Init 的资源能在后续失败时逆序回滚。
3. 依赖字段在 Init 前已经完成注入。

同一个实例只注册一次，并只设置一个稳定名称。用不同名称重复 Register 会创建多个 bean definition，可能导致同一对象被重复注入、Init 和 Close。一个实例需要多个类型入口时，在同一次 Register 中重复使用 `WithDeclare[T]()`；所有类型索引都会指向同一个 bean。

### 14.4 让依赖关系保持明确

1. 默认使用必填依赖，只有业务允许能力缺失或明确存在降级路径时才设置 `ioc:"required=false"`。
2. 使用 `ioc:"-"` 标记不属于依赖图的业务字段，避免读者误认为容器会赋值。
3. 接口用于稳定业务边界；注册实现时通过 `WithDeclare[Interface]()` 显式声明，不依赖隐式类型猜测。
4. 集合注入适合插件或策略集合；单值核心依赖不应为了绕过歧义改成 slice。
5. 需要字段注入的 bean 使用非 nil struct 指针；不要依赖值类型 bean 的静默跳过行为。

可选依赖的使用方必须处理 nil：

```go
if this.Metrics != nil {
    this.Metrics.Record(order)
}
```

### 14.5 使用稳定、面向角色的名称

多实现名称应描述其在应用中的角色，例如 `Primary`、`Replica`、`Cache`，而不是容易随实现替换而变化的 SDK 或结构体名称。

```go
if err := container.Register(
    &MysqlRepository{},
    ioc.WithName("Primary"),
    ioc.WithDeclare[Repository](),
); err != nil {
    return err
}
```

建议：

1. 单值字段名与角色名称精确对齐。
2. 一个 bean 只保留一个名称；同一次 Register 重复设置 `WithName` 时后者覆盖前者，不会产生多个等价名称。
3. 一个 bean 可以通过重复 `WithDeclare[T]()` 声明多个类型，声明会累积去重且都指向同一实例。
4. 默认名称会去掉指针、包路径和泛型参数；例如 `Cache[User]` 与 `Cache[Order]` 默认都叫 `Cache`，但因精确声明类型不同可以共存。
5. 名称只需在同一声明类型下唯一；同一类型存在多个实例，或多个实现向同一接口声明了相同默认名称时，使用 `WithName` 表达角色。
6. map key 固定使用 bean 最终名称；需要稳定路由 key 时直接使用稳定 `WithName`，不要在实例上另建 `GetName` 命名体系。

### 14.6 按资源所有权选择 Managed 或 Borrowed

| 对象来源 | 推荐注册方式 | 关闭责任 |
|---|---|---|
| 由当前容器初始化并拥有 | 默认 managed | Container |
| 由 `app-runtime` 或外部组件拥有 | `WithBorrowed()` | 外部所有者 |
| 外部拥有但需要补充字段 | borrowed + `WithInject(true)` | 外部所有者 |

不要通过 `WithInject(true)` 推断生命周期所有权；注入授权和资源所有权是两个独立维度。

Logger、ConfigManager、共享 Client 等外部基础对象通常应作为 borrowed 注册，避免重复 Init/Close。

### 14.7 使用可组合的失败语义

应用和库边界推荐：

```go
container := ioc.New(
    ioc.WithLogger(logger),
    ioc.WithCloseTimeout(5*time.Second),
)
```

1. Init error 固定首错停止并返回 error，调用方决定日志、panic、退出码和更外层资源回滚。
2. Close 固定继续，尽可能释放全部资源并聚合错误。
3. Build 失败回滚同样固定继续关闭成功 Init 项。
4. timeout 是最后的等待边界，不是 Closer 忽略 context 的替代方案。
5. Closer panic 会被恢复并聚合，但必须通过 `ClosePanicTotal` 和日志告警推动修复。
6. 应用只需保留任意正常完成的 Close 返回值；并发和重复调用会共享首次结果。

### 14.8 把循环依赖降级当作迁移工具

生产代码优先保持默认拒绝循环依赖。开启 `WithAllowCircularDependency(true)` 时必须同时具备：

1. 明确记录需要兼容的循环链路。
2. 验证各方 Init 不依赖对方已经完成 Init。
3. 监控 `BuildCircularDegradedTotal` 和 `LastCircularDegrade`。
4. 制定拆环方案和移除开关的条件。

不要把降级开关作为解决 `circular_dependency` 的常规修复。

### 14.9 谨慎重试失败的 Build

Build 失败后容器虽然恢复为 `created`，但依赖字段不会回滚。推荐决策：

| 失败场景 | 建议 |
|---|---|
| 缺少注册且可通过追加 Register 修正 | 补充注册后可以重试 |
| 已注册 bean 的名称、声明或所有权错误 | 当前没有修改/注销 API，重新创建容器 |
| Init 完全幂等，且回滚已经验证 | 可以受控重试 |
| 存在不可重复外部副作用 | 丢弃容器和实例，重新创建 |
| Init 意外 panic 被上层 recover | 优先重建，不复用半变更对象图 |

### 14.10 让生命周期可测试、可观测

1. 测试中记录 Init/Close trace，断言依赖优先和逆序关闭。
2. borrowed 测试同时断言字段是否被修改、Init/Close 是否被调用。
3. 应用启动失败时记录稳定 ErrorCode 和 `op/result/stage/reason`，不要只输出错误文本。
4. 对循环降级、Init 失败和 Close timeout 建立指标告警。
5. 在目标硬件和真实 bean 图上运行 benchmark，不把开发机结果直接当生产容量承诺。

### 14.11 评审清单

提交新的 IOC 装配代码前检查：

1. 对象由谁创建、由谁关闭是否明确？
2. Register 阶段是否包含外部资源副作用？
3. 多实现是否在共同声明类型下具有稳定且唯一的角色名称？
4. 可选依赖是否真的存在合法降级路径？
5. Init 和 Close 是否响应 context，并保持可重试/幂等边界？
6. 是否引入循环依赖或 Service Locator？
7. Build 失败后的回滚、重试和日志是否可验证？

## 15. 常见问题

| 现象 | 原因 | 处理 |
|---|---|---|
| `container.not_built` | Build 成功前 Resolve | 调整调用阶段，先完成 Build |
| `dependency_not_found` | 缺少注册/接口声明，或字段默认必填 | 检查 `WithDeclare[T]()`；合法缺失时设置 `required=false` |
| `dependency_ambiguous` | 多候选字段名没有命中唯一名称 | 使用稳定 `WithName` 并对齐字段名 |
| `resolve.ambiguous` | 主动按类型解析多个实现 | 使用 `ResolveByName` 或 `ResolveAll` |
| `register.duplicate_name` | 相同声明类型下出现同名 bean | 为冲突候选设置不同的角色名称 |
| `circular_dependency` | 字段依赖图存在环 | 优先拆环，只在兼容场景开启降级 |
| 值类型 bean 的依赖字段为空 | 注册值不可写，字段注入被跳过 | 注册非 nil struct 指针；不需要注入时显式 `WithInject(false)` |
| borrowed 自身字段为空 | borrowed 默认不注入 | 明确需要时增加 `WithInject(true)` |
| borrowed 没有 Close | 生命周期由外部拥有 | 在外部所有者关闭阶段释放 |
| Close timeout 后任务仍在运行 | Closer 忽略 context | 修改 Closer 主动响应取消 |
| 并发 Close 返回 `wait_canceled` | waiter 自身 context 先结束 | executor 仍继续；使用更合适的 waiter deadline 或稍后读取共享结果 |
| Close 返回 `close_panic` | Closer 发生编程异常 | 根据日志 stack 修复 Closer；容器已经继续收敛其他资源 |
| Build 失败后字段仍有值 | 字段写入不做内存回滚 | 创建新容器/实例，或保证重试安全 |

## 16. 可运行示例与相关文档

1. [quickstart-ioc](../../examples/quickstart-ioc/README.md)：strict、allow-circular 和 prod 模式。
2. [app-runtime 使用指南](app-runtime-usage.md)：由 Runtime 托管 IOC。
3. [ioc 模块入口](../../ioc/README.md)：公共入口和关键默认值。
4. [IOC 架构设计](../architecture/ioc-design.md)：设计理由、状态机、错误码和实现边界。
5. [quickstart-app](../../examples/quickstart-app/README.md)：IOC Module 与业务 Worker 的应用装配示例。
