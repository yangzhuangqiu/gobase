package nacos

import (
	"context"
	"io"
	"sync"

	"github.com/yangzhuangqiu/gobase/config/provider"
)

// session 把 Nacos Client、厂商 Source 定位和资源所有权收敛为一次运行会话。
type session struct {
	// client 是本会话独占使用的 Nacos 访问入口。
	client Client
	// sources 是 Open 阶段固化的 Nacos Source 集合。
	sources []BootstrapSource
	// owned 表示 Client 是否由 Provider 创建并应随 Session 关闭。
	owned bool
	// closeOnce 保证重复关闭不会重复释放底层资源。
	closeOnce sync.Once
	// closeErr 保存首次关闭结果，供后续 Close 稳定返回。
	closeErr error
}

// newSession 创建独立 Nacos 会话并复制 Source 声明。
func newSession(client Client, sources []BootstrapSource, owned bool) *session {
	return &session{client: client, sources: append([]BootstrapSource{}, sources...), owned: owned}
}

// Sources 返回本会话 Source 的独立接口切片。
func (this *session) Sources() []provider.Source {
	result := make([]provider.Source, 0, len(this.sources))
	for index := range this.sources {
		result = append(result, this.sources[index])
	}
	return result
}

// Load 拉取属于本会话的 Nacos Source。
func (this *session) Load(ctx context.Context, source provider.Source) ([]byte, error) {
	item, ok := source.(BootstrapSource)
	if !ok {
		return nil, errorForNacosInvalidSource(messagef("op=config_nacos.session.load result=failed stage=validate_source reason=foreign_source"))
	}
	return this.client.GetConfig(ctx, item.GroupID, item.DataID)
}

// Watch 监听属于本会话的 Nacos Source，并把厂商内容回调收敛为失效通知。
func (this *session) Watch(ctx context.Context, source provider.Source, onChange func()) (io.Closer, error) {
	item, ok := source.(BootstrapSource)
	if !ok {
		return nil, errorForNacosInvalidSource(messagef("op=config_nacos.session.watch result=failed stage=validate_source reason=foreign_source"))
	}
	if onChange == nil {
		return nil, errorForNacosInvalidSource(messagef("op=config_nacos.session.watch result=failed stage=validate_callback reason=on_change_required"))
	}
	return this.client.WatchConfig(ctx, item.GroupID, item.DataID, func(_ []byte) { onChange() })
}

// Close 释放 Provider 创建的 Nacos Client；显式注入的 borrowed Client 不由会话关闭。
func (this *session) Close() error {
	this.closeOnce.Do(func() {
		if !this.owned {
			return
		}
		this.closeErr = this.client.Close()
	})
	return this.closeErr
}
