# Util 使用指南

本文按任务说明 `util` 各子包的安全用法、失败分支和资源边界。API 列表以代码注释为准；模块范围和关键默认值见 [util README](../../util/README.md)。

## 1. 选择子包

| 任务 | 子包 | 不适用场景 |
|---|---|---|
| gzip/Base64 | `compressx` | 大流式数据；超过 64 MiB 时需显式确定业务上限 |
| 类型转换 | `convx` | 复杂 schema 解码和业务校验 |
| AES 加解密 | `crypto/aesx` | 密码哈希、密钥管理、完整信封协议 |
| 语义错误 | `errorsx` | 全局业务错误码治理 |
| 文件读写 | `filex` | 原子事务写、文件锁、超大行流处理 |
| 摘要/HMAC | `hashx` | 密码存储、通用数字签名协议 |
| 进制编码 | `radixx` | Unicode 字符表、大整数 |
| 随机值 | `randx` | 密钥托管、分布式唯一 ID |
| 同步重试 | `retryx` | 跨进程任务调度、持久化重试队列 |
| 字符串辅助 | `stringx` | 需要保留序列化错误的 JSON 编码 |
| 时区与格式 | `timex` | 日历系统、交易日和节假日规则 |

## 2. 按 Kind 处理错误

```go
value, err := convx.ToInt(input)
if err != nil {
	switch {
	case errorsx.IsKind(err, convx.KindUnsupportedType):
		return fallback
	case errorsx.IsKind(err, convx.KindOverflow):
		return fmt.Errorf("value out of range: %w", err)
	default:
		return err
	}
}
```

规则：

1. 使用各子包公开 Kind 常量，不硬编码错误文本或 kind 字符串。
2. `Kind` 表达调用方动作，`Op/detail` 表达内部阶段；例如文件打开和关闭失败都属于 `filex.io_error`，具体阶段位于 detail。
3. `errorsx.IsKind` 会遍历普通线性包装链与 `errors.Join` 的全部兄弟分支，匹配结果不依赖 Join 顺序。
4. `KindOf/OpOf` 只提取 `errors.As` 遍历中的首个 `errorsx.Error`。
5. `Detail` 和 cause 可能包含路径或输入摘要，但不应包含 token、密钥和完整敏感载荷。

## 3. 安全使用 AES 与摘要

### 3.1 优先 AES-GCM

```go
key, err := aesx.GenerateKey(32)
if err != nil {
	return err
}
nonce, err := aesx.GenerateNonce()
if err != nil {
	return err
}
ciphertext, err := aesx.EncryptGCM(plaintext, key, nonce)
if err != nil {
	return err
}

// 协议必须同时保存 nonce、ciphertext 和 key version；nonce 不是秘密。
```

安全约束：

1. 同一个 key 下每次加密使用唯一 nonce；复用 GCM nonce 会破坏机密性和认证性。
2. 当前 API 不自动把 nonce 拼入密文，也不保存 key version；序列化格式由调用协议负责。
3. 当前 GCM API 不支持 AAD。需要绑定协议头、租户或版本时，不能假设现有密文已经认证这些字段。
4. 解密认证失败返回 `aesx.authentication_failed`，不要降级到未认证明文。
5. 公共 API 只提供 `EncryptGCM/DecryptGCM` 等模式专用入口；不存在可组合出 `GCM + PKCS7` 的通用 Mode API。
6. Base64/Hex 与加密 bytes API 正交组合：先 `EncryptGCM`，再 `Encode`；解密时顺序相反。

### 3.2 CBC、ECB 与 padding

1. CBC 需要 16-byte 随机 IV，通常使用 PKCS7；它只加密、不认证，生产协议还需要安全 MAC 或更优先改用 AEAD。
2. ECB 会泄露明文模式，只用于明确要求的兼容协议。
3. Zero Padding 无法区分数据末尾真实 `0x00`，不适合一般文本或任意二进制协议。
4. `PaddingNone` 要求输入已经按 AES block 对齐。

### 3.3 Hash 与 HMAC

1. MD5、SHA-1 和 HMAC-SHA1 只用于既有兼容协议，不用于新安全设计。
2. SHA-256/SHA-512 是摘要，不是密码哈希；密码存储应使用 Argon2、scrypt、bcrypt 等专用 KDF。
3. 认证场景使用 HMAC 而不是 `hash(secret + message)`；所有 HMAC helper 参数顺序固定为 `(key, message)`。
4. 验证 MAC 时使用标准库 `hmac.Equal` 做常量时间比较；当前 helper 只负责计算编码结果。

## 4. 生成随机值

```go
token, err := randx.AlphanumericByCrypto(32)
```

