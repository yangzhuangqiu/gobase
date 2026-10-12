package ioc

import (
	"context"
	"testing"

	"github.com/yangzhuangqiu/gobase/errcode"
)

type genericUser struct{}

type genericOrder struct{}

// GenericCache 是验证不同泛型闭包类型与同类型多实例的测试缓存。
type GenericCache[T any] struct {
	// Value 是测试用于区分不同泛型实例和业务角色的标识值。
	Value string
}

type genericCacheConsumer struct {
	// UserCache 验证 User 闭包类型按精确类型注入。
	UserCache *GenericCache[genericUser]
	// OrderCache 验证 Order 闭包类型按精确类型注入。
	OrderCache *GenericCache[genericOrder]
}

type genericNamedCacheConsumer struct {
	// Primary 验证同一闭包类型按业务名称分流。
	Primary *GenericCache[genericUser]
	// Replica 验证同一闭包类型按业务名称分流。
	Replica *GenericCache[genericUser]
	// Caches 验证 map key 与类型范围内的 bean 名称一致。
	Caches map[string]*GenericCache[genericUser]
}

type genericRepository[T any] interface {
	RepositoryID() string
}

// GenericMySQLRepository 是 genericRepository 的 MySQL 测试实现。
type GenericMySQLRepository[T any] struct{}

// RepositoryID 返回 MySQL 泛型仓储的测试标识。
func (this *GenericMySQLRepository[T]) RepositoryID() string {
	return "mysql"
}

// GenericRedisRepository 是 genericRepository 的 Redis 测试实现。
type GenericRedisRepository[T any] struct{}

// RepositoryID 返回 Redis 泛型仓储的测试标识。
func (this *GenericRedisRepository[T]) RepositoryID() string {
	return "redis"
}

type genericRepositoryConsumer struct {
	// GenericMySQLRepository 按默认名称选择 MySQL 泛型仓储。
	GenericMySQLRepository genericRepository[genericUser]
	// GenericRedisRepository 按默认名称选择 Redis 泛型仓储。
	GenericRedisRepository genericRepository[genericUser]
	// Repositories 验证泛型声明类型下的名称集合完整性。
	Repositories map[string]genericRepository[genericUser]
}

