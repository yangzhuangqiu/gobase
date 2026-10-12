package retryx

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

// Options 描述重试策略。
type Options struct {
	// Attempts 是最大尝试次数（包含第一次执行）。
	Attempts int
	// InitialDelay 是首次失败后的等待时长；0 表示立即进入下一次尝试。
	InitialDelay time.Duration
	// MaxDelay 是 jitter 后实际等待时长上限；0 表示不限制。
	MaxDelay time.Duration
	// Multiplier 是指数退避倍率（建议 >= 1）。
	Multiplier float64
	// JitterFraction 是抖动比例（0~1），例如 0.2 表示 ±20% 抖动。
	JitterFraction float64
	// ShouldRetry 返回 true 时继续重试；返回 false 时立即失败；nil 表示所有错误均可重试。
	ShouldRetry func(err error) bool
	// Observer 在调用 goroutine 内同步上报 attempt/delay/error；nil 表示关闭观测。
	Observer func(event Event)
}

// EventType 表示重试生命周期中的事件类型。
type EventType string

const (
	// EventRetrying 表示本次失败后将继续重试。
	EventRetrying EventType = "retrying"
	// EventSuccess 表示本次执行成功。
	EventSuccess EventType = "success"
	// EventNonRetryable 表示命中不可重试错误并立即返回。
	EventNonRetryable EventType = "non_retryable"
	// EventExhausted 表示重试次数耗尽。
	EventExhausted EventType = "exhausted"
	// EventContextDone 表示被 context 取消或超时终止。
	EventContextDone EventType = "context_done"
)

// Event 描述 retryx 执行过程中的可观测事件。
type Event struct {
	// Type 表示当前生命周期阶段，供日志和指标分类。
	Type EventType
	// Attempt 表示刚执行或即将执行的尝试序号，从 1 开始。
	Attempt int
	// MaxAttempts 表示配置允许的最大尝试次数，包含第一次执行。
	MaxAttempts int
	// Delay 表示 Retrying 事件之后的实际等待时间，其他事件固定为 0。
	Delay time.Duration
	// Err 表示触发当前事件的 operation 或 context 错误；成功事件为 nil。
	Err error
}

// Retryable 表示“错误是否可重试”的自描述协议。
type Retryable interface {
	Retryable() bool
}

// ExhaustedError 表示达到最大重试次数后仍失败。
type ExhaustedError struct {
	// Attempts 表示已经执行的总尝试次数。
	Attempts int
	// LastErr 表示最后一次 operation 失败，供 errors.Is/As 继续匹配。
	LastErr error
}

// Error 返回重试耗尽错误的标准化文本。
//
// 典型调用场景:
// - 直接打印错误时，快速定位为 retryx.exhausted 终态。
func (this *ExhaustedError) Error() string {
	if this == nil {
		return "retry attempts exhausted"
	}
	if this.LastErr == nil {
		return fmt.Sprintf("retry attempts exhausted: attempts=%d", this.Attempts)
	}
	return fmt.Sprintf("retry attempts exhausted: attempts=%d last_err=%v", this.Attempts, this.LastErr)
}

// Unwrap 返回最后一次失败错误，支持 errors.Is / errors.As。
func (this *ExhaustedError) Unwrap() error {
	if this == nil {
		return nil
	}
	return this.LastErr
}

// DefaultOptions 返回默认重试配置。
func DefaultOptions() Options {
	return Options{
		Attempts:       3,
		InitialDelay:   100 * time.Millisecond,
		MaxDelay:       2 * time.Second,
		Multiplier:     2.0,
		JitterFraction: 0.2,
	}
}

// Do 按配置执行重试。
// 参数:
// - ctx: 控制取消与截止时间。
// - options: 重试选项。
// - operation: 第 n 次尝试执行函数（attempt 从 1 开始）。
func Do(ctx context.Context, options Options, operation func(attempt int) error) error {
	return do(ctx, options, operation, "retryx.Do")
}

// do 执行共享重试状态机，并把错误和观测事件归属到公开调用入口 op。
// 阶段角色：统一 Do 与 DoValue 的校验、尝试、等待和终态语义。
func do(ctx context.Context, options Options, operation func(attempt int) error, op string) error {
	if operation == nil {
		return errorsx.New(op, KindInvalidOperation)
	}
	if err := validateOptions(options, op); err != nil {
		return err
	}

	// 尝试阶段：每轮先检查 context，再同步执行 operation 和策略回调。
	var lastErr error
	delay := options.InitialDelay
	for attempt := 1; attempt <= options.Attempts; attempt++ {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				emitObserver(options.Observer, Event{
					Type:        EventContextDone,
					Attempt:     attempt,
					MaxAttempts: options.Attempts,
					Delay:       0,
					Err:         err,
				})
				return errorsx.Wrap(err, op, KindContextDone)
			}
		}

		err := operation(attempt)
		if err == nil {
			emitObserver(options.Observer, Event{
				Type:        EventSuccess,
				Attempt:     attempt,
				MaxAttempts: options.Attempts,
				Delay:       0,
				Err:         nil,
			})
			return nil
		}
		if options.ShouldRetry != nil && !options.ShouldRetry(err) {
			emitObserver(options.Observer, Event{
				Type:        EventNonRetryable,
				Attempt:     attempt,
				MaxAttempts: options.Attempts,
				Delay:       0,
				Err:         err,
			})
			return errorsx.Wrapf(err, op, KindNonRetryable, "attempt=%d", attempt)
		}
		lastErr = err

		if attempt == options.Attempts {
			break
		}

		// 等待阶段：jitter 后再次限制 MaxDelay，保证字段契约约束实际等待值。
		wait := limitDelay(applyJitter(delay, options.JitterFraction), options.MaxDelay)
		emitObserver(options.Observer, Event{
			Type:        EventRetrying,
			Attempt:     attempt,
			MaxAttempts: options.Attempts,
			Delay:       wait,
			Err:         err,
		})
		if err := waitDelay(ctx, wait); err != nil {
			emitObserver(options.Observer, Event{
				Type:        EventContextDone,
				Attempt:     attempt,
				MaxAttempts: options.Attempts,
				Delay:       0,
				Err:         err,
			})
			return errorsx.Wrap(err, op, KindContextDone)
		}

		// 退避阶段：倍率运算采用饱和语义，避免 duration 环绕为负数或短等待。
		delay = nextDelay(delay, options.Multiplier, options.MaxDelay)
	}
	emitObserver(options.Observer, Event{
		Type:        EventExhausted,
		Attempt:     options.Attempts,
		MaxAttempts: options.Attempts,
		Delay:       0,
		Err:         lastErr,
	})
	exhausted := &ExhaustedError{Attempts: options.Attempts, LastErr: lastErr}
	return errorsx.Wrap(exhausted, op, KindExhausted)
}

