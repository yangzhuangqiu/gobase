package zap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	baselog "github.com/yangzhuangqiu/gobase/log"
)

// TestUserUsecaseLogZap 验证业务通过根门面写入 Zap provider 的最小链路。
//
// 测试方法：装配 typed ContextExtractor，以显式名称记录事件并断言 name/context/业务字段。
func TestUserUsecaseLogZap(t *testing.T) {
	type requestIDKey struct{}
	logFile := filepath.Join(t.TempDir(), "usecase.log")
	provider := newFileProviderForTest(t, logFile, "info", WithContextExtractors(
		func(ctx context.Context) []baselog.Field {
			requestID, _ := ctx.Value(requestIDKey{}).(string)
			return []baselog.Field{baselog.F("request_id", requestID)}
		},
	))
	factory := provider.Factory()
	ctx := context.WithValue(context.Background(), requestIDKey{}, "rid-usecase-1")
	factory.Get("auth.login").Info(
		ctx,
		"user login succeeded",
		baselog.Op("login"),
		baselog.Result(baselog.ResultOK),
	)
	if err := provider.Close(); err != nil {
		t.Fatalf("close provider failed: %v", err)
	}
	raw, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log failed: %v", err)
	}
	content := string(raw)
	for _, expected := range []string{"user login succeeded", "rid-usecase-1", "auth.login", "\"result\":\"ok\""} {
		if !strings.Contains(content, expected) {
			t.Fatalf("expected %q in output: %s", expected, content)
		}
	}
}
