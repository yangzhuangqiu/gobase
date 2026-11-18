package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// commandCountingBinder 记录同一 App 顺序执行时声明 Flag 的次数。
type commandCountingBinder struct {
	// BindCount 是当前 Binder 收到独立命令 Flag 集合的次数。
	BindCount int
}

// Bind 记录一次声明阶段；测试不需要注册具体业务 Flag。
func (this *commandCountingBinder) Bind(CommandFlags) {
	this.BindCount++
}

// commandFlagLifecycleBinder 提供可观察的 Flag 补全与校验阶段。
type commandFlagLifecycleBinder struct {
	// BindCount 记录命令树声明阶段的调用次数。
	BindCount int
	// Trace 记录 Complete 与 ValidateFlags 的执行顺序。
	Trace *[]string
	// CompleteErr 是 Complete 阶段返回的测试错误。
	CompleteErr error
	// ValidateErr 是 ValidateFlags 阶段返回的测试错误。
	ValidateErr error
}

// Bind 实现 FlagBinder；本用例只验证解析后的补全与校验阶段，因此不声明具体 Flag。
func (this *commandFlagLifecycleBinder) Bind(CommandFlags) { this.BindCount++ }

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

// TestUseCaseFlagBinderDeclaresCommandTreeOnce 验证顺序复用同一 App 时缓存命令树且只调用一次 Binder。
//
// 测试方法：同一 App 使用两个 executionSession 连续执行子命令，断言 Binder 只声明一次、每次 session 均独立恢复且两次 Runner 均成功。
func TestUseCaseFlagBinderDeclaresCommandTreeOnce(t *testing.T) {
	binder := &commandCountingBinder{}
	runCount := 0
	restoreCounts := [2]int{}
	// 第一阶段：构造可重复执行的纯命令，并让 root Binder 记录命令树声明。
	application := New(
		WithFlagBinder(binder),
		WithCommands(NewCommand("doctor",
			WithoutRuntime(),
			WithWorkloads(Runner(func(context.Context, Runtime, CommandContext) error {
				runCount++
				return nil
			})),
		)),
	)
	// 第二阶段：顺序绑定不同 session，验证缓存 Cobra/Flag 集合仍可重复进入业务生命周期。
	for index := 0; index < 2; index++ {
		current := index
		session := newExecutionSession(func() { restoreCounts[current]++ })
		if err := application.executeContext(context.Background(), []string{"doctor"}, session); err != nil {
			t.Fatalf("execute repeated command: %v", err)
		}
	}
	// 第三阶段：Binder 只声明一次，但 Runner 和 session 恢复必须按执行次数发生。
	if binder.BindCount != 1 || runCount != 2 || restoreCounts != [2]int{1, 1} {
		t.Fatalf("unexpected cached command execution: bind=%d run=%d restore=%v", binder.BindCount, runCount, restoreCounts)
	}
}

// TestUseCaseFlagBinderRulesStayInSelectedNode 验证 Binder 声明次数与执行期作用域相互独立。
//
// 测试方法：root 与子命令分别声明可观察 Binder，顺序执行子命令两次；断言两个 Bind 都只发生一次，
// 但只有选中子命令的 Complete/Validate 按执行次数调用，root 规则不会泄漏到子命令。
func TestUseCaseFlagBinderRulesStayInSelectedNode(t *testing.T) {
	rootTrace := make([]string, 0)
	commandTrace := make([]string, 0, 4)
	rootBinder := &commandFlagLifecycleBinder{Trace: &rootTrace}
	commandBinder := &commandFlagLifecycleBinder{Trace: &commandTrace}
	application := New(
		WithFlagBinder(rootBinder),
		WithCommands(NewCommand("doctor",
			WithoutRuntime(),
			WithFlagBinder(commandBinder),
			WithWorkloads(Runner(func(context.Context, Runtime, CommandContext) error { return nil })),
		)),
	)
	for index := 0; index < 2; index++ {
		if err := application.ExecuteContext(context.Background(), []string{"doctor"}); err != nil {
			t.Fatalf("execute command index=%d: %v", index, err)
		}
	}
	if rootBinder.BindCount != 1 || commandBinder.BindCount != 1 || len(rootTrace) != 0 ||
		!slices.Equal(commandTrace, []string{"complete", "validate", "complete", "validate"}) {
		t.Fatalf("unexpected binder scope: root_bind=%d command_bind=%d root=%v command=%v",
			rootBinder.BindCount, commandBinder.BindCount, rootTrace, commandTrace)
	}
}
