package webhertzobserve

import "reflect"

// isNilValue 统一识别依赖 interface 中的 typed nil，避免 Init 阶段延迟为 panic。
func isNilValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
