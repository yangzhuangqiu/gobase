# App Design

`app` 的稳定语义是命令行应用启动门面：普通用户只导入 app，高级用户才下钻 app-runtime。Cobra/Pflag 是私有实现；config、log、ioc 的运行时所有权由 `app-runtime` 承担。

本文只定义边界、生命周期和设计不变量。入门摘要见 [app README](../../app/README.md)，任务型用法见 [App Usage Guide](../guides/app-usage.md)。

## Design Scope and Goals

### In Scope

1. root/subcommand 命令树、help、version 与参数校验。
2. `FlagBinder` 的 Bind，以及可选的 Complete、ValidateFlags。
3. bootstrap/pinned/普通业务 flags 分类。
4. Runtime Build 前输入准备，以及 Runtime 存活期间的 Before/Runner/Shutdown 编排。
5. 通过 `ExecuteContext(ctx, args)` 提供可测试公共入口。
6. 通过 `Run/Main` 与高频 Runtime Option 提供约定优先的极简入口。
7. 进程信号、宿主取消、Runner 结果与 Runtime Close 的统一生命周期。

### Out of Scope

1. ConfigManager、日志 provider 和 IOC 容器资源所有权。
2. Bean Definitions/Modules 与 bean 生命周期。
3. Cobra/Pflag 公共类型与原生 Command 扩展。
4. HTTP、gRPC、Consumer 等具体框架的工作负载 controller。

## Core Objects

1. `App`：命令行入口，私有持有 Cobra root 和 `app-runtime.Application` 蓝图。
2. `Command`：稳定业务子命令描述，使用 `ArgumentValidator`、`RuntimeMode` 与 `CommandRunner`。
3. `CommandContext`：命令名、路径、参数快照。
4. `CommandFlags`：业务 flags 抽象，隔离 Pflag。
5. `FlagBinder`：调用 CommandFlags 声明 Flag，并绑定普通 Flag 的业务字段。
6. `FlagCompleter/FlagValidator`：Runtime Build 前的可选补全与校验能力。
7. `BeforeRunHook`：Runtime Build 后、Runner 前的不产生长期工作负载的准备切面。
8. `ShutdownHook`：Runner 收敛期间停止主动工作负载的应用级契约。
9. `Runtime/Module`：app 自有只读 Runtime 接口与 app-runtime Module 别名。

## Dependency Direction

```text
main → app → app-runtime → config / log / ioc
```

`app-runtime` 禁止 import `app`；`app` 导出签名禁止包含 Cobra/Pflag 类型。

## Core Design Choices

1. **稳定模型与 CLI 实现隔离**：公共层使用 `Command`、`CommandContext`、`CommandFlags`，Cobra/Pflag 只在包内完成解析与渲染。代价是不能直接透传 Cobra 的任意扩展能力。
2. **命令编排与资源内核分离**：app 决定何时 Build、执行 hooks/Runner、何时 Close；app-runtime 决定 config/log/ioc 如何构建、回滚和关闭。这样避免两层同时拥有资源。
3. **默认启用 Runtime，显式整体禁用**：业务命令默认使用 `RuntimeModeEnabled`；纯 CLI 命令显式选择 `RuntimeModeDisabled`。当前不提供“只开 config、关闭 log/ioc”等容易产生所有权歧义的组合。
4. **高频门面与高级下钻并存**：常用能力通过 app Option 暴露，高级 provider、Plan、IOC policy 继续由 app-runtime 定义。`WithRuntime` 与 `WithRuntimeOptions` 互斥，避免两份蓝图来源竞争。
5. **主动工作负载先于 Runtime 关闭**：app 并发协调 Shutdown Hooks 与 Runner 收敛，二者全部完成后才调用 Runtime Close；app 不检查 Runtime 内部是否使用 IOC，也不参与其资源排序。

## Command Lifecycle

完整 Runtime 命令：

