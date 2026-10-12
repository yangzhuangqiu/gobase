package provider

import (
	"strings"
	"testing"

	"github.com/yangzhuangqiu/gobase/errcode"
)

func TestNormalizeBootstrapSchemaType(t *testing.T) {
	cases := map[BootstrapSchemaType]BootstrapSchemaType{
		BootstrapSchemaTypeString:      BootstrapSchemaTypeString,
		BootstrapSchemaType("STRING"):  BootstrapSchemaTypeString,
		BootstrapSchemaType("boolean"): BootstrapSchemaTypeBool,
		BootstrapSchemaTypeBool:        BootstrapSchemaTypeBool,
		BootstrapSchemaType("array"):   BootstrapSchemaTypeList,
		BootstrapSchemaTypeList:        BootstrapSchemaTypeList,
		BootstrapSchemaType("object"):  BootstrapSchemaTypeMap,
		BootstrapSchemaTypeMap:         BootstrapSchemaTypeMap,
		BootstrapSchemaType("float"):   BootstrapSchemaTypeFloat64,
		BootstrapSchemaTypeFloat64:     BootstrapSchemaTypeFloat64,
	}
	for input, expect := range cases {
		if got := NormalizeBootstrapSchemaType(input); got != expect {
			t.Fatalf("normalize mismatch input=%s expect=%s got=%s", input, expect, got)
		}
	}
}

func TestValidateBootstrapSchemaTypes(t *testing.T) {
	err := ValidateBootstrapSchemaTypes([]BootstrapKey{
		{Key: "config", Type: BootstrapSchemaTypeString},
		{Key: "nacos.timeoutMs", Type: BootstrapSchemaTypeUint64},
		{Key: "nacos.sources", Type: BootstrapSchemaTypeList},
		{Key: "nacos.extra", Type: "object"},
	})
	if err != nil {
		t.Fatalf("expected valid schema, got: %v", err)
	}
}

func TestValidateBootstrapSchemaTypesRejectsUnknownType(t *testing.T) {
	err := ValidateBootstrapSchemaTypes([]BootstrapKey{
		{Key: "nacos.address", Type: "ipv4"},
	})
	if err == nil {
		t.Fatalf("expected schema type invalid error")
	}
	if !errcode.IsCode[ErrorCode](err, ErrCodeProviderBootstrapSchemaTypeInvalid) {
		t.Fatalf("expected code %s, got: %v", ErrCodeProviderBootstrapSchemaTypeInvalid, err)
	}
	if !strings.Contains(err.Error(), "allowed=") {
		t.Fatalf("expected allowed types hint, got: %v", err)
	}
}
