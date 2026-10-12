# util

`util` 是只依赖 Go 标准库的通用工具模块，提供转换、压缩、AES、错误语义、文件、摘要、进制、随机、重试、字符串和时间能力。调用方按子包导入，不存在需要初始化的 util 根包。

## 设计范围与目标

### In Scope

1. 高频、低耦合的纯函数和轻状态工具。
2. 通过 `errorsx.Kind` 提供可分支的失败语义。
3. 为随机、AES、文件和重试提供明确的安全与资源边界。
4. 保持子包独立，调用方只引入实际需要的能力。

### Out of Scope

1. 不承载业务对象转换、领域校验或框架适配。
2. 不提供 HTTP、数据库、MQ 等远端 I/O。
3. 不提供密码哈希、密钥管理、密钥派生或完整加密信封协议。
4. 不提供全局重试调度、文件事务或分布式时间服务。

## 子包与职责

| 子包 | 主要职责 | 关键边界 |
|---|---|---|
| `compressx` | 内存 gzip 与 Base64 组合 | 默认解压上限 64 MiB，可通过 `*Limit` 显式调整 |
| `convx` | `any` 到数值、布尔和 duration | 整数 duration 按毫秒解释 |
| `crypto/aesx` | 模式专用 AES-GCM/CBC/ECB、padding、编码 | nonce/IV 由调用方保存和管理 |
| `errorsx` | `op/kind/detail/cause` 错误载体 | `IsKind` 遍历单链与 Join；不检测循环链 |
| `filex` | 文件读写、分批读行、目录与 BOM | 写入非原子、无 fsync |
| `hashx` | MD5/SHA/HMAC 摘要 | MD5/SHA-1 仅兼容用途 |
| `radixx` | 自定义 byte digits 与 Base62 | digits 不是 Unicode rune 集合 |
| `randx` | math 伪随机和 crypto 安全随机 | 安全用途必须选择 `ByCrypto` |
| `retryx` | context 感知重试、退避、事件 | 不会中断正在执行的 operation |
| `stringx` | 字符串、便捷 JSON 和数值解析 | `ToJSON/ToJSONBytes` 会吞序列化错误 |
| `timex` | 上海默认时区、显式 Timezone 和日历边界 | Unix timestamp 转换直接组合标准库 |

## 最小使用示例

```go
value, err := convx.ToDuration("1500ms")
if err != nil {
	if errorsx.IsKind(err, convx.KindParseError) {
		return fmt.Errorf("invalid timeout: %w", err)
	}
	return err
}

result, err := retryx.DoValue(ctx, retryx.DefaultOptions(), func(attempt int) (string, error) {
	return load(ctx)
})
```

加密、文件、重试和错误分支的生产用法见 [Util 使用指南](../docs/guides/util-usage.md)。

## 公共入口与核心契约

1. util 根目录没有公共 Go package；公共 API 位于各子包。
2. 返回 error 的子包使用 `errorsx.Error`；`Kind` 只表达调用方可采取动作的原因，`Op/detail` 表达内部阶段和诊断上下文。
3. `hashx` 等不会失败的纯计算函数直接返回结果，不制造无意义 error。
4. `stringx.ToJSON/ToJSONBytes` 会吞掉序列化错误，只适用于调用方已经保证可序列化的值。
5. `randx` 包级 math 方法并发安全，但不具备密码学安全性；`randx.Math` 实例不保证并发安全。
6. `timex` 包级 API 固定使用 `Asia/Shanghai`，其他时区使用 `timex.New` 或 `NewWithLocation`。
7. `aesx` 只公开模式专用加解密入口；Base64/Hex 通过独立 `Encode/Decode` 与 bytes API 组合。

## 默认值与关键约束

1. `retryx.DefaultOptions()`：3 次尝试、首次延迟 `100ms`、实际等待最大 `2s`、倍率 `2`、抖动 `±20%`。jitter 后仍受 `MaxDelay` 约束；零值 `Options{}` 不是有效默认配置。
2. `filex.ReadLines/ReadLinesBatch`：单行上限 1 MiB，超过返回 `filex.line_too_long`。
3. `aesx`：key 只接受 16/24/32 bytes；CBC IV 为 16 bytes；GCM nonce 为 12 bytes，GCM API 不暴露 padding 参数。
4. `timex`：日期布局 `2006-01-02`，秒布局 `2006-01-02 15:04:05`，毫秒布局 `2006-01-02 15:04:05.000`。
5. `convx.ToDuration`：整数及纯数字文本按毫秒解释；带单位文本使用 Go duration，并额外接受 `min`；毫秒乘法越界返回 `convx.overflow`。
6. `radixx`：自定义 digits 至少 2 bytes、不能重复且不能包含保留负号 `-`；Base62 alphabet 为 `0-9a-zA-Z`。
7. `compressx.Gunzip` 系列默认使用 `DefaultGunzipMaxOutputBytes`（64 MiB）；`*Limit` 的 `maxOutputBytes` 必须大于 0。结果恰好等于上限时允许成功，超过时返回 `compressx.output_limit_exceeded`。

