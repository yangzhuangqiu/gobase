package appwebhertz

import (
	"context"
	"fmt"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/middlewares/server/recovery"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

// newRecoveryMiddleware 构造 Hertz Recovery，并按 typed policy 或内建安全策略终结 panic 请求。
// 阶段角色：位于 Request ID 内层、CORS/Gzip/Filter 外层，保证后续阶段 panic 均转为最终 500。
func newRecoveryMiddleware(config RecoveryConfig, handler RecoveryHandler, logger baselog.Logger) app.HandlerFunc {
	if handler == nil {
		handler = defaultRecoveryHandler(config.Response, logger)
	}
	return recovery.Recovery(recovery.WithRecoveryHandler(func(ctx context.Context, request *app.RequestContext, panicValue any, stack []byte) {
		markRequestObservationPanic(request)
		handler(ctx, request, panicValue, stack)
	}))
}

// defaultRecoveryHandler 返回不向客户端暴露 panic 内容的内建诊断和响应策略。
// 阶段角色：仅在调用方未提供 typed RecoveryHandler 时使用。
func defaultRecoveryHandler(strategy RecoveryResponseStrategy, logger baselog.Logger) RecoveryHandler {
	return func(ctx context.Context, request *app.RequestContext, panicValue any, stack []byte) {
		// 只记录 panic 类型和受控 stack，不把可能包含业务数据的 panic 文本写入日志或响应。
		logger.Error(ctx, "Request panic recovered",
			baselog.F("op", "app_web_hertz.recovery"), baselog.F("result", "failed"), baselog.F("reason", "panic"),
			baselog.F("request_id", GetRequestID(request)), baselog.F("panic_type", panicTypeName(panicValue)), baselog.F("stack", string(stack)),
		)
		switch strategy {
		case RecoveryResponseText:
			request.AbortWithMsg("internal server error", consts.StatusInternalServerError)
		case RecoveryResponseEmpty:
			request.AbortWithStatus(consts.StatusInternalServerError)
		default:
			request.AbortWithStatusJSON(consts.StatusInternalServerError, map[string]string{"code": "internal_server_error"})
		}
	}
}

// panicTypeName 返回不包含 panic 内容的稳定类型名称。
// 阶段角色：Recovery 诊断保留类型线索，同时阻止潜在业务数据进入日志。
func panicTypeName(value any) string {
	return fmt.Sprintf("%T", value)
}