1. 解析 Cobra/Pflag 输入，并在进入 app 生命周期前执行 `ArgumentValidator`。
2. 对 root FlagBinder 执行 Complete，再执行 ValidateFlags；子命令拥有不同 binder 时按相同顺序继续执行。
3. 构造 `app-runtime.Input`，区分 bootstrap、pinned 与普通业务 flags。
4. 调用 `Application.Build`；失败回滚由 app-runtime 负责，app 不再调用 Close。
5. 按声明顺序执行全部 BeforeRunHooks，并聚合本阶段错误。
6. before 阶段无错误时启动业务 Runner；Runner 结果、宿主取消或进程信号均可触发 Shutdown。
7. 取消 run context，并发执行全部 Shutdown Hooks，同时等待 Runner 完全退出。
8. Shutdown Hooks 与 Runner 都收敛后，使用独立 deadline context 关闭 Runtime。
9. 聚合 Runner、宿主 cause、Shutdown Hook 与 Runtime Close 错误；纯进程信号且其他阶段成功时返回 nil。

before 阶段任一 hook 失败后不执行 Runner/Shutdown Hooks，但同阶段其余 BeforeRunHooks 仍会执行，Runtime 仍会关闭。Shutdown Hook 返回时必须保证其工作负载已经停止访问 Runtime；完整不变量见 [App 生命周期管理](app-lifecycle-design.md)。`App` 持有可变 CLI 解析状态，不保证并发执行安全；相互隔离的执行应使用不同 App 实例。

## Runtime Modes

1. `RuntimeModeEnabled`：默认模式，构建 config/log/ioc。
2. `RuntimeModeDisabled`：用于 doctor 等仍需 Runner 的纯 CLI 命令，Runner 收到 nil Runtime，且不执行 BeforeRunHooks；应用 Shutdown Hooks 默认仍执行并收到 nil Runtime。

help、version、bootstrap schema 输出属于 Runner 之前的 CLI 分支，不依赖 RuntimeMode，也不构建 Runtime。command-only 应用未声明 root Runner 时，root 空执行直接展示 help。

## CLI Integration Semantics

1. bootstrap flags：`--config`、`--bootstrap`、`--bootstrap-env-prefix`、`--bootstrap-without-config-file`。
2. pinned flags：显式 `Config*` flags 与可选 `--override`。
3. 普通业务 flags：通过 `CommandFlags` 无前缀方法声明，不进入 Runtime config。
4. `--bootstrap config=...` 与 `--config` 同时出现但值不一致时 fail-fast；值相同则归一为单一 bootstrap 输入。
5. `--list-bootstrap-keys` 汇总 config core 与 Runtime provider schema。
6. 输出通过 `WithOutput/WithErrorOutput` 注入，不暴露底层 Command。
7. bootstrap 环境变量前缀为空时，由 Runtime application name 转为大写下划线形式。
8. pinned 层内先应用 `--override`，再以显式 `Config*` flags 覆盖同名 key；普通业务 flags 不进入 `app-runtime.Input.PinnedValues`。

## Public Contracts

1. `app.Main(options...)`：进程级极简入口，统一错误输出和失败退出码。
2. `app.Run(options...)`：常规入口，返回错误给 main。
3. `App.Run/ExecuteContext(ctx, args)`：实例级入口；Run 安装进程信号，ExecuteContext 只服从宿主 context；均不保证同一实例并发执行安全。
4. `WithConfig/WithLogging/WithBeanDefinitions/WithModules`：按能力聚合的 Runtime 门面。
5. `WithRuntimeOptions`：中级 Runtime 扩展口。
6. `WithRuntime`：完整运行时蓝图替换口。
7. `WithRunner`：两参数 root runner。
8. `WithFlagBinder`：root FlagBinder；子命令通过 `Command.FlagBinder` 独立声明 Flag。
9. `WithCommands`：保留 CommandContext 的子命令集合；command-only 应用无需 root Runner。
10. `WithBeforeRunHook`：Runner 前的可回滚准备。
11. `WithShutdown(...)`：进程信号、Shutdown/Runtime Close deadline 与应用 Shutdown Hooks。

## Boundary Invariants

