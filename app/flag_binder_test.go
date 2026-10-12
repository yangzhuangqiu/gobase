package app

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// commandFlagLifecycleBinder 提供可观察的 Flag 补全与校验阶段。
type commandFlagLifecycleBinder struct {
	// Trace 记录 Complete 与 ValidateFlags 的执行顺序。
	Trace *[]string
	// CompleteErr 是 Complete 阶段返回的测试错误。
	CompleteErr error
	// ValidateErr 是 ValidateFlags 阶段返回的测试错误。
	ValidateErr error
}

// Bind 实现 FlagBinder；本用例只验证解析后的补全与校验阶段，因此不声明具体 Flag。
func (this *commandFlagLifecycleBinder) Bind(CommandFlags) {}

// Complete 记录 Flag 补全阶段并返回预设错误。
func (this *commandFlagLifecycleBinder) Complete() error {
	*this.Trace = append(*this.Trace, "complete")
	return this.CompleteErr
}

// ValidateFlags 记录 Flag 校验阶段并返回预设错误集合。
func (this *commandFlagLifecycleBinder) ValidateFlags() []error {
	*this.Trace = append(*this.Trace, "validate")
	return []error{this.ValidateErr}
}

// TestUseCaseFlagBinderCompletesBeforeValidation 验证 FlagBinder 的可选扩展按补全后校验的顺序执行并聚合错误。
//
// 测试方法：
// 1. 构造同时实现 FlagCompleter 与 FlagValidator 的 binder；
// 2. 让两个阶段分别返回可识别错误；
// 3. 断言执行顺序以及 complete_flags、validate_flags 两个错误阶段均被保留。
func TestUseCaseFlagBinderCompletesBeforeValidation(t *testing.T) {
	trace := make([]string, 0, 2)
	completeErr := errors.New("complete failed")
	validateErr := errors.New("validate failed")
	binder := &commandFlagLifecycleBinder{
		Trace:       &trace,
		CompleteErr: completeErr,
		ValidateErr: validateErr,
	}

	err := New().applyFlagRules(binder)
	if !slices.Equal(trace, []string{"complete", "validate"}) {
		t.Fatalf("unexpected flag lifecycle order: %v", trace)
	}
	if !errors.Is(err, completeErr) || !errors.Is(err, validateErr) {
		t.Fatalf("expected complete and validate errors, got: %v", err)
	}
	if !strings.Contains(err.Error(), "stage=complete_flags") || !strings.Contains(err.Error(), "stage=validate_flags") {
		t.Fatalf("expected flag lifecycle stages, got: %v", err)
	}
}
