package app

import "github.com/spf13/pflag"

// collectOverrideValues 收集命令行 `--override key=value` 明细。
func collectOverrideValues(flagSets []*pflag.FlagSet) ([]keyValueOverride, error) {
	result := make([]keyValueOverride, 0)
	for _, fs := range flagSets {
		if fs == nil {
			continue
		}
		flag := fs.Lookup(overrideFlagName)
		if flag == nil || !flag.Changed {
			continue
		}
		values, err := fs.GetStringArray(overrideFlagName)
		if err != nil {
			return nil, errorForAppCLIFlagReadFailed(messagef("stage=get_flag source=cli key=%s", overrideFlagName), err)
		}
		for _, raw := range values {
			item, parseErr := parseOverrideExpression(raw)
			if parseErr != nil {
				return nil, parseErr
			}
			result = append(result, item)
		}
	}
	return result, nil
}

// parseOverrideExpression 解析单个 `--override key=value` 表达式。
func parseOverrideExpression(raw string) (keyValueOverride, error) {
	item, err := parseKVExpression(raw)
	if err != nil {
		return item, errorf("stage=parse_override source=cli key=%s reason=%w", raw, err)
	}
	return item, nil
}
