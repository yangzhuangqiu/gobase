# quickstart-log

`quickstart-log` 合并了四个入口实现：

1. 轻量入口：`quickstart_log_entry.go`（5 分钟上手）。
2. 全量入口：`quickstart_log_full_entry.go`（覆盖 `app + config + log` 常见配合方式）。
3. 非 `app` 轻量入口：`quickstart_log_plain_entry.go`（仅 `log + log-zap`）。
4. 非 `app` 全量入口：`quickstart_log_plain_full_entry.go`（手工读取 YAML + 动态调级）。

`main.go` 是统一入口，通过环境变量切换：

1. `QUICKSTART_LOG_MODE=app-entry`：`app` 轻量入口（默认值）。
2. `QUICKSTART_LOG_MODE=app-full`：`app` 全量入口。
3. `QUICKSTART_LOG_MODE=plain-entry`：非 `app` 轻量入口。
4. `QUICKSTART_LOG_MODE=plain-full`：非 `app` 全量入口。

## 从仓库根目录运行

```powershell
# 查看参数帮助
./scripts/go.ps1 -C examples run ./quickstart-log --help

# 轻量入口（默认）
./scripts/go.ps1 -C examples run ./quickstart-log
./scripts/go.ps1 -C examples run ./quickstart-log --config ./quickstart-log/application.yaml
./scripts/go.ps1 -C examples run ./quickstart-log --enable-set --set log.level=debug

# app 全量入口（PowerShell）
$env:QUICKSTART_LOG_MODE="app-full"; ./scripts/go.ps1 -C examples run ./quickstart-log
$env:QUICKSTART_LOG_MODE="app-full"; ./scripts/go.ps1 -C examples run ./quickstart-log --log.level=debug --emit-debug=true
$env:QUICKSTART_LOG_MODE="app-full"; ./scripts/go.ps1 -C examples run ./quickstart-log explain --key log.level
$env:QUICKSTART_LOG_MODE="app-full"; ./scripts/go.ps1 -C examples run ./quickstart-log doctor

# 非 app 轻量入口（仅 log/log-zap）
$env:QUICKSTART_LOG_MODE="plain-entry"; ./scripts/go.ps1 -C examples run ./quickstart-log

# 非 app 全量入口（手工读取配置 + 可选动态调级）
$env:QUICKSTART_LOG_MODE="plain-full"; ./scripts/go.ps1 -C examples run ./quickstart-log --config ./quickstart-log/application.yaml --emit-debug=true --dynamic-level=debug
```

## 设计要点

1. 业务层推荐只记三句口诀：`NewProvider -> Get -> Close`。
2. `app` 入口下业务层通过 `app.Runtime.Logger(this)` 写日志，无需导入 app-runtime，也无需手工初始化与关闭。
3. `app-runtime` 在 config 后初始化日志，并在 IOC 后、config 前关闭日志 provider。
4. provider 的运行期更新只支持 level，其余字段按启动期语义处理。
5. `app-log-zap` 默认读取 `log.zap` scope；本示例为了与非 app 入口共用 `application.yaml`，显式传入 `applogzap.WithScope("log")`。
6. 非 `app` 场景推荐使用 `logzap.NewProvider(cfg)`；需要动态调级时由宿主显式调用 `ApplyRuntimeConfig(logzap.RuntimeConfig{Level: ...})`。

## 配置说明

1. `application.yaml` 提供了 console 示例（默认可直接运行）。
2. 文件日志示例以注释给出，取消注释后可观察落盘输出。
3. request/user 等调用链信息以结构化 Field 显式记录；生产装配可使用 typed ContextExtractor。
4. app 两个入口通过 `applogzap.WithScope("log")` 让 `log.*` 同时驱动 Runtime provider；因此配置文件和 `--log.level=debug` 等 Config Flag 会实际改变日志行为。
5. 新应用仍推荐使用默认 `log.zap` scope；只有需要兼容既有 `log.*` 配置时才覆盖 scope。

## 相关文档

1. [Log 使用指南](../../docs/guides/log-usage.md)
2. [App 使用指南](../../docs/guides/app-usage.md)
3. [Log 架构设计](../../docs/architecture/log-design.md)
4. [log 模块 README](../../log/README.md)
