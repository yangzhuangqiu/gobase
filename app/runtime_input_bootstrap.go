package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

// collectBootstrapOverrides 解析 bootstrap 专用 flags，映射为结构化 overrides。
func collectBootstrapOverrides(flagSets []*pflag.FlagSet) (map[string]any, error) {
	result := make(map[string]any)
	var errs []error
	configFile := ""
	configSet := false
	configFromBootstrap := ""
	bootstrapConfigSet := false

	for _, fs := range flagSets {
		if fs == nil {
			continue
		}
		current := fs
		if flag := current.Lookup(configFlagName); flag != nil && flag.Changed {
			value, err := current.GetString(configFlagName)
			if err != nil {
				errs = append(errs, errorForAppCLIFlagReadFailed(messagef("stage=get_flag source=cli key=%s", configFlagName), err))
			} else {
				value = strings.TrimSpace(value)
				if configSet && configFile != value {
					errs = append(errs, errorForAppCLIConflict(messagef(fmt.Sprintf("config flag conflict value1=%s value2=%s", configFile, value))))
				}
				configFile = value
				configSet = true
			}
		}
		if flag := current.Lookup(bootstrapFlagName); flag != nil && flag.Changed {
			values, err := current.GetStringArray(bootstrapFlagName)
			if err != nil {
				errs = append(errs, errorForAppCLIFlagReadFailed(messagef("stage=get_flag source=cli key=%s", bootstrapFlagName), err))
				continue
			}
			for _, raw := range values {
				item, parseErr := parseBootstrapExpression(raw)
				if parseErr != nil {
					errs = append(errs, parseErr)
					continue
				}
				setByDottedKey(result, item.Key, item.Val)
				if item.Key == configFlagName {
					configFromBootstrap = strings.TrimSpace(fmt.Sprintf("%v", item.Val))
					bootstrapConfigSet = true
				}
			}
		}
	}
	if configSet {
		if bootstrapConfigSet && configFromBootstrap != configFile {
			errs = append(errs, errorForAppCLIConflict(messagef(fmt.Sprintf("config conflict with --bootstrap config value1=%s value2=%s", configFile, configFromBootstrap))))
		}
		result[configFlagName] = configFile
	}
	if err := errors.Join(errs...); err != nil {
		return nil, errorf("stage=collect_flags reason=%w", err)
	}
	return result, nil
}

// parseBootstrapExpression 解析单个 `--bootstrap key=value` 表达式。
func parseBootstrapExpression(raw string) (keyValueOverride, error) {
	item, err := parseKVExpression(raw)
	if err != nil {
		return item, errorf("stage=parse_bootstrap source=cli key=%s reason=%w", raw, err)
	}
	if strings.HasPrefix(item.Key, bootstrapFlagName+".") {
		return item, errorForAppCLIInvalidBootstrapKey(messagef("stage=parse_bootstrap source=cli key=" + item.Key + " reason=invalid_key remove_prefix=" + bootstrapFlagName + "."))
	}
	if item.Key == configFlagName {
		value := strings.TrimSpace(strings.SplitN(strings.TrimSpace(raw), "=", 2)[1])
		if value == "" {
			return item, errorForAppCLIEmptyConfigFile(messagef("stage=parse_bootstrap source=cli key=" + item.Key + " reason=empty_config_file"))
		}
		item.Val = value
	}
	return item, nil
}

// collectBootstrapEnvPrefix 读取 bootstrap 环境变量前缀。
func collectBootstrapEnvPrefix(flagSets []*pflag.FlagSet) (string, error) {
	selected := ""
	selectedSet := false
	var errs []error
	for _, fs := range flagSets {
		if fs == nil {
			continue
		}
		current := fs
		flag := current.Lookup(bootstrapEnvPrefixFlagName)
		if flag == nil || !flag.Changed {
			continue
		}
		value, err := current.GetString(bootstrapEnvPrefixFlagName)
		if err != nil {
			errs = append(errs, errorForAppCLIFlagReadFailed(messagef("stage=get_flag source=cli key=%s", bootstrapEnvPrefixFlagName), err))
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			errs = append(errs, errorForAppCLIEmptyBootstrapEnvPrefix(messagef("stage=parse_flag source=cli key=%s reason=empty_bootstrap_env_prefix", bootstrapEnvPrefixFlagName)))
			continue
		}
		if selectedSet && selected != value {
			errs = append(errs, errorForAppCLIConflict(messagef("bootstrap env prefix conflict value1=%s value2=%s", selected, value)))
			continue
		}
		selected = value
		selectedSet = true
	}
	if err := errors.Join(errs...); err != nil {
		return "", errorf("stage=collect_flags reason=%w", err)
	}
	return selected, nil
}

// collectBootstrapWithoutConfigFile 读取 bootstrap 阶段是否禁用本地配置文件。
func collectBootstrapWithoutConfigFile(flagSets []*pflag.FlagSet) (bool, error) {
	selected := false
	selectedSet := false
	var errs []error
	for _, fs := range flagSets {
		if fs == nil {
			continue
		}
		current := fs
		flag := current.Lookup(bootstrapWithoutConfigFileFlagName)
		if flag == nil || !flag.Changed {
			continue
		}
		value, err := current.GetBool(bootstrapWithoutConfigFileFlagName)
		if err != nil {
			errs = append(errs, errorForAppCLIFlagReadFailed(messagef("stage=get_flag source=cli key=%s", bootstrapWithoutConfigFileFlagName), err))
			continue
		}
		if selectedSet && selected != value {
			errs = append(errs, errorForAppCLIConflict(messagef("bootstrap without config file conflict value1=%t value2=%t", selected, value)))
			continue
		}
		selected = value
		selectedSet = true
	}
	if err := errors.Join(errs...); err != nil {
		return false, errorf("stage=collect_flags reason=%w", err)
	}
	return selected, nil
}

// resolveBootstrapEnvPrefix 解析启动阶段最终环境变量前缀。
func resolveBootstrapEnvPrefix(appName string, cliPrefix string) string {
	cliPrefix = strings.TrimSpace(cliPrefix)
	if cliPrefix != "" {
		return cliPrefix
	}
	return strings.ReplaceAll(strings.ToUpper(appName), "-", "_")
}
