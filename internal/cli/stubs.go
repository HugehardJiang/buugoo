package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

var errNotImplemented = errors.New("尚未实现")

func stub(use, short string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, RunE: func(*cobra.Command, []string) error { return errNotImplemented }}
}

func newServeCmd(*globalOpts) *cobra.Command {
	return stub("serve", "启动服务（含 Web 界面）")
}
func newChatCmd(*globalOpts) *cobra.Command      { return stub("chat", "终端聊天") }
func newUpdateCmd(*globalOpts) *cobra.Command    { return stub("update", "自更新") }
func newUninstallCmd(*globalOpts) *cobra.Command { return stub("uninstall", "卸载") }
