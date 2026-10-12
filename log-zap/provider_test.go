package zap

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/slogtest"
	"time"

	"github.com/yangzhuangqiu/gobase/errcode"
	baselog "github.com/yangzhuangqiu/gobase/log"
	gozap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TestHandlerStructuredFieldsGroupErrorAndCaller 验证 Zap Handler 完整保留标准事件语义。
//
// 测试方法：写入带普通字段、Group 和 error 的事件，读取 JSON 断言字段结构与业务 caller。
func TestHandlerStructuredFieldsGroupErrorAndCaller(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "structured.log")
	provider := newFileProviderForTest(t, logFile, "debug")
	logger := provider.Factory().Get("order.service")
	logger.Error(
		context.Background(),
		"order persistence failed",
		baselog.Op("order.persist"),
		baselog.Result(baselog.ResultFailed),
		baselog.Group("order", baselog.F("id", "o-1001")),
		baselog.Err(errors.New("db down")),
	)
	if err := provider.Close(); err != nil {
		t.Fatalf("close provider failed: %v", err)
	}

	event := readSingleJSONEvent(t, logFile)
	if event["logger"] != "order.service" || event["op"] != "order.persist" || event["result"] != "failed" {
		t.Fatalf("unexpected root fields: %+v", event)
	}
	order, ok := event["order"].(map[string]any)
	if !ok || order["id"] != "o-1001" {
		t.Fatalf("unexpected group: %+v", event["order"])
	}
	if event["error"] != "db down" {
		t.Fatalf("unexpected error field: %+v", event["error"])
	}
	caller, _ := event["caller"].(string)
	if !strings.Contains(caller, "provider_test.go") {
		t.Fatalf("caller should point to business test, got=%s", caller)
	}
}

// TestProviderRedactsFieldsAndProtectsEnvelope 验证 Zap Provider 应用统一脱敏与顶层 schema 规则。
//
// 测试方法：配置键脱敏器，写入固定/嵌套敏感字段、重复 op 和伪造 envelope，读取 JSON 断言安全值与 canonical 字段。
func TestProviderRedactsFieldsAndProtectsEnvelope(t *testing.T) {
	// 第一步：创建带键脱敏策略的真实文件 Provider，并写入冲突与敏感字段。
	logFile := filepath.Join(t.TempDir(), "redaction-schema.log")
	provider := newFileProviderForTest(t, logFile, "info",
		WithRedactor(baselog.NewKeyRedactor("password", "auth.token")),
	)
	logger := provider.Factory().Get("secure.service").With(baselog.F("password", "plain-password"))
	logger.Info(context.Background(), "secure event",
		baselog.Op("secure.write"),
		baselog.Op("forged.operation"),
		baselog.Result(baselog.ResultOK),
		baselog.Group("auth", baselog.F("token", "plain-token")),
		baselog.Group("payload", baselog.F("level", "business"), baselog.F("level", "forged-business")),
		baselog.F("level", "FORGED"),
		baselog.F("msg", "forged message"),
		baselog.F("logger", "forged.logger"),
	)
	if err := provider.Close(); err != nil {
		t.Fatalf("close provider failed: %v", err)
	}

	// 第二步：从最终 JSON 验证脱敏、来源优先级、envelope 与嵌套 Group 规则。
	event := readSingleJSONEvent(t, logFile)
	if event["password"] != baselog.RedactedValue || event["op"] != "secure.write" {
		t.Fatalf("unexpected secured fields: %+v", event)
	}
	if event["level"] != "INFO" || event["msg"] != "secure event" || event["logger"] != "secure.service" {
		t.Fatalf("event envelope was forged: %+v", event)
	}
	auth, ok := event["auth"].(map[string]any)
	if !ok || auth["token"] != baselog.RedactedValue {
		t.Fatalf("nested sensitive field leaked: %+v", event["auth"])
	}
	payload, ok := event["payload"].(map[string]any)
	if !ok || payload["level"] != "business" {
		t.Fatalf("nested envelope-like field should be preserved: %+v", event["payload"])
	}
}

