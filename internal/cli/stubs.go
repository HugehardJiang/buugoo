package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

var errNotImplemented = errors.New("尚未实现")

func stub(use, short string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, RunE: func(*cobra.Command, []string) error { return errNotImplemented }}
}

func newUpdateCmd(*globalOpts) *cobra.Command    { return stub("update", "自更新") }
func newUninstallCmd(*globalOpts) *cobra.Command { return stub("uninstall", "卸载") }
