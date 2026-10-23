package appwebhertz

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
	// logger 是 LoggerFactory.Get 绑定的稳定组件名。
	logger string
	// level 是 Access Log 根据完成态选择的稳定日志级别。
	level baselog.Level
	// message 是人类可读事件名。
	message string
	// fields 是按键保存的结构化字段。
	fields map[string]any
}

// recordingLoggerFactory 返回共享事件存储的测试 Logger。
type recordingLoggerFactory struct {
	// mu 保护事件的并发读写。
	mu sync.Mutex
	// events 保存全部结构化事件。
	events []recordedLogEvent
}

// Get 记录组件名并返回共享 Logger。
func (this *recordingLoggerFactory) Get(name string) baselog.Logger {
	return &recordingLogger{factory: this, name: name}
}

// recordingLogger 实现最小根日志合同并保存事件。
type recordingLogger struct {
	// factory 是全部派生 Logger 共享的事件存储。
	factory *recordingLoggerFactory
	// name 是 Factory 派生时绑定的稳定组件名。
	name string
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
	this.factory.events = append(this.factory.events, recordedLogEvent{logger: this.name, level: level, message: message, fields: values})
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

// testServerLoggers 返回由共享记录工厂派生的完整 Server Logger 集合。
// 测试方法：复用生产解析入口，确保内部 builder 测试满足 Logger 前置条件。
func testServerLoggers() serverLoggers {
	loggers, err := resolveServerLoggers(&recordingLoggerFactory{})
	if err != nil {
		panic(err)
	}
	return loggers
}

// TestAccessLogWritesStableSafeFields 验证内建 Access Log 使用固定 Logger 和完成态白名单字段。
// 测试方法：构造慢请求快照，断言 Warn、稳定字段，并确认敏感请求内容不会进入事件。
func TestAccessLogWritesStableSafeFields(t *testing.T) {
	factory := &recordingLoggerFactory{}
	observer, err := newAccessLogObserver(factory.Get(accessLoggerName), AccessLogConfig{SlowThreshold: time.Second})
	if err != nil {
		t.Fatalf("new access log observer: %v", err)
	}

	observer.Observe(context.Background(), webhertz.HTTPRequestObservation{
		RequestID: "request-1", TraceID: "trace-1", Method: "GET", RoutePattern: "/orders/:id", Path: "/orders/42",
		Status: 200, Duration: 1500 * time.Millisecond, RequestBytes: 12, ResponseBytes: 34, Outcome: webhertz.RequestOutcomeSuccess,
	})
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if len(factory.events) != 1 || factory.events[0].logger != accessLoggerName {
		t.Fatalf("unexpected logger selection/events: events=%+v", factory.events)
	}
	event := factory.events[0]
	if event.level != baselog.LevelWarn || event.message != "http_request" || event.fields["reason"] != "slow" || event.fields["status"] != 200 {
		t.Fatalf("unexpected stable request event: %+v", event)
	}
	if event.fields["request_id"] != "request-1" || event.fields["trace_id"] != "trace-1" || event.fields["route"] != "/orders/:id" {
		t.Fatalf("unexpected correlated request event: %+v", event.fields)
	}
	for _, forbidden := range []string{"query", "headers", "body", "authorization", "cookie", "stack", "error"} {
		if _, exists := event.fields[forbidden]; exists {
			t.Fatalf("forbidden field present: %s", forbidden)
		}
	}
}

// TestAccessLogUsesErrorForPanicAndRequiresFactory 验证 panic 完成态使用 Error，且启用日志时必须提供 LoggerFactory。
// 测试方法：先传入 typed nil Factory 断言声明错误，再记录 panic 快照断言 result/reason。
func TestAccessLogUsesErrorForPanicAndRequiresFactory(t *testing.T) {
	var nilLogger *recordingLogger
	if _, err := newAccessLogObserver(nilLogger, AccessLogConfig{}); !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected nil factory error, got=%v", err)
	}

	factory := &recordingLoggerFactory{}
	observer, err := newAccessLogObserver(factory.Get(accessLoggerName), AccessLogConfig{})
	if err != nil {
		t.Fatalf("new panic access log observer: %v", err)
	}
	observer.Observe(context.Background(), webhertz.HTTPRequestObservation{Status: 500, Outcome: webhertz.RequestOutcomePanic})
	factory.mu.Lock()
	defer factory.mu.Unlock()
	event := factory.events[0]
	if event.level != baselog.LevelError || event.fields["result"] != "failed" || event.fields["reason"] != "panic" {
		t.Fatalf("unexpected panic request event: %+v", event)
	}
}
