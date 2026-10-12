# Architecture Overview (app + app-runtime + config + log + ioc)

## Usage Guides

1. [App Usage Guide](../guides/app-usage.md)：root、commands、flags、hooks 与测试。
2. [App Runtime Usage Guide](../guides/app-runtime-usage.md)：独立 Runtime、Config Options、Beans 与 Modules。
3. [IOC Usage Guide](../guides/ioc-usage.md)：注册、注入、生命周期与循环依赖。
4. [Web Hertz Usage Guide](../guides/web-hertz-usage.md)：Hertz 配置、原生扩展与优雅停止。
5. [App Web Hertz Usage Guide](../guides/app-web-hertz-usage.md)：配置/IOC、Controller/Filter 与 hlog 装配。

## Design Documents

1. `config-design.md`：配置内核设计与契约。
2. `app-design.md`：命令行框架与 CLI 适配设计。
3. `app-runtime-design.md`：config/log/ioc 运行时所有权与生命周期设计。
4. `log-design.md`：统一日志抽象与 log-zap 配置模型。
5. `ioc-design.md`：IOC 容器设计与 bean 生命周期语义。
6. `../testing/testing.md`：多模块测试策略与执行清单。
7. `errcode-design.md`：泛型错误码、错误链和治理边界。
8. `../guides/util-usage.md`：通用工具的安全使用与容量边界。
9. `web-hertz-design.md`：Hertz provider 构造流水线、状态机与并发停止语义。
10. `app-web-hertz-design.md`：应用 Web 装配流水线、hlog 租约与资源所有权。

## Dependency Direction

```text
main → app → app-runtime → config / log / ioc
  └────────→ app-log-zap → config / log / log-zap

application → app-web-hertz → config / ioc / Hertz hlog
                       └───→ web-hertz → Hertz
                                      └───→ web
```

1. `app` 是普通用户入口，提供 Run/Main、常用 Runtime 门面、命令、参数、help 和 hooks；Cobra/Pflag 是私有实现。
2. `app-runtime` 负责 ConfigManager、日志 provider、IOC 容器的构建、回滚和关闭。
3. `config` 负责配置来源、优先级、快照、热更新与 Explain。
4. `log` 负责日志统一契约与 provider SPI。
5. `ioc` 负责 bean 注册、注入、Init 和逆 Init 顺序 Close。
6. `app-log-zap` 通过结构化 `Logging` 契约接入，不依赖 app-runtime；禁止 `app-runtime → app-log-zap` 或反向依赖。
7. 禁止 `app-runtime → app`、`ioc → app-runtime`。

## Unified Runtime Semantics

1. Runtime 构建顺序：`config → log → ioc`。
2. Runtime 关闭顺序：`ioc → log → config`。
3. bean 依赖图是 Init/Close 顺序的唯一事实源。
4. borrowed 基础对象使用 `ioc.WithBorrowed()`，默认不注入字段且不由 IOC 执行 `Init/Close`。
5. Module 只通过 Registry 注册 bean，不定义第二套 Start/Stop。
6. app 的 Shutdown Hooks 与 Runner 全部收敛后才执行 Runtime Close；app 不感知 Runtime 内部 IOC 资源。

## Configuration Semantics

1. 运行时优先级：`CLI(pinned) > Remote > File > Default`。
2. 启动阶段优先级：`BootstrapOverrides > Env > ConfigFile.bootstrap > Default`。
3. bootstrap 仅用于启动，不进入运行时快照。
4. 动态 Provider 与已有连接适配器通过统一 Config Provider 入口在 `app-runtime.Application` 蓝图中注册。
5. app 只把私有 flags 转换为 `app-runtime.Input`。

## Multi-Module Layout

1. `app`：命令行应用启动门面，普通路径只需导入该模块。
2. `app-runtime`：高级运行时内核，可独立 Build/Close。
3. `config`、`config-nacos`、`config-consul`：配置内核与 provider。
4. `log`、`log-zap`、`app-log-zap`：日志契约、实现与 Runtime 适配。
5. `ioc`：轻量级 bean 容器。
6. `errcode`：泛型错误码载体与全链目标匹配。
7. `util`：标准库基础上的独立通用工具子包。
8. `examples`：端到端使用与编译验证。
9. `web`：只定义 Server 运行与优雅停止语义，不包含公共配置或具体框架类型。
10. `web-hertz`：实现 `web.Server`，独立拥有 Hertz 配置并保留原生 Middleware/Route 扩展。
11. `app-web-hertz`：从配置与 IOC 聚合应用 Controller/Filter，并管理可选 Hertz hlog 租约。

## Code Map

1. `app/` 对应 `app-design.md`。
2. `app-runtime/` 对应 `app-runtime-design.md`。
3. `config/` 对应 `config-design.md`。
4. `log/`、`log-zap/`、`app-log-zap/` 对应 `log-design.md`。
5. `ioc/` 对应 `ioc-design.md`。
6. `errcode/` 对应 `errcode-design.md`。
7. `util/` 对应 `../guides/util-usage.md`。
8. `examples/admin/`、`examples/quickstart-app/`、`examples/quickstart-web-hertz/` 用于端到端验证。
9. `web/server.go` 定义框架无关的 Server 生命周期契约。
10. `web-hertz/` 封装 Hertz 构造与单运行周期生命周期，模块入口见 `../../web-hertz/README.md`。
11. `app-web-hertz/` 负责应用装配与 hlog 租约，模块入口见 `../../app-web-hertz/README.md`。
