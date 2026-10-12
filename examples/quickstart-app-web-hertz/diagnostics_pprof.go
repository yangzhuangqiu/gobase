package main

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"runtime"
	runtimepprof "runtime/pprof"
	runtimetrace "runtime/trace"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultCPUProfileDuration 是未声明 seconds 时 CPU profile 的标准采样窗口。
	defaultCPUProfileDuration = 30 * time.Second
	// defaultTraceDuration 是未声明 seconds 时 runtime trace 的默认采样窗口。
	defaultTraceDuration = time.Second
	// maxDiagnosticProfileDuration 限制单次诊断请求长期占用 CPU/trace 全局采集器。
	maxDiagnosticProfileDuration = 5 * time.Minute
	// maxSymbolRequestBytes 限制 symbol POST 请求体，避免诊断端口无界分配。
	maxSymbolRequestBytes = 1 << 20
)

// registerPprofRoutes 使用 runtime/pprof 显式构造私有路由，避免导入 net/http/pprof 修改 DefaultServeMux。
func registerPprofRoutes(mux *http.ServeMux) {
	// 第一阶段：注册固定控制端点和无尾斜杠重定向。
	root := strings.TrimSuffix(diagnosticsPprofPrefix, "/")
	mux.HandleFunc(root, func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, diagnosticsPprofPrefix, http.StatusMovedPermanently)
	})
	mux.HandleFunc(diagnosticsPprofPrefix, servePprofIndex)
	mux.HandleFunc(diagnosticsPprofPrefix+"cmdline", servePprofCmdline)
	mux.HandleFunc(diagnosticsPprofPrefix+"profile", servePprofCPU)
	mux.HandleFunc(diagnosticsPprofPrefix+"symbol", servePprofSymbol)
	mux.HandleFunc(diagnosticsPprofPrefix+"trace", servePprofTrace)
	// 第二阶段：启动时冻结当前 runtime profile 名称，避免请求期修改路由表。
	for _, profile := range runtimepprof.Profiles() {
		name := profile.Name()
		mux.HandleFunc(diagnosticsPprofPrefix+name, func(writer http.ResponseWriter, request *http.Request) {
			serveRuntimeProfile(writer, request, name)
		})
	}
}

// servePprofIndex 返回当前 runtime profile 清单，只服务私有诊断 mux。
func servePprofIndex(writer http.ResponseWriter, _ *http.Request) {
	profiles := runtimepprof.Profiles()
	sort.Slice(profiles, func(left int, right int) bool { return profiles[left].Name() < profiles[right].Name() })
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(writer, "<html><body><h1>runtime profiles</h1><ul>")
	for _, profile := range profiles {
		name := html.EscapeString(profile.Name())
		_, _ = fmt.Fprintf(writer, `<li><a href="%s%s?debug=1">%s</a></li>`, diagnosticsPprofPrefix, name, name)
	}
	_, _ = io.WriteString(writer, "</ul></body></html>")
}

// servePprofCmdline 返回以 NUL 分隔的进程参数，保持常用 pprof 诊断语义。
func servePprofCmdline(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(writer, strings.Join(os.Args, "\x00"))
}

// serveRuntimeProfile 输出指定 runtime profile；debug=0 为 protobuf，其他值为文本。
func serveRuntimeProfile(writer http.ResponseWriter, request *http.Request, name string) {
	// 第一阶段：解析 profile 与 debug 输出模式，非法输入不启动 profile 写出。
	profile := runtimepprof.Lookup(name)
	if profile == nil {
		http.NotFound(writer, request)
		return
	}
	debug := 0
	if raw := strings.TrimSpace(request.URL.Query().Get("debug")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			http.Error(writer, "invalid debug", http.StatusBadRequest)
			return
		}
		debug = value
	}
	// 第二阶段：protobuf 与文本使用不同响应合同，均禁止 MIME sniffing。
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if debug == 0 {
		writer.Header().Set("Content-Type", "application/octet-stream")
		writer.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	} else {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	if err := profile.WriteTo(writer, debug); err != nil {
		http.Error(writer, "profile write failed", http.StatusInternalServerError)
	}
}

// servePprofCPU 采集受时长上限约束的 CPU profile，并响应请求取消。
func servePprofCPU(writer http.ResponseWriter, request *http.Request) {
	duration, err := parseProfileDuration(request, defaultCPUProfileDuration)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Content-Disposition", `attachment; filename="profile"`)
	if err := runtimepprof.StartCPUProfile(writer); err != nil {
		http.Error(writer, "cpu profile already in use", http.StatusInternalServerError)
		return
	}
	waitProfileDuration(request, duration)
	runtimepprof.StopCPUProfile()
}

// servePprofTrace 采集受时长上限约束的 runtime trace，并响应请求取消。
func servePprofTrace(writer http.ResponseWriter, request *http.Request) {
	duration, err := parseProfileDuration(request, defaultTraceDuration)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Content-Disposition", `attachment; filename="trace"`)
	if err := runtimetrace.Start(writer); err != nil {
		http.Error(writer, "runtime trace already in use", http.StatusInternalServerError)
		return
	}
	waitProfileDuration(request, duration)
	runtimetrace.Stop()
}

// servePprofSymbol 把 GET/POST 中的程序计数器解析为函数名。
func servePprofSymbol(writer http.ResponseWriter, request *http.Request) {
	// 第一阶段：完整校验 GET query 或受限 POST body，失败前不写响应状态。
	raw := request.URL.RawQuery
	if request.Method == http.MethodPost {
		body, err := io.ReadAll(io.LimitReader(request.Body, maxSymbolRequestBytes+1))
		if err != nil || len(body) > maxSymbolRequestBytes {
			http.Error(writer, "invalid symbol request", http.StatusBadRequest)
			return
		}
		raw = string(body)
	}
	// 第二阶段：逐个解析地址；未知地址按 pprof symbol 语义忽略而非返回动态错误。
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(writer, "num_symbols: 1\n")
	for _, token := range strings.FieldsFunc(raw, func(value rune) bool { return value == '+' || value == ' ' || value == '\n' }) {
		pc, err := parseProgramCounter(token)
		if err != nil {
			continue
		}
		function := runtime.FuncForPC(uintptr(pc))
		if function != nil {
			_, _ = fmt.Fprintf(writer, "%s\t%s\n", token, function.Name())
		}
	}
}

// parseProfileDuration 解析 seconds，并对昂贵采集设置正数和最大值约束。
func parseProfileDuration(request *http.Request, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(request.URL.Query().Get("seconds"))
	if raw == "" {
		return fallback, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("invalid seconds")
	}
	duration := time.Duration(seconds) * time.Second
	if duration > maxDiagnosticProfileDuration {
		return 0, fmt.Errorf("seconds exceeds maximum")
	}
	return duration, nil
}

// waitProfileDuration 等待采集窗口或请求取消，确保 Stop 始终由调用者执行。
func waitProfileDuration(request *http.Request, duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-request.Context().Done():
	}
}

// parseProgramCounter 同时接受 0x 前缀和 pprof 常见的裸十六进制地址。
func parseProgramCounter(raw string) (uint64, error) {
	value, err := strconv.ParseUint(raw, 0, 64)
	if err == nil {
		return value, nil
	}
	return strconv.ParseUint(raw, 16, 64)
}
