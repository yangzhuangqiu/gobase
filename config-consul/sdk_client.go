package consul

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
)

const (
	// DefaultWatchWaitTime 是 Consul 阻塞查询的默认等待时长。
	DefaultWatchWaitTime = 30 * time.Second
	// watchRetryDelay 是 watch 查询失败后的退避间隔。
	watchRetryDelay = 500 * time.Millisecond
)

// NewClient 基于 hashicorp/consul/api 创建 Consul Client。
//
// 参数：
// - options: Consul 连接参数与监听选项。
//
// 返回：
// - Client: 适配 Consul Session 契约的客户端。
// - error: 地址非法或 SDK 客户端创建失败时返回错误。
func NewClient(options ClientOptions) (Client, error) {
	if err := validateConsulWaitTime(options.WaitTimeMs); err != nil {
		return nil, err
	}
	address, scheme, pathPrefix, err := normalizeConsulAddress(options.Address, options.Scheme)
	if err != nil {
		return nil, err
	}

	cfg := api.DefaultConfig()
	cfg.Address = address
	cfg.Scheme = scheme
	cfg.PathPrefix = pathPrefix
	cfg.Datacenter = strings.TrimSpace(options.Datacenter)
	cfg.Namespace = strings.TrimSpace(options.Namespace)
	cfg.Token = strings.TrimSpace(options.Token)
	cfg.WaitTime = chooseConsulWaitTime(options.WaitTimeMs)

	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, errorForConsulSDKClientCreateFailed(messagef("create consul api client failed"), err)
	}
	return &consulAPIClient{
		client:     client,
		httpClient: cfg.HttpClient,
		waitTime:   chooseConsulWaitTime(options.WaitTimeMs),
	}, nil
}

// validateConsulWaitTime 拒绝转换为 time.Duration 时会溢出的毫秒值。
func validateConsulWaitTime(waitTimeMs uint64) error {
	const maxDurationMillis = uint64(1<<63-1) / uint64(time.Millisecond)
	if waitTimeMs > maxDurationMillis {
		return errorForConsulSDKInvalidInput(messagef("consul waitTimeMs out of range value=%d max=%d", waitTimeMs, maxDurationMillis))
	}
	return nil
}

// consulAPIClient 把 Consul SDK 适配为 Consul Client 接口。
type consulAPIClient struct {
	client     *api.Client
	httpClient idleConnectionCloser
	waitTime   time.Duration
}

// idleConnectionCloser 描述 Consul HTTP Client 的最小资源释放能力。
type idleConnectionCloser interface {
	// CloseIdleConnections 关闭当前 Client Transport 的空闲连接。
	CloseIdleConnections()
}

// Close 释放 Consul Client 持有的空闲 HTTP 连接；活动请求由 watcher context 先行取消。
func (this *consulAPIClient) Close() error {
	if this == nil || this.httpClient == nil {
		return nil
	}
	this.httpClient.CloseIdleConnections()
	return nil
}

// GetConfig 从 Consul KV 拉取指定路径配置。
//
// 参数：
// - ctx: 请求上下文，支持超时/取消。
// - groupID: 路径前缀（会映射为 KV path 前半段）。
// - dataID: 路径 key（会映射为 KV path 后半段）。
//
// 返回：
// - []byte: KV value 原始字节。
// - error: 拉取失败或 key 不存在时返回错误。
func (this *consulAPIClient) GetConfig(ctx context.Context, groupID, dataID string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := buildConsulKVPath(groupID, dataID)
	if err != nil {
		return nil, err
	}

	query := (&api.QueryOptions{}).WithContext(ctx)
	pair, _, err := this.client.KV().Get(path, query)
	if err != nil {
		return nil, err
	}
	if pair == nil {
		return nil, errorForConsulSDKInvalidInput(messagef("consul key not found path=" + path))
	}
	return append([]byte(nil), pair.Value...), nil
}

// WatchConfig 通过 Consul blocking query 监听指定 KV。
//
// 参数：
// - ctx: 监听上下文；取消后停止轮询。
// - groupID: 路径前缀。
// - dataID: 路径 key。
// - onChange: 内容变化回调。
//
// 返回：
// - io.Closer: 用于主动停止监听的句柄（幂等）。
// - error: 参数非法时返回错误。
func (this *consulAPIClient) WatchConfig(ctx context.Context, groupID, dataID string, onChange func(content []byte)) (io.Closer, error) {
	if onChange == nil {
		return nil, errorForConsulSDKInvalidInput(messagef("onChange callback is required"))
	}
	path, err := buildConsulKVPath(groupID, dataID)
	if err != nil {
		return nil, err
	}

	watchCtx, cancel := context.WithCancel(ctx)
	go this.watchLoop(watchCtx, path, onChange)
	return &consulWatchCloser{cancel: cancel}, nil
}

