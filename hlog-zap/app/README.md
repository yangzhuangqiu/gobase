# hlog-zap/app

`hlog-zap/app` 装饰调用方选择的应用 Logging，在 Runtime 日志构造阶段追加 `hlog-zap` Lease，让业务 LoggerFactory 和 Hertz 全局 hlog 共享同一个 Zap Provider。

## 设计范围

### In Scope

1. 包装符合结构化 `Logging.Build` 合同的既有日志实现。
2. 在 IOC Build 前安装进程级 Hertz hlog。
3. 在 IOC/Hertz Server 关闭后恢复 hlog，再关闭 Zap Provider。
4. 拒绝同进程并行安装多个 hlog 租约。
5. 通过 `ZapProvider()` 窄能力断言借用 delegate 的 Zap 核心，不取得独立关闭权。

### Out of Scope

1. 不负责构建或运行 Hertz Server。
2. 不向业务组件暴露 Zap Core。
3. 不支持多个并行 Runtime 各自持有不同的全局 hlog。
4. 不创建或配置 `app-log-zap`；日志实现及其 Option 由 composition root 显式选择。

## 公共入口

```go
app.WithLogging(
	apphlogzap.Wrap(
		applogzap.New(
			applogzap.WithScope("log.zap"),
		),
	),
)
```

`Wrap` 返回结构化兼容 `app-runtime.Logging` 的包装器。正常应用无需直接操作 hlog，也无需手工保存和恢复 `hlog.DefaultLogger()`。

配置 Option 只传给 delegate；本子包不增加第二套日志 scope 或 Zap 配置模型。delegate Build 返回的 Provider 必须支持以下窄能力：

```go
interface {
	ZapProvider() *logzap.Provider
}
```

`app-log-zap` 的标准 Build 结果满足该能力；业务代码仍只依赖 `log.LoggerFactory`。

## 运行期语义

固定顺序为：delegate Provider 创建 → hlog 安装 → IOC Build/Server Run → IOC Close → hlog 恢复 → delegate Provider Close。安装失败会关闭已创建 Provider；恢复和关闭均幂等。

hlog 是进程级可变状态，Lease 活跃期间第二次 Build 返回 `hlog_zap.app.build_failed`，错误链同时保留 `hlog_zap.install.conflict`。delegate 缺少 `ZapProvider()` 能力时 Build 关闭已交付 Provider 并失败；恢复或 Provider 关闭失败返回 `hlog_zap.app.close_failed`。

## 最佳实践

1. Hertz app Runtime 显式使用 `apphlogzap.Wrap(applogzap.New(...))`，不再额外调用 `hlog.SetLogger`。
2. 普通非 Hertz Runtime 继续使用 `app-log-zap`，避免无意义地修改全局 hlog。
3. 测试必须关闭返回的 Provider，确保 hlog 恢复并释放下一用例的全局租约。

## File Map

- `logging.go`：可包装 Logging 的结构化输入合同。
- `wrapper.go`：delegate Build、窄能力校验与 hlog 原子安装。
- `zap_provider.go`：Zap Provider 窄能力类型断言。
- `runtime_provider.go`：恢复 hlog 和关闭 delegate Provider 的组合资源。
- `errors.go`：稳定构建与关闭错误语义。
- `doc.go`：包级职责与生命周期边界。
- `wrapper_test.go`：安装、恢复、幂等关闭和并行 Runtime 冲突合同。

## 相关文档

1. [app-log-zap](../../app-log-zap/README.md)
2. [hlog-zap](../README.md)
3. [app-web-hertz](../../app-web-hertz/README.md)
4. [quickstart-app-web-hertz](../../examples/quickstart-app-web-hertz/README.md)
