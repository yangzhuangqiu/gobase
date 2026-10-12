package webhertz

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

// RequestObservationMetadataProvider 定义 Observer 可选实现的请求期关联元数据合同。
//
// 语义：Provider 只在请求返回前同步读取 RequestContext，不得保存该池化对象；返回值只能补充关联字段和 panic 标记，不能改写指标维度。
type RequestObservationMetadataProvider interface {
	// RequestObservationMetadata 返回当前请求的受控关联元数据。
	RequestObservationMetadata(ctx context.Context, request *app.RequestContext) RequestObservationMetadata
}
