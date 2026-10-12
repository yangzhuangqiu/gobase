package log

import "strings"

const (
	// FieldKeyOperation 是稳定操作标识字段键。
	FieldKeyOperation = "op"
	// FieldKeyResult 是稳定执行结果字段键。
	FieldKeyResult = "result"
	// FieldKeyReason 是稳定分支或失败原因字段键。
	FieldKeyReason = "reason"
)

const (
	// ResultOK 表示操作按预期完成。
	ResultOK = "ok"
	// ResultFailed 表示操作未完成且需要失败处理。
	ResultFailed = "failed"
	// ResultDegraded 表示操作通过降级路径继续。
	ResultDegraded = "degraded"
	// ResultSkipped 表示操作因稳定前置条件未执行。
	ResultSkipped = "skipped"
	// ResultRunning 表示操作已经开始但尚未完成。
	ResultRunning = "running"
)

// Op 创建稳定的操作标识字段。
func Op(value string) Field {
	return F(FieldKeyOperation, strings.TrimSpace(value))
}

// Result 创建执行结果字段；调用方应优先使用预定义常量，扩展值必须保持跨模块稳定。
func Result(value string) Field {
	return F(FieldKeyResult, strings.TrimSpace(value))
}

// Reason 创建稳定、可聚合的分支原因字段。
func Reason(value string) Field {
	return F(FieldKeyReason, strings.TrimSpace(value))
}
