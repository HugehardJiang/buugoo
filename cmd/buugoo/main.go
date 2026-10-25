// Buugoo（布谷）：一个仿照人类记忆机制设计的 AI 朋友。
package main

import (
	"os"

	"github.com/HugehardJiang/buugoo/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
