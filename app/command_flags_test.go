package app

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
)

// TestUseCaseCommandFlagsAllTypedDeclarations 验证 CommandFlags 的配置/命令参数声明都会按预期注册与分层。
//
// 测试方法：
// 1. 在同一个 FlagSet 上声明全部 Config* 和无前缀类型；
// 2. 设置一组显式值并收集 pinned；
// 3. 断言 Config* 进入 pinned，普通命令 Flag 不进入 pinned。
func TestUseCaseCommandFlagsAllTypedDeclarations(t *testing.T) {
	fs := pflag.NewFlagSet("typed-options", pflag.ContinueOnError)
	flags := newPFlagCommandFlags(fs)

	flags.ConfigString("cfg.s", "config string")
	flags.ConfigInt("cfg.i", "config int")
	flags.ConfigInt64("cfg.i64", "config int64")
	flags.ConfigUint("cfg.u", "config uint")
	flags.ConfigUint64("cfg.u64", "config uint64")
	flags.ConfigBool("cfg.b", "config bool")
	flags.ConfigFloat64("cfg.f64", "config float64")
	flags.ConfigDuration("cfg.d", "config duration")
	flags.ConfigStringArray("cfg.arr", "config string array")

	var rtString string
	var rtInt int
	var rtInt64 int64
	var rtUint uint
	var rtUint64 uint64
	var rtBool bool
	var rtFloat64 float64
	var rtDuration time.Duration
	var rtArray []string
	flags.String("rt.s", &rtString, "", "runtime string")
	flags.Int("rt.i", &rtInt, 0, "runtime int")
	flags.Int64("rt.i64", &rtInt64, 0, "runtime int64")
	flags.Uint("rt.u", &rtUint, 0, "runtime uint")
	flags.Uint64("rt.u64", &rtUint64, 0, "runtime uint64")
	flags.Bool("rt.b", &rtBool, false, "runtime bool")
	flags.Float64("rt.f64", &rtFloat64, 0, "runtime float64")
	flags.Duration("rt.d", &rtDuration, 0, "runtime duration")
	flags.StringArray("rt.arr", &rtArray, nil, "runtime string array")

	_ = fs.Set("cfg.s", "v1")
	_ = fs.Set("cfg.i", "7")
	_ = fs.Set("cfg.i64", "8")
	_ = fs.Set("cfg.u", "9")
	_ = fs.Set("cfg.u64", "10")
	_ = fs.Set("cfg.b", "true")
	_ = fs.Set("cfg.f64", "3.14")
	_ = fs.Set("cfg.d", "2s")
	_ = fs.Set("cfg.arr", "a")
	_ = fs.Set("cfg.arr", "b")

	_ = fs.Set("rt.s", "runtime")
	_ = fs.Set("rt.i", "11")
	_ = fs.Set("rt.i64", "12")
	_ = fs.Set("rt.u", "13")
	_ = fs.Set("rt.u64", "14")
	_ = fs.Set("rt.b", "true")
	_ = fs.Set("rt.f64", "6.28")
	_ = fs.Set("rt.d", "3s")
	_ = fs.Set("rt.arr", "x")

	pinned, err := collectPinnedValues([]*pflag.FlagSet{fs})
	if err != nil {
		t.Fatalf("collect pinned values failed: %v", err)
	}
	cfg, ok := pinned["cfg"].(map[string]any)
	if !ok {
		t.Fatalf("expected cfg map in pinned, got: %#v", pinned)
	}
	if cfg["s"] != "v1" || cfg["i"] != 7 || cfg["i64"] != int64(8) {
		t.Fatalf("unexpected config scalar values: %#v", cfg)
	}
	if cfg["u"] != uint(9) || cfg["u64"] != uint64(10) {
		t.Fatalf("unexpected config uint values: %#v", cfg)
	}
	if cfg["b"] != true || cfg["f64"] != 3.14 {
		t.Fatalf("unexpected config bool/float values: %#v", cfg)
	}
	if cfg["d"] != 2*time.Second {
		t.Fatalf("unexpected config duration: %#v", cfg["d"])
	}
	arr, ok := cfg["arr"].([]string)
	if !ok || len(arr) != 2 || arr[0] != "a" || arr[1] != "b" {
		t.Fatalf("unexpected config array: %#v", cfg["arr"])
	}
	if _, exists := pinned["rt"]; exists {
		t.Fatalf("expected runtime options excluded from pinned, got: %#v", pinned["rt"])
	}
}
