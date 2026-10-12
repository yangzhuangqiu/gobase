// Package ioc 提供轻量级 IoC 容器。
//
// 语义：
// 1. 通过 Register 直接注册对象，或通过 Define/RegisterDefinition 消费模块默认装配策略；
// 2. 通过 Build(ctx) 显式执行依赖注入与初始化；
// 3. 通过 Resolve/ResolveAll/ResolveByName 获取依赖；
// 4. 通过 Close(ctx) 收敛生命周期资源，并让并发/重复调用共享首次结果；
// 5. 通过 WithLogger 注入 gobase/log 日志实现；
// 6. 通过 WithAllowCircularDependency 配置循环依赖策略；
// 7. Init 返回错误时固定停止、逆序回滚成功项并由 Build 返回 error；
// 8. 通过 Metrics 获取运行期可观测指标；
// 9. 通过 WithBorrowed 声明外部所有权，borrowed 对象默认不注入字段；
// 10. 通过 ResolveAs[T] 等泛型辅助降低反射使用成本。
package ioc