// TestProviderApplyRuntimeConfigAffectsExistingLogger 验证纯 Provider 可由外部适配层通过窄配置更新既有 Logger。
//
// 测试方法：以 error 创建文件 Provider，调用 ApplyRuntimeConfig 切换到 debug，复用同一 Logger 并断言仅更新后的 Debug 落盘。
func TestProviderApplyRuntimeConfigAffectsExistingLogger(t *testing.T) {
	// 第一步：创建不依赖 watcher 或 ConfigManager 的纯日志核心。
	logFile := filepath.Join(t.TempDir(), "runtime-config.log")
	provider := newFileProviderForTest(t, logFile, "error")
	logger := provider.Factory().Get("runtime.config")
	logger.Debug(context.Background(), "debug before update")

	// 第二步：通过窄配置更新动态级别，并复用构造期 Logger。
	if err := provider.ApplyRuntimeConfig(RuntimeConfig{Level: "debug"}); err != nil {
		t.Fatalf("apply runtime config failed: %v", err)
	}
	logger.Debug(context.Background(), "debug after update")
	if err := provider.Close(); err != nil {
		t.Fatalf("close provider failed: %v", err)
	}

	// 第三步：验证更新只影响后续事件，且不需要重建 LoggerFactory/Logger。
	raw, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read runtime config log failed: %v", err)
	}
	content := string(raw)
	if strings.Contains(content, "debug before update") || !strings.Contains(content, "debug after update") {
		t.Fatalf("unexpected runtime config output: %s", content)
	}
}

// TestProviderApplyRuntimeConfigRejectsInvalidLevel 验证非法动态级别不会污染当前运行状态。
//
// 测试方法：记录更新前 AtomicLevel，应用非法值并断言错误码与更新后级别保持不变。
func TestProviderApplyRuntimeConfigRejectsInvalidLevel(t *testing.T) {
	// 先固化合法运行状态，再验证非法候选不会越过解析边界。
	provider, err := NewProvider(Config{Level: "warn"})
	if err != nil {
		t.Fatalf("new provider failed: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	before := provider.level.Level()
	err = provider.ApplyRuntimeConfig(RuntimeConfig{Level: "verbose"})
	if !errcode.IsCode[ErrorCode](err, ErrCodeLogLevelInvalid) {
		t.Fatalf("expected invalid level error, got=%v", err)
	}
	if after := provider.level.Level(); after != before {
		t.Fatalf("invalid runtime config changed level: before=%v after=%v", before, after)
	}
}

// TestFileModeValidation 验证 file 模式缺少 filename 会失败。
//
// 测试方法：以不完整 file 配置创建 Provider，断言稳定错误码。
func TestFileModeValidation(t *testing.T) {
	_, err := NewProvider(Config{Mode: ModeFile, Format: FormatJSON})
	if !errcode.IsCode[ErrorCode](err, ErrCodeLogFileFilenameRequired) {
		t.Fatalf("expected filename required, got=%v", err)
	}
}

// TestProviderCloseIdempotentAndReleasesFile 验证 Close 幂等并释放文件句柄。
//
// 测试方法：写入文件后连续 Close 两次，随后立即删除文件。
func TestProviderCloseIdempotentAndReleasesFile(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "release.log")
	provider := newFileProviderForTest(t, logFile, "info")
	provider.Factory().Get("release.test").Info(context.Background(), "release writer")
	if err := provider.Close(); err != nil {
		t.Fatalf("first close failed: %v", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("second close failed: %v", err)
	}
	if err := os.Remove(logFile); err != nil && !os.IsNotExist(err) {
		t.Fatalf("file handle not released: %v", err)
	}
}

// TestProviderFactoryStableAndDisabledAfterClose 验证 Provider 拥有稳定 Factory，且关闭会禁用既有日志入口。
//
// 测试方法：重复获取 Factory 并比较实例；关闭 Provider 后复用关闭前取得的 Logger 写入，断言文件中不存在关闭后事件。
func TestProviderFactoryStableAndDisabledAfterClose(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "factory-lifecycle.log")
	provider := newFileProviderForTest(t, logFile, "info")
	firstFactory := provider.Factory()
	secondFactory := provider.Factory()
	if firstFactory == nil || firstFactory != secondFactory {
		t.Fatalf("provider factory should be stable: first=%p second=%p", firstFactory, secondFactory)
	}
	logger := firstFactory.Get("factory.lifecycle")
	logger.Info(context.Background(), "before close")
	if err := provider.Close(); err != nil {
		t.Fatalf("close provider failed: %v", err)
	}
	logger.Info(context.Background(), "after close")

	raw, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read lifecycle log failed: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "before close") || strings.Contains(content, "after close") {
		t.Fatalf("unexpected provider lifecycle output: %s", content)
	}
}

// TestProviderIgnoreRuntimeConfigAfterClose 验证关闭后显式动态配置不会改变 level。
//
// 测试方法：关闭 Provider 后调用 ApplyRuntimeConfig，断言原子 level 保持不变。
func TestProviderIgnoreRuntimeConfigAfterClose(t *testing.T) {
	provider, err := NewProvider(Config{Level: "error"})
	if err != nil {
		t.Fatalf("new provider failed: %v", err)
	}
	before := provider.level.Level()
	if err = provider.Close(); err != nil {
		t.Fatalf("close provider failed: %v", err)
	}
	if err = provider.ApplyRuntimeConfig(RuntimeConfig{Level: "debug"}); err != nil {
		t.Fatalf("apply runtime config after close failed: %v", err)
	}
	if after := provider.level.Level(); after != before {
		t.Fatalf("level changed after close: before=%v after=%v", before, after)
	}
}

