package main

import (
	"fmt"
	"os"
	"strings"
)

const quickstartLogModeEnv = "QUICKSTART_LOG_MODE"

func main() {
	mode := strings.TrimSpace(strings.ToLower(os.Getenv(quickstartLogModeEnv)))
	if mode == "" {
		mode = "app-entry"
	}

	var err error
	switch mode {
	case "app-entry":
		err = runQuickstartLog()
	case "app-full":
		err = runQuickstartLogFull()
	case "plain-entry":
		err = runQuickstartLogPlain()
	case "plain-full":
		err = runQuickstartLogPlainFull()
	default:
		err = fmt.Errorf("invalid %s=%s, supported values: app-entry|app-full|plain-entry|plain-full", quickstartLogModeEnv, mode)
	}

	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "quickstart-log run failed: %v\n", err)
		os.Exit(1)
	}
}
