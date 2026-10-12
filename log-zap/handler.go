package zap

import (
	"context"
	"log/slog"
	"runtime"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// handler 把标准 slog.Record 映射到当前 Provider 的 Zap core。
// 阶段角色：作为唯一 provider SPI，替代旧 Logger adapter 与命名缓存。
type handler struct {
	// provider 提供可热更新、可关闭的 Zap 运行时快照。
	provider *Provider
	// attrs 是 WithAttrs 累积的不可变固定字段。
	attrs []slog.Attr
	// groups 是尚未物化到后续属性上的嵌套组路径。
	groups []string
}

var _ slog.Handler = (*handler)(nil)

// newHandler 创建与 Provider 动态运行时绑定的根 Handler。
func newHandler(provider *Provider) *handler {
	return &handler{provider: provider}
}

// Enabled 判断 Record 是否满足当前动态 level。
func (this *handler) Enabled(_ context.Context, level slog.Level) bool {
	if this.provider == nil || this.provider.closed.Load() {
		return false
	}
	runtimeState := this.provider.runtime.Load()
	if runtimeState == nil || runtimeState.base == nil {
		return false
	}
	return runtimeState.base.Core().Enabled(toZapLevel(level))
}

// Handle 把标准事件写入 Zap core。
//
// 流程：
// 1. 获取当前 runtime，并通过 Core.Check 执行 level/sampling/hook 判定；
// 2. 合并固定字段、当前 group 与 Record 字段；
// 3. 使用 Record.PC 构造 caller 后交给 CheckedEntry 写入。
func (this *handler) Handle(_ context.Context, record slog.Record) error {
	if this.provider == nil || this.provider.closed.Load() {
		return nil
	}
	runtimeState := this.provider.runtime.Load()
	if runtimeState == nil || runtimeState.base == nil {
		return nil
	}
	zapLevel := toZapLevel(record.Level)
	core := runtimeState.base.Core()
	entry := zapcore.Entry{Time: record.Time, Level: zapLevel, Message: record.Message}
	if runtimeState.addSource && record.PC != 0 {
		entry.Caller = callerFromPC(record.PC)
	}
	checked := core.Check(entry, nil)
	if checked == nil {
		return nil
	}

	// Check 通过后才承担属性归并与 Zap 字段转换成本。
	attrs := make([]slog.Attr, 0, len(this.attrs)+record.NumAttrs())
	attrs = append(attrs, this.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, wrapAttrWithGroups(attr, this.groups))
		return true
	})
	checked.Write(slogAttrsToZapFields(mergeSlogGroups(attrs))...)
	return nil
}

// WithAttrs 派生携带固定字段的不可变 Handler。
func (this *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	result := &handler{provider: this.provider}
	result.attrs = append(result.attrs, this.attrs...)
	for _, attr := range attrs {
		result.attrs = append(result.attrs, wrapAttrWithGroups(attr, this.groups))
	}
	result.groups = append(result.groups, this.groups...)
	return result
}

// WithGroup 派生字段组；空名称不改变当前 Handler。
func (this *handler) WithGroup(name string) slog.Handler {
	name = strings.TrimSpace(name)
	if name == "" {
		return this
	}
	result := &handler{provider: this.provider}
	result.attrs = append(result.attrs, this.attrs...)
	result.groups = append(result.groups, this.groups...)
	result.groups = append(result.groups, name)
	return result
}

// toZapLevel 把任意 slog.Level 映射到 Zap 支持的四档记录级别。
func toZapLevel(level slog.Level) zapcore.Level {
	switch {
	case level < slog.LevelInfo:
		return zapcore.DebugLevel
	case level < slog.LevelWarn:
		return zapcore.InfoLevel
	case level < slog.LevelError:
		return zapcore.WarnLevel
	default:
		return zapcore.ErrorLevel
	}
}

// callerFromPC 把标准 Record 调用点转换为 Zap caller。
func callerFromPC(programCounter uintptr) zapcore.EntryCaller {
	frames := runtime.CallersFrames([]uintptr{programCounter})
	frame, _ := frames.Next()
	return zapcore.EntryCaller{
		Defined:  true,
		PC:       programCounter,
		File:     frame.File,
		Line:     frame.Line,
		Function: frame.Function,
	}
}

// wrapAttrWithGroups 把当前 Handler group 固化到单个 Attr。
func wrapAttrWithGroups(attr slog.Attr, groups []string) slog.Attr {
	result := attr
	for index := len(groups) - 1; index >= 0; index-- {
		result = slog.Attr{Key: groups[index], Value: slog.GroupValue(result)}
	}
	return result
}

