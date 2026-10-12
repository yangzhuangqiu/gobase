package consul

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/yangzhuangqiu/gobase/config/provider"
)

const (
	// ProviderName 是 Consul provider 的稳定标识。
	ProviderName = "consul"
	// BootstrapKeyConsulAddress 是 Consul 地址 key。
	BootstrapKeyConsulAddress = "consul.address"
	// BootstrapKeyConsulScheme 是 Consul scheme key。
	BootstrapKeyConsulScheme = "consul.scheme"
	// BootstrapKeyConsulDatacenter 是 Consul datacenter key。
	BootstrapKeyConsulDatacenter = "consul.datacenter"
	// BootstrapKeyConsulNamespace 是 Consul namespace key。
	BootstrapKeyConsulNamespace = "consul.namespace"
	// BootstrapKeyConsulToken 是 Consul token key。
	BootstrapKeyConsulToken = "consul.token"
	// BootstrapKeyConsulSources 是 Consul source 列表声明 key。
	BootstrapKeyConsulSources = "consul.sources"
	// BootstrapKeyConsulWaitTimeMs 是 Consul wait time key。
	BootstrapKeyConsulWaitTimeMs = "consul.waitTimeMs"
)

// ClientOptions 描述 Consul API 客户端构建参数。
type ClientOptions struct {
	// Address 是 Consul 地址，可包含 scheme/path（例如 http://127.0.0.1:8500）。
	Address string
	// Scheme 是请求协议；为空时按地址或默认值推导。
	Scheme string
	// Datacenter 是目标数据中心名称。
	Datacenter string
	// Namespace 是目标命名空间。
	Namespace string
	// Token 是 ACL 访问令牌。
	Token string
	// WaitTimeMs 是阻塞查询等待时长（毫秒）；0 使用默认值。
	WaitTimeMs uint64
}

// binding 描述 Consul Provider 从 bootstrap 解析出的内部连接声明。
type binding struct {
	// Sources 是远端 KV 源声明列表。
	Sources []BootstrapSource
	// Client 是调用方显式注入的客户端实现。
	// 语义：设置后将跳过 ClientOptions 建连路径。
	Client Client
	// ClientOptions 是基于官方 SDK 建连参数。
	// 语义：当 Client 为空且 Sources 非空时使用该参数创建客户端。
	ClientOptions *ClientOptions
}

// Provider 实现 provider.Provider，并持有构造阶段的 typed bootstrap 默认值。
type Provider struct {
	// bootstrap 是 Provider 本地默认值；file/env/Build Input 的同名字段均可覆盖它。
	bootstrap *Bootstrap
	// inputErr 保存构造器数量错误，延迟到 bootstrap 校验阶段 fail-fast。
	inputErr error
	// client 是 NewWithClient 显式注入的 borrowed Client；非 nil 时不解析连接 bootstrap。
	client Client
	// sources 是与显式 Client 配套的 Source 快照。
	sources []BootstrapSource
}

// NewWithClient 使用调用方持有的 Consul Client 创建 Provider。
//
// 典型调用场景：已有连接、测试替身或自定义 SDK 适配器接入 Config；Client 保持 borrowed，不由 Session.Close 释放。
// 参数：
// - client：必填，提供 Consul Get/Watch 能力；
// - sources：至少一个，Prefix/Key 必须完整。
func NewWithClient(client Client, sources ...BootstrapSource) *Provider {
	result := &Provider{client: client, sources: append([]BootstrapSource{}, sources...)}
	if isNilClient(client) {
		result.inputErr = errorForConsulInvalidBinding(messagef("op=config_consul.new_with_client result=failed stage=declare reason=client_required"))
		return result
	}
	if len(sources) == 0 {
		result.inputErr = errorForConsulInvalidBinding(messagef("op=config_consul.new_with_client result=failed stage=declare reason=sources_required"))
		return result
	}
	result.inputErr = validateDeclaredBootstrapSources(result.sources)
	return result
}

