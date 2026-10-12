package appwebhertz

import (
	"context"
	"strings"

	baseconfig "github.com/yangzhuangqiu/gobase/config"
)

const (
	// DefaultScope 是 IOC Module 默认读取的独立 Hertz 配置路径。
	DefaultScope = "web.hertz"
)

// resolveScope 归一化 IOC Module 的独立配置路径。
// 阶段角色：空白声明回退稳定默认值，非空值原样作为 dotted scope 使用。
func resolveScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return DefaultScope
	}
	return scope
}

// loadConfig 从 Manager 当前快照读取并校验一次 Server/标准 Middleware 启动配置。
//
// 阶段角色：IOC Server bean 初始化时读取一次；监听地址等启动配置不订阅热更新。
func loadConfig(ctx context.Context, manager baseconfig.Manager, scope string) (Config, error) {
	if ctx == nil {
		return Config{}, errorForConfigLoadFailed(
			messagef("op=app_web_hertz.load_config result=failed stage=precheck scope=%s reason=context_required", scope),
		)
	}
	if isNilInterface(manager) {
		return Config{}, errorForConfigLoadFailed(
			messagef("op=app_web_hertz.load_config result=failed stage=precheck scope=%s reason=manager_required", scope),
		)
	}
	if err := ctx.Err(); err != nil {
		return Config{}, errorForConfigLoadFailed(
			messagef("op=app_web_hertz.load_config result=failed stage=precheck scope=%s reason=context_done", scope),
			err,
		)
	}
	snapshot := manager.Current()
	if snapshot == nil {
		return Config{}, errorForConfigLoadFailed(
			messagef("op=app_web_hertz.load_config result=failed stage=current scope=%s reason=snapshot_not_initialized", scope),
		)
	}

	// 第一阶段：scope 缺失或类型不匹配由 config 保留稳定 cause。
	var input Config
	if err := snapshot.Decode(scope, &input); err != nil {
		return Config{}, errorForConfigLoadFailed(
			messagef("op=app_web_hertz.load_config result=failed stage=decode scope=%s reason=decode_failed", scope),
			err,
		)
	}
	// 第二阶段：从同一 Snapshot 同时冻结 provider 与标准 Middleware 配置。
	resolved, err := resolveConfiguredConfig(input)
	if err != nil {
		return Config{}, errorForConfigLoadFailed(
			messagef("op=app_web_hertz.load_config result=failed stage=validate_config scope=%s reason=config_invalid", scope),
			err,
		)
	}
	return resolved, nil
}
