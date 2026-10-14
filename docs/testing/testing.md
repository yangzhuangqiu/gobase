# gobase 多模块测试规范

适用范围：

1. `config`（配置内核）
2. `config-nacos`（Nacos provider 与 SDK 适配）
3. `config-consul`（Consul provider 与 SDK 适配）
4. `app`（命令编排与配置接入）
5. `app-runtime`（config/log/ioc 生命周期内核）
6. `app-log-zap`（ConfigManager 与纯 Zap Provider 装配）
7. `ioc`（依赖注入容器）
8. `errcode`（泛型错误链语义）
9. `util`（通用工具与安全边界）

## 1. 第一性原理

1. 语义先行：测试是行为契约，不是实现细节快照。
2. 稳定先行：文档可过期，测试必须可执行且可回归。
3. 可观测先行：关键失败必须断言 `op/stage/reason/code`。
4. 最小惊讶：调用方最关心的路径必须有“用例级”测试。

## 2. 风险优先级（新增测试准入）

1. `P0`（发布阻断）：会导致错误配置生效、热更新失效、启动失败。
2. `P1`（提测阻断）：可观测性缺失、错误语义不可分支、关键生命周期未收敛。
3. `P2`（常规优化）：文档一致性、低影响兼容场景、可维护性改进。
4. 新增测试按风险驱动，不按数量驱动；同类风险优先补缺口而非复制整例。

## 3. 分层定义

1. 单元测试：纯函数和模块内语义，禁止真实外部依赖。
2. 用例测试：调用方视角验证公开 API 的“如何使用 + 预期结果”。
3. 回归测试：历史缺陷必须固化，防止同类问题复发。
4. 集成测试：连接真实系统（例如 Nacos），验证跨组件链路。
5. 性能测试：关键路径 benchmark，关注 `ns/op` 与 `allocs/op` 趋势。

## 4. 用例即契约（强制）

1. 公开 API 必须对应至少一个 `Test...` 用例测试。
2. 每个关键边界（无效输入、生命周期、并发取消）必须有失败用例。
3. 行为变更与修 bug 必须同 PR 更新用例/回归测试。
4. 无用例测试的行为说明不进入“完成态”。

## 5. 业务场景最佳实践

1. 以调用链路组织场景，不以源码文件组织场景。
2. 每个场景用 Given/When/Then 表达：
   - Given：输入来源与初始状态。
   - When：执行动作（Build/Observe/Run/Watch/Close）。
   - Then：断言结果值 + 来源元信息 + 生命周期行为。
3. 错误断言优先可分支语义：
   - 优先 `errors.Is` / `errors.As` / `errcode.IsCode`
   - 再断言 `stage=`、`reason=` 等稳定文本标记。
4. 用例测试避免依赖实现细节（内部字段、私有函数）。

## 6. 场景到测试映射（当前）

| 模块 | 核心场景 | 入口测试文件 |
| --- | --- | --- |
| `config` | Build/Read/Explain、Watch/Refresh、Close 收敛 | `config/usecase_test.go` + `config/manager_test.go` |
| `config-nacos` | Bootstrap 计划生成、SDK Get/Watch/Close | `config-nacos/usecase_test.go` + `config-nacos/sdk_client_test.go` |
| `config-consul` | Bootstrap 计划生成、SDK Get/Watch/Close | `config-consul/usecase_test.go` + `config-consul/sdk_client_test.go` |
| `app` | 命令输入、RuntimeMode 与 hooks 编排 | `app/runtime_input_test.go` + `app/command_lifecycle_test.go` + `app/shutdown_execution_test.go` |
| `app-runtime` | config/log/ioc 构建、回滚与关闭 | `app-runtime/runtime_test.go` |
| `ioc` | Register/Build/Resolve/Close 生命周期 | `ioc/usecase_test.go` |

### 6.1 config