// New 创建 Consul Provider 对象。
//
// 参数：
// - bootstrap：可选且最多一个；未传时完全由 file/env/Build Input 提供 Consul bootstrap。
// - 传入时作为 Provider 本地默认值；file/env/Build Input 的同名字段具有更高优先级。
//
// 返回：
// - Provider：不建立连接、不启动 watcher，可安全保存到可复用应用声明。
func New(bootstrap ...Bootstrap) *Provider {
	result := &Provider{}
	// 阶段一：构造器只接受零个或一个完整 typed bootstrap，避免多值隐式合并。
	if len(bootstrap) > 1 {
		result.inputErr = errorForConsulInvalidBootstrap(messagef("op=config_consul.new result=failed stage=declare reason=multiple_bootstrap_values"))
		return result
	}
	// 阶段二：复制 Sources，隔离调用方后续对输入 slice 的修改。
	if len(bootstrap) == 1 {
		value := bootstrap[0]
		value.Sources = append([]BootstrapSource{}, value.Sources...)
		result.bootstrap = &value
		result.inputErr = validateDeclaredBootstrapSources(value.Sources)
	}
	return result
}

// validateDeclaredBootstrapSources 校验 typed Bootstrap 中每个 Source 的必要定位字段。
// 阶段角色：构造声明期记录错误，确保 Build 在创建 SDK Client 前 fail-fast。
func validateDeclaredBootstrapSources(sources []BootstrapSource) error {
	for index, source := range sources {
		prefixValue := strings.TrimSpace(source.Prefix)
		keyValue := strings.TrimSpace(source.Key)
		if prefixValue != "" && keyValue != "" {
			continue
		}
		return errorForConsulInvalidSource(messagef("op=config_consul.new result=failed stage=validate_source source_index=%d reason=prefix_key_required", index))
	}
	return nil
}

// Name 返回 provider 标识。
func (this *Provider) Name() string {
	return ProviderName
}

// bootstrapDefaults 返回构造阶段 typed bootstrap 的独立默认值映射。
// 阶段角色：ValidateBootstrap/Open 在解析外部 bootstrap 前准备 Provider 本地默认层。
func (this *Provider) bootstrapDefaults() map[string]any {
	if this == nil || this.bootstrap == nil {
		return map[string]any{}
	}
	return newBootstrapCodec().Encode(*this.bootstrap)
}

// LoadBootstrapFromEnv 读取 Consul 的 bootstrap env 扩展字段。
//
// 参数：
// - envPrefix: 已标准化前缀（如 APP_）。
//
// 返回：
// - map[string]any: 仅包含 schema 自动映射无法覆盖的字段（当前为 indexed sources）。
func (this *Provider) LoadBootstrapFromEnv(envPrefix string) map[string]any {
	return newBootstrapEnvAdapter().Load(envPrefix)
}

// ValidateBootstrap 执行 Consul bootstrap 语义校验（不触发建连）。
func (this *Provider) ValidateBootstrap(values map[string]any) error {
	if this.inputErr != nil {
		return this.inputErr
	}
	if !isNilClient(this.client) {
		if _, exists := values[bootstrapConsulRootKey]; exists {
			return errorForConsulInvalidBinding(messagef("op=config_consul.validate_bootstrap result=failed stage=validate_direct_client reason=bootstrap_not_allowed_with_client"))
		}
		return nil
	}
	_, err := this.parseBootstrap(this.effectiveBootstrap(values))
	return err
}

