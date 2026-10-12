# quickstart-web-hertz

该示例演示纯 `web-hertz` provider 的最小完整运行路径：

1. 使用独立 `webhertz.Config` 设置监听地址。
2. 安装 Hertz 内置 recovery 与原生访问日志 Middleware。
3. 使用 `WithNoRoute`、`WithNoMethod` 返回稳定 JSON 404/405。
4. 使用 `RouteFunc` 注册普通路由、路由分组和路径参数。
5. 把操作系统信号转换为 context，由 `Server.Run` 完成优雅停止。

## 从仓库根目录运行

```powershell
./scripts/go.ps1 -C examples run ./quickstart-web-hertz
```

默认监听 `127.0.0.1:18080`。另开一个 PowerShell 窗口请求：

```powershell
Invoke-RestMethod http://127.0.0.1:18080/healthz
Invoke-RestMethod http://127.0.0.1:18080/api/v1/hello/gobase
Invoke-WebRequest http://127.0.0.1:18080/missing -SkipHttpErrorCheck
Invoke-WebRequest http://127.0.0.1:18080/healthz -Method Post -SkipHttpErrorCheck
```

覆盖监听地址：

```powershell
$env:WEB_HERTZ_ADDRESS="127.0.0.1:28080"
./scripts/go.ps1 -C examples run ./quickstart-web-hertz
```

按 `Ctrl+C` 后，信号 context 会被取消。`web-hertz` 触发一次共享 Hertz Shutdown，并等待连接排空或 `exitWaitTime` 到期。

## 代码阅读顺序

1. `loadConfig`：provider 独立配置，不读取根 `web` 公共配置。
2. `accessLog`：直接使用 Hertz `app.HandlerFunc`，不引入平行 Filter 抽象。
3. `notFoundHandler` / `methodNotAllowedHandler`：通过窄 Option 定义 Engine fallback，不取得 Engine。
4. `registerRoutes`：直接使用 Hertz 根路径 `*route.RouterGroup`，不封装 GET/Group。
5. `run`：应用拥有信号，provider 只消费 context。

完整用法和设计理由分别见：

1. [web-hertz 使用指南](../../docs/guides/web-hertz-usage.md)
2. [web-hertz 架构设计](../../docs/architecture/web-hertz-design.md)
