package nacos

import (
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/nacos-group/nacos-sdk-go/clients"
	"github.com/nacos-group/nacos-sdk-go/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/common/constant"
	"github.com/nacos-group/nacos-sdk-go/vo"
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
	// NamespaceID 是 Nacos 命名空间 ID。
	NamespaceID string
	// Username 是认证用户名（按需填写）。
	Username string
	// Password 是认证密码（按需填写）。
	Password string
	// TimeoutMs 是 SDK 请求超时（毫秒）；0 表示使用默认值 10000。
	TimeoutMs uint64
	// Scheme 是请求协议（http/https）；为空时优先地址内 scheme，否则默认 http。
	Scheme string
	// ContextPath 是 Nacos HTTP 上下文路径；为空时默认 /nacos。
	ContextPath string
	// LogDir 是 SDK 日志目录；为空时默认 %TEMP%/nacos/log。
	LogDir string
	// CacheDir 是 SDK 缓存目录；为空时默认 %TEMP%/nacos/cache。
	CacheDir string
	// LogLevel 是 SDK 日志级别；为空时默认 warn。
	LogLevel string
}

// NewClient 基于 nacos-sdk-go 构建真实 Nacos 客户端。
//
// 参数：
// - options: Nacos SDK 连接参数与运行时选项。
//
// 返回：
// - Client: 满足 Nacos Session 契约的客户端适配实现。
// - error: 参数非法或 SDK 客户端创建失败时返回错误。
func NewClient(options ClientOptions) (Client, error) {
	if strings.TrimSpace(options.Address) == "" {
		return nil, errorForNacosSDKInvalidInput(messagef("nacos address is required"))
	}

	serverConfigs, err := parseServerConfigs(options)
	if err != nil {
		return nil, err
	}

	clientConfig := constant.ClientConfig{
		NamespaceId:         strings.TrimSpace(options.NamespaceID),
		TimeoutMs:           chooseTimeout(options.TimeoutMs),
		Username:            strings.TrimSpace(options.Username),
		Password:            strings.TrimSpace(options.Password),
		NotLoadCacheAtStart: true,
		LogDir:              chooseLogDir(options.LogDir),
		CacheDir:            chooseCacheDir(options.CacheDir),
		LogLevel:            chooseLogLevel(options.LogLevel),
		ContextPath:         chooseContextPath(options.ContextPath),
	}

	configClient, err := clients.CreateConfigClient(map[string]interface{}{
		"serverConfigs": serverConfigs,
		"clientConfig":  clientConfig,
	})
	if err != nil {
		return nil, errorForNacosSDKClientCreateFailed(messagef("create nacos config client failed"), err)
	}

	return &nacosSDKClient{client: configClient}, nil
}

// nacosSDKClient 把 nacos-sdk-go 适配为 Nacos Client 接口。
type nacosSDKClient struct {
	client config_client.IConfigClient
}

// Close 结束 Client 生命周期。
// 当前 nacos-sdk-go 的 IConfigClient 未提供整体关闭能力，因此这是明确的幂等 no-op；watcher 由各自 closer 释放。
func (this *nacosSDKClient) Close() error { return nil }

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

// parseServerConfigs 解析一个或多个逗号分隔的 Nacos 地址。
//
// 语义：
// 1. 会忽略空白片段；
// 2. 至少需要解析出一个有效地址，否则返回错误。
func parseServerConfigs(options ClientOptions) ([]constant.ServerConfig, error) {
	parts := strings.Split(options.Address, ",")
	configs := make([]constant.ServerConfig, 0, len(parts))
	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parsed, err := parseOneAddress(raw, options)
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

// parseOneAddress 将单个地址字符串转换为 SDK 的 ServerConfig。
//
// 语义：
// 1. 未携带 scheme 时默认补 `http://` 便于 URL 解析；
// 2. 最终 scheme 按“显式参数优先，再回退地址自身”规则确定。
func parseOneAddress(raw string, options ClientOptions) (constant.ServerConfig, error) {
	candidate := raw
	if !strings.Contains(candidate, "://") {
		candidate = "http://" + candidate
	}
	uri, err := url.Parse(candidate)
	if err != nil {
		return constant.ServerConfig{}, errorForNacosSDKInvalidInput(messagef("parse nacos address %s failed", raw), err)
	}
	host := strings.TrimSpace(uri.Hostname())
	if host == "" {
		return constant.ServerConfig{}, errorForNacosSDKInvalidInput(messagef("invalid nacos address " + raw + ": empty host"))
	}
	port, err := parsePort(uri)
	if err != nil {
		return constant.ServerConfig{}, err
	}
	scheme := chooseScheme(options.Scheme, uri.Scheme)
	return constant.ServerConfig{
		Scheme:      scheme,
		ContextPath: chooseContextPath(options.ContextPath),
		IpAddr:      host,
		Port:        port,
	}, nil
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

// chooseLogLevel 应用稳定默认日志级别。
func chooseLogLevel(level string) string {
	level = strings.TrimSpace(level)
	if level == "" {
		return "warn"
	}
	return level
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

// chooseScheme 按“显式参数优先”规则解析 scheme。
func chooseScheme(preferred, fallback string) string {
	preferred = strings.TrimSpace(preferred)
	if preferred != "" {
		return preferred
	}
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return "http"
	}
	return fallback
}
