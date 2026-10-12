package log

import (
	"context"
	"sync"
	"testing"
)

// TestLoggerFactoryNormalizesNameBehavior 验证 factory 对同一名称提供一致行为。
//
// 测试方法：分别以带空白名称和归一化名称获取 Logger，各记录一次并断言 logger 字段一致；不比较实例身份。
func TestLoggerFactoryNormalizesNameBehavior(t *testing.T) {
	handler := newCaptureHandler()
	factory := NewLoggerFactory(handler)
	first := factory.Get(" order.service ")
	second := factory.Get("order.service")
	first.Info(context.Background(), "first")
	second.Info(context.Background(), "second")
	events := handler.snapshot()
	if len(events) != 2 {
		t.Fatalf("unexpected logger field: %+v", events)
	}
	for _, event := range events {
		if event.fields["logger"] != "order.service" {
			t.Fatalf("normalized logger behavior mismatch: %+v", events)
		}
	}
}

// TestLoggerFactoryFallbackNeverReturnsNil 验证 provider 缺失时仍返回可记录 Logger。
//
// 测试方法：使用 nil provider 获取 root Logger，断言非 nil 并调用 Info 无 panic。
func TestLoggerFactoryFallbackNeverReturnsNil(t *testing.T) {
	logger := NewLoggerFactory(nil).Get("")
	if logger == nil {
		t.Fatalf("fallback logger should not be nil")
	}
	logger.Info(context.Background(), "fallback logger works")
}

// TestLoggerFactoryConcurrentSameNameBehavior 验证同名 Logger 并发获取时行为一致。
//
// 测试方法：多个 goroutine 同时 Get 相同名称并记录事件，断言数量和 logger 字段一致，不依赖实例身份。
func TestLoggerFactoryConcurrentSameNameBehavior(t *testing.T) {
	handler := newCaptureHandler()
	factory := NewLoggerFactory(handler)
	const workers = 16
	var wait sync.WaitGroup
	wait.Add(workers)
	for index := 0; index < workers; index++ {
		go func() {
			defer wait.Done()
			factory.Get("concurrent.same").Info(context.Background(), "concurrent event")
		}()
	}
	wait.Wait()
	events := handler.snapshot()
	if len(events) != workers {
		t.Fatalf("unexpected concurrent event count: got=%d want=%d", len(events), workers)
	}
	for _, event := range events {
		if event.fields["logger"] != "concurrent.same" {
			t.Fatalf("concurrent logger behavior mismatch: %+v", event)
		}
	}
}
