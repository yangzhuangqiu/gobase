package webhertz

import (
	"reflect"

	baselog "github.com/yangzhuangqiu/gobase/log"
)

// WithRequestObservers 在声明位置安装单个完成态 Middleware，并按顺序 fan-out Observer。
//
// 参数:
// - observers: 必填且不得包含 typed nil；声明顺序决定元数据合并和完成通知顺序。
//
// 说明:
// - Observer panic 会被隔离并记录稳定诊断，不影响响应或后续 Observer；
// - 多次调用会安装多个独立计时边界，通常应由装配层聚合后只调用一次。
func WithRequestObservers(observers ...RequestObserver) Option {
	return func(target *buildOptions) error {
		resolved := make([]RequestObserver, 0, len(observers))
		for index, observer := range observers {
			if isNilRequestObserver(observer) {
				return errorForDeclarationInvalid(messagef("op=web_hertz.option result=failed stage=declare_request_observers reason=observer_required observer_index=%d", index))
			}
			resolved = append(resolved, observer)
		}
		if len(resolved) > 0 {
			// Logger 在全部 Option 解析后才冻结；延迟读取避免声明顺序改变诊断出口。
			target.middleware = append(target.middleware, newRequestObservationMiddleware(resolved, func() baselog.Logger { return target.logger }))
		}
		return nil
	}
}

// isNilRequestObserver 识别 nil interface 和携带可空具体类型的 typed nil。
func isNilRequestObserver(observer RequestObserver) bool {
	if observer == nil {
		return true
	}
	value := reflect.ValueOf(observer)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
