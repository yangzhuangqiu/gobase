package config

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

// benchmarkSubscriber 用于 refresh 基准中的最小订阅回调开销模拟。
type benchmarkSubscriber struct {
	reloads atomic.Int64
}

// Scope 返回空范围，表示监听整份配置。
func (this *benchmarkSubscriber) Scope() string {
	return ""
}

// OnUpdate 仅做计数，避免引入额外业务逻辑噪声。
func (this *benchmarkSubscriber) OnUpdate(Update) error {
	this.reloads.Add(1)
	return nil
}

// BenchmarkCurrentGet 衡量高频读取路径（Current + Get/String）的吞吐与分配。
func BenchmarkCurrentGet(b *testing.B) {
	manager := newDefaultManager()
	if err := manager.installInitial(snapshotState{
		raw: map[string]any{
			"service": map[string]any{"address": ":8080"},
			"worker":  map[string]any{"concurrency": 16},
		},
		meta: map[string]ValueMeta{
			"service.address":    {Source: SourceKindDefault, Layer: "default", Priority: 100},
			"worker.concurrency": {Source: SourceKindDefault, Layer: "default", Priority: 100},
		},
	}); err != nil {
		b.Fatalf("install initial snapshot failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snapshot := manager.Current()
		if snapshot == nil {
			b.Fatal("expected non-nil snapshot")
		}
		if _, ok := snapshot.Get("worker.concurrency"); !ok {
			b.Fatal("expected worker.concurrency key")
		}
		if snapshot.String("service.address", "") == "" {
			b.Fatal("expected service.address value")
		}
	}
}

// BenchmarkRefresh 衡量 refresh 在无订阅与有订阅场景下的耗时与分配。
func BenchmarkRefresh(b *testing.B) {
	b.Run("no_subscriber", func(b *testing.B) {
		manager := newDefaultManager()
		if err := manager.installInitial(snapshotState{raw: map[string]any{"service": map[string]any{"address": ":8080"}}}); err != nil {
			b.Fatalf("install initial snapshot failed: %v", err)
		}

		var versionCounter atomic.Int64
		manager.setRebuild(func(ctx context.Context) (snapshotState, error) {
			n := versionCounter.Add(1)
			return snapshotState{
				raw: map[string]any{
					"service": map[string]any{"address": fmt.Sprintf(":%d", 8080+n)},
				},
			}, nil
		})

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := manager.refresh(context.Background()); err != nil {
				b.Fatalf("refresh failed: %v", err)
			}
		}
	})

	b.Run("with_subscriber", func(b *testing.B) {
		manager := newDefaultManager()
		if err := manager.installInitial(snapshotState{raw: map[string]any{"service": map[string]any{"address": ":8080"}}}); err != nil {
			b.Fatalf("install initial snapshot failed: %v", err)
		}

		subscriber := &benchmarkSubscriber{}
		if _, err := manager.Subscribe(subscriber); err != nil {
			b.Fatalf("subscribe failed: %v", err)
		}

		var versionCounter atomic.Int64
		manager.setRebuild(func(ctx context.Context) (snapshotState, error) {
			n := versionCounter.Add(1)
			return snapshotState{
				raw: map[string]any{
					"service": map[string]any{"address": fmt.Sprintf(":%d", 9080+n)},
				},
			}, nil
		})

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := manager.refresh(context.Background()); err != nil {
				b.Fatalf("refresh failed: %v", err)
			}
		}
	})
}