// TestZapHandlerSlogContract 使用标准 slogtest 验证 Handler 的 Attr/Group 派生语义。
//
// 测试方法：使用内存 JSON core 运行 slogtest，并把每行输出解码为结果 map。
func TestZapHandlerSlogContract(t *testing.T) {
	buffer := &lockedBuffer{}
	atomicLevel := gozap.NewAtomicLevelAt(zapcore.DebugLevel)
	core := zapcore.NewCore(buildZapEncoder(FormatJSON), zapcore.AddSync(buffer), atomicLevel)
	provider := &Provider{level: atomicLevel}
	provider.runtime.Store(&providerRuntime{base: gozap.New(core), addSource: false})
	if err := slogtest.TestHandler(newHandler(provider), func() []map[string]any {
		return decodeJSONLines(t, buffer.String())
	}); err != nil {
		t.Fatalf("slog handler contract failed: %v", err)
	}
}

// TestZapHandlerHonorsCoreSamplingCheck 验证 Handler 通过 Core.Check 执行 Zap 采样语义。
//
// 测试方法：使用每周期仅保留首条的 sampler core 连续写入同消息三次，断言最终只有一条 JSON 事件。
func TestZapHandlerHonorsCoreSamplingCheck(t *testing.T) {
	// 第一步：构造每周期只保留首条事件的 sampler core。
	buffer := &lockedBuffer{}
	baseCore := zapcore.NewCore(buildZapEncoder(FormatJSON), zapcore.AddSync(buffer), zapcore.DebugLevel)
	sampledCore := zapcore.NewSamplerWithOptions(baseCore, time.Hour, 1, 0)
	provider := &Provider{}
	provider.runtime.Store(&providerRuntime{base: gozap.New(sampledCore), addSource: false})
	handler := newHandler(provider)
	for index := 0; index < 3; index++ {
		record := slog.NewRecord(time.Now(), slog.LevelInfo, "sampled event", 0)
		if err := handler.Handle(context.Background(), record); err != nil {
			t.Fatalf("handle sampled event failed: %v", err)
		}
	}
	// 第二步：解码最终输出并断言采样判定发生在实际写入前。
	events := decodeJSONLines(t, buffer.String())
	if len(events) != 1 {
		t.Fatalf("unexpected sampled event count: got=%d want=1 events=%+v", len(events), events)
	}
}

// lockedBuffer 为 slogtest 提供并发安全的内存 writer。
type lockedBuffer struct {
	data strings.Builder
}

// Write 实现 io.Writer；slogtest 当前串行写入，因此 Builder 无需额外锁。
func (this *lockedBuffer) Write(input []byte) (int, error) {
	return this.data.Write(input)
}

// String 返回当前输出文本。
func (this *lockedBuffer) String() string {
	return this.data.String()
}

// Sync 实现 zapcore.WriteSyncer。
func (this *lockedBuffer) Sync() error {
	return nil
}

// newFileProviderForTest 创建文件模式 Provider 并注册失败清理。
func newFileProviderForTest(t *testing.T, filename string, level string, options ...Option) *Provider {
	t.Helper()
	provider, err := NewProvider(Config{
		Mode: ModeFile, Format: FormatJSON, Level: level,
		Rotate: RotateConfig{Filename: filename},
	}, options...)
	if err != nil {
		t.Fatalf("new provider failed: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	return provider
}

// readSingleJSONEvent 读取唯一 JSON 日志事件。
func readSingleJSONEvent(t *testing.T, filename string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read log failed: %v", err)
	}
	events := decodeJSONLines(t, string(raw))
	if len(events) != 1 {
		t.Fatalf("expected one event, got=%d content=%s", len(events), raw)
	}
	return events[0]
}

// decodeJSONLines 解码 Zap JSON 行输出。
func decodeJSONLines(t *testing.T, raw string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	result := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		item := map[string]any{}
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			t.Fatalf("decode log line failed: line=%s err=%v", line, err)
		}
		// Zap 使用 ts 作为固定时间键；结果适配为 slogtest 约定的 time。
		if timestamp, exists := item["ts"]; exists {
			delete(item, "ts")
			if text, ok := timestamp.(string); !ok || !strings.HasPrefix(text, "0001-") {
				item[slog.TimeKey] = timestamp
			}
		}
		result = append(result, item)
	}
	return result
}

var _ slog.Handler = (*handler)(nil)
