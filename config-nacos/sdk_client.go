package nacos

import (
	"context"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

const (
	// DefaultNacosPort 用于地址未显式携带端口时的默认端口。
	DefaultNacosPort uint64 = 8848
	// maxNacosPort 是网络端口允许的最大值。
	maxNacosPort uint64 = 65535
)

// ClientOptions 控制基于 Nacos SDK 的客户端创建参数。
type ClientOptions struct {
	// Address 是 Nacos 地址，可为单地址或逗号分隔多地址。
	// 示例：127.0.0.1:8848 或 10.0.0.1:8848,10.0.0.2:8848。
	Address string
	// Servers 是结构化服务节点；非空时整体覆盖 Address。
	Servers []Server
	// NamespaceID 是 Nacos 命名空间 ID。
	NamespaceID string
	// Username 是认证用户名（按需填写）。
	Username string
	// Password 是认证密码（按需填写）。
	Password string
	// TimeoutMs 是常规配置，控制 SDK 请求超时（毫秒）；0 表示使用默认值 10000。
	TimeoutMs uint64
	// LogDir 是高级 SDK 配置，控制 SDK 自有日志目录；为空时默认 os.TempDir()/nacos/log。
	LogDir string
	// CacheDir 是高级 SDK 配置，控制 SDK 快照缓存目录；为空时默认 os.TempDir()/nacos/cache。
	CacheDir string
	// DisableUseSnapshot 控制远程读取失败时是否禁止使用本地快照；默认 false，保留 SDK 的快照回退行为。
	DisableUseSnapshot bool
	// LogLevel 是高级 SDK 配置，控制进程级 SDK logger；为空时默认 warn。
	// 约束：仅允许 debug、info、warn、error；SDK 首个 Client 的配置在进程内生效。
	LogLevel string
}

// NewClient 基于 nacos-sdk-go/v2 构建真实 Nacos 客户端。
//
// 参数：
// - options: Nacos SDK 连接参数与运行时选项。
//
// 返回：
// - Client: 满足 Nacos Session 契约的客户端适配实现。
// - error: 参数非法或 SDK 客户端创建失败时返回错误。
func NewClient(options ClientOptions) (Client, error) {
	// 阶段一：连接入口至少声明 address 或一个结构化 server。
	if len(options.Servers) == 0 && strings.TrimSpace(options.Address) == "" {
		return nil, errorForNacosSDKInvalidInput(messagef("nacos address or servers is required"))
	}

	// 阶段二：在调用 SDK 前完成节点与进程级配置校验。
	serverConfigs, err := parseServerConfigs(options)
	if err != nil {
		return nil, err
	}
	logLevel, err := chooseLogLevel(options.LogLevel)
	if err != nil {
		return nil, err
	}

	clientConfig := buildNacosClientConfig(options, logLevel)

	// 阶段三：校验完成后才创建真实 SDK client，避免非法配置产生后台资源。
	configClient, err := clients.NewConfigClient(vo.NacosClientParam{
		ServerConfigs: serverConfigs,
		ClientConfig:  &clientConfig,
	})
	if err != nil {
		return nil, errorForNacosSDKClientCreateFailed(messagef("create nacos config client failed"), err)
	}

	return &nacosSDKClient{client: configClient}, nil
}

// buildNacosClientConfig 将适配层选项映射为 SDK ClientConfig。
// 阶段角色：参数校验完成后集中落实默认值与上游字段名，供真实 Client 创建使用。
func buildNacosClientConfig(options ClientOptions, logLevel string) constant.ClientConfig {
	return constant.ClientConfig{
		NamespaceId:         strings.TrimSpace(options.NamespaceID),
		TimeoutMs:           chooseTimeout(options.TimeoutMs),
		Username:            strings.TrimSpace(options.Username),
		Password:            strings.TrimSpace(options.Password),
		NotLoadCacheAtStart: true,
		LogDir:              chooseLogDir(options.LogDir),
		CacheDir:            chooseCacheDir(options.CacheDir),
		DisableUseSnapShot:  options.DisableUseSnapshot,
		LogLevel:            logLevel,
	}
}

// nacosSDKClient 把 nacos-sdk-go/v2 适配为 Nacos Client 接口。
type nacosSDKClient struct {
	client config_client.IConfigClient
}

// Close 结束 Client 生命周期并释放 nacos-sdk-go/v2 持有的 gRPC 连接。
// v2 CloseClient 内部保证幂等且不返回错误，因此适配层稳定返回 nil。
func (this *nacosSDKClient) Close() error {
	this.client.CloseClient()
	return nil
}

// GetConfig 从 Nacos 拉取一份配置文本。
//
// 参数：
// - ctx: 请求上下文，支持超时/取消。
// - groupID: Nacos group。
// - dataID: Nacos dataId。
//
// 返回：
// - []byte: 原始配置文本字节。
// - error: 拉取失败时返回错误。
func (this *nacosSDKClient) GetConfig(ctx context.Context, groupID, dataID string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	content, err := this.client.GetConfig(vo.ConfigParam{Group: groupID, DataId: dataID})
	if err != nil {
		return nil, err
	}
	return []byte(content), nil
}

// WatchConfig 注册 SDK 监听，并桥接为字节回调。
//
// 参数：
// - ctx: 监听上下文；取消后会自动取消 SDK 监听。
// - groupID: Nacos group。
// - dataID: Nacos dataId。
// - onChange: 变更回调，入参为最新内容字节。
//
// 返回：
// - io.Closer: 用于主动取消监听的句柄（幂等）。
// - error: 监听注册失败时返回错误。
func (this *nacosSDKClient) WatchConfig(ctx context.Context, groupID, dataID string, onChange func(content []byte)) (io.Closer, error) {
	if onChange == nil {
		return nil, errorForNacosSDKInvalidInput(messagef("onChange callback is required"))
	}

	param := vo.ConfigParam{
		Group:  groupID,
		DataId: dataID,
		OnChange: func(_, _, _, data string) {
			onChange([]byte(data))
		},
	}
	if err := this.client.ListenConfig(param); err != nil {
		return nil, err
	}

	closer := &nacosWatchCloser{client: this.client, param: param}
	go func() {
		<-ctx.Done()
		_ = closer.Close()
	}()
	return closer, nil
}

// nacosWatchCloser 负责幂等取消一次 ListenConfig 注册。
type nacosWatchCloser struct {
	client   config_client.IConfigClient
	param    vo.ConfigParam
	once     sync.Once
	closeErr error
}

// Close 取消已注册的 Nacos 监听。
func (this *nacosWatchCloser) Close() error {
	this.once.Do(func() {
		this.closeErr = this.client.CancelListenConfig(vo.ConfigParam{
			Group:  this.param.Group,
			DataId: this.param.DataId,
		})
	})
	return this.closeErr
}

// parseServerConfigs 按结构化节点优先规则生成 SDK ServerConfig。
//
// 语义：
// 1. Servers 非空时完全覆盖 Address；
// 2. Servers 为空时解析 Address 中的逗号分隔地址；
// 3. 至少需要解析出一个有效节点，否则返回错误。
func parseServerConfigs(options ClientOptions) ([]constant.ServerConfig, error) {
	// 首选结构化节点；该分支刻意不读取 Address，形成明确的整体覆盖语义。
	if len(options.Servers) > 0 {
		configs := make([]constant.ServerConfig, 0, len(options.Servers))
		for index, server := range options.Servers {
			parsed, err := parseDeclaredServer(index, server)
			if err != nil {
				return nil, err
			}
			configs = append(configs, parsed)
		}
		return configs, nil
	}

	// 未声明 Servers 时才进入 Address 简写路径，并忽略空白片段。
	parts := strings.Split(options.Address, ",")
	configs := make([]constant.ServerConfig, 0, len(parts))
	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parsed, err := parseOneAddress(raw)
		if err != nil {
			return nil, err
		}
		configs = append(configs, parsed)
	}
	if len(configs) == 0 {
		return nil, errorForNacosSDKInvalidInput(messagef("no valid nacos server address"))
	}
	return configs, nil
}

