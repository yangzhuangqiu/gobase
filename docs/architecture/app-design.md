# App 命令节点设计

## 1. 目标与边界

`app` 把 root 与任意深度子命令统一建模为命令节点。App 只提供进程级默认值；节点声明本地能力；执行前由字段级归并器冻结 `executionPlan`。

核心目标：

1. root 和子命令使用相同的 Option 构建体验。
2. `Module` 是所有命令唯一的装配入口；实现 `WorkloadModule` 时同时贡献主动执行单元，一次性函数由 `Runner` 适配。
3. 每类能力都有显式继承、追加、覆盖或局部规则。
4. 命令树/Flag 只构建一次，每次执行只切换 `executionSession`。
5. 公共模型不泄漏 Cobra，也不把控制状态藏进 `context.Value`。

Runtime 内部的 config/log/IOC 构建、资源依赖关系与关闭顺序不属于 Command 模型。

## 2. 构建契约

```go
type Option interface { /* App only */ }
type CommandOption interface { /* Command only */ }
type NodeOption interface {
    Option
    CommandOption
}

func New(options ...Option) *App
func NewCommand(use string, options ...CommandOption) Command
func WithCommands(commands ...Command) NodeOption
```

Option 分类是编译期语义约束：

- App 级：名称、版本、输出、Runtime 蓝图、进程信号与默认 Shutdown Policy。
- Node 级：描述、参数规则、Runtime 需求、FlagBinder、Bean Definitions、Module、Hook、子命令。
- Command 级：关闭预算覆盖、清空祖先 Shutdown Hook。

`Command` 不公开可直接写入的字段，防止调用方绕过 Option 校验和归并规则。

## 3. 节点归并模型

选中路径设为 `root → n1 → ... → leaf`：

| 字段 | 归并方式 | 原因 |
|---|---|---|
| 描述/别名/参数校验 | leaf 局部 | 只描述当前 CLI 节点 |
| Runtime 需求 | 最近显式值 | 子节点可以通过语义 Option 覆盖进程默认行为 |
| FlagBinder | leaf 局部 | Flag 的声明、补全和校验必须同域 |
| Bean Definitions | leaf 局部 | 选择命令只构建该节点私有对象，避免无关资源和失败域泄漏 |
| Modules | leaf 局部 | 选择命令只构建该节点模块，并只执行其贡献的 Workload |
| GlobalBeforeRunHooks | App 全局 | 审计、权限校验等跨命令策略对每次实际执行生效 |
| BeforeRunHooks | leaf 局部 | 局部准备只依赖选中命令实际拥有的对象图 |
| ShutdownHooks | leaf 局部 | 补偿动作必须与选中命令实际拥有的资源和 Workload 对齐 |
| ShutdownBudget | 每个字段最近显式值 | 允许只覆盖一个预算，不意外遮蔽另一个 |
| Commands | 递归子树 | 分组节点和叶子节点统一 |

字段级规则由 `executionPlan` 固化，不依赖 Option 调用顺序的隐式副作用。

Bean Definitions 与 Modules 的默认边界都是节点局部。只有明确需要对每个 Runtime 命令生效的
基础对象，才通过 `WithRuntimeOptions(appruntime.WithBeanDefinitions(...))` 进入稳定 Runtime 蓝图；
该入口是高级逃生口，不形成命令树继承关系。

## 4. Module 与 Workload 两阶段执行模型

```go
type Module interface {
    Name() string
    Register(ioc.Registry) error
}

type WorkloadModule interface {
    Module
    Workloads() []Workload
}

type Workload interface {
    Name() string
    Run(context.Context, Runtime, CommandContext) error
    Shutdown(context.Context, Runtime, CommandContext) error
}

type RunFunc func(context.Context, Runtime, CommandContext) error

func Runner(run RunFunc) Module
```

`Runner` 的名称固定为 `runner`，它是不注册 Bean 的 workload-only Module；其 Shutdown 无副作用，适合自行完成的一次性任务。Server/Consumer/Scheduler Module 在 `Register` 阶段声明依赖图，在 `Workloads` 阶段返回纯内存生命周期适配器。

root 与所有子命令都走同一个执行函数：没有 root runner、subcommand runner 两套路径。执行计划先收集并校验选中节点 Module 的 Workload，再把选中节点 Bean Definitions 与 Modules 注入本次 Runtime Build。两者都不继承。分组节点可以没有 Workload，此时直接执行节点会展示 help；没有子命令的叶子节点必须由 Module 贡献至少一个 Workload。

## 5. FlagBinder 作用域

`FlagBinder.Bind` 在缓存命令树首次构建时，对每个声明 Binder 的节点调用一次。顺序复用同一 App 不重新声明 Flags。

解析后只对选中节点的 Binder 执行：

1. `Complete()`；
2. `ValidateFlags()`。

祖先业务 Binder 不参与子命令执行。进程级 config/bootstrap/version flags 由 root 作为 framework persistent flags 提供，不属于业务 Binder 继承。

## 6. Runtime 与配置

Runtime 需求：

- root 默认需要 Runtime；普通命令不需要声明任何 Option；
- 子命令未声明时自动继承最近祖先；内部使用未声明/需要/禁用三态完成归并；
- `WithoutRuntime()` 声明当前节点及未覆盖子节点不构建 Runtime；
- `RequireRuntime()` 仅用于在禁用 Runtime 的子树中恢复完整 Runtime；
- 不构建 Runtime 时仍执行 GlobalBeforeRunHook 与选中节点 BeforeRunHook；Hook/Workload/ShutdownHook 收到 nil Runtime。

Shutdown duration 优先级：

```text
Command path budget > App ShutdownPolicy > ConfigManager startup snapshot > built-in default
```

Signals 是 Runtime Build 前的进程控制输入，只能由 App `WithShutdownPolicy` 显式声明。

## 7. 执行与缓存

```text
New/NewCommand options
  -> build recursive command tree once
  -> bind framework and node flags once
  -> each Execute binds executionSession
  -> selected callback uses frozen executionPlan
  -> restore previous session binding
```

App 持有 Cobra 的可变解析状态，不支持并发执行同一个实例；顺序执行受支持。并发隔离需要不同 App 实例。

## 8. 错误与可观测性

- 叶子缺少 Workload：`app.lifecycle.workload_required`。
- 声明错误在 Runtime Build 前以 `validate_app_setup` 失败。
- 生命周期错误包含 `op=app.command result=failed stage=... reason=...`。
- 底层可分支错误通过 `errors.Is/As` 保留。
- 运行、取消、Shutdown 和 Close 错误在资源安全边界后聚合。

## 9. File Map

| 文件 | 责任 |
|---|---|
| `app/command.go` | 不可变 Command 与局部配置模型 |
| `app/option.go` / `command_option.go` / `node_option.go` | 三类公共 Option 合同 |
| `app/options.go` | Option 实现和节点声明入口 |
| `app/command_builder.go` | 递归命令树构建 |
| `app/execution_plan.go` | 字段级归并规则 |
| `app/command_execution.go` | 统一命令执行流程 |
| `app/workload_module.go` | 两阶段 Module 合同与 Workload 收集校验 |
| `app/flag_binder.go` | Binder 执行期规则 |
| `app/command_inheritance_test.go` | root/一级/多级继承矩阵证据 |
