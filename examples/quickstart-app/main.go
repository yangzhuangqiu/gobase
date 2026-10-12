package main

import (
	"fmt"
	"os"
	"strings"
)

const quickstartModeEnv = "QUICKSTART_MODE"

func main() {
	mode := strings.TrimSpace(strings.ToLower(os.Getenv(quickstartModeEnv)))
	if mode == "" {
		mode = "minimal"
	}

	var err error
	switch mode {
	case "minimal":
		err = runQuickstartApp()
	case "full":
		err = runQuickstartAppFull()
	default:
		err = fmt.Errorf("invalid %s=%s, supported values: minimal|full", quickstartModeEnv, mode)
	}

	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "quickstart run failed: %v\n", err)
		os.Exit(1)
	}
}
