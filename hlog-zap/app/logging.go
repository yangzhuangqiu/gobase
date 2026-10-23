package apphlogzap

import (
	"context"
	"reflect"

	"github.com/yangzhuangqiu/gobase/config"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

// Logging 定义可被 Hertz hlog 能力装饰的应用日志构造合同。
//
// 典型调用场景：app-log-zap Bootstrap 作为 delegate 传给 Wrap，应用 Runtime 仍只观察标准 Build 行为。
type Logging interface {
	// Build 创建本次 Runtime 独占的日志 Provider。
	//
	// 返回:
	// - Provider: 必须同时暴露 ZapProvider 窄能力，成功后由包装器接管关闭；
	// - error: delegate 必须自行回滚尚未交付的半成品。
	Build(ctx context.Context, manager config.Manager) (baselog.Provider, error)
}

// isNilLogging 识别 nil interface 与携带可空具体类型的 typed nil。
// 阶段角色：Wrap 无错误返回值，因此在 Runtime Build 入口阻止 nil delegate panic。
func isNilLogging(logging Logging) bool {
	if logging == nil {
		return true
	}
	value := reflect.ValueOf(logging)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
