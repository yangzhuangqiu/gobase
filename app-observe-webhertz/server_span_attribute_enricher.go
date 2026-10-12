package webhertzobserve

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
)

// ServerSpanAttributeEnricher 定义基于安全完成态快照追加低风险 Span Attribute 的合同。
//
// 典型调用场景:
// 1. 应用按有限枚举追加 deployment、tenant_class 等稳定维度；
// 2. 适配器在响应完成、Span End 前同步调用，不向实现暴露 Header、Body、query 或池化 RequestContext。
type ServerSpanAttributeEnricher interface {
	// EnrichServerSpan 返回要追加的 Attribute；保留键和疑似敏感键会被适配器过滤。
	// 实现必须快速、并发安全，且不得把高基数原始路径编码到值中。
	EnrichServerSpan(ctx context.Context, snapshot ServerSpanSnapshot) []attribute.KeyValue
}