1. 启动读取与解释：`Build -> Current -> Snapshot.Decode -> Snapshot.Explain`
   - `config/usecase_test.go:TestUseCaseBuildReadExplain`
2. 远端热更新传播：`Watch -> refresh -> Observe/Subscribe`
   - `config/usecase_test.go:TestUseCaseObserveWithWatcherRefresh`
3. 生命周期收敛：`Close` 后 watcher 不再发布新快照
   - `config/usecase_test.go:TestUseCaseCloseStopsWatcherEffect`
4. 内核边界与回归：
   - `config/builder_test.go`
   - `config/usecase_test.go`
   - `config/manager_test.go`
   - `config/manager_bench_test.go`

### 6.2 config-nacos

1. 典型接入：`Env -> Bootstrap -> Session`
   - `config-nacos/usecase_test.go:TestUseCaseBootstrapEnvToSession`
2. 兼容性边界：拒绝 legacy 字符串 source
   - `config-nacos/usecase_test.go:TestUseCaseValidateBootstrapRejectsLegacyStringSource`
3. 声明约束：仅连接器无 sources 失败
   - `config-nacos/usecase_test.go:TestUseCaseOpenRequiresSources`
4. SDK 核心行为：`Get/Watch/Close`
   - `config-nacos/sdk_client_test.go`
5. 参数与解析语义：
   - `config-nacos/sdk_options_test.go`
   - `config-nacos/provider_test.go`
   - `config-nacos/bootstrap_test.go`
6. 真实链路（可选）：
   - `config-nacos/integration_test.go`（`integration` tag）
   - token 缺失时可用 username/password 自动登录；测试结束会恢复原配置，原先不存在则删除测试项

### 6.3 config-consul

1. 典型接入：`Env -> Bootstrap -> Session`
   - `config-consul/usecase_test.go:TestUseCaseBootstrapEnvToSession`
2. 兼容性边界：拒绝 legacy 字符串 source
   - `config-consul/usecase_test.go:TestUseCaseValidateBootstrapRejectsLegacyStringSource`
3. 声明约束：仅连接器无 sources 失败
   - `config-consul/usecase_test.go:TestUseCaseOpenRequiresSources`
4. SDK 核心行为：`Get/Watch/Close`
   - `config-consul/sdk_client_test.go`
5. 参数与解析语义：
   - `config-consul/sdk_options_test.go`
   - `config-consul/provider_test.go`
   - `config-consul/bootstrap_test.go`
6. 真实链路（可选）：
   - `config-consul/integration_test.go`（`integration` tag）

### 6.4 app

1. 极简门面：包级 Run、Runtime 名称传播、高频 Runtime Option 和 Main 成功路径
   - `app/launcher_test.go:TestUseCasePackageRunUsesRuntimeFacade`
   - `app/launcher_test.go:TestUseCasePackageRunSupportsCommandOnlyApplication`
   - `app/launcher_test.go:TestUseCaseMainReturnsAfterSuccessfulRun`
2. Root 命令场景：CLI pinned 输入进入 app-runtime
   - `app/runtime_input_test.go:TestUseCaseCLIInputFlowsToRuntime`
3. 生命周期场景：Shutdown Hooks 与 Runner 收敛后才关闭 Runtime
   - `app/command_lifecycle_test.go:TestUseCaseRuntimeLivesThroughShutdownHooks`
   - `app/shutdown_execution_test.go:TestUseCaseHostCancellationRunsShutdownBeforeRunnerConverges`
4. 失败场景：before hook 失败仍关闭 Runtime
   - `app/command_lifecycle_test.go:TestUseCaseRuntimeClosesWhenBeforeHookFails`
5. 子命令场景：`RuntimeModeDisabled` 不构建 app-runtime
   - `app/command_lifecycle_test.go:TestUseCaseDisabledRuntimeCommand`
   - `app/shutdown_execution_test.go:TestUseCaseCommandCanDisableInheritedShutdownHooks`
