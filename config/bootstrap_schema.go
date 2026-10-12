package config

import "github.com/yangzhuangqiu/gobase/config/provider"

// CoreBootstrapSchema 返回 config 内核自有 bootstrap 键定义。
func CoreBootstrapSchema() []provider.BootstrapKey {
	return []provider.BootstrapKey{
		{
			Key:          BootstrapKeyConfigFile,
			Type:         provider.BootstrapSchemaTypeString,
			Required:     false,
			DefaultValue: "auto-discovery(application.yaml|application.yml|config/application.yaml|config/application.yml)",
			Description:  "Local config file path used at bootstrap stage.",
		},
		// 注意：
		// bootstrap.configFile.watchInterval 属于“仅配置文件生效”键，
		// 故不进入 env 自动映射 schema，避免被 env 覆盖。
	}
}