// parseOneAddress 将 Address 中的单个简写地址转换为标准 SDK ServerConfig。
//
// 语义：
// 1. 未携带 scheme 时使用 http；
// 2. contextPath 固定使用 /nacos；
// 3. grpcPort 保持 0，由 SDK 按 HTTP 端口 +1000 推导。
func parseOneAddress(raw string) (constant.ServerConfig, error) {
	// Address 是标准节点简写；协议、路径等非标准连接参数必须进入 Servers。
	if strings.Contains(raw, "://") {
		return constant.ServerConfig{}, errorForNacosSDKInvalidInput(messagef("invalid nacos address %s: scheme requires structured servers", raw))
	}
	candidate := "http://" + raw
	uri, err := url.Parse(candidate)
	if err != nil {
		return constant.ServerConfig{}, errorForNacosSDKInvalidInput(messagef("parse nacos address %s failed", raw), err)
	}
	// URL 仅作为 host/port 的可靠解析器，额外 URL 成分不属于简写契约。
	host := strings.TrimSpace(uri.Hostname())
	if host == "" {
		return constant.ServerConfig{}, errorForNacosSDKInvalidInput(messagef("invalid nacos address " + raw + ": empty host"))
	}
	if uri.Path != "" || uri.RawQuery != "" || uri.Fragment != "" || uri.User != nil {
		return constant.ServerConfig{}, errorForNacosSDKInvalidInput(messagef("invalid nacos address %s: non-standard fields require structured servers", raw))
	}
	port, err := parsePort(uri)
	if err != nil {
		return constant.ServerConfig{}, err
	}
	if err = validateDerivedGrpcPort(port); err != nil {
		return constant.ServerConfig{}, err
	}
	return constant.ServerConfig{
		Scheme:      "http",
		ContextPath: chooseContextPath(""),
		IpAddr:      host,
		Port:        port,
	}, nil
}