// BootstrapSchema 返回 Consul bootstrap 可发现键定义。
func (this *Provider) BootstrapSchema() []provider.BootstrapKey {
	return []provider.BootstrapKey{
		{Key: BootstrapKeyConsulAddress, Type: provider.BootstrapSchemaTypeString, Required: false, DefaultValue: "", Description: "Consul server address."},
		{Key: BootstrapKeyConsulScheme, Type: provider.BootstrapSchemaTypeString, Required: false, DefaultValue: "http", Description: "HTTP scheme."},
		{Key: BootstrapKeyConsulDatacenter, Type: provider.BootstrapSchemaTypeString, Required: false, DefaultValue: "", Description: "Target datacenter."},
		{Key: BootstrapKeyConsulNamespace, Type: provider.BootstrapSchemaTypeString, Required: false, DefaultValue: "", Description: "Target namespace."},
		{Key: BootstrapKeyConsulToken, Type: provider.BootstrapSchemaTypeString, Required: false, DefaultValue: "", Description: "ACL token."},
		{Key: BootstrapKeyConsulWaitTimeMs, Type: provider.BootstrapSchemaTypeUint64, Required: false, DefaultValue: "30000", Description: "Blocking query wait time in milliseconds."},
		{Key: BootstrapKeyConsulSources, Type: provider.BootstrapSchemaTypeList, Required: false, DefaultValue: "[]", Description: "Remote source list."},
		{Key: "consul.sources[].name", Type: provider.BootstrapSchemaTypeString, Required: false, DefaultValue: "<prefix>/<key>", Description: "Source display name."},
		{Key: "consul.sources[].prefix", Type: provider.BootstrapSchemaTypeString, Required: true, DefaultValue: "", Description: "Consul KV path prefix."},
		{Key: "consul.sources[].key", Type: provider.BootstrapSchemaTypeString, Required: true, DefaultValue: "", Description: "Consul KV key."},
		{Key: "consul.sources[].priority", Type: provider.BootstrapSchemaTypeInt, Required: false, DefaultValue: "0", Description: "Source merge priority."},
		{Key: "consul.sources[].optional", Type: provider.BootstrapSchemaTypeBool, Required: false, DefaultValue: "false", Description: "Allow source degrade on failure."},
	}
}

// Open 从 bootstrap map 建立 Consul 运行会话。
// 未声明 Consul 时返回 nil；非 nil Session 的关闭所有权移交给调用方。
func (this *Provider) Open(ctx context.Context, values map[string]any) (provider.Session, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if this.inputErr != nil {
		return nil, this.inputErr
	}
	if !isNilClient(this.client) {
		if _, exists := values[bootstrapConsulRootKey]; exists {
			return nil, errorForConsulInvalidBinding(messagef("op=config_consul.open result=failed stage=validate_direct_client reason=bootstrap_not_allowed_with_client"))
		}
		return this.buildSession(&binding{Client: this.client, Sources: append([]BootstrapSource{}, this.sources...)})
	}
	binding, err := this.parseBootstrap(this.effectiveBootstrap(values))
	if err != nil {
		return nil, err
	}
	return this.buildSession(binding)
}

// effectiveBootstrap 合并 Provider 构造默认值与调用输入，调用输入保持更高优先级。
// 阶段角色：统一 ValidateBootstrap/Open 的最终解析输入，保证 file/env/Build Input 可覆盖代码默认值。
func (this *Provider) effectiveBootstrap(values map[string]any) map[string]any {
	return mergeProviderBootstrap(this.bootstrapDefaults(), values)
}

// mergeProviderBootstrap 深度合并 Provider 默认值与外部 bootstrap，overlay 同键覆盖 base。
func mergeProviderBootstrap(base, overlay map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(overlay))
	// 阶段一：递归复制构造器默认值，避免返回值共享内部 map/list。
	for key, value := range base {
		result[key] = normalizeValue(value)
	}
	// 阶段二：调用输入按 key 覆盖；双方都是 map 时继续深度合并。
	for key, value := range overlay {
		normalized := normalizeValue(value)
		baseMap, baseOK := result[key].(map[string]any)
		overlayMap, overlayOK := normalized.(map[string]any)
		if baseOK && overlayOK {
			result[key] = mergeProviderBootstrap(baseMap, overlayMap)
			continue
		}
		result[key] = normalized
	}
	return result
}

// parseBootstrap 解析 bootstrap.consul 声明。
//
// 参数：
// - values: 归一化后的 bootstrap map。
//
// 返回：
// - *binding: 绑定声明；未声明 Consul 时返回 nil。
// - error: 字段非法或格式错误时返回错误。
func (this *Provider) parseBootstrap(values map[string]any) (*binding, error) {
	codec := newBootstrapCodec()
	spec, err := codec.Decode(values)
	if err != nil {
		return nil, err
	}
	binding := codec.toBinding(spec)
	if binding == nil {
		return nil, nil
	}
	return binding, nil
}

