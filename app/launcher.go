package app

import (
	"fmt"
	"os"
)

// Run 创建并执行一个使用进程参数的命令行应用。
//
// 典型调用场景：main 函数需要保留错误处理控制权时，直接传入全部 Option 并处理返回错误。
func Run(options ...Option) error {
	return New(options...).Run()
}

// Main 创建并执行进程级命令行应用，失败时写入错误输出并以状态码 1 退出。
//
// 典型调用场景：应用不需要自定义错误映射，main 函数只负责声明启动选项。
func Main(options ...Option) {
	application := New(options...)
	if err := application.Run(); err != nil {
		output := application.errorOutput
		if output == nil {
			output = os.Stderr
		}
		_, _ = fmt.Fprintln(output, err)
		os.Exit(1)
	}
}
