package main

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
)

// TestLoadConfigUsesDefaultAndEnvironmentOverride 验证示例地址具有确定默认值并支持 trim 后覆盖。
// 测试方法：分别设置空白和显式环境变量，断言 loadConfig 返回预期 Address。
func TestLoadConfigUsesDefaultAndEnvironmentOverride(t *testing.T) {
	t.Setenv(quickstartAddressEnv, "   ")
	if actual := loadConfig().Address; actual != defaultQuickstartAddress {
		t.Fatalf("unexpected default address: %q", actual)
	}

	t.Setenv(quickstartAddressEnv, " 127.0.0.1:28080 ")
	if actual := loadConfig().Address; actual != "127.0.0.1:28080" {
		t.Fatalf("unexpected override address: %q", actual)
	}
}

// TestQuickstartRoutesReturnTeachingResponses 验证示例路由、分组和路径参数可以直接执行。
// 测试方法：使用 Hertz ut 对两个路径发起无网络请求，断言状态码和 JSON 响应。
func TestQuickstartRoutesReturnTeachingResponses(t *testing.T) {
	engine := route.NewEngine(config.NewOptions(nil))
	if err := registerRoutes(engine.Group("/")); err != nil {
		t.Fatalf("register routes: %v", err)
	}

	health := ut.PerformRequest(engine, "GET", "/healthz", nil).Result()
	if health.StatusCode() != consts.StatusOK || string(health.Body()) != `{"status":"ok"}` {
		t.Fatalf("unexpected health response: status=%d body=%s", health.StatusCode(), health.Body())
	}

	hello := ut.PerformRequest(engine, "GET", "/api/v1/hello/gobase", nil).Result()
	if hello.StatusCode() != consts.StatusOK || string(hello.Body()) != `{"message":"hello, gobase"}` {
		t.Fatalf("unexpected hello response: status=%d body=%s", hello.StatusCode(), hello.Body())
	}
}
