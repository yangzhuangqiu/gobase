package webhertzobserve

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/yangzhuangqiu/gobase/errcode"
	baselog "github.com/yangzhuangqiu/gobase/log"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// recordedLogEvent 保存测试 Logger 收到的结构化事件副本。
type recordedLogEvent struct {
	// level 是 Observer 选择的稳定日志级别。
	level baselog.Level
	// message 是人类可读事件名。
	message string
	// fields 是按键保存的结构化字段。
	fields map[string]any
}

// recordingLoggerFactory 返回共享事件存储的测试 Logger。
type recordingLoggerFactory struct {
	// mu 保护名称和事件的并发读写。
	mu sync.Mutex
	// name 保存最后一次 Get 的组件名。
	name string
	// events 保存全部结构化事件。
	events []recordedLogEvent
}

// Get 记录组件名并返回共享 Logger。
func (this *recordingLoggerFactory) Get(name string) baselog.Logger {
	this.mu.Lock()
	this.name = name
	this.mu.Unlock()
	return &recordingLogger{factory: this}
}

// recordingLogger 实现最小根日志合同并保存事件。
type recordingLogger struct {
	// factory 是全部派生 Logger 共享的事件存储。
	factory *recordingLoggerFactory
}

// Enabled 在测试中允许全部级别。
func (this *recordingLogger) Enabled(context.Context, baselog.Level) bool { return true }

// Log 保存级别、消息和字段值副本。
func (this *recordingLogger) Log(_ context.Context, level baselog.Level, message string, fields ...baselog.Field) {
	values := make(map[string]any, len(fields))
	for _, field := range fields {
		values[field.Key] = field.Value
	}
	this.factory.mu.Lock()
	this.factory.events = append(this.factory.events, recordedLogEvent{level: level, message: message, fields: values})
	this.factory.mu.Unlock()
}

// Debug 以 Debug 级别委托 Log。
func (this *recordingLogger) Debug(ctx context.Context, message string, fields ...baselog.Field) {
	this.Log(ctx, baselog.LevelDebug, message, fields...)
}

// Info 以 Info 级别委托 Log。
func (this *recordingLogger) Info(ctx context.Context, message string, fields ...baselog.Field) {
	this.Log(ctx, baselog.LevelInfo, message, fields...)
}

// Warn 以 Warn 级别委托 Log。
func (this *recordingLogger) Warn(ctx context.Context, message string, fields ...baselog.Field) {
	this.Log(ctx, baselog.LevelWarn, message, fields...)
}

// Error 以 Error 级别委托 Log。
func (this *recordingLogger) Error(ctx context.Context, message string, fields ...baselog.Field) {
	this.Log(ctx, baselog.LevelError, message, fields...)
}

// With 返回共享测试 Logger；本测试不需要固定字段派生。
func (this *recordingLogger) With(...baselog.Field) baselog.Logger { return this }

// TestRequestLogObserverWritesStableSafeFields 验证日志适配器只记录完成态白名单字段，并保护核心字段不被 Enricher 覆盖。
// 测试方法：构造慢请求快照和含保留键的 Enricher，断言 Warn、稳定字段、无 query/body 字段及应用字段保留。
func TestRequestLogObserverWritesStableSafeFields(t *testing.T) {
	// 第一阶段：构造自定义组件名和字段 Enricher，故意尝试覆盖核心 status。
	factory := &recordingLoggerFactory{}
	observer, err := NewRequestLogObserver(factory,
		WithRequestLoggerName(" api.http "),
		WithRequestLogEnricher(func(context.Context, webhertz.HTTPRequestObservation) []baselog.Field {
			return []baselog.Field{baselog.F("status", 999), baselog.F("tenant_class", "internal")}
		}),
	)
	if err != nil {
		t.Fatalf("new request log observer: %v", err)
	}

	// 第二阶段：记录慢请求，验证级别、白名单和核心字段保护。
	observer.ObserveRequestLog(context.Background(), webhertz.HTTPRequestObservation{
		RequestID: "request-1", TraceID: "trace-1", Method: "GET", RoutePattern: "/orders/:id", Path: "/orders/42",
		Status: 200, Duration: 1500 * time.Millisecond, RequestBytes: 12, ResponseBytes: 34, Outcome: webhertz.RequestOutcomeSuccess,
	}, true)
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if factory.name != "api.http" || len(factory.events) != 1 {
		t.Fatalf("unexpected logger selection/events: name=%s events=%d", factory.name, len(factory.events))
	}
	event := factory.events[0]
	if event.level != baselog.LevelWarn || event.message != "http_request" || event.fields["reason"] != "slow" || event.fields["status"] != 200 {
		t.Fatalf("unexpected stable request event: %+v", event)
	}
	if event.fields["tenant_class"] != "internal" || event.fields["request_id"] != "request-1" || event.fields["route"] != "/orders/:id" {
		t.Fatalf("unexpected enriched request event: %+v", event.fields)
	}
	for _, forbidden := range []string{"query", "headers", "body", "authorization", "cookie", "stack", "error"} {
		if _, exists := event.fields[forbidden]; exists {
			t.Fatalf("forbidden field present: %s", forbidden)
		}
	}
}

// TestRequestLogObserverUsesErrorForPanicAndRejectsNilDependencies 验证 panic 完成态使用 Error，且 nil Factory/Option 在构造期失败。
// 测试方法：分别传入 typed nil Factory、nil Option 断言错误码，再记录 panic 快照断言 result/reason。
func TestRequestLogObserverUsesErrorForPanicAndRejectsNilDependencies(t *testing.T) {
	// 第一阶段：构造边界拒绝 typed nil Factory 和 nil Option。
	var nilFactory *recordingLoggerFactory
	if _, err := NewRequestLogObserver(nilFactory); !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected nil factory error, got=%v", err)
	}
	if _, err := NewRequestLogObserver(&recordingLoggerFactory{}, RequestLogOption(nil)); !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected nil option error, got=%v", err)
	}

	// 第二阶段：panic outcome 使用 Error 且保留稳定失败原因。
	factory := &recordingLoggerFactory{}
	observer, err := NewRequestLogObserver(factory)
	if err != nil {
		t.Fatalf("new panic request log observer: %v", err)
	}
	observer.ObserveRequestLog(context.Background(), webhertz.HTTPRequestObservation{Status: 500, Outcome: webhertz.RequestOutcomePanic}, false)
	factory.mu.Lock()
	defer factory.mu.Unlock()
	event := factory.events[0]
	if event.level != baselog.LevelError || event.fields["result"] != "failed" || event.fields["reason"] != "panic" {
		t.Fatalf("unexpected panic request event: %+v", event)
	}
}
