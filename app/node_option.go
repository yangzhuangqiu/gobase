package app

// NodeOption 定义 root 与子命令都支持的节点语义。
// 典型调用场景：描述、Bean Definitions、Module、Hook、FlagBinder 和子命令树使用同一套声明方式。
type NodeOption interface {
	Option
	CommandOption
}
