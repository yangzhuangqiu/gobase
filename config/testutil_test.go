package config

import (
	"testing"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// assertErrorContains 断言错误文本包含期望片段（统一失败信息格式）。
func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error to contain %q, got nil", want)
	}
	if !containsToken(err.Error(), want) {
		t.Fatalf("expected error to contain %q, got: %v", want, err)
	}
}

// assertErrorToken 断言错误文本中包含 key=value 语义标记。
func assertErrorToken(t *testing.T, err error, key, value string) {
	t.Helper()
	assertErrorContains(t, err, key+"="+value)
}

// assertErrorCode 断言错误链中包含指定错误码。
func assertErrorCode[T comparable](t *testing.T, err error, code T) {
	t.Helper()
	if !errcode.IsCode[T](err, code) {
		t.Fatalf("expected error code %v, got: %v", code, err)
	}
}

// containsToken 使用统一入口封装字符串匹配，避免测试散落直接 Contains。
func containsToken(message, want string) bool {
	return len(want) == 0 || (len(message) >= len(want) && indexOf(message, want) >= 0)
}

// indexOf 返回子串首次出现位置，不存在时返回 -1。
func indexOf(message, want string) int {
	for i := 0; i+len(want) <= len(message); i++ {
		if message[i:i+len(want)] == want {
			return i
		}
	}
	return -1
}
