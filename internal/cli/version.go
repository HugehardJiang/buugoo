package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/HugehardJiang/buugoo/internal/buildinfo"
)

func newVersionCmd() *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "查看版本",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if short {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), buildinfo.Version)
				return err
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), buildinfo.String())
			return err
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "只输出版本号")
	return cmd
}