1. app 导出签名不包含 Cobra/Pflag 类型。
2. App 不直接暴露 ConfigManager、Logger 或 IOC 构建方法，业务通过 `app.Runtime` 使用运行能力。
3. app 只编排命令生命周期，不实现第二套 bean Start/Stop。
4. 高频 Runtime Option 可以由 app 提供门面，但资源所有权和实现必须留在 app-runtime。
5. 高级 CLI 原生扩展应进入独立适配层，不能反向污染稳定公共模型。
6. Runtime Build 成功后由 app 负责且只负责调用一次 Close；Build 失败回滚和 Runtime 内部关闭顺序属于 app-runtime。
7. `RuntimeModeDisabled` 的 Runner 必须把 Runtime 视为 nil，不能依赖 Runtime hooks 提供前置条件。
8. app 不发现 IOC bean 或推断内部资源顺序；它只借用不透明 Runtime，并在应用工作负载停止后调用 Runtime Close。

## Error Semantics

| 来源 | 稳定结果 |
|---|---|
| app 声明冲突、nil App、未知内部失败 | `app.internal` |
| app 自身 CLI 表达式、flag 读取、冲突、schema mode 错误 | `app.cli.*` |
| root/command 缺少 Runner | `app.lifecycle.runner_required` / `app.lifecycle.command_runner_required` |
| `ArgumentValidator` | Runtime Build 前直接返回原业务错误 |
| FlagBinder 扩展、hooks、Runner | 保留原错误链，并由 app 追加执行 stage；无稳定业务码时外层可能归为 `app.internal` |
| `ExecuteContext` 宿主取消 | 完整收敛后返回 `context.Cause(ctx)`；Runner 的纯取消错误不重复返回 |
| `Run/Main` 进程信号 | 其他阶段成功时视为正常退出；业务/关闭错误仍保留 |
| app-runtime Build/Close | 保留 `app_runtime.*` 错误码，app 追加命令 stage |

错误聚合遵循以下规则：

1. Complete 与 ValidateFlags 的非 nil 错误聚合返回，stage 分别为 `complete_flags`、`validate_flags`。
2. 同一 before 阶段及所有 Shutdown Hooks 的非 nil 错误分别聚合返回。
3. before 阶段失败与 Close 错误聚合；Runner 和 Shutdown Hooks 均不执行。
4. Runner、宿主 cause、Shutdown Hooks 与 Close 错误聚合，调用方可通过 `errors.Is/As` 检查原始 cause。
5. 一个 Shutdown Hook 失败或 panic 不跳过其他 Hooks 与 Runtime Close。
6. `Main` 输出最终错误并固定以状态码 1 退出；`Run/ExecuteContext` 只返回错误。

## Context, Concurrency, and Ownership

1. nil 执行 context 会被替换为 `context.Background()`；run context 传给 Runtime Build、BeforeRunHooks 和 Runner，并在 Shutdown 时取消。
2. Shutdown Hooks 使用从执行 context 去除取消状态后创建的独立 graceful deadline；默认 15 秒。
3. Runtime Close 再使用另一个独立 deadline context；默认 10 秒，deadline 只提供协作式取消，不强制终止 goroutine。
4. Hooks 并发执行；声明顺序只稳定错误聚合，不提供先后保证。有依赖的动作必须封装在一个 Hook 中。
5. Runner 保持在命令调用 goroutine；宿主/信号触发监听与 Shutdown Hooks 在独立 goroutine 执行，以解除阻塞型 Runner。
6. `App` 内部缓存 Cobra command 与 flag 状态，因此不承诺并发安全。`app-runtime.Application` 是可复用蓝图，但每次命令执行产生的 Runtime 资源彼此独立。
7. app-runtime 持有 config manager、log provider 与 IOC container；app 只持有本次 Runtime 的关闭责任；Runner/Hooks 借用 Runtime，不取得所有权。

## Observability

