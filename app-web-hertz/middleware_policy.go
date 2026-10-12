package appwebhertz

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

// RecoveryHandler 定义 Recovery 捕获 panic 后的应用策略合同。
// 实现负责记录受控诊断并终结响应，不得再次执行请求链。
type RecoveryHandler func(ctx context.Context, request *app.RequestContext, panicValue any, stack []byte)

// RequestIDGenerator 定义缺少可信入站 Request ID 时的生成合同。
// 返回值必须是 1..128 字节的可打印关联标识；错误会终结当前请求并返回稳定 500。
type RequestIDGenerator func(ctx context.Context, request *app.RequestContext) (string, error)

// CORSOriginValidator 定义静态 AllowOrigins 之外的动态 Origin 判定合同。
// 实现必须快速、并发安全且不得持有池化 RequestContext。
type CORSOriginValidator func(ctx context.Context, request *app.RequestContext, origin string) bool

// GzipPredicate 定义通过基础协议检查后是否压缩当前响应的附加策略。
// 调用发生在下游 Handler 完成后，实现可读取最终状态、Header 和 Body，但不得保留 RequestContext。
type GzipPredicate func(ctx context.Context, request *app.RequestContext) bool