6. 公共模型：输出与参数校验不依赖 Cobra 类型
   - `app/command_execution_test.go:TestUseCasePublicOutputWithoutCobraCommand`
   - `app/command_execution_test.go:TestUseCaseArgumentValidatorHidesCobra`
7. 进程信号场景：纯信号退出成功并恢复默认处理
   - `app/shutdown_execution_test.go:TestUseCaseRunTreatsSignalOnlyShutdownAsSuccess`

### 6.5 app-runtime

1. 基础运行时：config/log/ioc 构建与基础 bean 注入
   - `app-runtime/runtime_test.go:TestUseCaseBuildRuntime`
2. 构建回滚：Module 注册失败关闭已取得资源
   - `app-runtime/runtime_test.go:TestUseCaseBuildFailureClosesAcquiredResources`
3. 并发关闭：所有调用等待并返回首次结果
   - `app-runtime/runtime_test.go:TestUseCaseConcurrentCloseReturnsFirstResult`

### 6.6 ioc

1. 基础装配链路：`Register -> Build -> Resolve`
   - `ioc/usecase_test.go:TestUseCaseRegisterBuildResolve`
2. 多实现注入：按字段名分流 + `ResolveAll/ResolveByName`
   - `ioc/usecase_test.go:TestUseCaseInjectMultiByFieldName`
   - `ioc/usecase_test.go:TestUseCaseResolveAllAndByName`
3. 聚合注入：`slice/map` 与 map key 规则
   - `ioc/usecase_test.go:TestUseCaseInjectSliceAndMap`
4. 边界语义：缺失依赖、循环依赖、状态机约束
   - `ioc/usecase_test.go:TestUseCaseRequiredDependencyMissing`
   - `ioc/usecase_test.go:TestUseCaseCircularDependency`
   - `ioc/usecase_test.go:TestUseCaseBuildAndResolveStateGuard`
5. 生命周期收敛：Init 顺序、Close 逆序
   - `ioc/usecase_test.go:TestUseCaseInitAndCloseOrder`

### 6.7 app-log-zap

1. Runtime 装配与文件资源关闭
   - `app-log-zap/bootstrap_test.go:TestUseCaseAppLogZapBootstrapAndClose`
2. 运行期仅 level 热更新
   - `app-log-zap/bootstrap_test.go:TestUseCaseAppLogZapRuntimeLogLevelHotReload`
3. lifecycle context 只取消 subscription，显式 Close 才关闭核心
   - `app-log-zap/bootstrap_test.go:TestAppLogZapLifecycleContextCancelsSubscriptionWithoutClosingCore`
4. 分层错误码与失败回滚
   - `app-log-zap/bootstrap_test.go:TestRuntimeProviderBuildFailureRollsBackSubscription`
   - `app-log-zap/global/global_test.go:TestGlobalProviderBuildFailureUsesLayeredErrorCodes`

### 6.8 errcode

1. 泛型 code 提取与 cause 保留
   - `errcode/errors_test.go:TestNewAndCodeOf`
   - `errcode/errors_test.go:TestWrapAndUnwrap`
2. 同类型嵌套与 Join 多分支匹配
   - `errcode/errors_test.go:TestIsCodeTraversesNestedSameType`
   - `errcode/errors_test.go:TestIsCodeTraversesJoinedErrors`

### 6.9 util

1. 安全与边界测试按子包放置，例如 `crypto/aesx`、`randx`、`retryx`、`filex`。
2. 跨子包错误分支用例位于 `util/errorsx/usecase_test.go`。
3. 新增 Kind 时同时覆盖失败分支和 `errorsx.IsKind` 断言。

## 7. 执行命令

