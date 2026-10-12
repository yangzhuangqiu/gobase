package ioc

import (
	"reflect"
	"slices"
	"strings"
)

const defaultTagKey = "ioc"

type fieldRule struct {
	// injectEnable 表示字段是否进入注入流程。
	injectEnable bool
	// required 表示依赖缺失时是否 fail-fast。
	required bool
}

// injectBean 执行单个 bean 的字段注入。
//
// 阶段角色：
// 1. Build 阶段对 struct bean 写入依赖；
// 2. 仅处理 interface/ptr/slice/map 四类字段；
// 3. 其余字段保持原值，不参与注入。
//
// 流程：
// 1. 先判断 bean 是否参与注入以及是否是 struct；
// 2. 再按字段逐个解析注入规则；
// 3. 最后按字段类型分派具体注入器。
func (this *container) injectBean(bean *beanDefinition) error {
	// 阶段一：仅处理允许注入的 bean，关闭注入直接跳过。
	if !bean.injectEnable {
		return nil
	}
	// 阶段二：仅 struct（含指针解引用）支持字段注入。
	structValue, ok := dereferenceStruct(bean.instanceValue)
	if !ok {
		return nil
	}
	structType := structValue.Type()
	// 阶段三：逐字段解析注入规则并路由到具体注入器。
	for index := 0; index < structValue.NumField(); index++ {
		field := structType.Field(index)
		fieldValue := structValue.Field(index)
		rule := parseFieldRule(field.Tag)
		if !rule.injectEnable {
			continue
		}
		if !fieldValue.CanSet() {
			continue
		}
		if err := this.injectField(bean, field, fieldValue, rule); err != nil {
			return err
		}
	}
	return nil
}

// injectField 根据字段类型路由注入逻辑。
// 阶段角色：字段注入阶段的类型分发入口。
func (this *container) injectField(owner *beanDefinition, field reflect.StructField, fieldValue reflect.Value, rule fieldRule) error {
	fieldType := field.Type
	switch fieldType.Kind() {
	case reflect.Interface, reflect.Ptr:
		// 单值依赖：按唯一候选或字段名分流策略注入。
		dependency, ok, err := this.resolveSingleFieldDependency(field.Name, fieldType, owner.name)
		if err != nil {
			return err
		}
		if !ok {
			if rule.required {
				return errorForIOCBuildDependencyMiss(messagef("op=ioc.build result=failed stage=inject reason=dependency_not_found owner=%s field=%s type=%s",
					owner.name, field.Name, fieldType,
				))
			}
			return nil
		}
		fieldValue.Set(dependency.instanceValue)
	case reflect.Slice:
		// 集合依赖：注入该声明类型的全部候选。
		return this.injectSliceField(owner, field, fieldValue, rule)
	case reflect.Map:
		// 映射依赖：按 map key 规则注入全量候选。
		return this.injectMapField(owner, field, fieldValue, rule)
	default:
		// 语义：非依赖类型字段跳过，避免污染业务值字段。
		return nil
	}
	return nil
}

// injectSliceField 按声明类型注入切片依赖集合。
func (this *container) injectSliceField(owner *beanDefinition, field reflect.StructField, fieldValue reflect.Value, rule fieldRule) error {
	elementType := field.Type.Elem()
	if elementType.Kind() != reflect.Interface && elementType.Kind() != reflect.Ptr {
		return nil
	}
	// 阶段一：按元素声明类型拉取全部候选。
	dependencies := this.lookupByType(elementType)
	if len(dependencies) == 0 {
		if rule.required {
			return errorForIOCBuildDependencyMiss(messagef("op=ioc.build result=failed stage=inject reason=slice_dependency_not_found owner=%s field=%s type=%s",
				owner.name, field.Name, elementType,
			))
		}
		return nil
	}
	// 阶段二：构造新切片并顺序写入候选实例。
	result := reflect.MakeSlice(fieldValue.Type(), 0, len(dependencies))
	for _, item := range dependencies {
		result = reflect.Append(result, item.instanceValue)
	}
	fieldValue.Set(result)
	return nil
}

