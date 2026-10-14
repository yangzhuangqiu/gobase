package nacos

import "strings"

type bootstrapCodec struct{}

var (
	bootstrapNacosRootKey     = bootstrapRootKey(BootstrapKeyNacosAddress)
	bootstrapFieldNacosAddr   = bootstrapLeafKey(BootstrapKeyNacosAddress)
	bootstrapFieldNacosServer = bootstrapLeafKey(BootstrapKeyNacosServers)
	bootstrapFieldNacosNS     = bootstrapLeafKey(BootstrapKeyNacosNamespace)
	bootstrapFieldNacosUser   = bootstrapLeafKey(BootstrapKeyNacosUsername)
	bootstrapFieldNacosPass   = bootstrapLeafKey(BootstrapKeyNacosPassword)
	bootstrapFieldNacosSource = bootstrapLeafKey(BootstrapKeyNacosSources)
	bootstrapFieldNacosTO     = bootstrapLeafKey(BootstrapKeyNacosTimeoutMs)
	bootstrapFieldNacosLogDir = bootstrapLeafKey(BootstrapKeyNacosLogDir)
	bootstrapFieldNacosCache  = bootstrapLeafKey(BootstrapKeyNacosCacheDir)
	bootstrapFieldNacosNoSnap = bootstrapLeafKey(BootstrapKeyNacosDisableUseSnapshot)
	bootstrapFieldNacosLevel  = bootstrapLeafKey(BootstrapKeyNacosLogLevel)
)

func newBootstrapCodec() *bootstrapCodec {
	return &bootstrapCodec{}
}

