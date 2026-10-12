package log

import "testing"

// TestSemanticFieldHelpersUseStableSchema 验证语义 helper 固定字段键和值域文本。
//
// 测试方法：分别构造 op/result/reason 字段，向 Result 直接传入 string，断言键名与空白归一化稳定。
func TestSemanticFieldHelpersUseStableSchema(t *testing.T) {
	result := " failed "
	fields := []Field{
		Op(" order.persist "),
		Result(result),
		Reason(" repository_error "),
	}
	if fields[0].Key != FieldKeyOperation || fields[0].Value != "order.persist" {
		t.Fatalf("unexpected op field: %+v", fields[0])
	}
	if fields[1].Key != FieldKeyResult || fields[1].Value != ResultFailed {
		t.Fatalf("unexpected result field: %+v", fields[1])
	}
	if fields[2].Key != FieldKeyReason || fields[2].Value != "repository_error" {
		t.Fatalf("unexpected reason field: %+v", fields[2])
	}
}
