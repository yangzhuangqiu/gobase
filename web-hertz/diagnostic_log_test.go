package webhertz

import (
	"context"
	"sync"
	"testing"

	"github.com/yangzhuangqiu/gobase/errcode"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

// diagnosticEvent 保存测试 Logger 收到的结构化诊断事件。
type diagnosticEvent struct {
	// level 是调用入口选择的稳定日志级别。
	level baselog.Level
	// fields 是事件结构化字段的值快照。
	fields map[string]any
}

// diagnosticRecorder 实现实例 Logger 并保存并发安全事件快照。
type diagnosticRecorder struct {
	// mu 保护 events 的并发读写。
	mu sync.Mutex
	// events 保存 web-hertz 构造与生命周期事件。
	events []diagnosticEvent
}

// Enabled 允许测试覆盖全部日志级别。
func (this *diagnosticRecorder) Enabled(context.Context, baselog.Level) bool { return true }

// Log 保存事件级别和字段值。
func (this *diagnosticRecorder) Log(_ context.Context, level baselog.Level, _ string, fields ...baselog.Field) {
	values := make(map[string]any, len(fields))
	for _, field := range fields {
		values[field.Key] = field.Value
	}
	this.mu.Lock()
	this.events = append(this.events, diagnosticEvent{level: level, fields: values})
	this.mu.Unlock()
}

// Debug 以 Debug 级别委托 Log。
func (this *diagnosticRecorder) Debug(ctx context.Context, message string, fields ...baselog.Field) {
	this.Log(ctx, baselog.LevelDebug, message, fields...)
}

// Info 以 Info 级别委托 Log。
func (this *diagnosticRecorder) Info(ctx context.Context, message string, fields ...baselog.Field) {
	this.Log(ctx, baselog.LevelInfo, message, fields...)
}

// Warn 以 Warn 级别委托 Log。
func (this *diagnosticRecorder) Warn(ctx context.Context, message string, fields ...baselog.Field) {
	this.Log(ctx, baselog.LevelWarn, message, fields...)
}

// Error 以 Error 级别委托 Log。
func (this *diagnosticRecorder) Error(ctx context.Context, message string, fields ...baselog.Field) {
	this.Log(ctx, baselog.LevelError, message, fields...)
}

// With 返回共享记录器；本测试不使用固定字段派生。
func (this *diagnosticRecorder) With(...baselog.Field) baselog.Logger { return this }

// TestWithLoggerRoutesAssemblyAndLifecycleDiagnostics 验证实例 Logger 接管 provider 自有日志。
// 测试方法：注入记录器构造 Server 并在 Run 前关闭，断言 Debug 组装和 Info 生命周期事件均进入同一实例出口。
func TestWithLoggerRoutesAssemblyAndLifecycleDiagnostics(t *testing.T) {
	recorder := &diagnosticRecorder{}
	instance, err := New(Config{}, WithLogger(recorder))
	if err != nil {
		t.Fatalf("new server with logger: %v", err)
	}
	if err := instance.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown before run: %v", err)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.events) != 2 {
		t.Fatalf("expected assembly and shutdown events, got=%+v", recorder.events)
	}
	if recorder.events[0].level != baselog.LevelDebug || recorder.events[0].fields["op"] != "web_hertz.assemble" || recorder.events[0].fields["result"] != "ok" {
		t.Fatalf("unexpected assembly event: %+v", recorder.events[0])
	}
	if recorder.events[1].level != baselog.LevelInfo || recorder.events[1].fields["op"] != "web_hertz.shutdown" || recorder.events[1].fields["reason"] != "before_run" {
		t.Fatalf("unexpected lifecycle event: %+v", recorder.events[1])
	}
}

// TestWithLoggerRejectsTypedNil 验证实例 Logger 声明不会把 typed nil 推迟到运行期 panic。
// 测试方法：传入 typed nil 记录器，断言构造返回稳定 declaration_invalid 错误码。
func TestWithLoggerRejectsTypedNil(t *testing.T) {
	var logger *diagnosticRecorder
	_, err := New(Config{}, WithLogger(logger))
	if !errcode.IsCode[ErrorCode](err, ErrCodeDeclarationInvalid) {
		t.Fatalf("expected logger declaration error, got=%v", err)
	}
}