// buildSession 将 binding 转换为运行会话。
//
// 语义：
// 1. Client 与 ClientOptions 互斥；
// 2. 声明 Sources 时必须提供连接器（Client 或 ClientOptions）；
// 3. 仅声明连接器但未声明 Sources 会失败；
// 4. 未声明任何远端信息时返回 nil Session（表示不启用）。
func (this *Provider) buildSession(binding *binding) (provider.Session, error) {
	if binding == nil {
		return nil, nil
	}

	hasClient := !isNilClient(binding.Client)
	hasOptions := binding.ClientOptions != nil
	// 连接器语义互斥：显式 client 与 ClientOptions 自动建连不能同时配置。
	if hasClient && hasOptions {
		return nil, errorForConsulInvalidBinding(messagef("reason=consul_multiple_connectors_configured"))
	}

	hasSource := len(binding.Sources) > 0
	hasConnector := hasClient || hasOptions
	if hasSource && !hasConnector {
		return nil, errorForConsulInvalidBinding(messagef(fmt.Sprintf("reason=consul_connector_required_when_sources_configured source_count=%d", len(binding.Sources))))
	}
	if !hasSource && hasConnector {
		return nil, errorForConsulInvalidBinding(messagef("reason=consul_sources_required_when_connector_configured"))
	}
	if !hasSource {
		return nil, nil
	}

	sources := append([]BootstrapSource{}, binding.Sources...)
	for index := range sources {
		item := &sources[index]
		item.Prefix = strings.TrimSpace(item.Prefix)
		item.Key = strings.TrimSpace(item.Key)
		if item.Prefix == "" || item.Key == "" {
			return nil, errorForConsulInvalidSource(messagef(fmt.Sprintf("reason=consul_source_prefix_key_required index=%d", index)))
		}
		if strings.TrimSpace(item.Name) == "" {
			item.Name = item.Prefix + "/" + item.Key
		}
	}
	if hasClient {
		return newSession(binding.Client, sources, false), nil
	}

	client, err := NewClient(*binding.ClientOptions)
	if err != nil {
		return nil, errorForConsulSDKClientCreateFailed(messagef("stage=create_consul_client"), err)
	}
	return newSession(client, sources, true), nil
}

