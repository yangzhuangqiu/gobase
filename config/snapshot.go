package config

import (
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Snapshot 表示运行时某一时刻的只读配置快照。
//
// 该接口为“封口接口”：调用方只能使用只读能力，不能在包外实现。
// 这样可以保证内部实现可持续演进，不被外部实现约束。
type Snapshot interface {
	snapshotMarker()

	// Raw 返回嵌套 map 视图（深拷贝）。
	Raw() map[string]any
	// Flat 返回 dotted key 视图（深拷贝），用于批量诊断与导出。
	Flat() map[string]any
	// Explain 返回当前版本中某个 dotted key 的来源元信息。
	Explain(key string) (ValueMeta, bool)
	// Version 返回单调递增的版本号（从 1 开始）。
	Version() uint64
	// UpdatedAt 返回该快照的发布时间。
	UpdatedAt() time.Time

	// Get 按 dotted key 从快照中读取值。
	Get(key string) (any, bool)
	// Decode 将当前版本中的 scope 解码到 out；空 scope 表示完整配置。
	Decode(scope string, out any) error
	// String 读取 string/[]byte；不存在或类型不匹配时返回 defaultValue。
	String(key, defaultValue string) string
	// Int 读取无损整数或十进制字符串；不存在、溢出或类型不匹配时返回 defaultValue。
	Int(key string, defaultValue int) int
	// Bool 读取 bool 或布尔字符串；不存在或类型不匹配时返回 defaultValue。
	Bool(key string, defaultValue bool) bool
	// Duration 读取 time.Duration 或 duration 字符串；不存在或类型不匹配时返回 defaultValue。
	Duration(key string, defaultValue time.Duration) time.Duration
}

// snapshotState 是 Snapshot 的内部不可变实现。
type snapshotState struct {
	raw       map[string]any
	flat      map[string]any
	meta      map[string]ValueMeta
	version   uint64
	updatedAt time.Time
}

// deepCopyFlat 深拷贝扁平 map。
func deepCopyFlat(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = deepCopyValue(value)
	}
	return result
}

// copyMeta 深拷贝元信息并对齐快照版本。
func copyMeta(meta map[string]ValueMeta, version uint64) map[string]ValueMeta {
	result := make(map[string]ValueMeta, len(meta))
	for key, item := range meta {
		item.Version = version
		result[key] = item
	}
	return result
}

// diffSnapshots 计算两个快照之间的 key 级差异。
func diffSnapshots(oldSnap, newSnap *snapshotState) []Change {
	oldFlat := map[string]any{}
	newFlat := map[string]any{}
	oldMeta := map[string]ValueMeta{}
	newMeta := map[string]ValueMeta{}
	if oldSnap != nil {
		oldFlat = oldSnap.flat
		oldMeta = oldSnap.meta
	}
	if newSnap != nil {
		newFlat = newSnap.flat
		newMeta = newSnap.meta
	}

	seen := make(map[string]struct{}, len(oldFlat)+len(newFlat))
	for key := range oldFlat {
		seen[key] = struct{}{}
	}
	for key := range newFlat {
		seen[key] = struct{}{}
	}

	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]Change, 0, len(keys))
	for _, key := range keys {
		oldValue, oldOK := oldFlat[key]
		newValue, newOK := newFlat[key]
		if oldOK && newOK && isSameValue(oldValue, newValue) && isSameValueMeta(oldMeta[key], newMeta[key]) {
			continue
		}
		change := Change{Key: key}
		switch {
		case !oldOK && newOK:
			change.Kind = ChangeKindAdded
		case oldOK && !newOK:
			change.Kind = ChangeKindRemoved
		default:
			change.Kind = ChangeKindUpdated
		}
		if oldOK {
			change.Old = deepCopyValue(oldValue)
			change.OldMeta = oldMeta[key]
		}
		if newOK {
			change.New = deepCopyValue(newValue)
			change.NewMeta = newMeta[key]
		}
		result = append(result, change)
	}

	return result
}

// isSameValue 使用类型敏感比较，避免 "1" 与 1 被误判为相同。
func isSameValue(left, right any) bool {
	return reflect.DeepEqual(left, right)
}