// parseDeclaredServer 把一个结构化节点转换为 SDK ServerConfig。
// 阶段角色：SDK 创建前校验节点定位、协议及端口，错误携带 server_index 便于定位。
func parseDeclaredServer(index int, server Server) (constant.ServerConfig, error) {
	// 阶段一：先校验节点定位和 HTTP 端口，再处理依赖 HTTP 端口的 gRPC 默认值。
	host, err := normalizeServerHost(server.Host)
	if err != nil {
		return constant.ServerConfig{}, errorForNacosSDKInvalidInput(messagef("invalid nacos server server_index=%d reason=invalid_host", index), err)
	}
	port, err := chooseServerPort(server.Port)
	if err != nil {
		return constant.ServerConfig{}, errorForNacosSDKInvalidInput(messagef("invalid nacos server server_index=%d reason=http_port_out_of_range", index), err)
	}
	// 阶段二：显式和派生 gRPC 端口都必须在 SDK 建连前完成边界校验。
	if server.GrpcPort > maxNacosPort {
		return constant.ServerConfig{}, errorForNacosSDKInvalidInput(messagef("invalid nacos server server_index=%d reason=grpc_port_out_of_range", index))
	}
	if server.GrpcPort == 0 {
		if err = validateDerivedGrpcPort(port); err != nil {
			return constant.ServerConfig{}, errorForNacosSDKInvalidInput(messagef("invalid nacos server server_index=%d reason=derived_grpc_port_out_of_range", index), err)
		}
	}
	// 阶段三：规范协议与路径，构造每个节点独立的 SDK ServerConfig。
	scheme, err := chooseServerScheme(server.Scheme)
	if err != nil {
		return constant.ServerConfig{}, errorForNacosSDKInvalidInput(messagef("invalid nacos server server_index=%d reason=unsupported_scheme", index), err)
	}
	return constant.ServerConfig{
		Scheme:      scheme,
		ContextPath: chooseContextPath(server.ContextPath),
		IpAddr:      host,
		Port:        port,
		GrpcPort:    server.GrpcPort,
	}, nil
}