// Decode 将 bootstrap.nacos map 解析为结构化 Bootstrap。
//
// 语义：
// 1. 未声明 nacos 块时返回 nil,nil；
// 2. 对未知字段 fail-fast，避免静默吞错；
// 3. 仅当存在有效 connector 或 sources 时返回非 nil 结果。
func (this *bootstrapCodec) Decode(values map[string]any) (*Bootstrap, error) {
	// 阶段一：定位并归一化 nacos 根块，未声明时直接退出。
	raw, ok := lookupByDottedKey(values, bootstrapNacosRootKey)
	if !ok || raw == nil {
		return nil, nil
	}

	nacosMap, ok := normalizeValue(raw).(map[string]any)
	if !ok {
		return nil, errorForNacosInvalidBootstrap(messagef("invalid nacos bootstrap block"))
	}

	// 阶段二：先拒绝未知顶层字段，再做叶子类型和值域校验。
	allowed := map[string]struct{}{
		bootstrapFieldNacosAddr:   {},
		bootstrapFieldNacosServer: {},
		bootstrapFieldNacosNS:     {},
		bootstrapFieldNacosUser:   {},
		bootstrapFieldNacosPass:   {},
		bootstrapFieldNacosSource: {},
		bootstrapFieldNacosTO:     {},
		bootstrapFieldNacosLogDir: {},
		bootstrapFieldNacosCache:  {},
		bootstrapFieldNacosNoSnap: {},
		bootstrapFieldNacosLevel:  {},
	}
	for key := range nacosMap {
		if _, exists := allowed[key]; !exists {
			return nil, errorForNacosInvalidBootstrap(messagef("invalid nacos bootstrap field " + key))
		}
	}
	if err := validateStringFieldTypes(nacosMap,
		bootstrapFieldNacosAddr,
		bootstrapFieldNacosNS,
		bootstrapFieldNacosUser,
		bootstrapFieldNacosPass,
		bootstrapFieldNacosLogDir,
		bootstrapFieldNacosCache,
		bootstrapFieldNacosLevel,
	); err != nil {
		return nil, err
	}

	logLevel := readStringFromMap(nacosMap, bootstrapFieldNacosLevel)
	if logLevel != "" && !isSupportedNacosLogLevel(logLevel) {
		return nil, errorForNacosInvalidBootstrap(messagef("invalid nacos logLevel %q: expected debug|info|warn|error", logLevel))
	}

	spec := &Bootstrap{
		Address:     readStringFromMap(nacosMap, bootstrapFieldNacosAddr),
		NamespaceID: readStringFromMap(nacosMap, bootstrapFieldNacosNS),
		Username:    readStringFromMap(nacosMap, bootstrapFieldNacosUser),
		Password:    readStringFromMap(nacosMap, bootstrapFieldNacosPass),
		LogDir:      readStringFromMap(nacosMap, bootstrapFieldNacosLogDir),
		CacheDir:    readStringFromMap(nacosMap, bootstrapFieldNacosCache),
		LogLevel:    logLevel,
	}
	timeoutMs, err := readUint64FromMap(nacosMap, bootstrapFieldNacosTO)
	if err != nil {
		return nil, err
	}
	spec.TimeoutMs = timeoutMs
	disableUseSnapshot, err := readBoolFromMap(nacosMap, bootstrapFieldNacosNoSnap)
	if err != nil {
		return nil, err
	}
	spec.DisableUseSnapshot = disableUseSnapshot

	// 阶段三：结构化列表独立解析，错误保留节点索引。
	if rawServers, exists := nacosMap[bootstrapFieldNacosServer]; exists {
		servers, err := parseServersValue(rawServers)
		if err != nil {
			return nil, err
		}
		spec.Servers = servers
	}

	if rawSources, exists := nacosMap[bootstrapFieldNacosSource]; exists {
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
	if this.hasSDKOptions(spec) {
		binding.ClientOptions = &ClientOptions{
			Address:            strings.TrimSpace(spec.Address),
			Servers:            append([]Server{}, spec.Servers...),
			NamespaceID:        strings.TrimSpace(spec.NamespaceID),
			Username:           strings.TrimSpace(spec.Username),
			Password:           strings.TrimSpace(spec.Password),
			TimeoutMs:          spec.TimeoutMs,
			LogDir:             strings.TrimSpace(spec.LogDir),
			CacheDir:           strings.TrimSpace(spec.CacheDir),
			DisableUseSnapshot: spec.DisableUseSnapshot,
			LogLevel:           strings.TrimSpace(spec.LogLevel),
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
	// 阶段一：编码 connector 字段；servers 保持完整列表以实现整体覆盖。
	putIfNotEmpty(specMap, bootstrapFieldNacosAddr, spec.Address)
	if len(spec.Servers) > 0 {
		specMap[bootstrapFieldNacosServer] = encodeServers(spec.Servers)
	}
	putIfNotEmpty(specMap, bootstrapFieldNacosNS, spec.NamespaceID)
	putIfNotEmpty(specMap, bootstrapFieldNacosUser, spec.Username)
	putIfNotEmpty(specMap, bootstrapFieldNacosPass, spec.Password)
	if spec.TimeoutMs > 0 {
		specMap[bootstrapFieldNacosTO] = spec.TimeoutMs
	}
	putIfNotEmpty(specMap, bootstrapFieldNacosLogDir, spec.LogDir)
	putIfNotEmpty(specMap, bootstrapFieldNacosCache, spec.CacheDir)
	if spec.DisableUseSnapshot {
		specMap[bootstrapFieldNacosNoSnap] = true
	}
	putIfNotEmpty(specMap, bootstrapFieldNacosLevel, spec.LogLevel)

	// 阶段二：过滤不完整 Source，构造器校验仍负责向调用方报告声明错误。
	if len(spec.Sources) > 0 {
		items := make([]any, 0, len(spec.Sources))
		for _, source := range spec.Sources {
			groupID := strings.TrimSpace(source.GroupID)
			dataID := strings.TrimSpace(source.DataID)
			if groupID == "" || dataID == "" {
				continue
			}
			item := map[string]any{
				"groupId": groupID,
				"dataId":  dataID,
			}
			if source.Priority != 0 {
				item["priority"] = source.Priority
			}
			if source.Optional {
				item["optional"] = true
			}
			items = append(items, item)
		}
		if len(items) > 0 {
			specMap[bootstrapFieldNacosSource] = items
		}
	}

	if len(specMap) == 0 {
		return map[string]any{}
	}
	return map[string]any{bootstrapNacosRootKey: specMap}
}

// hasSDKOptions 判断是否声明了可触发 SDK 建连的字段。
func (this *bootstrapCodec) hasSDKOptions(spec *Bootstrap) bool {
	if spec == nil {
		return false
	}
	return strings.TrimSpace(spec.Address) != "" ||
		len(spec.Servers) > 0 ||
		strings.TrimSpace(spec.NamespaceID) != "" ||
		strings.TrimSpace(spec.Username) != "" ||
		strings.TrimSpace(spec.Password) != "" ||
		spec.TimeoutMs > 0 ||
		strings.TrimSpace(spec.LogDir) != "" ||
		strings.TrimSpace(spec.CacheDir) != "" ||
		spec.DisableUseSnapshot ||
		strings.TrimSpace(spec.LogLevel) != ""
}

// isZero 判断 Bootstrap 是否为空声明（无 connector 且无 sources）。
func (this *bootstrapCodec) isZero(spec *Bootstrap) bool {
	return spec == nil || (!this.hasSDKOptions(spec) && len(spec.Sources) == 0)
}

// putIfNotEmpty 仅在字符串非空时写入 map，避免无意义覆盖。
func putIfNotEmpty(target map[string]any, key string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	target[key] = value
}

func bootstrapRootKey(dottedKey string) string {
	dottedKey = strings.TrimSpace(dottedKey)
	if dottedKey == "" {
		return ""
	}
	parts := strings.SplitN(dottedKey, ".", 2)
	return strings.TrimSpace(parts[0])
}

func bootstrapLeafKey(dottedKey string) string {
	dottedKey = strings.TrimSpace(dottedKey)
	if dottedKey == "" {
		return ""
	}
	parts := strings.Split(dottedKey, ".")
	return strings.TrimSpace(parts[len(parts)-1])
}
