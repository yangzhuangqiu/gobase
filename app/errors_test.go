package app

import (
	"errors"
	"testing"
)

// TestErrorFactoriesCoverage 验证错误工厂方法均可返回非空错误对象。
//
// 测试方法：
// 1. 调用当前未覆盖的 errorForXxx 工厂；
// 2. 断言返回值非空，且 messagef 的空模板路径可用。
func TestErrorFactoriesCoverage(t *testing.T) {
	errs := []error{
		errorForAppRunnerRequired("runner required"),
		errorForAppCommandRunnerRequired("command runner required"),
		errorForAppCLIInvalidExpression("invalid expression"),
		errorForAppCLIEmptyKey("empty key"),
		errorForAppCLIFlagReadFailed("read failed"),
		errorForAppCLIFlagLookupFailed("lookup failed"),
		errorForAppCLIFlagTypeUnsupported("unsupported"),
	}
	for _, item := range errs {
		if item == nil {
			t.Fatal("expected non-nil error")
		}
	}
	if got := messagef(" "); got != "" {
		t.Fatalf("expected empty message, got: %q", got)
	}
	joined := errorf("stage=test reason=%w", errors.New("boom"))
	if joined == nil {
		t.Fatal("expected wrapped error")
	}
}