// injectMapField 按声明类型注入 map[string]T 依赖集合。
func (this *container) injectMapField(owner *beanDefinition, field reflect.StructField, fieldValue reflect.Value, rule fieldRule) error {
	if field.Type.Key().Kind() != reflect.String {
		return errorForIOCInternal(messagef("op=ioc.build result=failed stage=inject reason=map_key_must_string owner=%s field=%s type=%s",
			owner.name, field.Name, field.Type,
		))
	}
	valueType := field.Type.Elem()
	if valueType.Kind() != reflect.Interface && valueType.Kind() != reflect.Ptr {
		return nil
	}
	// 阶段一：按 value 声明类型拉取全部候选。
	dependencies := this.lookupByType(valueType)
	if len(dependencies) == 0 {
		if rule.required {
			return errorForIOCBuildDependencyMiss(messagef("op=ioc.build result=failed stage=inject reason=map_dependency_not_found owner=%s field=%s type=%s",
				owner.name, field.Name, valueType,
			))
		}
		return nil
	}
	// 阶段二：构造 map 并按 key 规则写入每个候选。
	result := reflect.MakeMap(fieldValue.Type())
	for _, item := range dependencies {
		// bean 名称已在当前 valueType 范围内保证唯一，可直接作为稳定 map key。
		result.SetMapIndex(reflect.ValueOf(item.name), item.instanceValue)
	}
	fieldValue.Set(result)
	return nil
}

// collectDependencies 收集某个 bean 在 Build 图中的依赖边。
//
// 流程：
// 1. 遍历可注入字段；
// 2. 计算字段依赖并写入去重集合；
// 3. 返回稳定排序后的依赖列表。
func (this *container) collectDependencies(bean *beanDefinition) ([]*beanDefinition, error) {
	if !bean.injectEnable {
		return nil, nil
	}
	// 阶段一：仅 struct bean 参与依赖收集。
	structValue, ok := dereferenceStruct(bean.instanceValue)
	if !ok {
		return nil, nil
	}
	structType := structValue.Type()
	edges := map[*beanDefinition]struct{}{}
	// 阶段二：逐字段收集依赖并去重。
	for index := 0; index < structValue.NumField(); index++ {
		field := structType.Field(index)
		fieldValue := structValue.Field(index)
		rule := parseFieldRule(field.Tag)
		if !rule.injectEnable {
			continue
		}
		if !fieldValue.CanSet() {
			continue
		}
		items, err := this.dependenciesForField(bean, field, rule)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			edges[item] = struct{}{}
		}
	}
	// 阶段三：输出稳定顺序，避免拓扑并列节点抖动。
	result := make([]*beanDefinition, 0, len(edges))
	for item := range edges {
		result = append(result, item)
	}
	slices.SortFunc(result, func(left *beanDefinition, right *beanDefinition) int {
		return left.seq - right.seq
	})
	return result, nil
}

