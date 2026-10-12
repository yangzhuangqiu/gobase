package main

import (
	"fmt"
	"os"
	"strings"
)

const quickstartConfigModeEnv = "QUICKSTART_CONFIG_MODE"

func main() {
	// [可选][推荐] 通过环境变量切换入口：minimal 先上手，full 看完整能力。
	mode := strings.TrimSpace(strings.ToLower(os.Getenv(quickstartConfigModeEnv)))
	if mode == "" {
		mode = "minimal"
	}

	var err error
	switch mode {
	case "minimal":
		err = runQuickstartConfig()
	case "full":
		err = runQuickstartConfigFull()
	default:
		err = fmt.Errorf("invalid %s=%s, supported values: minimal|full", quickstartConfigModeEnv, mode)
	}
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "quickstart-config run failed: %v\n", err)
		os.Exit(1)
	}
}
