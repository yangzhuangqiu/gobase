package log

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// capturedEvent 是契约测试观察到的标准日志事件快照。
type capturedEvent struct {
	// message 是人类可读事件描述。
	message string
	// level 是转换后的标准事件级别。
	level slog.Level
	// fields 是按键保存的结构化字段。
	fields map[string]any
	// caller 是 Record.PC 解析出的调用文件。
	caller string
}

// captureHandler 记录事件，用于验证新 Logger 到 slog.Handler 的契约。
type captureHandler struct {
	// mu 保护共享事件切片，支持并发 Logger 行为测试。
	mu *sync.Mutex
	// events 保存所有派生 Handler 共享的事件快照。
	events *[]capturedEvent
	// attrs 保存 WithAttrs 派生的固定字段结构。
	attrs []slog.Attr
	// enabled 控制测试事件是否进入 Handle，用于验证禁用级别短路语义。
	enabled bool
}

// newCaptureHandler 创建共享事件存储的测试 Handler。
func newCaptureHandler() *captureHandler {
	events := make([]capturedEvent, 0, 4)
	return &captureHandler{mu: &sync.Mutex{}, events: &events, enabled: true}
}

// Enabled 按测试配置决定事件是否进入 Handle。
func (this *captureHandler) Enabled(context.Context, slog.Level) bool {
	return this.enabled
}

// Handle 把 Record 与固定 Attr 转成稳定快照。
func (this *captureHandler) Handle(_ context.Context, record slog.Record) error {
	fields := make(map[string]any)
	for _, attr := range this.attrs {
		fields[attr.Key] = attr.Value.Any()
	}
	record.Attrs(func(attr slog.Attr) bool {
		fields[attr.Key] = attr.Value.Any()
		return true
	})
	caller := ""
	if record.PC != 0 {
		frames := runtime.CallersFrames([]uintptr{record.PC})
		frame, _ := frames.Next()
		caller = frame.File
	}
	this.mu.Lock()
	*this.events = append(*this.events, capturedEvent{
		message: record.Message,
		level:   record.Level,
		fields:  fields,
		caller:  caller,
	})
	this.mu.Unlock()
	return nil
}

// WithAttrs 返回固定字段隔离的 Handler 副本。
func (this *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	result := &captureHandler{mu: this.mu, events: this.events, enabled: this.enabled}
	result.attrs = append(result.attrs, this.attrs...)
	result.attrs = append(result.attrs, attrs...)
	return result
}

// WithGroup 在本组测试中不改变字段键，仅验证 Handler 契约可派生。
func (this *captureHandler) WithGroup(string) slog.Handler {
	return &captureHandler{mu: this.mu, events: this.events, attrs: append([]slog.Attr{}, this.attrs...), enabled: this.enabled}
}

// snapshot 返回当前事件快照。
func (this *captureHandler) snapshot() []capturedEvent {
	this.mu.Lock()
	defer this.mu.Unlock()
	result := make([]capturedEvent, len(*this.events))
	copy(result, *this.events)
	return result
}

// TestEventLoggerStructuredContract 验证新 Logger 以结构化字段构造标准事件。
//
// 测试方法：创建 capture Handler，记录 Info 事件，断言 level/message/field 均保持独立语义。
func TestEventLoggerStructuredContract(t *testing.T) {
	handler := newCaptureHandler()
	logger := newEventLogger(handler)
	logger.Info(context.Background(), "order accepted", F("op", "order.accept"), F("count", 2))

	events := handler.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected one event, got=%d", len(events))
	}
	if events[0].level != slog.LevelInfo || events[0].message != "order accepted" {
		t.Fatalf("unexpected event: %+v", events[0])
	}
	if events[0].fields["op"] != "order.accept" || events[0].fields["count"] != int64(2) {
		t.Fatalf("unexpected fields: %+v", events[0].fields)
	}
}

