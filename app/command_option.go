package app

// CommandOption 定义只作用于单个子命令节点的选项契约。
// 典型调用场景：NewCommand 接收 NodeOption 与子命令专用的继承控制选项。
type CommandOption interface {
	// applyCommand 在节点挂载前修改局部命令声明。
	applyCommand(command *commandConfiguration)
}
