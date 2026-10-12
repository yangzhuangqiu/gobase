package provider

// BootstrapKey 描述一个可发现的 bootstrap 键定义。
type BootstrapKey struct {
	// Key 是 bootstrap 下的 dotted 键（不含 bootstrap. 前缀），例如 config / nacos.address。
	Key string
	// Type 是该键的文本类型标识（建议使用 BootstrapSchemaType* 常量）。
	Type BootstrapSchemaType
	// Required 表示该键是否在声明对应能力时必填。
	Required bool
	// DefaultValue 是该键默认值说明（文本），无默认值时为空。
	DefaultValue string
	// Description 是该键语义说明。
	Description string
}
