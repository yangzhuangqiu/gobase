package app

import (
	"errors"
	"strconv"

	"github.com/spf13/pflag"
)

// collectPinnedValues 读取命令行中显式设置的“配置参数 flags”，映射到 pinned 层。
func collectPinnedValues(flagSets []*pflag.FlagSet) (map[string]any, error) {
	result := make(map[string]any)
	var errs []error
	for _, fs := range flagSets {
		if fs == nil {
			continue
		}
		current := fs
		current.Visit(func(flag *pflag.Flag) {
			if isFrameworkFlag(flag) {
				return
			}
			if !isConfigFlag(flag) {
				return
			}
			value, err := readFlagValue(current, flag.Name, flag.Value.Type())
			if err != nil {
				errs = append(errs, err)
				return
			}
			setByDottedKey(result, flag.Name, value)
		})
	}
	if err := errors.Join(errs...); err != nil {
		return nil, errorf("stage=collect_flags reason=%w", err)
	}
	return result, nil
}

// readFlagValue 按 flag 类型读取值，避免 string 化后丢失语义。
func readFlagValue(fs *pflag.FlagSet, name, typ string) (any, error) {
	switch typ {
	case "bool":
		return fs.GetBool(name)
	case "int":
		return fs.GetInt(name)
	case "int8":
		v, err := fs.GetInt8(name)
		return int(v), err
	case "int16":
		v, err := fs.GetInt16(name)
		return int(v), err
	case "int32":
		v, err := fs.GetInt32(name)
		return int(v), err
	case "int64":
		return fs.GetInt64(name)
	case "uint":
		return fs.GetUint(name)
	case "uint16":
		v, err := fs.GetUint16(name)
		return uint(v), err
	case "uint32":
		v, err := fs.GetUint32(name)
		return uint(v), err
	case "uint64":
		return fs.GetUint64(name)
	case "float32":
		v, err := fs.GetFloat32(name)
		return float64(v), err
	case "float64":
		return fs.GetFloat64(name)
	case "duration":
		return fs.GetDuration(name)
	case "stringSlice":
		return fs.GetStringSlice(name)
	case "stringArray":
		return fs.GetStringArray(name)
	case "intSlice":
		return fs.GetIntSlice(name)
	case "boolSlice":
		return fs.GetBoolSlice(name)
	case "string":
		return fs.GetString(name)
	default:
		flag := fs.Lookup(name)
		if flag == nil {
			return nil, errorForAppCLIFlagLookupFailed(messagef("stage=lookup_flag source=cli key=" + name + " reason=flag_not_found"))
		}
		if typed, err := tryParseByType(flag.Value.String(), typ); err == nil {
			return typed, nil
		}
		return flag.Value.String(), nil
	}
}

// tryParseByType 在未知 flag 类型下做最小化类型恢复。
func tryParseByType(raw, typ string) (any, error) {
	switch typ {
	case "count", "size":
		return strconv.Atoi(raw)
	default:
		return nil, errorForAppCLIFlagTypeUnsupported(messagef("stage=parse_unknown_type source=cli key=" + typ + " reason=unsupported_type"))
	}
}
