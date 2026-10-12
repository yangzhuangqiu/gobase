package app

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
	"github.com/yangzhuangqiu/gobase/config"
)

// keyValueOverride 是 bootstrap/runtime override 共用的键值语义载体。
type keyValueOverride struct {
	// Key 是去除空白后的 dotted 配置键。
	Key string
	// Raw 是调用方输入的原始表达式，用于错误上下文和字符串值保持。
	Raw string
	// Val 是完成最小类型恢复后的配置值。
	Val any
}

// buildRuntimeInput 把私有 Pflag 状态转换为 app-runtime 稳定输入。
func buildRuntimeInput(cmd *cobra.Command) (appruntime.BuildInput, error) {
	flagSets := []*pflag.FlagSet{cmd.Flags(), cmd.InheritedFlags()}
	bootstrapOverrides, err := collectBootstrapOverrides(flagSets)
	if err != nil {
		return appruntime.BuildInput{}, stageError("collect_bootstrap_overrides", err)
	}
	bootstrapEnvPrefix, err := collectBootstrapEnvPrefix(flagSets)
	if err != nil {
		return appruntime.BuildInput{}, stageError("collect_bootstrap_env_prefix", err)
	}
	disableConfigFile, err := collectBootstrapWithoutConfigFile(flagSets)
	if err != nil {
		return appruntime.BuildInput{}, stageError("collect_bootstrap_without_config_file", err)
	}
	explicitPinnedValues, err := collectPinnedValues(flagSets)
	if err != nil {
		return appruntime.BuildInput{}, stageError("collect_pinned_values", err)
	}
	overrideValues, err := collectOverrideValues(flagSets)
	if err != nil {
		return appruntime.BuildInput{}, stageError("collect_override_values", err)
	}
	pinnedValues, err := buildPinnedValues(explicitPinnedValues, overrideValues)
	if err != nil {
		return appruntime.BuildInput{}, stageError("apply_set_values", err)
	}
	var fileOptions []config.FileOption
	if disableConfigFile {
		fileOptions = []config.FileOption{config.FileDisabled()}
	}
	return appruntime.BuildInput{Config: config.Input{
		BootstrapOverrides: bootstrapOverrides,
		PinnedValues:       pinnedValues,
		EnvPrefix:          bootstrapEnvPrefix,
		File:               fileOptions,
	}}, nil
}

// parseKVExpression 解析 key=value，并恢复字面量类型。
func parseKVExpression(raw string) (keyValueOverride, error) {
	item := keyValueOverride{Raw: raw}
	text := strings.TrimSpace(raw)
	parts := strings.SplitN(text, "=", 2)
	if len(parts) != 2 {
		return item, errorForAppCLIInvalidExpression(messagef("invalid_expression expect=key=value"))
	}
	item.Key = strings.TrimSpace(parts[0])
	if item.Key == "" {
		return item, errorForAppCLIEmptyKey(messagef("empty_key"))
	}
	item.Val = parseLiteralValue(parts[1])
	return item, nil
}

// parseLiteralValue 对 bootstrap/runtime override 共用的 CLI 字面量做最小类型恢复。
//
// 解析规则：
// 1. bool/null；
// 2. 十进制整数（前导零保留字符串）；
// 3. 浮点（含 . / e / E）；
// 4. 其余保留字符串。
func parseLiteralValue(raw string) any {
	trimmed := strings.TrimSpace(raw)
	switch strings.ToLower(trimmed) {
	case "true":
		return true
	case "false":
		return false
	case "null", "nil", "~":
		return nil
	}

	if isIntegerLiteral(trimmed) {
		if shouldKeepIntegerAsString(trimmed) {
			return raw
		}
		if value, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			return value
		}
	}
	if isFloatLiteral(trimmed) {
		if value, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return value
		}
	}
	return raw
}

// isIntegerLiteral 判断字符串是否为十进制整数字面量。
func isIntegerLiteral(text string) bool {
	if text == "" {
		return false
	}
	if strings.HasPrefix(text, "+") {
		return false
	}
	if strings.HasPrefix(text, "-") {
		text = strings.TrimPrefix(text, "-")
	}
	if text == "" {
		return false
	}
	for _, ch := range text {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// shouldKeepIntegerAsString 判断整数文本是否应保留为字符串（避免前导零丢失）。
func shouldKeepIntegerAsString(text string) bool {
	if strings.HasPrefix(text, "-") {
		text = strings.TrimPrefix(text, "-")
	}
	return len(text) > 1 && strings.HasPrefix(text, "0")
}

// isFloatLiteral 判断字符串是否为浮点字面量。
func isFloatLiteral(text string) bool {
	if text == "" {
		return false
	}
	if !strings.ContainsAny(text, ".eE") {
		return false
	}
	if strings.HasPrefix(text, "+") {
		return false
	}
	_, err := strconv.ParseFloat(text, 64)
	return err == nil
}

// buildPinnedValues 生成运行时 pinned：`--override` 先应用，显式业务 flags 后覆盖。
func buildPinnedValues(explicit map[string]any, overrideValues []keyValueOverride) (map[string]any, error) {
	result := make(map[string]any)
	for _, item := range overrideValues {
		setByDottedKey(result, item.Key, item.Val)
	}
	mergeNestedMap(result, explicit)
	return result, nil
}

// setByDottedKey 按 dotted key 写入嵌套 map。
func setByDottedKey(target map[string]any, dottedKey string, value any) {
	parts := strings.Split(dottedKey, ".")
	if len(parts) == 0 {
		return
	}
	cursor := target
	for index := 0; index < len(parts)-1; index++ {
		part := strings.TrimSpace(parts[index])
		if part == "" {
			return
		}
		next, ok := cursor[part]
		if !ok {
			child := make(map[string]any)
			cursor[part] = child
			cursor = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			child = make(map[string]any)
			cursor[part] = child
		}
		cursor = child
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	if last == "" {
		return
	}
	cursor[last] = value
}

// mergeNestedMap 把 src 覆盖合并到 dst（map 递归、标量覆盖）。
func mergeNestedMap(dst map[string]any, src map[string]any) {
	if len(src) == 0 {
		return
	}
	for key, value := range src {
		current, exists := dst[key]
		if !exists {
			dst[key] = value
			continue
		}
		left, leftOK := current.(map[string]any)
		right, rightOK := value.(map[string]any)
		if leftOK && rightOK {
			mergeNestedMap(left, right)
			continue
		}
		dst[key] = value
	}
}
