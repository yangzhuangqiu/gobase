package app

// Option 定义只作用于 App 进程级构建的选项契约。
// 典型调用场景：New/Run/Main 接收进程默认值、Runtime 蓝图或 NodeOption。
type Option interface {
	// applyApp 在命令树冻结前修改 App 声明。
	applyApp(app *App)
}