// watchLoop 持续执行阻塞查询并在内容变化时触发回调。
//
// 语义：
// 1. 首次拉取仅建立基线，不触发回调；
// 2. 仅在 LastIndex 变化且内容有差异时触发 onChange；
// 3. 查询异常时按固定退避重试。
func (this *consulAPIClient) watchLoop(ctx context.Context, path string, onChange func(content []byte)) {
	var (
		lastIndex   uint64
		lastContent []byte
		hasBaseline bool
	)
	for {
		if err := ctx.Err(); err != nil {
			return
		}

		query := (&api.QueryOptions{
			WaitIndex: lastIndex,
			WaitTime:  this.waitTime,
		}).WithContext(ctx)

		pair, meta, err := this.client.KV().Get(path, query)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(watchRetryDelay)
			continue
		}

		current := []byte(nil)
		if pair != nil {
			current = append([]byte(nil), pair.Value...)
		}
		currentIndex := uint64(0)
		if meta != nil {
			currentIndex = meta.LastIndex
		}

		if !hasBaseline {
			hasBaseline = true
			lastIndex = currentIndex
			lastContent = current
			continue
		}

		if currentIndex == lastIndex {
			continue
		}
		lastIndex = currentIndex
		if bytes.Equal(lastContent, current) {
			continue
		}
		lastContent = current
		onChange(current)
	}
}

// consulWatchCloser 负责幂等停止 watch goroutine。
type consulWatchCloser struct {
	cancel context.CancelFunc
	once   sync.Once
}

// Close 终止 watch。
func (this *consulWatchCloser) Close() error {
	this.once.Do(func() {
		if this.cancel != nil {
			this.cancel()
		}
	})
	return nil
}

// chooseConsulWaitTime 选择阻塞查询等待时长。
func chooseConsulWaitTime(waitTimeMs uint64) time.Duration {
	if waitTimeMs == 0 {
		return DefaultWatchWaitTime
	}
	return time.Duration(waitTimeMs) * time.Millisecond
}

// normalizeConsulAddress 归一化地址与 scheme/pathPrefix。
//
// 返回：
// - address: 传给 Consul SDK 的 host:port。
// - scheme: 最终协议（显式参数优先）。
// - pathPrefix: URL path 前缀（可为空）。
// - error: 地址非法时返回错误。
func normalizeConsulAddress(rawAddress string, preferredScheme string) (string, string, string, error) {
	rawAddress = strings.TrimSpace(rawAddress)
	if rawAddress == "" {
		return "", "", "", errorForConsulSDKInvalidInput(messagef("consul address is required"))
	}
	preferredScheme = strings.TrimSpace(preferredScheme)

	if strings.Contains(rawAddress, "://") {
		parsed, err := url.Parse(rawAddress)
		if err != nil {
			return "", "", "", errorForConsulSDKInvalidInput(messagef("parse consul address failed"), err)
		}
		if strings.TrimSpace(parsed.Host) == "" {
			return "", "", "", errorForConsulSDKInvalidInput(messagef("invalid consul address " + rawAddress + ": empty host"))
		}
		scheme := strings.TrimSpace(parsed.Scheme)
		if preferredScheme != "" {
			scheme = preferredScheme
		}
		if scheme == "" {
			scheme = "http"
		}
		pathPrefix := strings.TrimSpace(parsed.Path)
		if pathPrefix == "/" {
			pathPrefix = ""
		}
		return strings.TrimSpace(parsed.Host), scheme, pathPrefix, nil
	}

	scheme := preferredScheme
	if scheme == "" {
		scheme = "http"
	}
	return rawAddress, scheme, "", nil
}

// buildConsulKVPath 把 Source 的 group/data 映射为 Consul KV path。
//
// 必填：groupID 与 dataID 均不能为空。
func buildConsulKVPath(groupID, dataID string) (string, error) {
	groupID = strings.Trim(strings.TrimSpace(groupID), "/")
	dataID = strings.Trim(strings.TrimSpace(dataID), "/")
	if groupID == "" || dataID == "" {
		return "", errorForConsulSDKInvalidInput(messagef("consul source requires non-empty groupID and dataID"))
	}
	return groupID + "/" + dataID, nil
}
