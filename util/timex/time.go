package timex

import (
	"time"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

const (
	// DateLayout 是日期布局（yyyy-MM-dd）。
	DateLayout = "2006-01-02"
	// DateTimeLayout 是日期时间布局（yyyy-MM-dd HH:mm:ss）。
	DateTimeLayout = "2006-01-02 15:04:05"
	// DateTimeMilliLayout 是毫秒级日期时间布局（yyyy-MM-dd HH:mm:ss.SSS）。
	DateTimeMilliLayout = "2006-01-02 15:04:05.000"
	// ShanghaiTimezoneName 是默认时区名称。
	ShanghaiTimezoneName = "Asia/Shanghai"
)

// defaultShanghai 是包级便捷 API 共享的只读上海时区处理对象。
var defaultShanghai = NewWithLocation(mustLoadShanghai())

// Timezone 表示“基于指定时区”的时间处理对象。
type Timezone struct {
	// location 是所有格式化、解析和日历边界计算使用的固定时区。
	location *time.Location
}

// New 创建一个指定时区的时间处理对象。
func New(name string) (*Timezone, error) {
	location, err := LoadLocation(name)
	if err != nil {
		return nil, err
	}
	return NewWithLocation(location), nil
}

// NewWithLocation 使用 location 创建时间处理对象。
// 参数: location 为空时回退到系统时区。
func NewWithLocation(location *time.Location) *Timezone {
	return &Timezone{location: resolveLocation(location)}
}

// Location 返回对象绑定的时区。
func (this *Timezone) Location() *time.Location {
	if this == nil {
		return resolveLocation(nil)
	}
	return resolveLocation(this.location)
}

// LoadLocation 加载时区。
func LoadLocation(name string) (*time.Location, error) {
	if name == "" {
		return nil, errorsx.New("timex.LoadLocation", KindInvalidTimezone)
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, errorsx.Wrapf(err, "timex.LoadLocation", KindInvalidTimezone, "name=%s", name)
	}
	return location, nil
}

// ShanghaiLocation 返回默认上海时区对象。
func ShanghaiLocation() *time.Location {
	return defaultShanghai.Location()
}

// Now 返回当前上海时区时间。
func Now() time.Time {
	return defaultShanghai.Now()
}

// NowDate 返回当前上海时区日期字符串（yyyy-MM-dd）。
func NowDate() string {
	return FormatDate(Now())
}

// NowDateTime 返回当前上海时区日期时间字符串（yyyy-MM-dd HH:mm:ss）。
func NowDateTime() string {
	return FormatDateTime(Now())
}

// NowDateTimeMilli 返回当前上海时区毫秒级日期时间字符串（yyyy-MM-dd HH:mm:ss.SSS）。
func NowDateTimeMilli() string {
	return FormatDateTimeMilli(Now())
}

// FormatDate 以默认上海时区格式化日期。
func FormatDate(timeValue time.Time) string {
	return defaultShanghai.FormatDate(timeValue)
}

// FormatDateTime 以默认上海时区格式化日期时间。
func FormatDateTime(timeValue time.Time) string {
	return defaultShanghai.FormatDateTime(timeValue)
}

// FormatDateTimeMilli 以默认上海时区格式化毫秒级日期时间。
func FormatDateTimeMilli(timeValue time.Time) string {
	return defaultShanghai.FormatDateTimeMilli(timeValue)
}

// Parse 按布局与默认上海时区解析时间。
func Parse(layout, raw string) (time.Time, error) {
	return defaultShanghai.Parse(layout, raw)
}

// ParseDate 按默认日期布局与默认上海时区解析时间。
func ParseDate(raw string) (time.Time, error) {
	return defaultShanghai.Parse(DateLayout, raw)
}

// ParseDateTime 按默认日期时间布局与默认上海时区解析时间。
func ParseDateTime(raw string) (time.Time, error) {
	return defaultShanghai.Parse(DateTimeLayout, raw)
}

// ParseDateTimeMilli 按默认毫秒日期时间布局与默认上海时区解析时间。
func ParseDateTimeMilli(raw string) (time.Time, error) {
	return defaultShanghai.Parse(DateTimeMilliLayout, raw)
}

// StartOfDay 返回 timeValue 在默认上海时区下当天 00:00:00。
func StartOfDay(timeValue time.Time) time.Time {
	return defaultShanghai.StartOfDay(timeValue)
}

// StartOfNextDay 返回 timeValue 在默认上海时区下下一天的 00:00:00。
// 典型调用场景：构造 [StartOfDay, StartOfNextDay) 的无损日范围。
func StartOfNextDay(timeValue time.Time) time.Time {
	return defaultShanghai.StartOfNextDay(timeValue)
}

// EndOfDay 返回 timeValue 在默认上海时区下当天最后一个纳秒。
func EndOfDay(timeValue time.Time) time.Time {
	return defaultShanghai.EndOfDay(timeValue)
}

// IsSameDay 判断两个时间在默认上海时区下是否为同一天。
func IsSameDay(a, b time.Time) bool {
	return defaultShanghai.IsSameDay(a, b)
}

// Now 返回当前时刻，并归一到对象绑定时区。
func (this *Timezone) Now() time.Time {
	return time.Now().In(this.Location())
}

// Format 按对象绑定时区和指定布局格式化时间。
func (this *Timezone) Format(timeValue time.Time, layout string) string {
	return timeValue.In(this.Location()).Format(layout)
}

// FormatDate 以对象绑定时区格式化日期。
func (this *Timezone) FormatDate(timeValue time.Time) string {
	return this.Format(timeValue, DateLayout)
}

// FormatDateTime 以对象绑定时区格式化日期时间。
func (this *Timezone) FormatDateTime(timeValue time.Time) string {
	return this.Format(timeValue, DateTimeLayout)
}

// FormatDateTimeMilli 以对象绑定时区格式化毫秒级日期时间。
func (this *Timezone) FormatDateTimeMilli(timeValue time.Time) string {
	return this.Format(timeValue, DateTimeMilliLayout)
}

// Parse 按布局与对象绑定时区解析时间。
func (this *Timezone) Parse(layout, raw string) (time.Time, error) {
	parsed, err := time.ParseInLocation(layout, raw, this.Location())
	if err != nil {
		return time.Time{}, errorsx.Wrapf(
			err,
			"timex.Timezone.Parse",
			KindParseError,
			"layout=%s raw=%s",
			layout,
			raw,
		)
	}
	return parsed, nil
}

// ParseDate 按默认日期布局与对象绑定时区解析时间。
func (this *Timezone) ParseDate(raw string) (time.Time, error) {
	return this.Parse(DateLayout, raw)
}

// ParseDateTime 按默认日期时间布局与对象绑定时区解析时间。
func (this *Timezone) ParseDateTime(raw string) (time.Time, error) {
	return this.Parse(DateTimeLayout, raw)
}

// ParseDateTimeMilli 按默认毫秒日期时间布局与对象绑定时区解析时间。
func (this *Timezone) ParseDateTimeMilli(raw string) (time.Time, error) {
	return this.Parse(DateTimeMilliLayout, raw)
}

// StartOfDay 返回 timeValue 在对象绑定时区下当天 00:00:00。
func (this *Timezone) StartOfDay(timeValue time.Time) time.Time {
	location := this.Location()
	localized := timeValue.In(location)
	return time.Date(localized.Year(), localized.Month(), localized.Day(), 0, 0, 0, 0, location)
}

// StartOfNextDay 返回 timeValue 在对象绑定时区下下一天的 00:00:00。
// 典型调用场景：为数据库或日志查询构造 exclusive 日范围上界。
func (this *Timezone) StartOfNextDay(timeValue time.Time) time.Time {
	return this.StartOfDay(timeValue).AddDate(0, 0, 1)
}

// EndOfDay 返回 timeValue 在对象绑定时区下当天最后一个纳秒。
func (this *Timezone) EndOfDay(timeValue time.Time) time.Time {
	return this.StartOfNextDay(timeValue).Add(-time.Nanosecond)
}

// IsSameDay 判断两个时间在对象绑定时区下是否为同一天。
func (this *Timezone) IsSameDay(a, b time.Time) bool {
	left := a.In(this.Location())
	right := b.In(this.Location())
	return left.Year() == right.Year() && left.Month() == right.Month() && left.Day() == right.Day()
}

// resolveLocation 解析显式时区；nil 按公开契约回退到进程 time.Local。
func resolveLocation(loc *time.Location) *time.Location {
	if loc == nil {
		return time.Local
	}
	return loc
}

// mustLoadShanghai 加载包级默认时区；系统数据库不可用时退化为固定 UTC+08:00。
// 阶段角色：保证包级便捷 API 初始化不失败；退化模式不保留历史夏令时规则。
func mustLoadShanghai() *time.Location {
	location, err := time.LoadLocation(ShanghaiTimezoneName)
	if err != nil {
		// 退化阶段固定使用当前 UTC+08:00 业务偏移，不承诺历史夏令时规则。
		return time.FixedZone(ShanghaiTimezoneName, 8*60*60)
	}
	return location
}
