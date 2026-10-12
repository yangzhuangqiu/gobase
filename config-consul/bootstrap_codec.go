package consul

import "strings"

type bootstrapCodec struct{}

var (
	// 由 BootstrapKeyConsul* 常量派生字段名，避免散落魔法字符串。
	bootstrapConsulRootKey      = bootstrapRootKey(BootstrapKeyConsulAddress)
	bootstrapFieldConsulAddress = bootstrapLeafKey(BootstrapKeyConsulAddress)
	bootstrapFieldConsulScheme  = bootstrapLeafKey(BootstrapKeyConsulScheme)
	bootstrapFieldConsulDC      = bootstrapLeafKey(BootstrapKeyConsulDatacenter)
	bootstrapFieldConsulNS      = bootstrapLeafKey(BootstrapKeyConsulNamespace)
	bootstrapFieldConsulToken   = bootstrapLeafKey(BootstrapKeyConsulToken)
	bootstrapFieldConsulSources = bootstrapLeafKey(BootstrapKeyConsulSources)
	bootstrapFieldConsulWaitMs  = bootstrapLeafKey(BootstrapKeyConsulWaitTimeMs)
)

func newBootstrapCodec() *bootstrapCodec {
	return &bootstrapCodec{}
}

// Decode 将 bootstrap.consul map 解析为结构化 Bootstrap。
//
// 语义：
// 1. 未声明 consul 块时返回 nil,nil；
// 2. 对未知字段 fail-fast，避免静默吞错；
// 3. address 是唯一规范地址字段，未知别名直接报错。
func (this *bootstrapCodec) Decode(values map[string]any) (*Bootstrap, error) {
	raw, ok := lookupByDottedKey(values, bootstrapConsulRootKey)
	if !ok || raw == nil {
		return nil, nil
	}
	consulMap, ok := normalizeValue(raw).(map[string]any)
	if !ok {
		return nil, errorForConsulInvalidBootstrap(messagef("invalid consul bootstrap block"))
	}

	allowed := map[string]struct{}{
		bootstrapFieldConsulAddress: {},
		bootstrapFieldConsulScheme:  {},
		bootstrapFieldConsulDC:      {},
		bootstrapFieldConsulNS:      {},
		bootstrapFieldConsulToken:   {},
		bootstrapFieldConsulWaitMs:  {},
		bootstrapFieldConsulSources: {},
	}
	for key := range consulMap {
		if _, exists := allowed[key]; !exists {
			return nil, errorForConsulInvalidBootstrap(messagef("invalid consul bootstrap field " + key))
		}
	}
	if err := validateStringFieldTypes(consulMap,
		bootstrapFieldConsulAddress,
		bootstrapFieldConsulScheme,
		bootstrapFieldConsulDC,
		bootstrapFieldConsulNS,
		bootstrapFieldConsulToken,
	); err != nil {
		return nil, err
	}

	spec := &Bootstrap{
		Address:    readStringFromMap(consulMap, bootstrapFieldConsulAddress),
		Scheme:     readStringFromMap(consulMap, bootstrapFieldConsulScheme),
		Datacenter: readStringFromMap(consulMap, bootstrapFieldConsulDC),
		Namespace:  readStringFromMap(consulMap, bootstrapFieldConsulNS),
		Token:      readStringFromMap(consulMap, bootstrapFieldConsulToken),
	}
	waitTimeMs, err := readUint64FromMap(consulMap, bootstrapFieldConsulWaitMs)
	if err != nil {
		return nil, err
	}
	if err = validateConsulWaitTime(waitTimeMs); err != nil {
		return nil, err
	}
	spec.WaitTimeMs = waitTimeMs

	if rawSources, exists := consulMap[bootstrapFieldConsulSources]; exists {
		sources, err := parseSourcesValue(rawSources)
		if err != nil {
			return nil, err
		}
		spec.Sources = sources
	}

	if this.isZero(spec) {
		return nil, nil
	}
	return spec, nil
}

