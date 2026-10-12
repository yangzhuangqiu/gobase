package ioc

import (
	"context"
	"fmt"
	"testing"
	"time"

	baselog "github.com/yangzhuangqiu/gobase/log"
)

type benchSilentLogger struct{}

// Enabled 关闭所有 benchmark 日志，隔离 IOC 主流程开销。
func (this *benchSilentLogger) Enabled(context.Context, baselog.Level) bool { return false }

// Log 丢弃显式级别事件。
func (this *benchSilentLogger) Log(context.Context, baselog.Level, string, ...baselog.Field) {}

// Debug 丢弃调试事件。
func (this *benchSilentLogger) Debug(context.Context, string, ...baselog.Field) {}

// Info 丢弃信息事件。
func (this *benchSilentLogger) Info(context.Context, string, ...baselog.Field) {}

// Warn 丢弃告警事件。
func (this *benchSilentLogger) Warn(context.Context, string, ...baselog.Field) {}

// Error 丢弃失败事件。
func (this *benchSilentLogger) Error(context.Context, string, ...baselog.Field) {}

// With 返回同一静默 Logger。
func (this *benchSilentLogger) With(...baselog.Field) baselog.Logger { return this }

type benchNoopBean struct{}

type benchRepo interface {
	ID() string
}

type benchRepoImpl struct {
	id string
}

func (this *benchRepoImpl) ID() string {
	return this.id
}

type benchConsumer struct {
	Repo0  benchRepo
	Repo1  benchRepo
	All    []benchRepo
	AllMap map[string]benchRepo
}

type benchFastCloser struct{}

func (this *benchFastCloser) Close(context.Context) error {
	return nil
}

type benchBlockingCloser struct {
	release <-chan struct{}
}

func (this *benchBlockingCloser) Close(context.Context) error {
	<-this.release
	return nil
}

// BenchmarkBuildLargeBeanGraph 基准评估“大规模 bean 装配”的 Build 开销。
func BenchmarkBuildLargeBeanGraph(b *testing.B) {
	sizes := []int{100, 500, 1000}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("beans_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for index := 0; index < b.N; index++ {
				container := New(WithLogger(&benchSilentLogger{}))
				for i := 0; i < size; i++ {
					if err := container.Register(&benchNoopBean{}, WithName(fmt.Sprintf("Noop%d", i)), WithInject(false)); err != nil {
						b.Fatalf("register failed: %v", err)
					}
				}
				if err := container.Build(context.Background()); err != nil {
					b.Fatalf("build failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkBuildReflectInjection 基准评估“反射注入阶段”在多 consumer 下的耗时。
func BenchmarkBuildReflectInjection(b *testing.B) {
	sizes := []int{100, 500, 1000}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("consumers_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for index := 0; index < b.N; index++ {
				container := New(WithLogger(&benchSilentLogger{}))
				for i := 0; i < 10; i++ {
					bean := &benchRepoImpl{id: fmt.Sprintf("repo-%d", i)}
					if err := container.Register(bean, WithName(fmt.Sprintf("Repo%d", i)), WithDeclare[benchRepo]()); err != nil {
						b.Fatalf("register repo failed: %v", err)
					}
				}
				for i := 0; i < size; i++ {
					if err := container.Register(&benchConsumer{}, WithName(fmt.Sprintf("Consumer%d", i))); err != nil {
						b.Fatalf("register consumer failed: %v", err)
					}
				}
				if err := container.Build(context.Background()); err != nil {
					b.Fatalf("build failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkCloseTimeoutOverhead 基准评估 Close 超时路径带来的额外开销。
func BenchmarkCloseTimeoutOverhead(b *testing.B) {
	const fastBeanCount = 200
	const timeoutBeanCount = 10
	b.Run("fast_close_without_timeout", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			container := New(WithLogger(&benchSilentLogger{}), WithCloseTimeout(0))
			for i := 0; i < fastBeanCount; i++ {
				if err := container.Register(&benchFastCloser{}, WithName(fmt.Sprintf("FastCloser%d", i))); err != nil {
					b.Fatalf("register fast closer failed: %v", err)
				}
			}
			if err := container.Build(context.Background()); err != nil {
				b.Fatalf("build failed: %v", err)
			}
			if err := container.Close(context.Background()); err != nil {
				b.Fatalf("close failed: %v", err)
			}
		}
	})
	b.Run("blocking_close_with_timeout", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			release := make(chan struct{})
			container := New(WithLogger(&benchSilentLogger{}), WithCloseTimeout(2*time.Millisecond))
			for i := 0; i < timeoutBeanCount; i++ {
				bean := &benchBlockingCloser{release: release}
				if err := container.Register(bean, WithName(fmt.Sprintf("BlockingCloser%d", i))); err != nil {
					b.Fatalf("register blocking closer failed: %v", err)
				}
			}
			if err := container.Build(context.Background()); err != nil {
				b.Fatalf("build failed: %v", err)
			}
			_ = container.Close(context.Background())
			close(release)
		}
	})
}