// TestGenericDefaultNameIsScopedByClosedType 验证泛型默认名称忽略类型参数且按闭包类型隔离。
//
// 测试方法：
// 1. 注册同一原始泛型类型的 User/Order 两个闭包实例，均不显式命名；
// 2. Build 后分别按精确类型和相同默认名称 GenericCache 解析；
// 3. 断言两个闭包类型可共存，并注入各自唯一候选。
func TestGenericDefaultNameIsScopedByClosedType(t *testing.T) {
	userCache := &GenericCache[genericUser]{Value: "user"}
	orderCache := &GenericCache[genericOrder]{Value: "order"}
	consumer := &genericCacheConsumer{}
	container := New()
	if err := container.Register(userCache); err != nil {
		t.Fatalf("register user cache failed: %v", err)
	}
	if err := container.Register(orderCache); err != nil {
		t.Fatalf("register order cache failed: %v", err)
	}
	if err := container.Register(consumer); err != nil {
		t.Fatalf("register consumer failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	resolvedUser, err := ResolveByNameAs[*GenericCache[genericUser]](container, "GenericCache")
	if err != nil {
		t.Fatalf("resolve user cache by default name failed: %v", err)
	}
	resolvedOrder, err := ResolveByNameAs[*GenericCache[genericOrder]](container, "GenericCache")
	if err != nil {
		t.Fatalf("resolve order cache by default name failed: %v", err)
	}
	if resolvedUser != userCache || resolvedOrder != orderCache {
		t.Fatalf("closed generic types should resolve their own bean")
	}
	if consumer.UserCache != userCache || consumer.OrderCache != orderCache {
		t.Fatalf("closed generic types should inject by exact type")
	}
}

// TestGenericMultipleInstancesResolveByRoleName 验证同一泛型闭包类型通过业务名称完成分流。
//
// 测试方法：
// 1. 注册两个 GenericCache[genericUser]，分别命名 Primary 和 Replica；
// 2. Build 后断言字段名选择对应实例，map 使用相同名称作为 key；
// 3. 使用泛型 ResolveByNameAs 验证主动解析与字段注入语义一致。
func TestGenericMultipleInstancesResolveByRoleName(t *testing.T) {
	primary := &GenericCache[genericUser]{Value: "primary"}
	replica := &GenericCache[genericUser]{Value: "replica"}
	consumer := &genericNamedCacheConsumer{}
	container := New()
	if err := container.Register(primary, WithName("Primary")); err != nil {
		t.Fatalf("register primary cache failed: %v", err)
	}
	if err := container.Register(replica, WithName("Replica")); err != nil {
		t.Fatalf("register replica cache failed: %v", err)
	}
	if err := container.Register(consumer); err != nil {
		t.Fatalf("register consumer failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	if consumer.Primary != primary || consumer.Replica != replica {
		t.Fatalf("role names should select matching closed generic bean")
	}
	if len(consumer.Caches) != 2 || consumer.Caches["Primary"] != primary || consumer.Caches["Replica"] != replica {
		t.Fatalf("generic map should use type-scoped bean names, got=%v", consumer.Caches)
	}
	resolved, err := ResolveByNameAs[*GenericCache[genericUser]](container, "Replica")
	if err != nil {
		t.Fatalf("resolve replica by name failed: %v", err)
	}
	if resolved != replica {
		t.Fatalf("resolve replica should return registered instance")
	}
}

// TestGenericMultipleImplementationsMatchDefaultName 验证泛型接口多实现按原始类型默认名称分流。
//
// 测试方法：
// 1. 注册 MySQL/Redis 泛型实现并声明为同一个 Repository[genericUser]；
// 2. 不设置 WithName，依赖具体原始类型名称作为默认名称；
// 3. Build 后断言字段、map 和 ResolveByNameAs 都选择正确实现。
func TestGenericMultipleImplementationsMatchDefaultName(t *testing.T) {
	mysql := &GenericMySQLRepository[genericUser]{}
	redis := &GenericRedisRepository[genericUser]{}
	consumer := &genericRepositoryConsumer{}
	container := New()
	if err := container.Register(mysql, WithDeclare[genericRepository[genericUser]]()); err != nil {
		t.Fatalf("register mysql repository failed: %v", err)
	}
	if err := container.Register(redis, WithDeclare[genericRepository[genericUser]]()); err != nil {
		t.Fatalf("register redis repository failed: %v", err)
	}
	if err := container.Register(consumer); err != nil {
		t.Fatalf("register consumer failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	if consumer.GenericMySQLRepository != mysql || consumer.GenericRedisRepository != redis {
		t.Fatalf("default raw type names should disambiguate generic implementations")
	}
	if len(consumer.Repositories) != 2 || consumer.Repositories["GenericMySQLRepository"] != mysql || consumer.Repositories["GenericRedisRepository"] != redis {
		t.Fatalf("repository map should use raw type default names, got=%v", consumer.Repositories)
	}
	resolved, err := ResolveByNameAs[genericRepository[genericUser]](container, "GenericRedisRepository")
	if err != nil {
		t.Fatalf("resolve generic repository by default name failed: %v", err)
	}
	if resolved != redis {
		t.Fatalf("resolve generic repository should return redis instance")
	}
}

// TestGenericNameConflictIsCheckedPerDeclareType 验证名称冲突按声明类型判断且失败注册不产生部分索引。
//
// 测试方法：
// 1. 为两个不同具体类型设置相同名称，并声明到同一个泛型接口；
// 2. 断言第二次注册因接口范围内名称重复而失败；
// 3. Build 后断言失败 Bean 的具体类型索引未被部分写入。
func TestGenericNameConflictIsCheckedPerDeclareType(t *testing.T) {
	container := New()
	if err := container.Register(
		&GenericMySQLRepository[genericUser]{},
		WithName("Repository"),
		WithDeclare[genericRepository[genericUser]](),
	); err != nil {
		t.Fatalf("register mysql repository failed: %v", err)
	}
	err := container.Register(
		&GenericRedisRepository[genericUser]{},
		WithName("Repository"),
		WithDeclare[genericRepository[genericUser]](),
	)
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCRegisterDuplicate) {
		t.Fatalf("same declare type and name should conflict, got=%v", err)
	}
	if container.Metrics().RegisterTotal != 1 {
		t.Fatalf("failed registration should not increase register total")
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build after rejected registration failed: %v", err)
	}
	_, err = ResolveAs[*GenericRedisRepository[genericUser]](container)
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCResolveNotFound) {
		t.Fatalf("rejected bean should not leave concrete type index, got=%v", err)
	}
}

// TestGenericSameClosedTypeRejectsDuplicateDefaultName 验证同一泛型闭包类型不能重复使用默认名称。
//
// 测试方法：连续注册两个 GenericCache[genericUser] 且不设置名称，断言第二次注册返回名称冲突。
func TestGenericSameClosedTypeRejectsDuplicateDefaultName(t *testing.T) {
	container := New()
	if err := container.Register(&GenericCache[genericUser]{Value: "first"}); err != nil {
		t.Fatalf("register first cache failed: %v", err)
	}
	err := container.Register(&GenericCache[genericUser]{Value: "second"})
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCRegisterDuplicate) {
		t.Fatalf("same closed type and default name should conflict, got=%v", err)
	}
}