// slogAttrsToZapFields 批量转换标准属性。
func slogAttrsToZapFields(attrs []slog.Attr) []zap.Field {
	result := make([]zap.Field, 0, len(attrs))
	for _, attr := range attrs {
		result = append(result, slogAttrToZapFields(attr)...)
	}
	return result
}

// mergeSlogGroups 合并同层同名 Group，避免 JSON 重复键覆盖先前字段。
// 阶段角色：收口 WithAttrs/WithGroup 多次派生形成的离散同名组。
func mergeSlogGroups(attrs []slog.Attr) []slog.Attr {
	result := make([]slog.Attr, 0, len(attrs))
	groupIndexes := make(map[string]int)
	var appendAttr func(attr slog.Attr)
	appendAttr = func(attr slog.Attr) {
		// 第一步：展开值并递归规范化当前属性的子组。
		attr.Value = attr.Value.Resolve()
		if attr.Value.Kind() != slog.KindGroup {
			result = append(result, attr)
			return
		}
		children := mergeSlogGroups(attr.Value.Group())
		if len(children) == 0 {
			return
		}
		if attr.Key == "" {
			// 空组只承担展开语义，其子字段提升到当前层。
			for _, child := range children {
				appendAttr(child)
			}
			return
		}
		if index, exists := groupIndexes[attr.Key]; exists {
			// 同层同名组按出现顺序合并，避免 JSON 重复键覆盖。
			existing := result[index].Value.Group()
			combined := make([]slog.Attr, 0, len(existing)+len(children))
			combined = append(combined, existing...)
			combined = append(combined, children...)
			merged := mergeSlogGroups(combined)
			result[index] = slog.Attr{Key: attr.Key, Value: slog.GroupValue(merged...)}
			return
		}
		groupIndexes[attr.Key] = len(result)
		result = append(result, slog.Attr{Key: attr.Key, Value: slog.GroupValue(children...)})
	}
	// 第二步：顺序归并顶层属性，保持调用方字段声明顺序。
	for _, attr := range attrs {
		appendAttr(attr)
	}
	return result
}

// slogAttrToZapFields 转换单个属性并递归保持 Group 结构。
func slogAttrToZapFields(attr slog.Attr) []zap.Field {
	// 第一步：解析 LogValuer，并优先保持标准 Group 嵌套语义。
	attr.Value = attr.Value.Resolve()
	if attr.Value.Kind() == slog.KindGroup {
		group := attr.Value.Group()
		if attr.Key == "" {
			return slogAttrsToZapFields(group)
		}
		return []zap.Field{zap.Object(attr.Key, slogGroupObject(group))}
	}
	if strings.TrimSpace(attr.Key) == "" {
		return nil
	}
	// 第二步：按标准值类型选择 Zap 强类型字段，未知类型保留为 Any。
	switch attr.Value.Kind() {
	case slog.KindBool:
		return []zap.Field{zap.Bool(attr.Key, attr.Value.Bool())}
	case slog.KindDuration:
		return []zap.Field{zap.Duration(attr.Key, attr.Value.Duration())}
	case slog.KindFloat64:
		return []zap.Field{zap.Float64(attr.Key, attr.Value.Float64())}
	case slog.KindInt64:
		return []zap.Field{zap.Int64(attr.Key, attr.Value.Int64())}
	case slog.KindString:
		return []zap.Field{zap.String(attr.Key, attr.Value.String())}
	case slog.KindTime:
		return []zap.Field{zap.Time(attr.Key, attr.Value.Time())}
	case slog.KindUint64:
		return []zap.Field{zap.Uint64(attr.Key, attr.Value.Uint64())}
	case slog.KindAny:
		if err, ok := attr.Value.Any().(error); ok {
			return []zap.Field{zap.NamedError(attr.Key, err)}
		}
		return []zap.Field{zap.Any(attr.Key, attr.Value.Any())}
	default:
		return []zap.Field{zap.Any(attr.Key, attr.Value.Any())}
	}
}

// slogGroupObject 把标准属性组适配为 Zap ObjectMarshaler。
type slogGroupObject []slog.Attr

// MarshalLogObject 按声明顺序编码嵌套字段组。
func (this slogGroupObject) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	for _, field := range slogAttrsToZapFields(this) {
		field.AddTo(encoder)
	}
	return nil
}