// dependenciesForField 返回字段依赖集合（用于拓扑排序）。
//
// 流程：
// 1. 根据字段种类选择依赖收集策略；
// 2. required=true 时缺失依赖直接失败；
// 3. 返回该字段涉及的全部候选 bean。
func (this *container) dependenciesForField(owner *beanDefinition, field reflect.StructField, rule fieldRule) ([]*beanDefinition, error) {
	fieldType := field.Type
	switch fieldType.Kind() {
	case reflect.Interface, reflect.Ptr:
		// 单值依赖：匹配唯一候选或字段名分流。
		item, ok, err := this.resolveSingleFieldDependency(field.Name, fieldType, owner.name)
		if err != nil {
			return nil, err
		}
		if !ok {
			if rule.required {
				return nil, errorForIOCBuildDependencyMiss(messagef("op=ioc.build result=failed stage=collect_dependency reason=dependency_not_found owner=%s field=%s type=%s",
					owner.name, field.Name, fieldType,
				))
			}
			return nil, nil
		}
		return []*beanDefinition{item}, nil
	case reflect.Slice:
		// 切片依赖：返回同声明类型的全量候选。
		elementType := fieldType.Elem()
		if elementType.Kind() != reflect.Interface && elementType.Kind() != reflect.Ptr {
			return nil, nil
		}
		items := this.lookupByType(elementType)
		if len(items) == 0 && rule.required {
			return nil, errorForIOCBuildDependencyMiss(messagef("op=ioc.build result=failed stage=collect_dependency reason=slice_dependency_not_found owner=%s field=%s type=%s",
				owner.name, field.Name, elementType,
			))
		}
		return items, nil
	case reflect.Map:
		// Map 依赖：key 必须 string，value 走类型匹配。
		if fieldType.Key().Kind() != reflect.String {
			return nil, errorForIOCInternal(messagef("op=ioc.build result=failed stage=collect_dependency reason=map_key_must_string owner=%s field=%s type=%s",
				owner.name, field.Name, fieldType,
			))
		}
		valueType := fieldType.Elem()
		if valueType.Kind() != reflect.Interface && valueType.Kind() != reflect.Ptr {
			return nil, nil
		}
		items := this.lookupByType(valueType)
		if len(items) == 0 && rule.required {
			return nil, errorForIOCBuildDependencyMiss(messagef("op=ioc.build result=failed stage=collect_dependency reason=map_dependency_not_found owner=%s field=%s type=%s",
				owner.name, field.Name, valueType,
			))
		}
		return items, nil
	default:
		return nil, nil
	}
}

// resolveSingleFieldDependency 解析 interface/ptr 字段依赖。
// 阶段角色：单值字段依赖决策（唯一/按名分流/歧义失败）。
func (this *container) resolveSingleFieldDependency(fieldName string, fieldType reflect.Type, ownerName string) (*beanDefinition, bool, error) {
	items := this.lookupByType(fieldType)
	if len(items) == 0 {
		return nil, false, nil
	}
	if len(items) == 1 {
		return items[0], true, nil
	}
	// 名称只在当前字段类型的候选集合内参与消歧，不进行全局名称查找。
	for _, item := range items {
		if item.name == fieldName {
			return item, true, nil
		}
	}
	return nil, false, errorForIOCBuildDependencyMulti(messagef("op=ioc.build result=failed stage=resolve_dependency reason=multiple_candidate owner=%s field=%s type=%s count=%d",
		ownerName, fieldName, fieldType, len(items),
	))
}

// lookupByType 返回声明类型匹配的 bean 列表（按注册顺序）。
func (this *container) lookupByType(declareType reflect.Type) []*beanDefinition {
	items := append([]*beanDefinition{}, this.beansByType[declareType]...)
	slices.SortFunc(items, func(left *beanDefinition, right *beanDefinition) int {
		return left.seq - right.seq
	})
	return items
}

// parseFieldRule 解析 ioc tag 的注入规则。
//
// 语义：
// 1. 默认 inject=true, required=true；
// 2. `ioc:"required=false"` 表示依赖缺失时跳过；
// 3. `ioc:"-"` 表示显式跳过该字段注入。
func parseFieldRule(tag reflect.StructTag) fieldRule {
	raw := strings.TrimSpace(tag.Get(defaultTagKey))
	result := fieldRule{injectEnable: true, required: true}
	// 空 tag：使用默认 required=true 语义。
	if raw == "" {
		return result
	}
	// 显式跳过：该字段完全不参与注入。
	if raw == "-" {
		return fieldRule{injectEnable: false, required: false}
	}
	// 键值解析：目前仅处理 required=true/false。
	for _, token := range strings.Split(raw, ",") {
		token = strings.TrimSpace(token)
		switch token {
		case "required=false":
			result.required = false
		case "required=true":
			result.required = true
		}
	}
	return result
}

// dereferenceStruct 取指针实际值，并判断是否为 struct。
func dereferenceStruct(value reflect.Value) (reflect.Value, bool) {
	if !value.IsValid() {
		return reflect.Value{}, false
	}
	// 解引用链：支持多层指针直到拿到实际值。
	current := value
	for current.Kind() == reflect.Ptr {
		if current.IsNil() {
			return reflect.Value{}, false
		}
		current = current.Elem()
	}
	if current.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	return current, true
}
