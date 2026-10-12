package app

import (
	"strings"

	"github.com/spf13/pflag"
)

// commandFlagScope 表示业务 Flag 是否进入配置覆盖链。
type commandFlagScope string

const (
	// commandFlagScopeAnnotationKey 是业务 Flag 分类的内部 annotation key。
	commandFlagScopeAnnotationKey = "app.flag.scope"
	// commandFlagScopeConfig 表示 Flag 的显式值进入 pinned 配置层。
	commandFlagScopeConfig commandFlagScope = "config"
	// commandFlagScopeFlag 表示 Flag 只绑定当前命令字段，不进入配置层。
	commandFlagScopeFlag commandFlagScope = "flag"
)

// markFlagScope 为刚注册的 Flag 标记 config/flag 语义分类，供 pinned 收集阶段分支。
func (this pflagCommandFlags) markFlagScope(name string, scope commandFlagScope) {
	flag := this.fs.Lookup(name)
	if flag == nil {
		return
	}
	setCommandFlagScope(flag, scope)
}

// setCommandFlagScope 将业务 Flag 分类写入内部 annotation。
func setCommandFlagScope(flag *pflag.Flag, scope commandFlagScope) {
	if flag == nil {
		return
	}
	if flag.Annotations == nil {
		flag.Annotations = map[string][]string{}
	}
	flag.Annotations[commandFlagScopeAnnotationKey] = []string{string(scope)}
}

// isConfigFlag 判断 Flag 是否应在 Runtime Build 前进入 pinned 配置层。
func isConfigFlag(flag *pflag.Flag) bool {
	if flag == nil {
		return false
	}
	scopes, exists := flag.Annotations[commandFlagScopeAnnotationKey]
	if !exists || len(scopes) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(scopes[0]), string(commandFlagScopeConfig))
}

// markFrameworkFlags 在 Flag 注册时打上框架标记，避免后续分类漏配。
func markFrameworkFlags(flagSet *pflag.FlagSet, names ...string) {
	if flagSet == nil {
		return
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		setFrameworkFlag(flagSet.Lookup(name))
	}
}

// setFrameworkFlag 标记某个 Flag 属于框架级参数。
func setFrameworkFlag(flag *pflag.Flag) {
	if flag == nil {
		return
	}
	if flag.Annotations == nil {
		flag.Annotations = map[string][]string{}
	}
	flag.Annotations[commandFrameworkFlagAnnotationKey] = []string{"true"}
}

// isFrameworkFlag 判断是否为框架 Flag，仅基于内部 annotation。
func isFrameworkFlag(flag *pflag.Flag) bool {
	if flag == nil {
		return false
	}
	if values, exists := flag.Annotations[commandFrameworkFlagAnnotationKey]; exists && len(values) > 0 {
		return strings.EqualFold(strings.TrimSpace(values[0]), "true")
	}
	return false
}
