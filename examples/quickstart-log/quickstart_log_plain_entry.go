package main

import (
	"context"
	"fmt"
	"os"

	baselog "github.com/yangzhuangqiu/gobase/log"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
)

type quickstartLogPlainService struct{}

// runQuickstartLogPlain 运行不依赖 app 的最小入口示例。
func runQuickstartLogPlain() error {
	provider, err := logzap.NewProvider(logzap.Config{})
	if err != nil {
		return err
	}
	defer provider.Close()

	logger := provider.Factory().Get("quickstart.log.plain")
	logger.Info(context.Background(), "plain quickstart log emitted",
		baselog.Op("quickstart.log.plain.run"),
		baselog.Result(baselog.ResultOK),
		baselog.F("request_id", "qs-plain-rid-001"),
		baselog.F("user_id", "qs-plain-uid-001"),
	)
	_, _ = fmt.Fprintln(os.Stdout, "plain minimal done")
	return nil
}