// isNilClient 识别普通 nil 与装入 Client 接口后的 typed nil。
// 阶段角色：Provider 声明和 Open 校验，避免无效连接延迟到 Session.Load 时 panic。
func isNilClient(client Client) bool {
	if client == nil {
		return true
	}
	value := reflect.ValueOf(client)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// parseSourcesValue 解析 source 声明（仅支持结构化 map/array）。
func parseSourcesValue(value any) ([]BootstrapSource, error) {
	switch typed := normalizeValue(value).(type) {
	case map[string]any:
		item, err := parseSourceMap(typed)
		if err != nil {
			return nil, err
		}
		return []BootstrapSource{item}, nil
	case []map[string]any:
		result := make([]BootstrapSource, 0, len(typed))
		for _, raw := range typed {
			item, err := parseSourceMap(raw)
			if err != nil {
				return nil, err
			}
			result = append(result, item)
		}
		return result, nil
	case []any:
		result := make([]BootstrapSource, 0, len(typed))
		for _, item := range typed {
			normalized := normalizeValue(item)
			switch current := normalized.(type) {
			case map[string]any:
				parsed, err := parseSourceMap(current)
				if err != nil {
					return nil, err
				}
				result = append(result, parsed)
			default:
				return nil, errorForConsulInvalidSource(messagef(fmt.Sprintf("invalid consul source item type %T, expect map", current)))
			}
		}
		return result, nil
	default:
		return nil, errorForConsulInvalidSource(messagef(fmt.Sprintf("invalid consul source value type %T, expect map or array", typed)))
	}
}

// parseSourceMap 解析 map source 声明。
//
// 必填字段：prefix+key。
func parseSourceMap(raw map[string]any) (BootstrapSource, error) {
	result := BootstrapSource{}
	for key, value := range raw {
		switch key {
		case "name":
			parsed, err := parseStringValue(value)
			if err != nil {
				return result, errorForConsulInvalidSource(messagef("invalid consul source name"), err)
			}
			result.Name = parsed
		case "prefix":
			parsed, err := parseStringValue(value)
			if err != nil {
				return result, errorForConsulInvalidSource(messagef("invalid consul source prefix"), err)
			}
			result.Prefix = parsed
		case "key":
			parsed, err := parseStringValue(value)
			if err != nil {
				return result, errorForConsulInvalidSource(messagef("invalid consul source key"), err)
			}
			result.Key = parsed
		case "priority":
			parsed, err := parseIntValue(value)
			if err != nil {
				return result, errorForConsulInvalidSource(messagef("invalid consul source priority"), err)
			}
			result.Priority = parsed
		case "optional":
			parsed, err := parseBoolValue(value)
			if err != nil {
				return result, errorForConsulInvalidSource(messagef("invalid consul source optional"), err)
			}
			result.Optional = parsed
		default:
			return result, errorForConsulInvalidSource(messagef("invalid consul source field " + key))
		}
	}
	if result.Prefix == "" || result.Key == "" {
		return result, errorForConsulInvalidSource(messagef("invalid consul source spec, prefix/key required"))
	}
	if result.Name == "" {
		result.Name = result.Prefix + "/" + result.Key
	}
	return result, nil
}

// collectEnvConsulSources 汇总结构化 Consul source 环境变量。
//
// 键形如：
// - <PREFIX>CONSUL_SOURCE_0_PREFIX
// - <PREFIX>CONSUL_SOURCE_0_KEY
// - <PREFIX>CONSUL_SOURCE_0_PRIORITY
// - <PREFIX>CONSUL_SOURCE_0_OPTIONAL
// - <PREFIX>CONSUL_SOURCE_0_NAME
func collectEnvConsulSources(prefix string) []map[string]any {
	result := make([]map[string]any, 0)
	seen := false
	for index := 0; index < 64; index++ {
		base := fmt.Sprintf("%sCONSUL_SOURCE_%d_", prefix, index)
		name := strings.TrimSpace(os.Getenv(base + "NAME"))
		prefixValue := strings.TrimSpace(os.Getenv(base + "PREFIX"))
		keyValue := strings.TrimSpace(os.Getenv(base + "KEY"))
		priority := strings.TrimSpace(os.Getenv(base + "PRIORITY"))
		optional := strings.TrimSpace(os.Getenv(base + "OPTIONAL"))

		if name == "" && prefixValue == "" && keyValue == "" && priority == "" && optional == "" {
			if seen {
				break
			}
			continue
		}
		seen = true

		item := map[string]any{}
		if name != "" {
			item["name"] = name
		}
		if prefixValue != "" {
			item["prefix"] = prefixValue
		}
		if keyValue != "" {
			item["key"] = keyValue
		}
		if priority != "" {
			item["priority"] = priority
		}
		if optional != "" {
			item["optional"] = optional
		}
		result = append(result, item)
	}
	return result
}

// readStringFromMap 从 map 中读取字符串字段。
func readStringFromMap(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	value, _ := raw.(string)
	return strings.TrimSpace(value)
}

// validateStringFieldTypes 对声明为 string 的 bootstrap 字段执行严格类型校验。
func validateStringFieldTypes(values map[string]any, keys ...string) error {
	for _, key := range keys {
		raw, ok := values[key]
		if !ok || raw == nil {
			continue
		}
		if _, ok := raw.(string); !ok {
			return errorForConsulInvalidValueType(messagef("invalid string field %s type %T", key, raw))
		}
	}
	return nil
}

// parseStringValue 严格解析 Provider source 的文本定位字段。
func parseStringValue(value any) (string, error) {
	parsed, ok := value.(string)
	if !ok {
		return "", errorForConsulInvalidValueType(messagef("unsupported string value type %T", value))
	}
	return strings.TrimSpace(parsed), nil
}

// readUint64FromMap 从 map 中读取 uint64 字段。
func readUint64FromMap(values map[string]any, key string) (uint64, error) {
	raw, ok := values[key]
	if !ok || raw == nil {
		return 0, nil
	}
	return parseUint64Value(raw)
}

// parseIntValue 解析任意输入为 int。
func parseIntValue(value any) (int, error) {
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int8:
		return int(typed), nil
	case int16:
		return int(typed), nil
	case int32:
		return int(typed), nil
	case int64:
		converted := int(typed)
		if int64(converted) != typed {
			return 0, errorForConsulInvalidValueType(messagef("int64 value out of int range"))
		}
		return converted, nil
	case uint:
		converted := int(typed)
		if converted < 0 || uint(converted) != typed {
			return 0, errorForConsulInvalidValueType(messagef("uint value out of int range"))
		}
		return converted, nil
	case uint8:
		return int(typed), nil
	case uint16:
		return int(typed), nil
	case uint32:
		converted := int(typed)
		if converted < 0 || uint32(converted) != typed {
			return 0, errorForConsulInvalidValueType(messagef("uint32 value out of int range"))
		}
		return converted, nil
	case uint64:
		converted := int(typed)
		if converted < 0 || uint64(converted) != typed {
			return 0, errorForConsulInvalidValueType(messagef("uint64 value out of int range"))
		}
		return converted, nil
	case float32, float64:
		return 0, errorForConsulInvalidValueType(messagef("floating-point value is not allowed for int"))
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, errorForConsulInvalidValueType(messagef("invalid int value"), err)
		}
		return parsed, nil
	default:
		return 0, errorForConsulInvalidValueType(messagef(fmt.Sprintf("unsupported int value type %T", value)))
	}
}

