# gobase

`gobase` 是一个多模块 Go 基础库仓库。当前模块：

1. `github.com/yangzhuangqiu/gobase/config`：配置内核（加载、合并、快照、热更新、Explain）。
2. `github.com/yangzhuangqiu/gobase/config-nacos`：Nacos provider 与 SDK 适配。
3. `github.com/yangzhuangqiu/gobase/config-consul`：Consul provider 与 KV blocking query SDK 适配。
4. `github.com/yangzhuangqiu/gobase/app`：不泄漏 Cobra/Pflag、提供 Run/Main 与常用 Runtime 门面的命令行应用入口。
5. `github.com/yangzhuangqiu/gobase/app-runtime`：按需暴露、统一托管 config、log、ioc 的高级应用运行时内核。
6. `github.com/yangzhuangqiu/gobase/app-log-zap`：app-runtime 与 log-zap 的生命周期装配适配。
7. `github.com/yangzhuangqiu/gobase/app-observe-webhertz`：Web 请求完成态到结构化日志/Prometheus 的按需适配。
8. `github.com/yangzhuangqiu/gobase/log`：统一日志抽象与 SPI 接入。
9. `github.com/yangzhuangqiu/gobase/log-zap`：zap provider 实现模块。
10. `github.com/yangzhuangqiu/gobase/hlog-zap`：Hertz hlog 与 log-zap 集成模块。
11. `github.com/yangzhuangqiu/gobase/ioc`：轻量级依赖注入容器模块。
12. `github.com/yangzhuangqiu/gobase/errcode`：泛型错误码载体与错误链查询。
13. `github.com/yangzhuangqiu/gobase/examples`：端到端示例模块。
14. `github.com/yangzhuangqiu/gobase/util`：转换、加密、随机、重试、文件、时间等标准库工具集合。
15. `github.com/yangzhuangqiu/gobase/web`：不包含公共配置和框架类型的 Web Server 稳定运行契约。
16. `github.com/yangzhuangqiu/gobase/web-hertz`：Hertz 生命周期适配、独立配置和原生 Middleware/Route/Observation 扩展。
17. `github.com/yangzhuangqiu/gobase/app-web-hertz`：配置/IOC 到 web-hertz 的应用装配与 Controller/Filter/Observer 聚合。

## Repository Layout

```text
gobase/
  config/
    go.mod
  config-nacos/
    go.mod
  config-consul/
    go.mod
  app/
    go.mod
  app-runtime/
    go.mod
  app-web-hertz/
    go.mod
  app-log-zap/
    go.mod
  app-observe-webhertz/
    go.mod
  log/
    go.mod
  hlog-zap/
    go.mod
  ioc/
    go.mod
  log-zap/
    go.mod
  errcode/
    go.mod
  examples/
    go.mod
    admin/
    quickstart-app/
    quickstart-app-web-hertz/
    quickstart-web-hertz/
  util/
    go.mod
  web/
    go.mod
  web-hertz/
    go.mod
  docs/
```

## Local Development

仓库使用 `go.work` 管理多模块开发。

常用命令（按仓库约定通过脚本执行）：

```powershell
./scripts/go.ps1 -C config test ./...
./scripts/go.ps1 -C config-nacos test ./...
./scripts/go.ps1 -C config-consul test ./...
./scripts/go.ps1 -C app test ./...
./scripts/go.ps1 -C app-runtime test ./...
./scripts/go.ps1 -C app-web-hertz test ./...
./scripts/go.ps1 -C app-log-zap test ./...
./scripts/go.ps1 -C app-observe-webhertz test ./...
./scripts/go.ps1 -C log test ./...
./scripts/go.ps1 -C hlog-zap test ./...
./scripts/go.ps1 -C ioc test ./...
./scripts/go.ps1 -C log-zap test ./...
./scripts/go.ps1 -C errcode test ./...
./scripts/go.ps1 -C util test ./...
./scripts/go.ps1 -C web test ./...
./scripts/go.ps1 -C web-hertz test ./...
./scripts/go.ps1 -C examples run ./admin --help
./scripts/go.ps1 -C examples run ./quickstart-app --help
./scripts/go.ps1 -C examples run ./quickstart-app-web-hertz
./scripts/go.ps1 -C examples run ./quickstart-web-hertz
./scripts/go.ps1 -C examples run ./quickstart-ioc
```

## Docs

1. [app 模块入口](app/README.md)
2. [app-runtime 模块入口](app-runtime/README.md)
3. [ioc 模块入口](ioc/README.md)
4. [app 任务型使用指南](docs/guides/app-usage.md)
5. [Runtime 高级使用指南](docs/guides/app-runtime-usage.md)
6. [IOC 任务型使用指南](docs/guides/ioc-usage.md)
7. [架构总览](docs/architecture/architecture-overview.md)
8. [Config Design](docs/architecture/config-design.md)
9. [App Design](docs/architecture/app-design.md)
10. [App Runtime Design](docs/architecture/app-runtime-design.md)
11. [IOC Design](docs/architecture/ioc-design.md)
12. [Testing Playbook](docs/testing/testing.md)
13. [Multi-Module Release SOP](docs/release/multi-module-release-sop.md)
14. [config 模块入口](config/README.md)
15. [Config 使用指南](docs/guides/config-usage.md)
16. [config-nacos 模块入口](config-nacos/README.md)
17. [config-consul 模块入口](config-consul/README.md)
18. [app-log-zap 模块入口](app-log-zap/README.md)
19. [errcode 模块入口](errcode/README.md)
20. [Errcode 架构设计](docs/architecture/errcode-design.md)
21. [util 模块入口](util/README.md)
22. [Util 使用指南](docs/guides/util-usage.md)
23. [web 模块入口](web/README.md)
24. [web-hertz 模块入口](web-hertz/README.md)
25. [web-hertz 使用指南](docs/guides/web-hertz-usage.md)
26. [web-hertz 架构设计](docs/architecture/web-hertz-design.md)
27. [quickstart-web-hertz](examples/quickstart-web-hertz/README.md)
28. [app-web-hertz 模块入口](app-web-hertz/README.md)
29. [app-web-hertz 使用指南](docs/guides/app-web-hertz-usage.md)
30. [app-web-hertz 架构设计](docs/architecture/app-web-hertz-design.md)
31. [quickstart-app-web-hertz](examples/quickstart-app-web-hertz/README.md)
32. [app-observe-webhertz 模块入口](app-observe-webhertz/README.md)
33. [Web Hertz 能力配置化与可观测性方案](docs/architecture/web-hertz-capability-observability-proposal.md)
