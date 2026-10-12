package app

import (
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/yangzhuangqiu/gobase/errcode"
)

// TestParseBootstrapExpressionRejectsLegacyPrefix 验证 bootstrap 参数不接受重复 bootstrap. 前缀。
//
// 测试方法：
// 1. 解析 bootstrap.nacos.address 表达式；
// 2. 断言返回 invalid_bootstrap_key；
// 3. 检查错误包含可操作的 remove_prefix 提示。
func TestParseBootstrapExpressionRejectsLegacyPrefix(t *testing.T) {
	_, err := parseBootstrapExpression("bootstrap.nacos.address=127.0.0.1")
	if !errcode.IsCode[ErrorCode](err, ErrCodeAppCLIInvalidBootstrapKey) {
		t.Fatalf("expect invalid bootstrap key, got=%v", err)
	}
	if !strings.Contains(err.Error(), "remove_prefix=bootstrap.") {
		t.Fatalf("expect migration hint, got=%v", err)
	}
}

// TestCollectBootstrapOverridesMergesEquivalentConfig 验证 --config 与 bootstrap config 等值时合并为单一输入。
//
// 测试方法：
// 1. 在同一 FlagSet 设置 --config 与 --bootstrap config=...；
// 2. 调用 collectBootstrapOverrides；
// 3. 断言没有冲突且最终 config 值稳定。
func TestCollectBootstrapOverridesMergesEquivalentConfig(t *testing.T) {
	flags := pflag.NewFlagSet("bootstrap", pflag.ContinueOnError)
	flags.String(configFlagName, "", "")
	flags.StringArray(bootstrapFlagName, nil, "")
	_ = flags.Set(configFlagName, "application.yaml")
	_ = flags.Set(bootstrapFlagName, "config=application.yaml")
	values, err := collectBootstrapOverrides([]*pflag.FlagSet{flags})
	if err != nil {
		t.Fatalf("collect bootstrap failed: %v", err)
	}
	if values[configFlagName] != "application.yaml" {
		t.Fatalf("unexpected config value: %#v", values)
	}
}

// TestCollectBootstrapOverridesRejectsConfigConflict 验证两个配置文件入口不一致时 fail-fast。
//
// 测试方法：
// 1. 分别设置 --config 与 --bootstrap config；
// 2. 使用不同值收集输入；
// 3. 断言返回 app.cli.conflict。
func TestCollectBootstrapOverridesRejectsConfigConflict(t *testing.T) {
	flags := pflag.NewFlagSet("bootstrap", pflag.ContinueOnError)
	flags.String(configFlagName, "", "")
	flags.StringArray(bootstrapFlagName, nil, "")
	_ = flags.Set(configFlagName, "a.yaml")
	_ = flags.Set(bootstrapFlagName, "config=b.yaml")
	_, err := collectBootstrapOverrides([]*pflag.FlagSet{flags})
	if !errcode.IsCode[ErrorCode](err, ErrCodeAppCLIConflict) {
		t.Fatalf("expect config conflict, got=%v", err)
	}
}

// TestCollectBootstrapEnvPrefixRejectsConflictingScopes 验证 local/inherited 前缀不一致时拒绝启动。
//
// 测试方法：
// 1. 两个 FlagSet 显式设置不同 env prefix；
// 2. 调用 collectBootstrapEnvPrefix；
// 3. 断言返回 app.cli.conflict。
func TestCollectBootstrapEnvPrefixRejectsConflictingScopes(t *testing.T) {
	local := pflag.NewFlagSet("local", pflag.ContinueOnError)
	inherited := pflag.NewFlagSet("inherited", pflag.ContinueOnError)
	local.String(bootstrapEnvPrefixFlagName, "", "")
	inherited.String(bootstrapEnvPrefixFlagName, "", "")
	_ = local.Set(bootstrapEnvPrefixFlagName, "APP")
	_ = inherited.Set(bootstrapEnvPrefixFlagName, "OTHER")
	_, err := collectBootstrapEnvPrefix([]*pflag.FlagSet{local, inherited})
	if !errcode.IsCode[ErrorCode](err, ErrCodeAppCLIConflict) {
		t.Fatalf("expect env prefix conflict, got=%v", err)
	}
}

// TestCollectWithoutConfigFileRejectsConflictingScopes 验证禁用文件策略在命令层出现矛盾时 fail-fast。
//
// 测试方法：
// 1. local/inherited 分别设置 true/false；
// 2. 调用 collectBootstrapWithoutConfigFile；
// 3. 断言返回 app.cli.conflict。
func TestCollectWithoutConfigFileRejectsConflictingScopes(t *testing.T) {
	local := pflag.NewFlagSet("local", pflag.ContinueOnError)
	inherited := pflag.NewFlagSet("inherited", pflag.ContinueOnError)
	local.Bool(bootstrapWithoutConfigFileFlagName, false, "")
	inherited.Bool(bootstrapWithoutConfigFileFlagName, false, "")
	_ = local.Set(bootstrapWithoutConfigFileFlagName, "true")
	_ = inherited.Set(bootstrapWithoutConfigFileFlagName, "false")
	_, err := collectBootstrapWithoutConfigFile([]*pflag.FlagSet{local, inherited})
	if !errcode.IsCode[ErrorCode](err, ErrCodeAppCLIConflict) {
		t.Fatalf("expect without-file conflict, got=%v", err)
	}
}
