package global

import baselog "github.com/yangzhuangqiu/gobase/log"

var globalFallbackFactory = baselog.NewLoggerFactory(nil)

// Get 返回指定稳定名称的包级 logger。
func Get(name string) baselog.Logger {
	if runtimeInstance.factory == nil {
		return globalFallbackFactory.Get(name)
	}
	return runtimeInstance.factory.Get(name)
}

// Logger 返回全局 root logger。
func Logger() baselog.Logger {
	if runtimeInstance.logger == nil {
		return globalFallbackFactory.Get("root")
	}
	return runtimeInstance.logger
}

// Provider 返回当前全局组合 Provider，供高级宿主检查或复用。
func Provider() baselog.Provider {
	return runtimeInstance.provider
}

// Close 按日志组合 Provider→ConfigManager 顺序关闭全局资源。
func Close() error {
	return runtimeInstance.shutdown()
}
