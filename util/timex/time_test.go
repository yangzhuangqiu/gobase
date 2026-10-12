package timex

import (
	"testing"
	"time"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

// TestDefaultShanghaiFormatAndParse 验证包级 API 固定使用上海时区完成格式化和解析。
// 测试方法：构造上海固定时间，格式化后再解析并比较 Unix 时刻。
func TestDefaultShanghaiFormatAndParse(t *testing.T) {
	// 准备阶段：显式上海 location 避免测试依赖机器 time.Local。
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location error: %v", err)
	}

	now := time.Date(2026, 3, 24, 10, 20, 30, 0, loc)
	formatted := FormatDateTime(now)
	if formatted != "2026-03-24 10:20:30" {
		t.Fatalf("FormatDateTime mismatch: %s", formatted)
	}

	parsed, err := ParseDateTime(formatted)
	if err != nil {
		t.Fatalf("ParseDateTime error: %v", err)
	}
	if parsed.Unix() != now.Unix() {
		t.Fatalf("parsed unix mismatch: got=%d want=%d", parsed.Unix(), now.Unix())
	}
}

// TestDefaultShanghaiStartOfDay 验证日历边界覆盖完整纳秒范围。
// 测试方法：断言日初、下一日零点和日末分别为 00:00、次日 00:00、日末最后纳秒。
func TestDefaultShanghaiStartOfDay(t *testing.T) {
	// 日历阶段：依次验证日初、日末和下一日 exclusive 上界。
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location error: %v", err)
	}

	value := time.Date(2026, 3, 24, 18, 59, 59, 0, loc)
	start := StartOfDay(value)
	if start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 {
		t.Fatalf("unexpected start of day: %s", start)
	}

	end := EndOfDay(value)
	if end.Hour() != 23 || end.Minute() != 59 || end.Second() != 59 {
		t.Fatalf("unexpected end of day: %s", end)
	}
	if end.Nanosecond() != int(time.Second-time.Nanosecond) {
		t.Fatalf("unexpected end-of-day nanosecond: %d", end.Nanosecond())
	}
	next := StartOfNextDay(value)
	if !end.Add(time.Nanosecond).Equal(next) {
		t.Fatalf("end and next-day boundary mismatch: end=%s next=%s", end, next)
	}
}

// TestNewTimezone 验证显式 Timezone 对象使用调用方指定时区。
// 测试方法：创建东京对象，格式化并解析 UTC 时刻，断言结果 location 为 Asia/Tokyo。
func TestNewTimezone(t *testing.T) {
	// 对象阶段：东京 Timezone 应独立于包级上海默认值。
	tokyo, err := New("Asia/Tokyo")
	if err != nil {
		t.Fatalf("New timezone error: %v", err)
	}

	value := time.Date(2026, 3, 24, 21, 22, 23, 0, time.UTC)
	formatted := tokyo.FormatDateTime(value)
	parsed, err := tokyo.ParseDateTime(formatted)
	if err != nil {
		t.Fatalf("ParseDateTime error: %v", err)
	}
	if parsed.Location().String() != "Asia/Tokyo" {
		t.Fatalf("location mismatch: %s", parsed.Location().String())
	}
}

// TestNowNotZero 验证包级与对象级 Now 入口返回有效当前时间。
// 测试方法：分别调用上海默认和 nil-location 对象，断言时间与便捷文本均非零值。
func TestNowNotZero(t *testing.T) {
	// 包级与对象级阶段分别检查当前时间和三种常用文本。
	if Now().IsZero() {
		t.Fatal("Now should not be zero")
	}

	defaultTimezone := NewWithLocation(nil)
	if defaultTimezone.Now().IsZero() {
		t.Fatal("Now should not be zero")
	}
	if NowDate() == "" || NowDateTime() == "" {
		t.Fatal("NowDate/NowDateTime should not be empty")
	}
	if NowDateTimeMilli() == "" {
		t.Fatal("NowDateTimeMilli should not be empty")
	}
}

// TestParseAndFormatComposition 验证精简后的核心 API 可直接组合完成时间戳转换。
// 测试方法：解析日期和毫秒时间，再用 time.Time 的 Unix/UnixMilli 与 Format 方法完成往返。
func TestParseAndFormatComposition(t *testing.T) {
	// 秒级阶段先验证 date/datetime，随后验证毫秒往返。
	parsedDate, err := ParseDate("2024-03-24")
	if err != nil {
		t.Fatalf("ParseDate error: %v", err)
	}
	parsedDateTime, err := ParseDateTime("2024-03-24 08:00:00")
	if err != nil {
		t.Fatalf("ParseDateTime error: %v", err)
	}
	if FormatDate(time.Unix(parsedDate.Unix(), 0)) != "2024-03-24" {
		t.Fatalf("FormatDate mismatch for parsed date: %s", parsedDate)
	}
	if FormatDateTime(time.Unix(parsedDateTime.Unix(), 0)) != "2024-03-24 08:00:00" {
		t.Fatalf("FormatDateTime mismatch for parsed datetime: %s", parsedDateTime)
	}
	milli := parsedDateTime.UnixMilli()
	if FormatDateTimeMilli(time.UnixMilli(milli)) != "2024-03-24 08:00:00.000" {
		t.Fatalf("FormatDateTimeMilli mismatch for parsed datetime milli: %d", milli)
	}

	milliParsed, err := ParseDateTimeMilli("2024-03-24 08:00:00.123")
	if err != nil {
		t.Fatalf("ParseDateTimeMilli error: %v", err)
	}
	if FormatDateTimeMilli(time.UnixMilli(milliParsed.UnixMilli())) != "2024-03-24 08:00:00.123" {
		t.Fatalf("FormatDateTimeMilli mismatch for parsed milli datetime: %s", milliParsed)
	}
}

// TestIsSameDay 验证默认上海时区决定跨 UTC 时刻的日历归属。
// 测试方法：构造三个 UTC 时刻，断言上海日期下的一组同日和一组跨日。
func TestIsSameDay(t *testing.T) {
	left := time.Date(2024, 3, 24, 16, 30, 0, 0, time.UTC)
	right := time.Date(2024, 3, 24, 18, 0, 0, 0, time.UTC)
	if !IsSameDay(left, right) {
		t.Fatal("expected same day in Asia/Shanghai")
	}

	other := time.Date(2024, 3, 24, 15, 0, 0, 0, time.UTC)
	if IsSameDay(left, other) {
		t.Fatal("expected different day in Asia/Shanghai")
	}
}

// TestErrorKinds 验证无效时区和时间文本暴露稳定可分支 Kind。
// 测试方法：分别加载空时区并解析非法文本，断言 KindInvalidTimezone 与 KindParseError。
func TestErrorKinds(t *testing.T) {
	if _, err := LoadLocation(""); err == nil {
		t.Fatal("expected empty location name error")
	} else if !errorsx.IsKind(err, KindInvalidTimezone) {
		t.Fatalf("expected kind %s, got err=%v", KindInvalidTimezone, err)
	}

	if _, err := ParseDateTime("bad-format"); err == nil {
		t.Fatal("expected parse error")
	} else if !errorsx.IsKind(err, KindParseError) {
		t.Fatalf("expected kind %s, got err=%v", KindParseError, err)
	}
}
