//go:build integration

package nacos

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAuthenticateIntegrationNacos 验证显式 token 优先及账号密码自动登录语义。
// 测试方法：分别注入已有 token 和模拟登录服务，断言跳过请求或取得临时 token，且凭据以表单传递。
func TestAuthenticateIntegrationNacos(t *testing.T) {
	// 显式 token 路径不依赖账号密码，也不应发起登录请求。
	t.Run("explicit token skips login", func(t *testing.T) {
		env := integrationNacosEnv{accessToken: "provided-token"}
		actual, err := authenticateIntegrationNacos(env)
		if err != nil {
			t.Fatalf("authenticate with explicit token failed: %v", err)
		}
		if actual.accessToken != env.accessToken {
			t.Fatalf("expected explicit token to remain unchanged")
		}
	})

	// 自动登录路径校验端点、表单凭据和临时 token 解析。
	t.Run("credentials obtain token", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodPost || request.URL.Path != "/v1/auth/users/login" {
				t.Errorf("unexpected login request: %s %s", request.Method, request.URL.Path)
			}
			if err := request.ParseForm(); err != nil {
				t.Errorf("parse login form failed: %v", err)
			}
			if request.Form.Get("username") != "integration-user" || request.Form.Get("password") != "integration-password" {
				t.Errorf("unexpected login credentials")
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"accessToken":"temporary-token","tokenTtl":18000}`))
		}))
		defer server.Close()

		env := integrationNacosEnv{httpBase: server.URL, username: "integration-user", password: "integration-password"}
		actual, err := authenticateIntegrationNacos(env)
		if err != nil {
			t.Fatalf("auto login failed: %v", err)
		}
		if actual.accessToken != "temporary-token" {
			t.Fatalf("expected temporary token from login response")
		}
	})
}

// TestRestoreIntegrationNacosConfig 验证 cleanup 对已有和不存在配置采用对应恢复策略。
// 测试方法：模拟 OpenAPI 的 GET/POST/DELETE，断言已有配置恢复原文，不存在配置删除测试写入项。
func TestRestoreIntegrationNacosConfig(t *testing.T) {
	// 原配置存在时必须发布原始文本，不能保留测试值。
	t.Run("restore existing content", func(t *testing.T) {
		var restoredContent string
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			switch request.Method {
			case http.MethodGet:
				_, _ = writer.Write([]byte("original: value\n"))
			case http.MethodPost:
				if err := request.ParseForm(); err != nil {
					t.Errorf("parse restore form failed: %v", err)
				}
				restoredContent = request.Form.Get("content")
				_, _ = writer.Write([]byte("true"))
			default:
				t.Errorf("unexpected restore method: %s", request.Method)
			}
		}))
		defer server.Close()

		env := integrationNacosEnv{httpBase: server.URL, groupID: "g", dataID: "d", accessToken: "token"}
		backup, err := loadIntegrationNacosConfig(env)
		if err != nil {
			t.Fatalf("backup existing config failed: %v", err)
		}
		if err = restoreIntegrationNacosConfig(env, backup); err != nil {
			t.Fatalf("restore existing config failed: %v", err)
		}
		if !backup.exists || restoredContent != "original: value\n" {
			t.Fatalf("expected original content to be restored")
		}
	})

	// 原配置不存在时必须删除测试创建项，恢复“不存在”状态。
	t.Run("delete newly created config", func(t *testing.T) {
		deleted := false
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			switch request.Method {
			case http.MethodGet:
				writer.WriteHeader(http.StatusNotFound)
			case http.MethodDelete:
				deleted = true
				_, _ = writer.Write([]byte("true"))
			default:
				t.Errorf("unexpected cleanup method: %s", request.Method)
			}
		}))
		defer server.Close()

		env := integrationNacosEnv{httpBase: server.URL, groupID: "g", dataID: "d"}
		backup, err := loadIntegrationNacosConfig(env)
		if err != nil {
			t.Fatalf("backup missing config failed: %v", err)
		}
		if err = restoreIntegrationNacosConfig(env, backup); err != nil {
			t.Fatalf("delete newly created config failed: %v", err)
		}
		if backup.exists || !deleted {
			t.Fatalf("expected missing config to be deleted during cleanup")
		}
	})
}
