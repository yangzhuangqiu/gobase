package provider

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestNoBootstrapSchemaTypeStringLiteralInProductionCode 保证生产代码不再新增 Type: "xxx" 裸字符串写法。
//
// 语义：
// 1. 该检查作为 CI 护栏，推动统一使用 BootstrapSchemaType* 常量；
// 2. 仅检查非 *_test.go 文件，避免影响故障注入类测试用例。
func TestNoBootstrapSchemaTypeStringLiteralInProductionCode(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime caller unavailable")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	targets := []string{
		"config",
		"config-nacos",
		"config-consul",
		"app",
		"examples/quickstart-app",
	}
	pattern := regexp.MustCompile(`\bType:\s*"[^"]+"`)

	var violations []string
	for _, target := range targets {
		base := filepath.Join(repoRoot, target)
		walkErr := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if strings.EqualFold(d.Name(), "vendor") {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) != ".go" {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}

			file, openErr := os.Open(path)
			if openErr != nil {
				return openErr
			}
			defer func() { _ = file.Close() }()

			scanner := bufio.NewScanner(file)
			lineNo := 0
			for scanner.Scan() {
				lineNo++
				text := scanner.Text()
				if !pattern.MatchString(text) {
					continue
				}
				relative, relErr := filepath.Rel(repoRoot, path)
				if relErr != nil {
					relative = path
				}
				violations = append(violations, fmt.Sprintf("%s:%d %s", filepath.ToSlash(relative), lineNo, strings.TrimSpace(text)))
			}
			return scanner.Err()
		})
		if walkErr != nil {
			t.Fatalf("scan target %s failed: %v", target, walkErr)
		}
	}
	if len(violations) == 0 {
		return
	}
	t.Fatalf("found Type string literal usage, please use BootstrapSchemaType* constants:\n%s", strings.Join(violations, "\n"))
}
