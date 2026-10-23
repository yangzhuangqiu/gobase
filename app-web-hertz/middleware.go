package appwebhertz

import (
	"github.com/cloudwego/hertz/pkg/app"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

// standardMiddlewarePlan 把完成态 Observation 前后的标准能力分段，固定其相对位置。
type standardMiddlewarePlan struct {
	// beforeObservation 当前只包含 Request ID，使快照能取得服务端关联标识。
	beforeObservation []app.HandlerFunc
	// afterObservation 包含 Recovery、CORS 和 Gzip，完成后快照读取最终状态与响应大小。
	afterObservation []app.HandlerFunc
}

// prepareStandardMiddleware 按公开固定阶段构造本运行周期的标准请求链。
//
// 阶段角色：配置和 typed policy 均已冻结，输出直接追加在 advanced outer Middleware 之后、应用 Filter 之前。
func prepareStandardMiddleware(config MiddlewareConfig, options *buildOptions, logger baselog.Logger) (standardMiddlewarePlan, error) {
	// 第一阶段：外层关联与恢复能力先装配，使内层阶段都处于同一 Request ID/Recovery 边界。
	plan := standardMiddlewarePlan{
		beforeObservation: make([]app.HandlerFunc, 0, 1),
		afterObservation:  make([]app.HandlerFunc, 0, 3),
	}
	if config.RequestID.Enabled {
		plan.beforeObservation = append(plan.beforeObservation, newRequestIDMiddleware(config.RequestID, options.requestIDGenerator, logger))
	}
	if config.Recovery.Enabled {
		plan.afterObservation = append(plan.afterObservation, newRecoveryMiddleware(config.Recovery, options.recoveryHandler, logger))
	}
	// 第二阶段：协议响应能力固定在应用 Filter 外层；CORS 构造可能因 typed 策略缺失而失败。
	if config.CORS.Enabled {
		handler, err := newCORSMiddleware(config.CORS, options.corsOriginValidator)
		if err != nil {
			return standardMiddlewarePlan{}, err
		}
		plan.afterObservation = append(plan.afterObservation, handler)
	}
	if config.Gzip.Enabled {
		plan.afterObservation = append(plan.afterObservation, newGzipMiddleware(config.Gzip, options.gzipPredicate, logger))
	}
	return plan, nil
}