// TestEventLoggerWithDoesNotModifyParent 验证 With 只影响派生 Logger。
//
// 测试方法：父子 Logger 各记录一次，断言固定字段只出现在子事件。
func TestEventLoggerWithDoesNotModifyParent(t *testing.T) {
	handler := newCaptureHandler()
	parent := newEventLogger(handler)
	child := parent.With(F("component", "order"))
	parent.Info(context.Background(), "parent")
	child.Info(context.Background(), "child")

	events := handler.snapshot()
	if len(events) != 2 {
		t.Fatalf("expected two events, got=%d", len(events))
	}
	if _, exists := events[0].fields["component"]; exists {
		t.Fatalf("parent event polluted: %+v", events[0].fields)
	}
	if events[1].fields["component"] != "order" {
		t.Fatalf("child field missing: %+v", events[1].fields)
	}
}

// TestEventLoggerWithKeepsReferenceValueOwnership 验证 With 不深复制 Field.Value 中的引用值。
//
// 测试方法：使用 map 派生 Logger 后修改原 map，再记录事件并断言 Handler 观察到调用方持有的最新值。
func TestEventLoggerWithKeepsReferenceValueOwnership(t *testing.T) {
	handler := newCaptureHandler()
	metadata := map[string]any{"version": 1}
	logger := newEventLogger(handler).With(F("metadata", metadata))
	metadata["version"] = 2
	logger.Info(context.Background(), "reference value")

	events := handler.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected one event, got=%d", len(events))
	}
	actual, ok := events[0].fields["metadata"].(map[string]any)
	if !ok || actual["version"] != 2 {
		t.Fatalf("reference value ownership mismatch: %+v", events[0].fields)
	}
}

// TestEventLoggerCallerPointsToBusinessCall 验证门面修正后的 caller 指向测试调用点。
//
// 测试方法：从测试方法直接写日志，解析 Record.PC 并断言文件不是门面实现文件。
func TestEventLoggerCallerPointsToBusinessCall(t *testing.T) {
	handler := newCaptureHandler()
	newEventLogger(handler).Info(context.Background(), "caller check")
	events := handler.snapshot()
	if len(events) != 1 || !strings.HasSuffix(events[0].caller, "slog_logger_test.go") {
		t.Fatalf("unexpected caller: %+v", events)
	}
}

// TestContextHandlerAddsTypedExtractedFields 验证 context extractor 在统一 Handler 边界附加字段。
//
// 测试方法：使用私有 typed key 写入 context，经装饰 Handler 记录后断言 request_id 字段存在。
func TestContextHandlerAddsTypedExtractedFields(t *testing.T) {
	type requestIDKey struct{}
	handler := newCaptureHandler()
	decorated := newContextHandler(handler, []ContextExtractor{
		func(ctx context.Context) []Field {
			value, _ := ctx.Value(requestIDKey{}).(string)
			return []Field{F("request_id", value)}
		},
	})
	ctx := context.WithValue(context.Background(), requestIDKey{}, "req-1001")
	newEventLogger(decorated).Info(ctx, "request completed")

	events := handler.snapshot()
	if len(events) != 1 || events[0].fields["request_id"] != "req-1001" {
		t.Fatalf("unexpected context fields: %+v", events)
	}
}

// TestContextHandlerSkipsExtractorWhenLevelDisabled 验证禁用级别不会执行 ContextExtractor。
//
// 测试方法：关闭下游 Handler 的 Enabled，记录一次 Info，断言 extractor 未调用且没有事件进入 Handle。
func TestContextHandlerSkipsExtractorWhenLevelDisabled(t *testing.T) {
	handler := newCaptureHandler()
	handler.enabled = false
	extractorCalls := 0
	decorated := newContextHandler(handler, []ContextExtractor{
		func(context.Context) []Field {
			extractorCalls++
			return []Field{F("request_id", "req-disabled")}
		},
	})
	newEventLogger(decorated).Info(context.Background(), "disabled event")

	if extractorCalls != 0 {
		t.Fatalf("disabled level must skip extractor: calls=%d", extractorCalls)
	}
	if events := handler.snapshot(); len(events) != 0 {
		t.Fatalf("disabled level must skip Handle: %+v", events)
	}
}