// isSameValueMeta 比较会影响 Explain 语义的来源字段；Version 随发布递增，不参与变化判断。
func isSameValueMeta(left, right ValueMeta) bool {
	return left.Source == right.Source &&
		left.Layer == right.Layer &&
		left.Priority == right.Priority &&
		left.Pinned == right.Pinned
}

// snapshotMarker 用于封口 Snapshot 接口实现。
func (this *snapshotState) snapshotMarker() {}

// Raw 返回嵌套 map（深拷贝）。
func (this *snapshotState) Raw() map[string]any {
	if this == nil {
		return nil
	}
	return deepCopyMap(this.raw)
}

// Flat 返回 dotted key map（深拷贝）。
func (this *snapshotState) Flat() map[string]any {
	if this == nil {
		return nil
	}
	return deepCopyFlat(this.flat)
}

// Explain 返回当前 Snapshot 中某个 key 的来源元信息。
func (this *snapshotState) Explain(key string) (ValueMeta, bool) {
	if this == nil {
		return ValueMeta{}, false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return ValueMeta{}, false
	}
	meta, ok := this.meta[key]
	return meta, ok
}

// Version 返回快照版本号。
func (this *snapshotState) Version() uint64 {
	if this == nil {
		return 0
	}
	return this.version
}

// UpdatedAt 返回快照发布时间。
func (this *snapshotState) UpdatedAt() time.Time {
	if this == nil {
		return time.Time{}
	}
	return this.updatedAt
}

// Get 按 dotted key 从快照读取值。
func (this *snapshotState) Get(key string) (any, bool) {
	if this == nil {
		return nil, false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false
	}
	value, ok := this.flat[key]
	return deepCopyValue(value), ok
}

// Decode 将当前 Snapshot 的指定 scope 原子解码到 out。
func (this *snapshotState) Decode(scope string, out any) error {
	target, err := newScopeTarget(out)
	if err != nil {
		return errorForConfigDecodeFailed(messagef("op=config.snapshot.decode result=failed stage=validate_out scope=%s", strings.TrimSpace(scope)), err)
	}
	if err = decodeScopeIntoTarget(strings.TrimSpace(scope), this, target); err != nil {
		return errorForConfigDecodeFailed(messagef("op=config.snapshot.decode result=failed stage=decode scope=%s", strings.TrimSpace(scope)), err)
	}
	if err = copyDecodedTarget(out, target); err != nil {
		return errorForConfigDecodeFailed(messagef("op=config.snapshot.decode result=failed stage=copy scope=%s", strings.TrimSpace(scope)), err)
	}
	return nil
}

// String 按 key 读取字符串值。
func (this *snapshotState) String(key, defaultValue string) string {
	value, ok := this.Get(key)
	if !ok {
		return defaultValue
	}
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return defaultValue
	}
}

// Int 按 key 读取整型值。
func (this *snapshotState) Int(key string, defaultValue int) int {
	value, ok := this.Get(key)
	if !ok {
		return defaultValue
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		if converted := int(typed); int64(converted) == typed {
			return converted
		}
		return defaultValue
	case uint:
		if converted := int(typed); converted >= 0 && uint(converted) == typed {
			return converted
		}
		return defaultValue
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		if converted := int(typed); converted >= 0 && uint32(converted) == typed {
			return converted
		}
		return defaultValue
	case uint64:
		if converted := int(typed); converted >= 0 && uint64(converted) == typed {
			return converted
		}
		return defaultValue
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return defaultValue
		}
		return parsed
	default:
		return defaultValue
	}
}

// Bool 按 key 读取布尔值。
func (this *snapshotState) Bool(key string, defaultValue bool) bool {
	value, ok := this.Get(key)
	if !ok {
		return defaultValue
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		if err != nil {
			return defaultValue
		}
		return parsed
	default:
		return defaultValue
	}
}

// Duration 按 key 读取时长值。
func (this *snapshotState) Duration(key string, defaultValue time.Duration) time.Duration {
	value, ok := this.Get(key)
	if !ok {
		return defaultValue
	}
	switch typed := value.(type) {
	case time.Duration:
		return typed
	case string:
		parsed, err := time.ParseDuration(strings.TrimSpace(typed))
		if err != nil {
			return defaultValue
		}
		return parsed
	default:
		return defaultValue
	}
}