// parseBoolValue 解析任意输入为 bool。
func parseBoolValue(value any) (bool, error) {
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		if err != nil {
			return false, errorForConsulInvalidValueType(messagef("invalid bool value"), err)
		}
		return parsed, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return false, errorForConsulInvalidValueType(messagef("numeric value is not allowed for bool"))
	default:
		return false, errorForConsulInvalidValueType(messagef(fmt.Sprintf("unsupported bool value type %T", value)))
	}
}

// parseUint64Value 解析任意输入为 uint64。
func parseUint64Value(value any) (uint64, error) {
	switch typed := value.(type) {
	case uint64:
		return typed, nil
	case uint:
		return uint64(typed), nil
	case uint32:
		return uint64(typed), nil
	case uint16:
		return uint64(typed), nil
	case uint8:
		return uint64(typed), nil
	case int:
		if typed < 0 {
			return 0, errorForConsulInvalidValueType(messagef("negative timeout value"))
		}
		return uint64(typed), nil
	case int64:
		if typed < 0 {
			return 0, errorForConsulInvalidValueType(messagef("negative timeout value"))
		}
		return uint64(typed), nil
	case float32, float64:
		return 0, errorForConsulInvalidValueType(messagef("floating-point value is not allowed for uint64"))
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, errorForConsulInvalidValueType(messagef("invalid uint64 value"), err)
		}
		return parsed, nil
	default:
		return 0, errorForConsulInvalidValueType(messagef(fmt.Sprintf("unsupported uint64 value type %T", value)))
	}
}

// putEnvByDottedKey 读取环境变量并按 dotted key 写入 map。
func putEnvByDottedKey(target map[string]any, dottedKey string, envKey string) {
	value := strings.TrimSpace(os.Getenv(envKey))
	if value == "" {
		return
	}
	setByDottedKey(target, dottedKey, value)
}

// lookupByDottedKey 在嵌套 map 中按 dotted key 查找值。
func lookupByDottedKey(input map[string]any, dottedKey string) (any, bool) {
	dottedKey = strings.TrimSpace(dottedKey)
	if dottedKey == "" {
		return input, true
	}
	parts := strings.Split(dottedKey, ".")
	var cursor any = input
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		child, ok := cursor.(map[string]any)
		if !ok {
			return nil, false
		}
		next, exists := child[part]
		if !exists {
			return nil, false
		}
		cursor = next
	}
	return cursor, true
}

// setByDottedKey 按 dotted key 写入嵌套 map。
func setByDottedKey(target map[string]any, dottedKey string, value any) {
	parts := strings.Split(dottedKey, ".")
	if len(parts) == 0 {
		return
	}
	cursor := target
	for index := 0; index < len(parts)-1; index++ {
		part := strings.TrimSpace(parts[index])
		if part == "" {
			return
		}
		next, ok := cursor[part]
		if !ok {
			child := make(map[string]any)
			cursor[part] = child
			cursor = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			child = make(map[string]any)
			cursor[part] = child
		}
		cursor = child
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	if last == "" {
		return
	}
	cursor[last] = value
}

// normalizeValue 递归清洗 map/list 结构，保证 key 为 string。
func normalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = normalizeValue(item)
		}
		return result
	case map[any]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[fmt.Sprint(key)] = normalizeValue(item)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, normalizeValue(item))
		}
		return result
	default:
		return value
	}
}
