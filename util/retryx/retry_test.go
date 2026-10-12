package retryx

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

type timeoutTestError struct{}

// Error 返回测试错误文本，供标准 error 协议使用。
func (this timeoutTestError) Error() string {
	return "timeout"
}

// Timeout 模拟 net.Error 的超时行为，但不实现 Retryable。
func (this timeoutTestError) Timeout() bool {
	return true
}

// Temporary 模拟非临时超时错误。
func (this timeoutTestError) Temporary() bool {
	return false
}

type temporaryTestError struct{}

// Error 返回测试错误文本，供标准 error 协议使用。
func (this temporaryTestError) Error() string {
	return "temporary"
}

// Temporary 模拟旧式临时错误接口，但不实现 Retryable。
func (this temporaryTestError) Temporary() bool {
	return true
}

type retryableTestError struct {
	// retryable 控制 Retryable 返回值，覆盖允许与拒绝两条策略分支。
	retryable bool
}

// Error 返回测试错误文本，供错误链匹配使用。
func (this retryableTestError) Error() string {
	return "retryable"
}

// Retryable 返回测试指定的重试决策。
func (this retryableTestError) Retryable() bool {
	return this.retryable
}

// TestDoSuccessAfterRetry 验证失败后成功时状态机立即返回成功。
// 测试方法：前两次返回错误、第三次成功，断言总尝试次数为 3。
func TestDoSuccessAfterRetry(t *testing.T) {
	// 尝试阶段：前两次失败推动状态机，第三次成功终止。
	attempts := 0
	err := Do(context.Background(), Options{
		Attempts:       3,
		InitialDelay:   0,
		MaxDelay:       0,
		Multiplier:     1,
		JitterFraction: 0,
	}, func(attempt int) error {
		attempts++
		if attempt < 3 {
			return errors.New("temporary")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts mismatch: %d", attempts)
	}
}

// TestDoExhausted 验证次数耗尽同时保留 typed error、Kind 和最后 cause。
// 测试方法：连续失败两次，分别使用 IsExhausted、IsKind 和 errors.Is 断言。
func TestDoExhausted(t *testing.T) {
	// 终态阶段：typed error、Kind 和 cause 必须同时可观察。
	finalErr := errors.New("still failing")
	err := Do(context.Background(), Options{
		Attempts:       2,
		InitialDelay:   0,
		MaxDelay:       0,
		Multiplier:     1,
		JitterFraction: 0,
	}, func(attempt int) error {
		return finalErr
	})
	if err == nil {
		t.Fatal("expected exhausted error")
	}
	if !IsExhausted(err) {
		t.Fatalf("expected exhausted error, got: %v", err)
	}
	if !errorsx.IsKind(err, KindExhausted) {
		t.Fatalf("expected kind %s, got: %v", KindExhausted, err)
	}
	if !errors.Is(err, finalErr) {
		t.Fatalf("expected wrapped final error, got: %v", err)
	}
}

// TestDoNonRetryable 验证 ShouldRetry 拒绝后立即短路且保留原始错误。
// 测试方法：策略对指定错误返回 false，断言没有耗尽并命中 KindNonRetryable。
func TestDoNonRetryable(t *testing.T) {
	// 策略阶段：ShouldRetry 明确拒绝后不得继续消耗剩余次数。
	nonRetryableErr := errors.New("bad request")
	err := Do(context.Background(), Options{
		Attempts:       5,
		InitialDelay:   0,
		MaxDelay:       0,
		Multiplier:     1,
		JitterFraction: 0,
		ShouldRetry: func(err error) bool {
			return !errors.Is(err, nonRetryableErr)
		},
	}, func(attempt int) error {
		return nonRetryableErr
	})
	if err == nil {
		t.Fatal("expected non-retryable error")
	}
	if IsExhausted(err) {
		t.Fatalf("non-retryable should not be exhausted: %v", err)
	}
	if !errors.Is(err, nonRetryableErr) {
		t.Fatalf("expected wrapped non-retryable error: %v", err)
	}
	if !errorsx.IsKind(err, KindNonRetryable) {
		t.Fatalf("expected kind %s, got: %v", KindNonRetryable, err)
	}
}

// TestDoContextDone 验证尝试开始前的 context 取消会阻止 operation 执行。
// 测试方法：传入已取消 context，断言 cause 为 context.Canceled 且 Kind 可分支。
func TestDoContextDone(t *testing.T) {
	// 准备阶段：在 Do 前取消 context，确保 operation 不获得执行机会。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Do(ctx, DefaultOptions(), func(attempt int) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected context done error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error: %v", err)
	}
	if !errorsx.IsKind(err, KindContextDone) {
		t.Fatalf("expected kind %s, got: %v", KindContextDone, err)
	}
}

// TestDefaultOptions 验证默认配置满足公开约束并可直接用于 Do。
// 测试方法：逐项断言次数、延迟、倍率和 jitter 均位于合法范围。
func TestDefaultOptions(t *testing.T) {
	// 约束阶段：逐字段检查默认值能够通过 validateOptions。
	options := DefaultOptions()
	if options.Attempts <= 0 {
		t.Fatalf("invalid attempts: %d", options.Attempts)
	}
	if options.InitialDelay <= 0 {
		t.Fatalf("invalid initial delay: %s", options.InitialDelay)
	}
	if options.MaxDelay <= 0 {
		t.Fatalf("invalid max delay: %s", options.MaxDelay)
	}
	if options.Multiplier < 1 {
		t.Fatalf("invalid multiplier: %.2f", options.Multiplier)
	}
	if options.JitterFraction < 0 || options.JitterFraction > 1 {
		t.Fatalf("invalid jitter: %.2f", options.JitterFraction)
	}
}

// TestApplyJitter 验证抖动结果位于配置比例允许的区间。
// 测试方法：对 100ms 应用 20% jitter，断言结果位于 80ms 到 120ms。
func TestApplyJitter(t *testing.T) {
	delay := 100 * time.Millisecond
	jittered := applyJitter(delay, 0.2)
	if jittered < 80*time.Millisecond || jittered > 120*time.Millisecond {
		t.Fatalf("jitter out of range: %s", jittered)
	}
}

// TestDoClampsInitialDelayToMaxDelay 验证 MaxDelay 约束首次实际等待而非仅后续倍率值。
// 测试方法：配置 10ms 初始延迟和 1ms 上限，从 Retrying 事件断言实际 delay 为 1ms。
func TestDoClampsInitialDelayToMaxDelay(t *testing.T) {
	// 观测阶段：通过 Retrying 事件读取状态机实际采用的 wait。
	var retryDelay time.Duration
	err := Do(context.Background(), Options{
		Attempts:       2,
		InitialDelay:   10 * time.Millisecond,
		MaxDelay:       time.Millisecond,
		Multiplier:     1,
		JitterFraction: 0,
		Observer: func(event Event) {
			if event.Type == EventRetrying {
				retryDelay = event.Delay
			}
		},
	}, func(attempt int) error {
		if attempt == 1 {
			return errors.New("retry once")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	if retryDelay != time.Millisecond {
		t.Fatalf("retry delay mismatch: got=%s want=%s", retryDelay, time.Millisecond)
	}
}

// TestNextDelaySaturatesOnOverflow 验证极端倍率不会把 duration 环绕成负数。
// 测试方法：对 MaxInt64 duration 使用 MaxFloat64 倍率，断言结果饱和在 MaxInt64。
func TestNextDelaySaturatesOnOverflow(t *testing.T) {
	got := nextDelay(time.Duration(math.MaxInt64), math.MaxFloat64, 0)
	if got != time.Duration(math.MaxInt64) {
		t.Fatalf("next delay mismatch: got=%s want=%s", got, time.Duration(math.MaxInt64))
	}
}

// TestDoRejectsNonFiniteOptions 验证 NaN/Inf 配置在 operation 执行前失败。
// 测试方法：分别传入 NaN multiplier 和 Inf jitter，断言返回 KindInvalidOptions。
func TestDoRejectsNonFiniteOptions(t *testing.T) {
	cases := []Options{
		{Attempts: 1, Multiplier: math.NaN()},
		{Attempts: 1, Multiplier: 1, JitterFraction: math.Inf(1)},
	}
	for _, options := range cases {
		if err := Do(nil, options, func(attempt int) error { return nil }); err == nil {
			t.Fatal("expected invalid options error")
		} else if !errorsx.IsKind(err, KindInvalidOptions) {
			t.Fatalf("expected kind %s, got: %v", KindInvalidOptions, err)
		}
	}
}

// TestObserverEvents 验证重试成功链路按顺序发出 Retrying 和 Success 事件。
// 测试方法：前两次失败、第三次成功，断言事件数量和类型序列。
func TestObserverEvents(t *testing.T) {
	// 收集阶段：Observer 同步追加事件，便于断言严格顺序。
	events := make([]Event, 0, 4)
	err := Do(context.Background(), Options{
		Attempts:       3,
		InitialDelay:   0,
		MaxDelay:       0,
		Multiplier:     1,
		JitterFraction: 0,
		Observer: func(event Event) {
			events = append(events, event)
		},
	}, func(attempt int) error {
		if attempt < 3 {
			return errors.New("temporary")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("unexpected events len: %d", len(events))
	}
	if events[0].Type != EventRetrying || events[1].Type != EventRetrying || events[2].Type != EventSuccess {
		t.Fatalf("unexpected event sequence: %#v", events)
	}
}

// TestObserverExhaustedEvent 验证失败终态只在最后发出 Exhausted 事件。
// 测试方法：连续失败两次，断言事件序列为 Retrying、Exhausted。
func TestObserverExhaustedEvent(t *testing.T) {
	// 终态阶段：最后一次失败不再发 Retrying，只发 Exhausted。
	events := make([]Event, 0, 3)
	err := Do(context.Background(), Options{
		Attempts:       2,
		InitialDelay:   0,
		MaxDelay:       0,
		Multiplier:     1,
		JitterFraction: 0,
		Observer: func(event Event) {
			events = append(events, event)
		},
	}, func(attempt int) error {
		return errors.New("always fail")
	})
	if err == nil {
		t.Fatal("expected exhausted error")
	}
	if len(events) != 2 {
		t.Fatalf("unexpected events len: %d", len(events))
	}
	if events[0].Type != EventRetrying || events[1].Type != EventExhausted {
		t.Fatalf("unexpected event sequence: %#v", events)
	}
}

// TestDoValue 验证泛型重试入口在成功后返回业务值。
// 测试方法：第一次失败、第二次返回 100，断言结果和错误。
func TestDoValue(t *testing.T) {
	// 值传递阶段：失败尝试的零值不得覆盖最终成功值。
	value, err := DoValue(context.Background(), Options{
		Attempts:       3,
		InitialDelay:   0,
		MaxDelay:       0,
		Multiplier:     1,
		JitterFraction: 0,
	}, func(attempt int) (int, error) {
		if attempt < 2 {
			return 0, errors.New("temporary")
		}
		return 100, nil
	})
	if err != nil {
		t.Fatalf("DoValue error: %v", err)
	}
	if value != 100 {
		t.Fatalf("DoValue mismatch: %d", value)
	}
}

// TestDoValueUsesOwnOperationName 验证泛型入口的错误 Op 不会退化成 retryx.Do。
// 测试方法：让单次尝试失败耗尽，断言 OpOf 返回 retryx.DoValue。
func TestDoValueUsesOwnOperationName(t *testing.T) {
	// 错误阶段：单次失败直接耗尽，外层 Op 必须保留泛型入口名称。
	_, err := DoValue(context.Background(), Options{
		Attempts:       1,
		Multiplier:     1,
		JitterFraction: 0,
	}, func(attempt int) (int, error) {
		return 0, errors.New("failed")
	})
	if err == nil {
		t.Fatal("expected exhausted error")
	}
	op, ok := errorsx.OpOf(err)
	if !ok || op != "retryx.DoValue" {
		t.Fatalf("unexpected op: op=%s ok=%v err=%v", op, ok, err)
	}
}

// TestShouldRetryByError 验证 Retryable 是唯一默认识别的重试协议。
// 测试方法：覆盖 true/false、Timeout、Temporary、context 和普通错误。
func TestShouldRetryByError(t *testing.T) {
	// 协议阶段：只有 Retryable() bool 参与判断，旧行为接口不能隐式生效。
	if !ShouldRetryByError(retryableTestError{retryable: true}) {
		t.Fatal("expected retryable=true to be recognized")
	}
	if ShouldRetryByError(retryableTestError{retryable: false}) {
		t.Fatal("expected retryable=false to be recognized")
	}
	if ShouldRetryByError(timeoutTestError{}) {
		t.Fatal("expected timeout error to be non-retryable without Retryable()")
	}
	if ShouldRetryByError(temporaryTestError{}) {
		t.Fatal("expected temporary error to be non-retryable without Retryable()")
	}
	if ShouldRetryByError(context.Canceled) {
		t.Fatal("expected context canceled to be non-retryable")
	}
	if ShouldRetryByError(errors.New("plain")) {
		t.Fatal("did not expect plain error to be retryable")
	}
}
