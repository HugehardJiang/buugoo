package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/logx"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func newMigrateCmd(g *globalOpts) *cobra.Command {
	var status bool
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "执行数据库迁移",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(g)
			if err != nil {
				return err
			}
			log, err := newLogger(cfg)
			if err != nil {
				return err
			}
			db, err := gormstore.Open(cfg.Database, log)
			if err != nil {
				return err
			}
			defer db.Close()
			out := cmd.OutOrStdout()
			if status {
				st, err := db.Status(cmd.Context())
				if err != nil {
					return err
				}
				for _, m := range st {
					mark := "未执行"
					if m.Applied {
						mark = "已执行"
					}
					fmt.Fprintf(out, "%-32s %s\n", m.ID, mark)
				}
				return nil
			}
			if err := db.Migrate(cmd.Context()); err != nil {
				return err
			}
			fmt.Fprintf(out, "数据库迁移完成（%s）\n", cfg.Database.Driver)
			return nil
		},
	}
	cmd.Flags().BoolVar(&status, "status", false, "只查看迁移状态")
	return cmd
}

func newLogger(cfg *config.Config) (*slog.Logger, error) {
	return logx.New(os.Stderr, cfg.Log.Level, cfg.Log.Format)
}
