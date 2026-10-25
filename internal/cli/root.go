// Package cli 定义 buugoo 的全部子命令。
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/paths"
)

// globalOpts 是所有子命令共享的参数。
type globalOpts struct {
	configPath string
}

// exitError 携带指定的退出码。
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// Execute 运行根命令并返回进程退出码。
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := newRootCmd()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		var ee *exitError
		if errors.As(err, &ee) {
			return ee.code
		}
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	g := &globalOpts{}
	root := &cobra.Command{
		Use:           "buugoo",
		Short:         "布谷 —— 记得你的 AI 朋友",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&g.configPath, "config", "c", "", "配置文件路径（默认依次查找 $BUUGOO_CONFIG、./config.yaml、用户配置目录、系统配置目录）")

	root.AddCommand(
		newServeCmd(g),
		newChatCmd(g),
		newMigrateCmd(g),
		newInitCmd(g),
		newVersionCmd(),
		newUpdateCmd(g),
		newUninstallCmd(g),
		newReembedCmd(g),
		newAdminCmd(g),
	)
	return root
}

// loadConfig 查找并加载配置文件。没有配置文件时使用默认值和环境变量，
// 数据目录回退到当前用户的默认数据目录。
func loadConfig(g *globalOpts) (*config.Config, error) {
	path, err := paths.FindConfig(g.configPath)
	if err != nil {
		return nil, err
	}
	fallback := ""
	if l, err := paths.Default(paths.ScopeUser); err == nil {
		fallback = l.DataDir
	}
	return config.Load(path, fallback)
}
