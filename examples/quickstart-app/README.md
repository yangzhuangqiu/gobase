# quickstart-app

`quickstart-app` 合并了两个入口实现：

1. 轻量入口：`quickstart_app_entry.go`（5 分钟上手）。
2. 全量入口：`quickstart_app_full_entry.go`（覆盖 `app.Run` 门面与 app-runtime 高级扩展点）。

`main.go` 是统一入口，通过环境变量切换：

1. `QUICKSTART_MODE=minimal`：轻量入口（默认值）。
2. `QUICKSTART_MODE=full`：全量入口。

## 从仓库根目录运行

```powershell
# 查看参数帮助
./scripts/go.ps1 -C examples run ./quickstart-app --help

# 轻量入口（默认）
./scripts/go.ps1 -C examples run ./quickstart-app
./scripts/go.ps1 -C examples run ./quickstart-app --config ./quickstart-app/application.yaml
./scripts/go.ps1 -C examples run ./quickstart-app --override service.address=:20080 --override worker.concurrency=32

# 全量入口（PowerShell）
$env:QUICKSTART_MODE="full"; ./scripts/go.ps1 -C examples run ./quickstart-app --bootstrap mockdemo.enabled=true
$env:QUICKSTART_MODE="full"; ./scripts/go.ps1 -C examples run ./quickstart-app explain --key service.address
$env:QUICKSTART_MODE="full"; ./scripts/go.ps1 -C examples run ./quickstart-app doctor
```

## 全量入口覆盖内容

1. `app.WithConfig/WithLogging/WithModules`、`WithFlagBinder`、hooks、subcommands、`RuntimeModeDisabled`。
2. `app.WithConfig(config.Provider(...))` 逐项注册动态或已有连接适配的 Provider。
3. `CommandFlags` 全类型参数声明。
4. `app.WithXxx` 处均标注了“必须/可选”和“是否推荐”。
5. 演示 `ioc.assemble.go + ioc.gen.go + app-runtime.Module` 的应用装配。
6. full 模式通过 `app.WithConfigBinding[T]` 把 `appruntime.ConfigValue[T]` 注入业务 Worker；Runner 只在 composition root 解析 bean。

## Nacos/Consul 示例

1. `quickstart_app_full_entry.go` 提供 Nacos/Consul Provider 注册模板；启用时取消 import 与对应 `config.Provider(...)` 注释，并在应用模块引入 provider 依赖。
2. `application.yaml` 使用当前 `sources[].groupId/dataId` 契约提供注释态 bootstrap 示例；默认不连接真实远端。标准部署只需声明 `address`，自动使用 `http`、`/nacos` 和每个 HTTP 端口 `+1000` 的 gRPC 端口。
3. 注册 Provider 本身不会连接远端，只有 bootstrap 中 connector 与 Sources 配置完整时才会构建远端 Plan。
4. `bootstrap.mockdemo.enabled=true` 仅在 `QUICKSTART_MODE=full` 下生效，因为 mockdemo Provider 只在全量入口注册。

### Nacos 配置分层与默认值

1. `servers` 非空时整体覆盖 `address`；每个节点可独立声明 `host/port/scheme/contextPath/grpcPort`。
2. server 节点中 `host` 必填；`port`、`scheme`、`contextPath` 默认分别为 `8848`、`http`、`/nacos`，`grpcPort` 省略时按该节点 `port + 1000` 推导。
3. `timeoutMs` 是常规配置，单位为毫秒，默认值为 `10000`。
4. `logDir`、`cacheDir`、`logLevel` 是高级 SDK 配置，普通应用可省略；默认值依次为 `<系统临时目录>/nacos/log`、`<系统临时目录>/nacos/cache`、`warn`。
5. `logLevel` 仅接受小写的 `debug`、`info`、`warn`、`error`，其他值会在 bootstrap 校验阶段失败。
6. Nacos SDK 的日志配置是进程级全局配置：同一进程内以首个创建的 Client 为准，后续 Client 的 `logDir`、`logLevel` 不保证生效。
7. 默认缓存目录会被同一机器上的进程共享。依赖本地缓存回退，或同时连接不同环境、集群时，应为每个应用和集群配置独立的 `cacheDir`，避免同名 namespace/group/dataId 的缓存互相覆盖。
8. 示例给出的 `./.runtime/quickstart-app/nacos/...` 是相对于进程工作目录的本地开发路径；生产环境应替换为应用可写且按应用、集群隔离的稳定目录。