// toBinding 把结构化 Bootstrap 转换为内部连接声明。
//
// 语义：
// 1. spec 为空或无有效字段时返回 nil；
// 2. sources 与 ClientOptions 分别映射到 binding 的两类输入。
func (this *bootstrapCodec) toBinding(spec *Bootstrap) *binding {
	if spec == nil || this.isZero(spec) {
		return nil
	}
	binding := &binding{
		Sources: append([]BootstrapSource{}, spec.Sources...),
	}
	if this.hasOptions(spec) {
		binding.ClientOptions = &ClientOptions{
			Address:    strings.TrimSpace(spec.Address),
			Scheme:     strings.TrimSpace(spec.Scheme),
			Datacenter: strings.TrimSpace(spec.Datacenter),
			Namespace:  strings.TrimSpace(spec.Namespace),
			Token:      strings.TrimSpace(spec.Token),
			WaitTimeMs: spec.WaitTimeMs,
		}
	}
	return binding
}

// Encode 把结构化 Bootstrap 转换为 Provider 内部默认值 map。
//
// 说明：
// 1. 零值字段会被省略，表示调用方没有声明对应默认值；
// 2. Provider.New 已对不完整 Source fail-fast；此处只做防御性过滤。
func (this *bootstrapCodec) Encode(spec Bootstrap) map[string]any {
	specMap := map[string]any{}
	putIfNotEmpty(specMap, bootstrapFieldConsulAddress, spec.Address)
	putIfNotEmpty(specMap, bootstrapFieldConsulScheme, spec.Scheme)
	putIfNotEmpty(specMap, bootstrapFieldConsulDC, spec.Datacenter)
	putIfNotEmpty(specMap, bootstrapFieldConsulNS, spec.Namespace)
	putIfNotEmpty(specMap, bootstrapFieldConsulToken, spec.Token)
	if spec.WaitTimeMs > 0 {
		specMap[bootstrapFieldConsulWaitMs] = spec.WaitTimeMs
	}

	if len(spec.Sources) > 0 {
		items := make([]any, 0, len(spec.Sources))
		for _, source := range spec.Sources {
			prefixValue := strings.TrimSpace(source.Prefix)
			keyValue := strings.TrimSpace(source.Key)
			if prefixValue == "" || keyValue == "" {
				continue
			}
			item := map[string]any{
				"prefix": prefixValue,
				"key":    keyValue,
			}
			name := strings.TrimSpace(source.Name)
			if name == "" {
				name = prefixValue + "/" + keyValue
			}
			item["name"] = name
			if source.Priority != 0 {
				item["priority"] = source.Priority
			}
			if source.Optional {
				item["optional"] = true
			}
			items = append(items, item)
		}
		if len(items) > 0 {
			specMap[bootstrapFieldConsulSources] = items
		}
	}

	if len(specMap) == 0 {
		return map[string]any{}
	}
	return map[string]any{bootstrapConsulRootKey: specMap}
}

// hasOptions 判断是否声明了可触发 ClientOptions 建连的字段。
func (this *bootstrapCodec) hasOptions(spec *Bootstrap) bool {
	if spec == nil {
		return false
	}
	return strings.TrimSpace(spec.Address) != "" ||
		strings.TrimSpace(spec.Scheme) != "" ||
		strings.TrimSpace(spec.Datacenter) != "" ||
		strings.TrimSpace(spec.Namespace) != "" ||
		strings.TrimSpace(spec.Token) != "" ||
		spec.WaitTimeMs > 0
}

// isZero 判断 Bootstrap 是否为空声明（无 connector 且无 sources）。
func (this *bootstrapCodec) isZero(spec *Bootstrap) bool {
	return spec == nil || (!this.hasOptions(spec) && len(spec.Sources) == 0)
}

// putIfNotEmpty 仅在字符串非空时写入 map，避免无意义覆盖。
func putIfNotEmpty(target map[string]any, key string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	target[key] = value
}

// bootstrapRootKey 从 dotted key 提取根节点（例如 consul.address -> consul）。
func bootstrapRootKey(dottedKey string) string {
	dottedKey = strings.TrimSpace(dottedKey)
	if dottedKey == "" {
		return ""
	}
	parts := strings.SplitN(dottedKey, ".", 2)
	return strings.TrimSpace(parts[0])
}

// bootstrapLeafKey 从 dotted key 提取末级字段（例如 consul.address -> address）。
func bootstrapLeafKey(dottedKey string) string {
	dottedKey = strings.TrimSpace(dottedKey)
	if dottedKey == "" {
		return ""
	}
	parts := strings.Split(dottedKey, ".")
	return strings.TrimSpace(parts[len(parts)-1])
}