1. app 自身不主动写业务日志；`Main` 仅在失败时向 error output 写最终错误。
2. app 生成的生命周期包装文本使用 `op=app.command result=failed stage=... reason=...`；部分稳定 lifecycle 错误和调用方原始错误不保证同时包含全部字段。
3. Runtime 的 `app_runtime.*` 错误码和 cause 会保留在错误链中，供日志层或进程入口分支处理。
4. 业务观测应在 BeforeRunHook、Shutdown Hook 或 Runner 中通过 Runtime logger 输出 `op/result/reason`，不得记录 token、凭据或完整敏感参数。

## Alternatives and Trade-offs

1. **直接暴露 `*cobra.Command`**：扩展成本最低，但会把 CLI 实现固化为公共契约，因此未采用。
2. **app 直接构建 config/log/ioc**：调用链更短，但会复制 app-runtime 的回滚与所有权逻辑，因此未采用。
3. **为每种资源提供独立 Runtime 开关**：表面更灵活，但会产生大量不完整组合与 hook 前置条件，因此当前只保留 Enabled/Disabled。
4. **关闭时沿用命令 context**：取消传播更直接，但命令取消会使资源来不及收敛；当前为 Shutdown 与 Runtime Close 分配独立 deadline。
5. **app 直接管理 IOC bean 的关闭顺序**：可自动定位 Server，但会跨越 Runtime 所有权并复制资源图，因此只保留不透明 Runtime Close 边界。

## Open Gaps / Future Improvements

1. 多级嵌套 Commands 尚无公共递归模型。
2. `Main` 的错误到退出码映射固定为 1。
3. app 层尚未提供内建 metrics/tracing 观测点。
4. 官方 HTTP/gRPC/Hertz 工作负载 adapter 尚未提供；当前由业务实现 Shutdown Hook 强契约。

App 生命周期横跨命令准备、Runner、主动工作负载、Runtime 借用/关闭和进程信号；完整语义见 [App 生命周期管理](app-lifecycle-design.md)。

## File Map

1. `app/app.go`：App 实例、执行入口与内部状态。
2. `app/launcher.go`：包级 Run/Main 极简入口。
3. `app/runtime_facade.go`：高频 Runtime 门面。
4. `app/options.go`：App Option 与 Runtime 蓝图注入。
5. `app/command.go`：Runner、Command 与 RuntimeMode 公共命令模型。
6. `app/command_builder.go`、`app/command_execution.go`：私有 Cobra 构建与命令事务。
7. `app/command_flags.go`：CommandFlags 契约与 Pflag 适配。
8. `app/flag_binder.go`、`app/flag_completer.go`、`app/flag_validator.go`：业务 Flag 绑定、补全与校验契约。
9. `app/flag_metadata.go`：业务/framework Flag 的内部 annotation 分类。
10. `app/command_context.go`：公共 CommandContext 的私有快照实现。
11. `app/runtime_input.go`、`app/runtime_input_bootstrap.go`、`app/runtime_input_pinned.go`、`app/runtime_input_override.go`：Runtime Input 与覆盖优先级采集。
12. `app/command_bootstrap_schema.go`：bootstrap schema 输出。
13. `app/help.go`：私有 help 渲染。
14. `app/errors.go`：稳定错误码与 stage 包装。
15. `app/before_run_hook.go`、`app/shutdown_hook.go`：生命周期公共契约及紧邻执行逻辑。
16. `app/shutdown.go`、`app/shutdown_option.go`、`app/shutdown_execution.go`：Shutdown 模型、声明与单次执行协调器。
17. `app/process_signal.go`：进程信号订阅、恢复与二次信号边界。
18. `app/launcher_test.go`、`app/runtime_facade_test.go`：普通路径与 Runtime 门面测试。
19. `app/command_execution_test.go`、`app/command_lifecycle_test.go`、`app/shutdown_execution_test.go`：命令与 Shutdown 生命周期测试。
20. `app/runtime_input*_test.go`、`app/flag_binder_test.go`：Runtime 输入与 Flag 规则测试。
21. `app-runtime/`：config/log/ioc 运行时内核。
22. `examples/quickstart-app/`：端到端示例。