// normalizeServerHost 校验结构化节点 host 不混入协议、路径或端口，并规范 IPv6 方括号。
// 阶段角色：保持 Server 字段单一职责，避免与 port/scheme 的值产生隐式优先级。
func normalizeServerHost(raw string) (string, error) {
	host := strings.TrimSpace(raw)
	if host == "" {
		return "", errorForNacosSDKInvalidInput(messagef("nacos server host is required"))
	}
	if strings.Contains(host, "://") || strings.ContainsAny(host, "/?#") || strings.ContainsAny(host, " \t\r\n") {
		return "", errorForNacosSDKInvalidInput(messagef("nacos server host must not contain scheme, path, query, fragment, or whitespace"))
	}
	if _, port, err := net.SplitHostPort(host); err == nil && port != "" {
		return "", errorForNacosSDKInvalidInput(messagef("nacos server host must not contain port"))
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	return host, nil
}

// parsePort 从 URL 中提取端口，缺失时回退默认端口。
func parsePort(uri *url.URL) (uint64, error) {
	portText := strings.TrimSpace(uri.Port())
	if portText == "" {
		return DefaultNacosPort, nil
	}
	port, err := strconv.ParseUint(portText, 10, 64)
	if err != nil {
		return 0, errorForNacosSDKInvalidInput(messagef("invalid nacos port %s", portText), err)
	}
	if port == 0 || port > maxNacosPort {
		return 0, errorForNacosSDKInvalidInput(messagef("invalid nacos port " + portText + ": out_of_range[1," + strconv.FormatUint(maxNacosPort, 10) + "]"))
	}
	return port, nil
}

// chooseServerPort 应用节点 HTTP 端口默认值并校验显式值。
// 阶段角色：统一 Address 与 Servers 最终交给 SDK 的端口边界。
func chooseServerPort(port uint64) (uint64, error) {
	if port == 0 {
		return DefaultNacosPort, nil
	}
	if port > maxNacosPort {
		return 0, errorForNacosSDKInvalidInput(messagef("invalid nacos port " + strconv.FormatUint(port, 10) + ": out_of_range[1," + strconv.FormatUint(maxNacosPort, 10) + "]"))
	}
	return port, nil
}

// validateDerivedGrpcPort 保证 SDK 的 Port+1000 推导结果仍是有效网络端口。
// 阶段角色：在 SDK 建连前拦截不可达端口，避免延迟为难定位的连接错误。
func validateDerivedGrpcPort(port uint64) error {
	if port > maxNacosPort-uint64(constant.RpcPortOffset) {
		return errorForNacosSDKInvalidInput(messagef("invalid nacos port " + strconv.FormatUint(port, 10) + ": derived grpc port out_of_range[1," + strconv.FormatUint(maxNacosPort, 10) + "]"))
	}
	return nil
}

// chooseTimeout 应用安全默认超时。
func chooseTimeout(timeoutMs uint64) uint64 {
	if timeoutMs == 0 {
		return 10_000
	}
	return timeoutMs
}

// chooseLogDir 应用稳定默认日志目录。
func chooseLogDir(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return filepath.Join(os.TempDir(), "nacos", "log")
	}
	return path
}

// chooseCacheDir 应用稳定默认缓存目录。
func chooseCacheDir(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return filepath.Join(os.TempDir(), "nacos", "cache")
	}
	return path
}

// chooseLogLevel 校验并应用 SDK 日志级别默认值。
// 阶段角色：创建 SDK Client 前 fail-fast，避免 SDK 把非法值静默回退为 info。
func chooseLogLevel(level string) (string, error) {
	level = strings.TrimSpace(level)
	if level == "" {
		return "warn", nil
	}
	if !isSupportedNacosLogLevel(level) {
		return "", errorForNacosSDKInvalidInput(messagef("invalid nacos log level %q: expected debug|info|warn|error", level))
	}
	return level, nil
}

// isSupportedNacosLogLevel 判断日志级别是否属于 SDK 明确定义的小写枚举。
// 阶段角色：复用 bootstrap 声明校验与直接 Client 创建校验的值域规则。
func isSupportedNacosLogLevel(level string) bool {
	switch level {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}

// chooseContextPath 应用稳定默认上下文路径。
func chooseContextPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/nacos"
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

// chooseServerScheme 校验并应用节点 HTTP 协议默认值。
// 阶段角色：禁止 SDK 接收无法建立 HTTP/OpenAPI 请求的协议值。
func chooseServerScheme(scheme string) (string, error) {
	scheme = strings.TrimSpace(scheme)
	if scheme == "" {
		return "http", nil
	}
	if scheme != "http" && scheme != "https" {
		return "", errorForNacosSDKInvalidInput(messagef("invalid nacos scheme %q: expected http|https", scheme))
	}
	return scheme, nil
}
