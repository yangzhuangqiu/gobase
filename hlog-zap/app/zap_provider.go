package apphlogzap

import (
	"reflect"

	baselog "github.com/yangzhuangqiu/gobase/log"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
)

// zapProviderAccessor 是包装器向 delegate Provider 请求 Zap 核心的最小能力。
// 该接口只用于类型断言，不转移 Provider 或核心的关闭所有权。
type zapProviderAccessor interface {
	ZapProvider() *logzap.Provider
}

// resolveZapProvider 从已交付 Provider 读取同生命周期的 Zap 核心借用视图。
// 阶段角色：hlog 安装前验证 delegate 是否满足包装协议，失败时由调用方关闭 Provider。
func resolveZapProvider(provider baselog.Provider) (*logzap.Provider, bool) {
	if isNilProvider(provider) {
		return nil, false
	}
	accessor, ok := provider.(zapProviderAccessor)
	if !ok {
		return nil, false
	}
	core := accessor.ZapProvider()
	return core, core != nil
}

// isNilProvider 识别 delegate 违反非 nil 返回合同的 typed nil，避免能力断言后 panic。
// 阶段角色：只保护外部 Logging 实现的交付边界。
func isNilProvider(provider baselog.Provider) bool {
	if provider == nil {
		return true
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
