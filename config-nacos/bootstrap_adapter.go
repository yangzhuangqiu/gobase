package nacos

type bootstrapEnvAdapter struct{}

func newBootstrapEnvAdapter() *bootstrapEnvAdapter {
	return &bootstrapEnvAdapter{}
}

// Load 从环境变量读取 Nacos 启动参数并转换为 bootstrap patch。
//
// 参数：
// - envPrefix: 已标准化前缀（如 APP_）。
//
// 返回：
// - map[string]any: 仅包含 schema 自动映射无法覆盖的扩展字段（当前为 indexed sources）。
func (this *bootstrapEnvAdapter) Load(envPrefix string) map[string]any {
	result := make(map[string]any)
	sources := collectEnvNacosSources(envPrefix)
	if len(sources) > 0 {
		items := make([]any, 0, len(sources))
		for _, item := range sources {
			items = append(items, item)
		}
		setByDottedKey(result, BootstrapKeyNacosSources, items)
	}
	return result
}
