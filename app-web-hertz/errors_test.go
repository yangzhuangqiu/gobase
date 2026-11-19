package appwebhertz

import "testing"

// TestErrorCodesUseStableHierarchicalNames 验证应用装配错误码按 domain.stage.result 分层，避免发布后再调整字符串合同。
// 测试方法：枚举全部公共错误码及期望文本，逐项断言值相等并确保没有遗漏当前公开分类。
func TestErrorCodesUseStableHierarchicalNames(t *testing.T) {
	tests := map[string]ErrorCode{
		"app_web_hertz.config.invalid":              ErrCodeConfigInvalid,
		"app_web_hertz.config.load_failed":          ErrCodeConfigLoadFailed,
		"app_web_hertz.declaration.invalid":         ErrCodeDeclarationInvalid,
		"app_web_hertz.route.register_failed":       ErrCodeRouteRegistrationFailed,
		"app_web_hertz.middleware.execution_failed": ErrCodeMiddlewareExecutionFailed,
		"app_web_hertz.server.build_failed":         ErrCodeServerBuildFailed,
		"app_web_hertz.module.register_failed":      ErrCodeModuleRegisterFailed,
		"app_web_hertz.server.state_invalid":        ErrCodeServerStateInvalid,
		"app_web_hertz.server.resolve_failed":       ErrCodeServerResolveFailed,
	}
	for expected, actual := range tests {
		if string(actual) != expected {
			t.Fatalf("unexpected error code: actual=%s expected=%s", actual, expected)
		}
	}
}
