package app

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/config/provider"
)

// handleListBootstrapKeys 在设置 list 标志时输出 bootstrap 键并结束执行链。
func (this *App) handleListBootstrapKeys(cmd *cobra.Command) (bool, error) {
	mode, err := cmd.Flags().GetString(listBootstrapKeysFlagName)
	if err != nil {
		return false, err
	}
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		return false, nil
	}
	switch mode {
	case "table":
		this.printBootstrapKeysTable(cmd.OutOrStdout())
		return true, nil
	case "json":
		return true, this.printBootstrapKeysJSON(cmd.OutOrStdout())
	default:
		return false, errorForAppListBootstrapKeysInvalidMode(messagef(fmt.Sprintf("invalid list-bootstrap-keys mode=%s, supported=table|json", mode)))
	}
}

type bootstrapSchemaEntry struct {
	Provider string
	Key      provider.BootstrapKey
}

type bootstrapSchemaJSONOutput struct {
	Contract bootstrapSchemaJSONContract `json:"contract"`
	Keys     []bootstrapSchemaJSONKey    `json:"keys"`
}

type bootstrapSchemaJSONContract struct {
	ConfigFlag    string `json:"configFlag"`
	BootstrapFlag string `json:"bootstrapFlag"`
	EnvPrefixFlag string `json:"envPrefixFlag"`
	NoFileFlag    string `json:"noFileFlag"`
}

type bootstrapSchemaJSONKey struct {
	Provider    string `json:"provider"`
	Key         string `json:"key"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

// printBootstrapKeysTable 输出核心+provider 的 bootstrap 键定义（表格格式）。
func (this *App) printBootstrapKeysTable(output io.Writer) {
	entries := this.collectBootstrapSchemaEntries()
	_, _ = fmt.Fprintln(output, "Bootstrap CLI contract:")
	_, _ = fmt.Fprintf(output, "  --%s <file>\n", configFlagName)
	_, _ = fmt.Fprintf(output, "  --%s key=value (repeatable)\n", bootstrapFlagName)
	_, _ = fmt.Fprintf(output, "  --%s <prefix>\n", bootstrapEnvPrefixFlagName)
	_, _ = fmt.Fprintf(output, "  --%s\n", bootstrapWithoutConfigFileFlagName)
	_, _ = fmt.Fprintln(output, "Supported bootstrap keys (table):")
	writer := tabwriter.NewWriter(output, 4, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "KEY\tTYPE\tREQUIRED\tDEFAULT\tPROVIDER\tDESCRIPTION")
	for _, item := range entries {
		required := "no"
		if item.Key.Required {
			required = "yes"
		}
		defaultValue := strings.TrimSpace(item.Key.DefaultValue)
		if defaultValue == "" {
			defaultValue = "-"
		}
		description := strings.TrimSpace(item.Key.Description)
		if description == "" {
			description = "-"
		}
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n",
			item.Key.Key,
			normalizeSchemaType(item.Key.Type),
			required,
			defaultValue,
			item.Provider,
			description,
		)
	}
	_ = writer.Flush()
}

// printBootstrapKeysJSON 输出核心+provider 的 bootstrap 键定义（JSON 格式）。
func (this *App) printBootstrapKeysJSON(output io.Writer) error {
	entries := this.collectBootstrapSchemaEntries()
	result := bootstrapSchemaJSONOutput{
		Contract: bootstrapSchemaJSONContract{
			ConfigFlag:    "--" + configFlagName + " <file>",
			BootstrapFlag: "--" + bootstrapFlagName + " key=value (repeatable)",
			EnvPrefixFlag: "--" + bootstrapEnvPrefixFlagName + " <prefix>",
			NoFileFlag:    "--" + bootstrapWithoutConfigFileFlagName,
		},
		Keys: make([]bootstrapSchemaJSONKey, 0, len(entries)),
	}
	for _, item := range entries {
		defaultValue := strings.TrimSpace(item.Key.DefaultValue)
		if defaultValue == "" {
			defaultValue = "-"
		}
		description := strings.TrimSpace(item.Key.Description)
		if description == "" {
			description = "-"
		}
		result.Keys = append(result.Keys, bootstrapSchemaJSONKey{
			Provider:    item.Provider,
			Key:         item.Key.Key,
			Type:        normalizeSchemaType(item.Key.Type),
			Required:    item.Key.Required,
			Default:     defaultValue,
			Description: description,
		})
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// collectBootstrapSchemaEntries 汇总并去重核心与 provider bootstrap 键定义。
func (this *App) collectBootstrapSchemaEntries() []bootstrapSchemaEntry {
	combined := make([]bootstrapSchemaEntry, 0, 16)
	for _, item := range config.CoreBootstrapSchema() {
		combined = append(combined, bootstrapSchemaEntry{Provider: "core", Key: item})
	}
	providerSchemas := this.runtimeApplication.BootstrapSchemas()
	providerNames := make([]string, 0, len(providerSchemas))
	for name := range providerSchemas {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)
	for _, name := range providerNames {
		for _, item := range providerSchemas[name] {
			combined = append(combined, bootstrapSchemaEntry{Provider: name, Key: item})
		}
	}

	seen := make(map[string]bootstrapSchemaEntry, len(combined))
	for _, item := range combined {
		key := strings.TrimSpace(item.Key.Key)
		if key == "" {
			continue
		}
		item.Key.Key = key
		unique := item.Provider + "::" + key
		if _, exists := seen[unique]; exists {
			continue
		}
		seen[unique] = item
	}

	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]bootstrapSchemaEntry, 0, len(keys))
	for _, key := range keys {
		result = append(result, seen[key])
	}
	return result
}

// normalizeSchemaType 统一 schema type 展示文本。
func normalizeSchemaType(value provider.BootstrapSchemaType) string {
	value = provider.BootstrapSchemaType(strings.TrimSpace(string(value)))
	if value == "" {
		return string(provider.BootstrapSchemaTypeString)
	}
	if normalized := provider.NormalizeBootstrapSchemaType(value); normalized != "" {
		return string(normalized)
	}
	return string(value)
}
