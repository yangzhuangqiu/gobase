package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// loadYAMLFile 读取本地 YAML 文件并转换为 map。
// 语义：仅用于 config 内核组装阶段，不作为对外 API。
func loadYAMLFile(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseYAMLBytes(content)
}

// parseYAMLBytes 将 YAML 字节解析为 map 并做 map key 归一化。
// 语义：空内容返回空 map，便于上层统一 merge。
func parseYAMLBytes(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return make(map[string]any), nil
	}
	result := make(map[string]any)
	if err := yaml.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return normalizeMap(result), nil
}
