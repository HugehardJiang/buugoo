// Buugoo（布谷）：一个仿照人类记忆机制设计的 AI 朋友。
package main

import (
	"os"
	// 内嵌时区数据库，保证 Windows 与精简容器中也能解析 Asia/Shanghai 等时区。
	_ "time/tzdata"

	"github.com/HugehardJiang/buugoo/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
