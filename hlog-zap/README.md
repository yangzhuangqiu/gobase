# hlog-zap

`hlog-zap` 是 Hertz `hlog` 到 Zap 的框架适配层。它既可以复用 `log-zap.Config` 独立构建 Logger，也可以从既有 `log-zap.Provider` 创建共享底层的借用 Logger。

## 设计范围

### In Scope

1. 构建 hertz-contrib Zap Logger。
2. 可选安装为进程级 `hlog` Logger。
3. 复用 log-zap 配置校验和轮转默认值。
4. 在适配层内部修正 Hertz 包装调用位置。
5. 从 `log-zap.Provider` 创建共享 level、encoder、writer 与关闭状态的借用 `hlog.FullLogger`。
6. 以单 owner Lease 安装借用 Logger，并在 Host 停止后恢复原 hlog。

### Out of Scope

1. 不提供根 `log.LoggerFactory`。
2. 不支持字符串 context key 自动提取。
3. 不暴露可配置 caller skip。
4. 不关闭借用的 Provider；Lease 只管理全局 Logger 恢复。

## 公共入口

```go
logger, err := hlogzap.NewFromProvider(zapProvider)
if err != nil {
    return err
}
```

`NewFromProvider` 不修改全局 hlog，也不拥有 Provider。需要进程级安装和恢复时使用：

```go
lease, err := hlogzap.InstallFromProvider(zapProvider)
if err != nil {
	return err
}
defer lease.Close()
```

应用 Runtime 推荐使用 [`hlog-zap/app`](app/README.md) 的 `apphlogzap.Wrap(applogzap.New(...))` 显式组合，由包装器保证 Server Close → hlog 恢复 → Provider Close 的顺序。

需要独立 Zap 底层时使用 `NewLogger`；需要立即安装独立 Logger 时使用 `Install`。

## 配置

复用 `log-zap.Config`：

- level：debug/info/warn/error；`trace` 是 Debug 兼容别名；
- mode：console/file；
- format：json/text；
- disableCaller；
- rotate。

caller 偏移由 Hertz 适配实现固定并通过测试锁定。根日志的 typed ContextExtractor 和结构化字段策略不适用于 hlog 格式化接口；需要请求字段时应在 Hertz 中间件显式写入或使用独立适配能力。

## 错误与生命周期

配置错误沿用 log-zap 稳定错误码；`NewFromProvider(nil)` 返回 `hlog_zap.provider.invalid`。`InstallFromProvider` 修改进程全局 hlog 状态，应在服务并发启动前调用；活跃 Lease 存在时，`InstallFromProvider` 与独立 `Install` 都返回 `hlog_zap.install.conflict`。Lease 有效期间 composition root 不得绕过本包直接调用 `hlog.SetLogger`。

共享 Provider 路径中：

1. `SetLevel` 更新同一个 Provider，业务 Logger 与 Hertz Logger 同时生效；
2. `SetOutput` 是 no-op，输出资源只能由 Provider 配置和拥有；
3. Provider Close 后既有 Hertz Logger 保持可调用但不再输出；
4. 适配器不执行 Sync/Close，Provider 生命周期必须覆盖 Hertz Host。
5. Lease Close 幂等恢复安装前的 DefaultLogger，并由 Hertz 重建配对的 SystemLogger，不关闭 Provider。

独立 `NewLogger/Install` 返回的 hertzzap Logger 支持 Sync，但当前 API 不暴露底层 lumberjack closer，文件模式资源释放仍是已知缺口。

## File Map

- `config.go`：共享配置类型别名。
- `install.go`：构建与安装独立 Logger，并管理共享 Provider 的单 owner Lease、冲突拒绝和幂等恢复。
- `provider_logger.go`：共享 `log-zap.Provider` 的借用 FullLogger 适配。
- `errors.go`：Hertz 边界错误构造。
