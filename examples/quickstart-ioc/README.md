# quickstart-ioc

`quickstart-ioc` 用于演示 `gobase/ioc` 的三种运行语义：

1. `strict`（默认）：强语义，循环依赖 fail-fast。
2. `allow-circular`：弱约束，允许循环依赖降级执行。
3. `prod`：生产化示例，展示 `ioc + log + log-zap` 多模块装配与 graceful shutdown。

## 从仓库根目录运行

```powershell
# 默认 strict 模式
./scripts/go.ps1 -C examples run ./quickstart-ioc

# 弱约束循环依赖模式
$env:QUICKSTART_IOC_MODE="allow-circular"; ./scripts/go.ps1 -C examples run ./quickstart-ioc

# 生产化模式（默认运行 20 秒后自动收敛）
$env:QUICKSTART_IOC_MODE="prod"; ./scripts/go.ps1 -C examples run ./quickstart-ioc

# 生产化模式自定义运行时长（秒）
$env:QUICKSTART_IOC_MODE="prod"; $env:QUICKSTART_IOC_RUN_SECONDS="5"; ./scripts/go.ps1 -C examples run ./quickstart-ioc
```

## 设计要点

1. `Register -> Build(ctx) -> Resolve -> Close(ctx)` 是推荐主流程。
2. 多实现单值字段按字段名分流（字段 `Primary` 命中 bean 名称 `Primary`）。
3. `slice/map` 字段会聚合同声明类型的全部实现。
4. `WithAllowCircularDependency(true)` 仅建议用于兼容场景，不建议默认开启。
5. `prod` 模式演示生命周期策略、错误聚合与信号优雅退出。
