package ioc

// ResolveAs 按类型解析单个实例并返回强类型结果。
//
// 参数：
// - container：IOC 容器实例，必须非 nil 且已 Build。
//
// 返回：
// - T：解析出的目标实例；
// - error：解析失败时返回语义错误。
func ResolveAs[T any](container Container) (T, error) {
	var zero T
	if container == nil {
		return zero, errorForIOCResolveInvalidTarget(messagef("op=ioc.resolve_as result=failed stage=precheck reason=container_is_nil"))
	}
	var target T
	if err := container.Resolve(&target); err != nil {
		return zero, err
	}
	return target, nil
}

// ResolveAllAs 按类型解析全部实例并返回强类型切片。
//
// 参数：
// - container：IOC 容器实例，必须非 nil 且已 Build。
//
// 返回：
// - []T：同声明类型实例集合（按注册顺序）；
// - error：解析失败时返回语义错误。
func ResolveAllAs[T any](container Container) ([]T, error) {
	if container == nil {
		return nil, errorForIOCResolveInvalidTarget(messagef("op=ioc.resolve_all_as result=failed stage=precheck reason=container_is_nil"))
	}
	var result []T
	if err := container.ResolveAll(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// ResolveByNameAs 按泛型声明类型范围内的名称解析单个实例并返回强类型结果。
//
// 参数：
// - container：IOC 容器实例，必须非 nil 且已 Build；
// - name：bean 名称，必须非空。
//
// 返回：
// - T：解析出的目标实例；
// - error：解析失败时返回语义错误。
func ResolveByNameAs[T any](container Container, name string) (T, error) {
	var zero T
	if container == nil {
		return zero, errorForIOCResolveInvalidTarget(messagef("op=ioc.resolve_by_name_as result=failed stage=precheck reason=container_is_nil"))
	}
	var target T
	if err := container.ResolveByName(name, &target); err != nil {
		return zero, err
	}
	return target, nil
}
