# quickstart-config

`quickstart-config` 是一个“教学脚手架”示例，目标是让新手快速理解 `config` 模块最核心的运行语义。

## 入口结构

1. `main.go`：统一入口，通过环境变量切换模式。
2. `quickstart_config_entry.go`：轻量模式（最小闭环）。
3. `quickstart_config_full_entry.go`：全量模式（覆盖主要能力）。
4. `application.yaml`：本地文件层示例配置。

## 模式切换

1. `QUICKSTART_CONFIG_MODE=minimal`：轻量入口（默认）。
2. `QUICKSTART_CONFIG_MODE=full`：全量入口。

## 建议学习路径（10~15 分钟）

1. 先跑 minimal，看 `Build -> Snapshot.Decode -> Snapshot.Explain` 的最小闭环。
2. 再跑 full，看 `default/file/remote/pinned` 四层叠加后的最终值。
3. 观察 full 中触发 refresh 前后输出，确认“远端变化生效 + pinned 稳定覆盖”。

## 从仓库根目录运行

```powershell
# minimal（默认）
./scripts/go.ps1 -C examples run ./quickstart-config

# full（PowerShell）
$env:QUICKSTART_CONFIG_MODE="full"; ./scripts/go.ps1 -C examples run ./quickstart-config
```

## 能力与场景（按必选/可选/推荐）

1. `Build`：`[必须][推荐]`
说明：构建 `config.Manager` 主入口。
场景：所有使用 config 的程序都必须调用。
2. `Manager.Current`：`[必须][推荐]`
说明：取得当前已经校验并发布的不可变 Snapshot。
场景：需要从同一版本读取多个 key、解码和排查来源。
3. `Snapshot.Decode`：`[必须][推荐]`
说明：把快照解码到业务结构体。
场景：业务代码读取配置的主路径。
4. `Explain`：`[可选][推荐]`
说明：查看 key 的来源层级与优先级。
场景：配置值“为什么是这个值”的排障。
5. `config.Defaults`：`[可选][推荐]`
说明：提供默认兜底。
场景：本地开发、单测、远端异常降级。
6. `config.File/config.FilePath`：`[可选][按场景推荐]`
说明：引入本地文件层。
场景：本地调试、灰度开关、环境差异化配置。
7. `config.Provider(customProvider)`：`[可选][高级]`
说明：自定义 Provider 通过 Session 聚合 Client、Sources 和关闭语义。
场景：已有远端连接或接入尚未提供官方模块的配置中心。
8. `config.PinnedValues`：`[可选][按场景推荐]`
说明：最高优先级强覆盖。
场景：应急开关、故障期间临时绕过。
9. `config.SnapshotValidators`：`[可选][推荐]`
说明：快照发布前语义校验，校验失败则拒绝发布新快照。
场景：防止非法配置进入运行态。
10. `config.OnRefreshError`：`[可选][推荐]`
说明：刷新失败时输出错误上下文，便于观察失败路径。
场景：联调、演练、线上问题排障。
11. `config.Observe[T]`：`[可选][推荐]`
说明：业务通过 `Observer.Current` pull scope 的最新 typed 配置块。
场景：请求路径读取动态参数，不原地修改共享对象。
12. `config.Subscribe[T]`：`[可选][按场景推荐]`
说明：配置发布后 push typed 事件，支持首次回放和取消。
场景：需要副作用处理（热切换连接池、实时刷新限流器）。

## Full 模式的来源与最终结果

Full 模式在一个 Builder 中声明四层来源：

```text
default < file < remote < pinned
```

首次 Build 的关键值如下：

| 配置键 | default | file | remote | pinned | 最终值/来源 |
|---|---|---|---|---|---|
| `service.name` | `from-default` | `from-file` | `from-remote` | - | `from-remote` / remote |
| `service.address` | `:8080` | `:18080` | - | `:28080` | `:28080` / pinned |
| `worker.concurrency` | `2` | `8` | `16` | - | `16` / remote |
| `feature.enable_cache` | `false` | `false` | `true` | - | `true` / remote |

代码中的读取路径分别用于：

1. `manager.Current().Decode`：从同一个 Snapshot 读取整份最终配置。
2. `snapshot.Explain("service.address")`：确认 `service.address` 最终来自 pinned。
3. `workerObservation.Current()`：每次主动获取最新 `worker` typed 配置块。
4. `config.Subscribe[T]`：在配置发布后处理 typed 副作用事件。

## Full 模式重点观察

1. 初始阶段 `service.address` 来源应是 `pinned`。
2. 触发远端 refresh 后，`worker.concurrency` 会变化。
3. 即使远端更新，`service.address` 仍保持 pinned 值不变。
4. 快照版本号会从 `1` 递增到 `2`。
5. 随后会触发一次“失败路径”刷新（`worker.concurrency=0`）：
说明：会打印 `[refresh-error] ...`；Snapshot 和 Observer 保持上一发布版本，Subscriber 不会收到这次非法候选（版本号不增加）。