// DoValue 按配置执行重试并返回业务值。
func DoValue[T any](ctx context.Context, options Options, operation func(attempt int) (T, error)) (T, error) {
	// 前置阶段：nil operation 在泛型闭包创建前返回准确 Op。
	var zeroValue T
	if operation == nil {
		return zeroValue, errorsx.New("retryx.DoValue", KindInvalidOperation)
	}

	var value T
	// 执行阶段：只在 operation 成功时保存结果；任何终态错误都返回 T 的零值。
	err := do(ctx, options, func(attempt int) error {
		current, err := operation(attempt)
		if err != nil {
			return err
		}
		value = current
		return nil
	}, "retryx.DoValue")
	if err != nil {
		return zeroValue, err
	}
	return value, nil
}

// validateOptions 校验重试状态机启动前必须满足的配置契约。
// 阶段角色：在 operation 执行前 fail-fast，避免半程才暴露配置错误。
func validateOptions(options Options, op string) error {
	// 基础字段阶段：次数和 delay 必须能直接驱动有限状态机。
	if options.Attempts <= 0 {
		return errorsx.Newf(op, KindInvalidOptions, "field=attempts value=%d", options.Attempts)
	}
	if options.InitialDelay < 0 || options.MaxDelay < 0 {
		return errorsx.Newf(
			op,
			KindInvalidOptions,
			"field=delay initial=%s max=%s",
			options.InitialDelay,
			options.MaxDelay,
		)
	}
	if math.IsNaN(options.Multiplier) || math.IsInf(options.Multiplier, 0) || options.Multiplier < 1 {
		return errorsx.Newf(op, KindInvalidOptions, "field=multiplier value=%v", options.Multiplier)
	}
	if math.IsNaN(options.JitterFraction) || math.IsInf(options.JitterFraction, 0) ||
		options.JitterFraction < 0 || options.JitterFraction > 1 {
		return errorsx.Newf(op, KindInvalidOptions, "field=jitter_fraction value=%v", options.JitterFraction)
	}
	return nil
}

// applyJitter 在基础延迟上应用对称随机抖动，并对 duration 上界采用饱和语义。
func applyJitter(delay time.Duration, jitterFraction float64) time.Duration {
	if delay <= 0 || jitterFraction == 0 {
		return delay
	}
	factor := 1 + (rand.Float64()*2-1)*jitterFraction
	jittered := float64(delay) * factor
	if jittered <= 0 {
		return 0
	}
	if jittered >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(jittered)
}

// limitDelay 把实际等待限制在 MaxDelay 内；maxDelay=0 表示只受 time.Duration 上界约束。
func limitDelay(delay, maxDelay time.Duration) time.Duration {
	if maxDelay > 0 && delay > maxDelay {
		return maxDelay
	}
	return delay
}

// nextDelay 计算下一轮基础退避，并在浮点乘法越界时采用饱和值。
// 阶段角色：阻止极端倍率把 duration 环绕为负数，破坏重试节奏。
func nextDelay(delay time.Duration, multiplier float64, maxDelay time.Duration) time.Duration {
	next := float64(delay) * multiplier
	if maxDelay > 0 && next >= float64(maxDelay) {
		return maxDelay
	}
	if next >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(next)
}

// waitDelay 等待指定时长，并在提供 context 时允许取消等待。
// 阶段角色：隔离 time.Sleep 与 timer 分支，使主状态机只处理终态映射。
func waitDelay(ctx context.Context, delay time.Duration) error {
	if ctx == nil {
		time.Sleep(delay)
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsExhausted 判断错误是否为重试耗尽。
func IsExhausted(err error) bool {
	var exhaustedError *ExhaustedError
	return errors.As(err, &exhaustedError)
}

// ShouldRetryByError 根据错误本身语义判断是否应重试。
// 语义:
// - 仅当错误实现 Retryable() 且返回 true 时可重试；
// - 未实现 Retryable() 的错误一律不重试。
func ShouldRetryByError(err error) bool {
	if err == nil {
		return false
	}

	var retryableError Retryable
	if errors.As(err, &retryableError) {
		return retryableError.Retryable()
	}
	return false
}

// emitObserver 在调用 goroutine 内同步发送事件；nil observer 表示关闭观测。
func emitObserver(observer func(event Event), event Event) {
	if observer == nil {
		return
	}
	observer(event)
}