```powershell
# config
& ./scripts/go.ps1 -C config test ./...
& ./scripts/go.ps1 -C config vet ./...

# config-nacos
& ./scripts/go.ps1 -C config-nacos test ./...
& ./scripts/go.ps1 -C config-nacos vet ./...

# config-consul
& ./scripts/go.ps1 -C config-consul test ./...
& ./scripts/go.ps1 -C config-consul vet ./...

# app
& ./scripts/go.ps1 -C app test ./...
& ./scripts/go.ps1 -C app vet ./...

# app-runtime
& ./scripts/go.ps1 -C app-runtime test ./...
& ./scripts/go.ps1 -C app-runtime vet ./...

# app-log-zap
& ./scripts/go.ps1 -C app-log-zap test ./...
& ./scripts/go.ps1 -C app-log-zap vet ./...

# ioc
& ./scripts/go.ps1 -C ioc test ./...
& ./scripts/go.ps1 -C ioc vet ./...

# errcode
& ./scripts/go.ps1 -C errcode test ./...
& ./scripts/go.ps1 -C errcode vet ./...

# util
& ./scripts/go.ps1 -C util test ./...
& ./scripts/go.ps1 -C util vet ./...

# examples
& ./scripts/go.ps1 -C examples test ./...
& ./scripts/go.ps1 -C examples vet ./...

# error semantics gate (production code only)
& ./scripts/check-error-semantic.ps1
```

## 8. 门禁矩阵（建议）

| 阶段 | 阻断级别 | 最小执行项 |
| --- | --- | --- |
| 开发自测 | 本地阻断 | 变更模块 `test + vet` + `check-error-semantic` |
| 提测前 | CI 阻断 | 受影响模块及其直接依赖方执行 `test + vet`；错误语义范围内同时执行 `check-error-semantic` |
| 发布前 | 发布阻断 | 全模块 `test + vet` + `integration`（有环境时）+ `race`（CI 支持 CGO）+ `check-error-semantic` |

## 9. 去重门槛（新增测试前必做）

1. 先检索同类断言是否已存在，避免复制整例：
   - `rg -n "TestUseCase.*<场景关键字>" <module>`
   - `rg -n "<错误码|stage=|reason=>关键字" <module>`
2. 如果已有覆盖，优先补“缺失断言”而不是新增同构场景。
3. 同一语义只允许一个“主断言点”：
   - `usecase_test.go` 断言调用链是否可用。
   - `*_test.go` 断言边界矩阵、并发和错误语义细节。
4. PR 描述需写明“新增测试覆盖的是哪个风险缺口（P0/P1/P2）”。

## 10. 新增用例模板

1. 命名：`TestUseCase<场景><预期>`
2. 结构：
   - `Arrange`：构造调用输入（defaults/bootstrap/plan/flags）
   - `Act`：执行公开入口（Build/Run/Observe/Close）
   - `Assert`：值断言 + 可观测断言 + 生命周期断言
3. 断言最小集合：
   - 业务值正确
   - 来源/错误语义可解释
   - 资源生命周期可收敛

## 11. 测试组织最佳实践

1. 命名统一：
   - 业务调用链测试统一放 `usecase_test.go`，函数统一 `TestUseCase...`。
2. 分工边界：
   - `usecase_test.go`：验证“调用方完整链路”是否可用（冒烟 + 关键业务场景）。
   - `*_test.go`：承载内部并发、错误链路、回归与能力矩阵细节语义。
3. 允许少量重叠但避免重复实现：
   - 重叠仅用于跨层保障（例如 usecase 的链路冒烟 + internal 的矩阵覆盖）。
   - 禁止把同一断言细节在多个层级逐字复制。
4. 变更驱动策略：
   - 新增公开能力：先补 `usecase`，再补内部矩阵或回归（按风险深度）。
   - 修复线上缺陷：必须补回归测试，并在文档“场景到测试映射”登记。

## 12. PR 模板（可复用）

1. PR 自检可直接使用：[pr-checklist.md](pr-checklist.md)
2. 建议在每个测试相关 PR 中粘贴该清单并逐项勾选，减少“规则只在文档里”的执行偏差。
