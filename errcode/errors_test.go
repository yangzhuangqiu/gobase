package errcode

import (
	stderrors "errors"
	"testing"
)

func TestNewAndCodeOf(t *testing.T) {
	err := New[StringCode]("APP.CLI.CONFLICT", "cli argument conflict")
	if !IsCode[StringCode](err, "APP.CLI.CONFLICT") {
		t.Fatalf("expected string code match")
	}
	code, ok := CodeOf[StringCode](err)
	if !ok || code != "APP.CLI.CONFLICT" {
		t.Fatalf("unexpected code: %v, ok=%v", code, ok)
	}
}

func TestWrapAndUnwrap(t *testing.T) {
	root := stderrors.New("root cause")
	err := Wrap[IntCode](root, 1001, "query user failed")
	if !IsCode[IntCode](err, 1001) {
		t.Fatalf("expected int code match")
	}
	if !stderrors.Is(err, root) {
		t.Fatalf("expected root cause in chain")
	}
}

func TestCodeOfWrongType(t *testing.T) {
	err := New[StringCode]("CFG.BOOTSTRAP.INVALID", "invalid bootstrap")
	if _, ok := CodeOf[IntCode](err); ok {
		t.Fatalf("expected different code type mismatch")
	}
}

func TestWrapf(t *testing.T) {
	root := stderrors.New("timeout")
	err := Wrapf[StringCode](root, "CFG.REMOTE.LOAD_FAILED", "load %s failed", "nacos")
	if !IsCode[StringCode](err, "CFG.REMOTE.LOAD_FAILED") {
		t.Fatalf("expected wrapped code")
	}
	if err.Error() == "" {
		t.Fatalf("expected non-empty error text")
	}
}

// TestIsCodeTraversesNestedSameType 验证外层和内层使用相同 code 类型时仍可命中内层语义。
// 测试方法：用 outer code 包装 inner coded error，分别断言两个 code 都可以通过 IsCode 分支。
func TestIsCodeTraversesNestedSameType(t *testing.T) {
	inner := New[StringCode]("inner", "inner failure")
	err := Wrap[StringCode](inner, "outer", "outer failure")
	if !IsCode[StringCode](err, "outer") {
		t.Fatal("expected outer code match")
	}
	if !IsCode[StringCode](err, "inner") {
		t.Fatal("expected nested inner code match")
	}
}

// TestIsCodeTraversesJoinedErrors 验证 errors.Join 的每个分支都保留可分支错误码。
// 测试方法：聚合两个 coded error，断言 IsCode 可以分别命中左右分支。
func TestIsCodeTraversesJoinedErrors(t *testing.T) {
	err := stderrors.Join(
		New[StringCode]("left", "left failure"),
		New[StringCode]("right", "right failure"),
	)
	if !IsCode[StringCode](err, "left") || !IsCode[StringCode](err, "right") {
		t.Fatalf("expected both joined codes, got=%v", err)
	}
}
