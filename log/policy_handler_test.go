package log

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
)

// TestKeyRedactorCoversFixedEventGroupAndContextFields 验证脱敏策略覆盖全部字段来源。
//
// 测试方法：从 With、单次 Group 和 ContextExtractor 分别写入敏感值，经统一 Factory 后断言均替换且普通字段保持原值。
func TestKeyRedactorCoversFixedEventGroupAndContextFields(t *testing.T) {
	// 第一步：装配三种敏感路径，并从固定字段、Group、context 分别提供原值。
	type contextSecretKey struct{}
	handler := newCaptureHandler()
	factory := NewLoggerFactory(
		handler,
		WithContextExtractors(func(ctx context.Context) []Field {
			return []Field{F("request_secret", ctx.Value(contextSecretKey{}))}
		}),
		WithRedactor(NewKeyRedactor("password", "auth.token", "credentials", "request_secret")),
	)
	logger := factory.Get("redaction.test").With(F("password", "plain-password"))
	ctx := context.WithValue(context.Background(), contextSecretKey{}, "plain-request-secret")
	logger.Info(ctx, "redaction event",
		Group("auth", F("token", "plain-token"), F("method", "password")),
		Group("credentials", F("client_id", "plain-client"), F("client_secret", "plain-secret")),
	)

	// 第二步：逐层断言敏感值替换，普通字段不受影响。
	events := handler.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected one event, got=%d", len(events))
	}
	if events[0].fields["password"] != RedactedValue || events[0].fields["request_secret"] != RedactedValue {
		t.Fatalf("top-level sensitive fields leaked: %+v", events[0].fields)
	}
	auth, ok := events[0].fields["auth"].([]slog.Attr)
	if !ok || len(auth) != 2 {
		t.Fatalf("unexpected auth group: %+v", events[0].fields["auth"])
	}
	if auth[0].Key != "token" || auth[0].Value.Any() != RedactedValue {
		t.Fatalf("group token leaked: %+v", auth)
	}
	if auth[1].Key != "method" || auth[1].Value.Any() != "password" {
		t.Fatalf("ordinary group field changed: %+v", auth)
	}
	if events[0].fields["credentials"] != RedactedValue {
		t.Fatalf("sensitive group leaked: %+v", events[0].fields["credentials"])
	}
}

// TestDisabledLevelDoesNotExecuteEventRedactor 验证禁用事件不会执行单次字段脱敏策略。
//
// 测试方法：使用 disabled Handler 和计数 Redactor 获取 Logger，记录基线后写入事件字段，断言调用次数不再增长。
func TestDisabledLevelDoesNotExecuteEventRedactor(t *testing.T) {
	handler := newCaptureHandler()
	handler.enabled = false
	var calls atomic.Int32
	factory := NewLoggerFactory(handler, WithRedactor(RedactorFunc(func(_ []string, value any) any {
		calls.Add(1)
		return value
	})))
	logger := factory.Get("redactor.disabled")
	baseline := calls.Load()
	logger.Debug(context.Background(), "disabled event", F("token", "plain-token"))
	if actual := calls.Load(); actual != baseline {
		t.Fatalf("disabled event executed redactor: before=%d after=%d", baseline, actual)
	}
}

// TestSchemaHandlerProtectsEnvelopeAndUsesFirstValue 验证顶层系统键不可伪造且重复标量键 first-wins。
//
// 测试方法：固定字段、事件字段与 ContextExtractor 写入冲突键，断言系统键被过滤、固定值和显式事件值优先。
func TestSchemaHandlerProtectsEnvelopeAndUsesFirstValue(t *testing.T) {
	// 第一步：构造 fixed/event/context 三层冲突以及顶层 envelope 伪造字段。
	handler := newCaptureHandler()
	factory := NewLoggerFactory(handler, WithContextExtractors(func(context.Context) []Field {
		return []Field{F("request_id", "context"), F("op", "context.op")}
	}))
	logger := factory.Get("schema.test").With(F("tenant", "fixed"))
	logger.Info(context.Background(), "schema event",
		F("tenant", "event"),
		F("request_id", "explicit"),
		F("op", "event.op"),
		F("op", "duplicate.op"),
		F("level", "forged"),
		F("msg", "forged"),
		F("logger", "forged"),
	)

	// 第二步：断言来源优先级和 envelope 保护规则。
	events := handler.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected one event, got=%d", len(events))
	}
	fields := events[0].fields
	if fields["logger"] != "schema.test" || fields["tenant"] != "fixed" {
		t.Fatalf("fixed fields should win: %+v", fields)
	}
	if fields["request_id"] != "explicit" || fields["op"] != "event.op" {
		t.Fatalf("explicit event fields should win: %+v", fields)
	}
	if _, exists := fields["level"]; exists {
		t.Fatalf("reserved level field should be filtered: %+v", fields)
	}
	if _, exists := fields["msg"]; exists {
		t.Fatalf("reserved msg field should be filtered: %+v", fields)
	}
}
