package aesx

import "github.com/yangzhuangqiu/gobase/util/errorsx"

const (
	// KindInvalidKey 表示 key 长度或生成尺寸不符合 AES 128/192/256 约束。
	KindInvalidKey errorsx.Kind = "aesx.invalid_key"
	// KindInvalidIV 表示 CBC IV 不是 AES block size，调用方应提供 16-byte IV。
	KindInvalidIV errorsx.Kind = "aesx.invalid_iv"
	// KindInvalidNonce 表示 GCM nonce 长度不符合当前 12-byte 契约。
	KindInvalidNonce errorsx.Kind = "aesx.invalid_nonce"
	// KindInvalidPadding 表示 padding 不支持或与输入 block 对齐状态冲突。
	KindInvalidPadding errorsx.Kind = "aesx.invalid_padding"
	// KindInvalidData 表示密文长度、padding 内容或编码文本不满足解密输入契约。
	KindInvalidData errorsx.Kind = "aesx.invalid_data"
	// KindAuthenticationFailed 表示 GCM 认证失败，调用方不得降级使用未认证数据。
	KindAuthenticationFailed errorsx.Kind = "aesx.authentication_failed"
	// KindInvalidEncoding 表示编码类型或 Base64/Hex 文本无效。
	KindInvalidEncoding errorsx.Kind = "aesx.invalid_encoding"
	// KindRandomSourceFailed 表示操作系统安全随机源不可用。
	KindRandomSourceFailed errorsx.Kind = "aesx.random_source_failed"
	// KindInternal 表示标准 AES primitive 初始化失败，调用方只能终止并排查运行环境。
	KindInternal errorsx.Kind = "aesx.internal"
)
