# web

`web` 是 Web Server 的框架无关运行门面，定义应用宿主稳定依赖的 `Server` 生命周期契约，以及高级组合层按需断言的 `StartupServer` 启动结果窄合同。Gin、Hertz 等具体实现由独立模块提供，并各自拥有完整配置和原生编程模型。

## 设计范围与目标

### In Scope

1. 定义阻塞运行、context 取消和优雅停止的稳定合同。
2. 统一单实例运行、并发停止和幂等释放语义。
3. 以独立可选接口表达首次启动成功/失败，不增加普通 Server 的使用负担。
4. 为具体 Web 框架实现提供最小依赖方向。

### Out of Scope

1. 不定义公共配置结构、配置路径、默认端口或超时。
2. 不抽象 Router、Controller、Handler、Context 或 Middleware。
3. 不创建监听器，不提供 Gin、Hertz 等具体实现。
4. 不管理进程信号、服务注册、调度任务、日志实现或 API 响应模型。
5. 不提供全局 Server 注册、自动发现或默认实例。

## 核心对象与职责

| 对象 | 职责 |
|---|---|
| `Server` | 在应用运行阶段阻塞服务，并在取消或显式优雅停止时收敛资源 |
| `StartupServer` | 为服务注册等高级组合层广播并缓存首次启动结果 |

## 公共入口与契约

应用宿主只依赖 `Server`：

```go
func run(ctx context.Context, server web.Server) error {
	return server.Run(ctx)
}
```

资源所有者应在构建回滚和应用关闭路径调用 `Shutdown`：

```go
shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

if err := server.Shutdown(shutdownCtx); err != nil {
	return err
}
```

`Server` 是单运行周期对象。首次 `Run` 返回或 `Shutdown` 被触发后不得再次运行；需要重启时应由具体实现重新构建一个 Server。

需要协调启动成功后的动作时，调用方可以断言独立窄合同：

```go
startupServer, ok := server.(web.StartupServer)
if !ok {
	return errors.New("server startup result is required")
}
if err := startupServer.WaitStarted(ctx); err != nil {
	return err
}
```

`WaitStarted` 只报告首次启动结果，不是 readiness 或持续健康检查，也不承诺提供可注册或可公开访问的地址。

## 配置模型与默认行为

根模块没有配置结构和默认值。地址、超时、请求体限制、运行模式及其他配置全部属于具体实现模块，例如当前的 `web-hertz.Config` 或未来的 `web-gin.Config`。

根契约不要求不同实现提供同名配置，也不把名称相同但运行语义不同的配置强行归一。

## 错误语义

1. `Run/Shutdown` 通过 error 报告输入、状态、监听和停止失败，不得 panic。
2. context 取消触发且优雅停止成功时，`Run` 返回 nil。
3. 具体错误码、阶段信息和底层 cause 由实现模块定义并保留。
4. `Shutdown` 的 context 只约束本次等待；deadline 到期不等于可以泄漏或重复释放底层资源。

## 运行时语义

1. 一个 Server 只允许一次运行周期；并发或重复 `Run` 返回错误。
2. `Run` 是主动工作负载入口，调用后阻塞到服务停止。
3. `Run` context 取消表示请求优雅停止，不授权实现直接丢弃在途请求。
4. `Shutdown` 可与 `Run` 并发，底层停止和资源释放触发必须幂等。
5. 未运行或已经完成停止时调用 `Shutdown` 返回 nil；未运行时调用也会终结该对象，后续 `Run` 返回错误。
6. Server 不安装操作系统信号处理器；应用宿主是信号转换和退出预算的唯一事实源。
7. `StartupServer` 的单个等待者取消只结束自身等待；首次启动成功或失败结果由实现缓存并广播。

## 最佳实践

1. 在应用 Runner 中调用 `Run(ctx)`，不要在 IOC 初始化阶段启动长期监听。
2. 由创建 Server 的一方负责 `Shutdown`，并给停止路径提供独立 deadline。
3. 业务 Controller 和 Middleware 直接依赖选定框架的原生类型。
4. 具体实现的关键日志应携带稳定 `op/result/reason`，且不得记录凭据或完整敏感请求。

## Open Gaps / Future Improvements

1. `web-hertz` 已实现该合同；未来其他框架 provider 应复用同一生命周期语义和合同测试思路。
2. 尚未定义 readiness、持续状态快照或监听地址查询能力；一次性启动结果已由独立窄接口表达，不扩展根 `Server`。

## File Map

1. `server.go`：Server 运行与优雅停止契约。
2. `startup_server.go`：高级组合层可选的首次启动结果合同。
3. `doc.go`：包级边界说明。

## 相关文档

1. [web-hertz 模块入口](../web-hertz/README.md)
2. [web-hertz 使用指南](../docs/guides/web-hertz-usage.md)
3. [web-hertz 架构设计](../docs/architecture/web-hertz-design.md)
