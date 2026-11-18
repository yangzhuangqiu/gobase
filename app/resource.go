package app

import "github.com/yangzhuangqiu/gobase/ioc"

// Resource 定义由 IOC 按依赖拓扑逆序释放的被动资源合同。
//
// 典型调用场景：数据库连接池、消息客户端、缓存客户端以及已停止后的 Server 底层资源。
// Resource 是 ioc.Closer 的语义别名；所有权仍唯一归 IOC，app 不建立第二套资源注册表。
type Resource = ioc.Closer
