package errorsx_test

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
	"github.com/yangzhuangqiu/gobase/util/filex"
	"github.com/yangzhuangqiu/gobase/util/retryx"
)

type usecaseRetryableError struct {
	// retryable 控制业务错误是否允许 retryx 继续尝试。
	retryable bool
}

// Error 返回固定业务错误文本，供错误链保留 cause。
func (this usecaseRetryableError) Error() string {
	return "usecase-retryable"
}

// Retryable 返回当前测试场景指定的重试决策。
func (this usecaseRetryableError) Retryable() bool {
	return this.retryable
}

// TestUseCaseReadFileBranchByKindAndStdError 验证业务层可同时按 Kind 和标准 cause 分支。
// 测试方法：读取不存在文件，断言 filex.KindIOError 与 fs.ErrNotExist。
func TestUseCaseReadFileBranchByKindAndStdError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.txt")
	_, err := filex.ReadString(path)
	if err == nil {
		t.Fatal("expected read error")
	}
	if !errorsx.IsKind(err, filex.KindIOError) {
		t.Fatalf("expected kind %s, got %v", filex.KindIOError, err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected fs.ErrNotExist in error chain: %v", err)
	}
}

// TestUseCaseRetryBranchByKind 验证 Retryable 协议与 retryx Kind 可组合用于业务短路。
// 测试方法：第一次允许重试、第二次拒绝，断言停止序号、Kind 和最终业务 cause。
func TestUseCaseRetryBranchByKind(t *testing.T) {
	// 策略阶段：第一次失败允许重试，第二次失败触发 non-retryable 终态。
	attempt := 0
	err := retryx.Do(nil, retryx.Options{
		Attempts:       3,
		InitialDelay:   0,
		MaxDelay:       0,
		Multiplier:     1,
		JitterFraction: 0,
		ShouldRetry:    retryx.ShouldRetryByError,
	}, func(currentAttempt int) error {
		attempt = currentAttempt
		if currentAttempt == 1 {
			return usecaseRetryableError{retryable: true}
		}
		return usecaseRetryableError{retryable: false}
	})
	if err == nil {
		t.Fatal("expected non-retryable error")
	}
	if attempt != 2 {
		t.Fatalf("expected stop at attempt=2, got=%d", attempt)
	}
	if !errorsx.IsKind(err, retryx.KindNonRetryable) {
		t.Fatalf("expected kind %s, got %v", retryx.KindNonRetryable, err)
	}
	var target usecaseRetryableError
	if !errors.As(err, &target) {
		t.Fatalf("expected wrapped usecaseRetryableError: %v", err)
	}
	if target.retryable {
		t.Fatalf("expected last error to be non-retryable, got=%v", target.retryable)
	}
}
