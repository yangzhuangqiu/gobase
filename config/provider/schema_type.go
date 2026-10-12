package provider

import (
	"sort"
	"strings"
)

// BootstrapSchemaType 定义 bootstrap schema 的类型枚举。
type BootstrapSchemaType string

const (
	// BootstrapSchemaTypeString 表示文本类型。
	BootstrapSchemaTypeString BootstrapSchemaType = "string"
	// BootstrapSchemaTypeBool 表示布尔类型。
	BootstrapSchemaTypeBool BootstrapSchemaType = "bool"
	// BootstrapSchemaTypeInt 表示 int 类型。
	BootstrapSchemaTypeInt BootstrapSchemaType = "int"
	// BootstrapSchemaTypeInt64 表示 int64 类型。
	BootstrapSchemaTypeInt64 BootstrapSchemaType = "int64"
	// BootstrapSchemaTypeUint 表示 uint 类型。
	BootstrapSchemaTypeUint BootstrapSchemaType = "uint"
	// BootstrapSchemaTypeUint64 表示 uint64 类型。
	BootstrapSchemaTypeUint64 BootstrapSchemaType = "uint64"
	// BootstrapSchemaTypeFloat32 表示 float32 类型。
	BootstrapSchemaTypeFloat32 BootstrapSchemaType = "float32"
	// BootstrapSchemaTypeFloat64 表示 float64 类型。
	BootstrapSchemaTypeFloat64 BootstrapSchemaType = "float64"
	// BootstrapSchemaTypeList 表示列表类型。
	BootstrapSchemaTypeList BootstrapSchemaType = "list"
	// BootstrapSchemaTypeMap 表示对象/映射类型。
	BootstrapSchemaTypeMap BootstrapSchemaType = "map"
)

var bootstrapSchemaTypeAliases = map[BootstrapSchemaType]BootstrapSchemaType{
	BootstrapSchemaTypeString:  BootstrapSchemaTypeString,
	BootstrapSchemaTypeBool:    BootstrapSchemaTypeBool,
	"boolean":                  BootstrapSchemaTypeBool,
	BootstrapSchemaTypeInt:     BootstrapSchemaTypeInt,
	BootstrapSchemaTypeInt64:   BootstrapSchemaTypeInt64,
	BootstrapSchemaTypeUint:    BootstrapSchemaTypeUint,
	BootstrapSchemaTypeUint64:  BootstrapSchemaTypeUint64,
	"float":                    BootstrapSchemaTypeFloat64,
	BootstrapSchemaTypeFloat32: BootstrapSchemaTypeFloat32,
	BootstrapSchemaTypeFloat64: BootstrapSchemaTypeFloat64,
	BootstrapSchemaTypeList:    BootstrapSchemaTypeList,
	"array":                    BootstrapSchemaTypeList,
	BootstrapSchemaTypeMap:     BootstrapSchemaTypeMap,
	"object":                   BootstrapSchemaTypeMap,
}

// NormalizeBootstrapSchemaType 归一化 schema 类型标识（alias -> canonical）。
func NormalizeBootstrapSchemaType(schemaType BootstrapSchemaType) BootstrapSchemaType {
	schemaType = BootstrapSchemaType(strings.TrimSpace(strings.ToLower(string(schemaType))))
	if schemaType == "" {
		return ""
	}
	normalized, ok := bootstrapSchemaTypeAliases[schemaType]
	if !ok {
		return ""
	}
	return normalized
}

// SupportedBootstrapSchemaTypes 返回允许的 canonical 类型列表。
func SupportedBootstrapSchemaTypes() []BootstrapSchemaType {
	types := []BootstrapSchemaType{
		BootstrapSchemaTypeString,
		BootstrapSchemaTypeBool,
		BootstrapSchemaTypeInt,
		BootstrapSchemaTypeInt64,
		BootstrapSchemaTypeUint,
		BootstrapSchemaTypeUint64,
		BootstrapSchemaTypeFloat32,
		BootstrapSchemaTypeFloat64,
		BootstrapSchemaTypeList,
		BootstrapSchemaTypeMap,
	}
	sort.Slice(types, func(i, j int) bool {
		return types[i] < types[j]
	})
	return types
}

// ValidateBootstrapSchemaTypes 校验 schema 的 Type 字段必须在允许集合内。
func ValidateBootstrapSchemaTypes(schema []BootstrapKey) error {
	allowedTypes := SupportedBootstrapSchemaTypes()
	allowed := make([]string, 0, len(allowedTypes))
	for _, item := range allowedTypes {
		allowed = append(allowed, string(item))
	}
	allowedText := strings.Join(allowed, ",")
	for _, item := range schema {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		normalized := NormalizeBootstrapSchemaType(item.Type)
		if normalized != "" {
			continue
		}
		return errorForProviderBootstrapSchemaTypeInvalid(messagef("invalid bootstrap schema type key=%s type=%q allowed=%s", key, item.Type, allowedText))
	}
	return nil
}