1. `IntN`、`Alphanumeric` 等包级入口使用带 mutex 的默认 math RNG，并发安全但可预测。
2. token、验证码、nonce、密钥材料使用 `ByCrypto` 或 `aesx.Generate*`。
3. `randx.NewMath(seed)` 适合可复现测试或调用方私有热点实例；实例不并发安全。
4. range 采用 `[startInclusive, endExclusive)`，空区间返回 `randx.invalid_range`。
5. charset 按 bytes 索引，不是 Unicode rune 集合；多字节字符可能被拆分。
6. `Int64Range` 与 `Int64RangeByCrypto` 支持所有满足 `startInclusive < endExclusive` 的 int64 半开区间，包括跨越正负边界的极端区间。

## 5. 配置重试

### 5.1 从默认值开始

```go
options := retryx.DefaultOptions()
options.Attempts = 5
options.ShouldRetry = retryx.ShouldRetryByError
options.Observer = func(event retryx.Event) {
	logger.Info(ctx, "remote retry event",
		log.F("op", "remote.retry"),
		log.F("event", event.Type),
		log.F("attempt", event.Attempt),
		log.F("delay", event.Delay),
		log.Err(event.Err),
	)
}

err := retryx.Do(ctx, options, func(attempt int) error {
	return callRemote(ctx)
})
```

`Options{}` 不是默认配置：`Attempts=0`、`Multiplier=0` 都会校验失败。默认值为：

| 字段 | 默认值 |
|---|---:|
| `Attempts` | 3，包含第一次执行 |
| `InitialDelay` | `100ms` |
| `MaxDelay` | `2s` |
| `Multiplier` | `2` |
| `JitterFraction` | `0.2`，即 ±20% |

### 5.2 终止与失败

1. `ShouldRetry=nil` 表示所有 operation error 都继续重试。
2. `ShouldRetryByError` 只重试实现 `Retryable() bool` 且返回 true 的 error；未实现协议的 error 不重试。
3. 次数耗尽返回包含 `*retryx.ExhaustedError` 的 `errorsx.Error`，保留最后 error；可同时用 `IsExhausted`、`errorsx.IsKind(err, retryx.KindExhausted)` 和 `errors.Is/As` 判断。
4. context 取消在尝试前和退避等待中生效；它不能强制终止已经运行的 operation，operation 自身必须使用 context。
5. nil context 被允许，但退避使用不可取消 `time.Sleep`。
6. Observer、ShouldRetry 和 operation 都在当前 goroutine 执行，panic 不会被 retryx 隔离。
7. `MaxDelay` 限制 jitter 后的每次实际等待；`InitialDelay` 较大或倍率溢出时也会饱和到上限。`MaxDelay=0` 表示不设置业务上限，但仍受 `time.Duration` 上界保护。

## 6. 文件读取与写入

### 6.1 读行与批处理

```go
err := filex.ReadLinesBatch(path, 500, func(lines []string) error {
	return consume(lines)
})
```

1. `ReadLines/ReadLinesBatch` 去掉 CR/LF，保留空行。
2. 单行内容上限 1 MiB；超限返回 `filex.line_too_long`。
3. batch callback 同步执行，返回 error 立即停止读取并包装为 `filex.handle_error`。
4. `ReadString` 去除 UTF-8 BOM；`ReadBytes` 保留原始 bytes。

### 6.2 写入边界

1. `WriteLines/WriteBytes/WriteString` 自动创建父目录；请求目录权限 `0755`、文件权限 `0644`，最终权限仍受进程 umask 和平台影响。
2. `appendMode=false` 截断覆盖，`true` 追加；`WriteLines` 为每项追加换行。
3. 写入不是原子的，没有文件锁或 fsync，并发写同一文件由调用方同步。
4. 当前实现检查 write/flush/close error，统一返回 `filex.io_error`，底层不存在或权限原因继续使用 `errors.Is/As` 判断。
5. 配置、凭据等关键文件应采用同目录临时文件、必要同步、权限收紧和原子 rename 的业务协议。

## 7. 类型转换

1. `convx.ToInt64` 支持整数、十进制 string 和 bytes；string/bytes 不自动 trim。
2. `ToFloat64` 把 uint64 转 float64 时可能损失精度，当前不返回精度错误。
3. `ToBool` 接受 bool、ParseBool 文本及有符号/无符号整数 `0/1`；其他整数返回 `convx.invalid_value`。
4. `ToDuration` 的整数和纯数字文本按毫秒解释；`1m` 与 `1min` 都表示一分钟。
5. 极大毫秒整数在乘法前执行范围校验，越界返回 `convx.overflow`，不会产生环绕 duration。

外部协议应先定义允许输入类型和范围，不把“可以转换”当作完整业务校验。

## 8. 时间与时区

1. `timex` 包级 API 始终使用 `Asia/Shanghai`；时区数据库加载失败时回退固定 UTC+08:00。
2. 跨时区代码创建 `Timezone`，不要依赖进程 `time.Local`。`NewWithLocation(nil)` 明确回退系统本地时区。
3. Parse 使用绑定时区和严格 layout，不自动 trim 输入。
4. `EndOfDay` 返回当天最后一个纳秒；范围查询仍优先使用 `[StartOfDay(value), StartOfNextDay(value))`，避免数据库精度差异。
5. Unix timestamp 本身与时区无关；时区只影响格式化与日历边界。