## 错误语义

1. `errorsx.Error` 文本包含 `op`、`result=failed`、`kind`、可选 detail 和 cause。
2. `Wrap/Wrapf(nil, ...)` 返回 nil；底层 cause 通过 `Unwrap` 保留。
3. `Kind` 是稳定分支字段，只描述调用方动作；实现步骤放入 `Op/detail`，底层系统原因通过 `errors.Is/As` 判断。
4. `errorsx.KindOf/OpOf` 返回 `errors.As` 遍历中的首个 `errorsx.Error`。
5. `errorsx.IsKind` 会遍历普通单链和 `errors.Join` 的全部分支；`KindOf/OpOf` 仍只提取遍历中的首个 `errorsx.Error`。
6. 各子包公开的 `kinds.go` 是可能失败类别的权威列表。

## 运行时语义

1. 大部分子包是无共享状态的同步纯函数。
2. `randx` 包级伪随机实例由 mutex 保护；显式 `Math` 的状态与并发同步由调用方拥有。
3. `retryx` 在调用 goroutine 中执行 operation、Observer 和等待；context 只能中断尝试前检查和退避等待。
4. `filex` 与 `compressx` 同步完成全部 I/O/内存处理，没有后台任务。
5. `timex` 初始化时加载上海时区；加载失败回退固定 UTC+08:00。
6. 所有回调由调用方负责并发安全和 panic-free；模块没有统一 panic recovery。
7. `timex.EndOfDay` 返回当天最后一个纳秒；范围查询优先使用 `[StartOfDay, StartOfNextDay)`。

## 最佳实践摘要

1. 新代码优先 AES-GCM，nonce 每个 key 下必须唯一；CBC/ECB 只用于明确的兼容协议。
2. token、密钥、验证码等安全随机使用 `randx.*ByCrypto`。
3. HMAC API 固定使用 `(key, message)` 参数顺序；验证摘要使用 `hmac.Equal`。
4. 重试从 `DefaultOptions` 开始显式调整，并让 operation 自身响应 context。
5. 重要文件更新使用业务层临时文件、同步和原子 rename 协议，不把 `filex.Write*` 当事务写。
6. 密码存储使用专用 KDF；不要使用 MD5、SHA 或普通 HMAC 代替密码哈希。

## Open Gaps / Future Improvements

1. `errorsx.IsKind` 尚无错误链环检测，自定义循环 `Unwrap` 可能无法终止。
2. `filex.Write*` 没有 fsync、原子替换和并发写协议。
3. `retryx` 不隔离 operation/Observer/ShouldRetry panic。
4. `randx` 自定义 charset/digits 按 byte 而非 rune 解释，多字节字符可能被拆分。
5. `aesx` 尚无自动封装 nonce、AAD 或密钥版本的加密信封 API。

## File Map

1. `compressx/gzip.go`：gzip、Base64 组合和 magic header 判断。
2. `convx/convert.go`：数值、布尔和 duration 转换。
3. `crypto/aesx/aes.go`：模式专用 AES、padding、随机 key/IV/nonce 和 Base64/Hex 编码。
4. `errorsx/errors.go`：统一 Kind 错误结构和查询。
5. `filex/file.go`：目录、文件读写、批量读行和 BOM。
6. `hashx/hash.go`：MD5、SHA 和 HMAC 摘要。
7. `radixx/radix.go`：自定义进制与 Base62。
8. `randx/random.go`、`randx/math.go`：crypto、包级 math 和显式 math 随机。
9. `retryx/retry.go`：退避状态机、Observer 和耗尽错误。
10. `stringx/string.go`：字符串、JSON 便捷转换和解析。
11. `timex/time.go`：上海默认时区和 Timezone 对象。
12. 各子包 `kinds.go`：公开可分支错误 Kind。

## 相关文档

1. [Util 使用指南](../docs/guides/util-usage.md)
2. [errorsx README](errorsx/README.md)
3. [测试规范](../docs/testing/testing.md)
