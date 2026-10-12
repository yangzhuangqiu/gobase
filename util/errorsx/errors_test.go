package errorsx

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestIsKind 验证普通单链中的外层与内层 Kind 均可稳定匹配。
// 测试方法：构造两层 errorsx.Wrap 链，分别断言目标命中和非目标不命中。
func TestIsKind(t *testing.T) {
	baseErr := errors.New("io")
	wrapped := Wrapf(baseErr, "filex.Read", "filex.open_error", "path=%s", "/tmp/a")
	if !IsKind(wrapped, "filex.open_error") {
		t.Fatal("expected kind match")
	}
	if IsKind(wrapped, "filex.write_error") {
		t.Fatal("unexpected kind match")
	}

	outer := Wrap(wrapped, "retryx.Do", "retryx.non_retryable")
	if !IsKind(outer, "filex.open_error") {
		t.Fatal("expected nested inner kind match")
	}
}

// TestIsKindTraversesJoinedSiblingBranches 验证目标 Kind 位于 errors.Join 非首分支时仍能命中。
// 测试方法：把不匹配错误放在首分支、目标错误放在第二分支，断言 IsKind 返回 true。
func TestIsKindTraversesJoinedSiblingBranches(t *testing.T) {
	joined := errors.Join(
		New("left", "kind.left"),
		New("right", "kind.right"),
	)
	if !IsKind(joined, "kind.right") {
		t.Fatal("expected kind in second joined branch to match")
	}
	if IsKind(joined, "kind.missing") {
		t.Fatal("unexpected missing kind match")
	}
}

// TestIsKindIgnoresJoinedBranchOrder 验证 Join 分支顺序不改变 Kind 匹配结果。
// 测试方法：交换左右错误顺序，断言两种排列都能命中同一目标 Kind。
func TestIsKindIgnoresJoinedBranchOrder(t *testing.T) {
	target := New("target", "kind.target")
	other := New("other", "kind.other")
	for index, joined := range []error{errors.Join(target, other), errors.Join(other, target)} {
		if !IsKind(joined, "kind.target") {
			t.Fatalf("expected target kind match for joined order index=%d", index)
		}
	}
}

// TestIsKindTraversesNestedJoinedErrors 验证标准单链包装后的嵌套 Join 仍会遍历全部分支。
// 测试方法：用 fmt.Errorf 包装内层 Join，把目标放在内层第二分支并断言命中。
func TestIsKindTraversesNestedJoinedErrors(t *testing.T) {
	nested := fmt.Errorf("outer: %w", errors.Join(
		New("nested-left", "kind.nested_left"),
		New("nested-right", "kind.nested_right"),
	))
	if !IsKind(nested, "kind.nested_right") {
		t.Fatal("expected kind in nested joined branch to match")
	}
}

// TestKindOfAndOpOf 验证提取函数返回错误链中的首个 Kind 和 Op。
// 测试方法：创建带 detail 的错误并分别断言 KindOf、OpOf 的值与命中状态。
func TestKindOfAndOpOf(t *testing.T) {
	err := Newf("randx.IntNByCrypto", "randx.invalid_max", "max=%d", 0)
	kind, ok := KindOf(err)
	if !ok || kind != "randx.invalid_max" {
		t.Fatalf("unexpected kind: %v %v", kind, ok)
	}
	op, ok := OpOf(err)
	if !ok || op != "randx.IntNByCrypto" {
		t.Fatalf("unexpected op: %s %v", op, ok)
	}
}

// TestWrapNilReturnsNil 验证 nil cause 不会被包装成伪错误。
// 测试方法：分别调用 Wrap 与 Wrapf，断言结果均为 nil。
func TestWrapNilReturnsNil(t *testing.T) {
	if Wrap(nil, "x.y", "x.z") != nil {
		t.Fatal("Wrap(nil, ...) should return nil")
	}
	if Wrapf(nil, "x.y", "x.z", "k=%d", 1) != nil {
		t.Fatal("Wrapf(nil, ...) should return nil")
	}
}

// TestKindOfAndOpOfMiss 验证普通错误不暴露 errorsx 元数据。
// 测试方法：传入标准库 error，断言 KindOf 和 OpOf 均返回未命中。
func TestKindOfAndOpOfMiss(t *testing.T) {
	plain := errors.New("plain")
	if kind, ok := KindOf(plain); ok || kind != "" {
		t.Fatalf("unexpected kind: %v %v", kind, ok)
	}
	if op, ok := OpOf(plain); ok || op != "" {
		t.Fatalf("unexpected op: %q %v", op, ok)
	}
}

// TestErrorStringFallback 验证空 Op/Kind 的日志文本使用稳定回退值。
// 测试方法：构造空字段 Error，断言文本包含 unknown op 和 unknown kind。
func TestErrorStringFallback(t *testing.T) {
	err := &Error{Op: "", Kind: "", Detail: "x=1"}
	message := err.Error()
	if !strings.Contains(message, "op=unknown") {
		t.Fatalf("missing unknown op: %s", message)
	}
	if !strings.Contains(message, "kind=unknown.unknown") {
		t.Fatalf("missing unknown kind: %s", message)
	}
}

// TestNilErrorString 验证 typed nil errorsx.Error 不会在日志格式化时 panic。
// 测试方法：直接调用 nil receiver 的 Error，断言返回稳定 unknown 文本。
func TestNilErrorString(t *testing.T) {
	var typed *Error
	message := typed.Error()
	if !strings.Contains(message, "op=unknown") || !strings.Contains(message, "err=nil") {
		t.Fatalf("unexpected nil error text: %s", message)
	}
}