## 9. 压缩、进制和字符串

### gzip

```go
raw, err := compressx.GunzipLimit(compressed, 8<<20) // 最多输出 8 MiB
if err != nil {
	if errorsx.IsKind(err, compressx.KindOutputLimitExceeded) {
		return fmt.Errorf("decompressed payload is too large: %w", err)
	}
	return err
}
```

1. `compressx` 在内存中持有完整输入和输出，不适合超大流。
2. `IsGzip` 只检查两个 magic bytes，不能证明数据可成功解压。
3. `Gunzip`、`GunzipToString` 和 `GunzipFromBase64` 默认使用 `DefaultGunzipMaxOutputBytes`（64 MiB），超过时返回 `compressx.output_limit_exceeded`。
4. `GunzipLimit`、`GunzipToStringLimit` 和 `GunzipFromBase64Limit` 用于显式收紧或提高业务上限，要求正数 `maxOutputBytes`；输出恰好等于上限时成功。
5. 所有入口仍会在内存中持有上限以内的完整输出；超大数据应改用业务层流式协议，而不是持续提高上限。

### radix

1. 自定义 digits 是 byte alphabet，至少 2 bytes、不能重复且不能包含保留负号 `-`。
2. `EncodeInt64/DecodeInt64` 支持负数和 MinInt64；padding 只对非负编码生效。
3. Base62 使用固定 `0-9a-zA-Z`，编码入口无需返回 error，解码仍可能失败或溢出。

### string

1. `Before/After/BeforeLast/AfterLast` 未找到分隔符时返回空串，无法区分“未找到”和“结果本来为空”。
2. `ToJSON/ToJSONBytes` 会吞掉序列化错误，仅用于调用方已保证可序列化的低风险场景；需要可靠序列化时直接使用 `encoding/json`。
3. `CamelCaseToSep` 会逐个拆分连续大写字母，例如 `ID` 变为 `i_d`。
4. 数值 Parse 方法不 trim 输入并返回 `stringx.parse_error`。

## 10. 测试建议

1. 随机测试只断言范围、长度和字符集；需要稳定序列时使用固定 seed 的 `NewMath`。
2. AES 测试覆盖 nonce/IV/key 长度、篡改认证失败和协议编码往返，不记录真实密钥。
3. retry 测试使用零延迟 options 或可控 context，断言 attempt、Event 和最终 error，不使用真实长 sleep。
4. filex 测试使用 `t.TempDir()`，覆盖 append/overwrite、1 MiB 行边界和 callback error。
5. 时间测试使用显式 `Timezone` 和固定 `time.Time`，避免依赖当前时间与机器本地时区。

## 11. 最佳实践

1. 在 API 边界先限制输入类型、长度和范围，再调用通用工具。
2. 安全能力选择基于威胁模型：AEAD、安全随机、KDF 和常量时间比较各自解决不同问题。
3. 回调保持快速、panic-free；需要异步、隔离或背压时由业务层显式设计。
4. 对 error 使用公开 Kind 分支，并保留底层 cause；需要提取元数据时注意 `KindOf/OpOf` 只返回首个匹配错误。
5. 对文件、压缩和重试设置业务容量上限，工具默认不会替代系统级保护。

## 12. 常见问题

| 现象 | 原因 | 处理 |
|---|---|---|
| `retryx.Options{}` 立即报错 | 零值不是默认策略 | 从 `DefaultOptions()` 开始修改 |
| context 已取消但 operation 仍运行 | context 只能中断 retry 状态机，不能杀死函数 | operation 内部检查并传递 context |
| `KindOf/OpOf` 返回的不是预期内层信息 | 提取 API 只返回遍历中的首个 `errorsx.Error` | 已知目标用 `IsKind`；需要全部元数据时显式遍历错误树 |
| GCM 解密认证失败 | key、nonce、密文错误或数据被篡改 | 不降级；检查信封字段和 nonce 唯一性 |
| 文件覆盖后出现半成品 | `Write*` 不是原子事务写 | 使用临时文件与原子 rename 协议 |
| 日范围查询受数据库精度影响 | inclusive 日末与存储精度不一致 | 使用 `StartOfNextDay` 作为 exclusive 上界 |
| 解压被拒绝或占用大量内存 | 结果超过默认 64 MiB，或显式上限设置过大 | 小载荷使用默认入口；其他场景用 `GunzipLimit` 设置合理业务上限 |
| Unicode 随机串损坏 | charset 按 byte 索引 | 使用 ASCII charset，或自行实现 rune 级生成 |

## 13. 相关文档

1. [util README](../../util/README.md)
2. [errorsx README](../../util/errorsx/README.md)
3. [测试规范](../testing/testing.md)
