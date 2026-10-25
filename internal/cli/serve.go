package cli

import (
	"github.com/spf13/cobra"

	"github.com/HugehardJiang/buugoo/internal/app"
)

func newServeCmd(g *globalOpts) *cobra.Command {
	var listen string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "启动服务（含 Web 界面）",
		Long:  "启动布谷核心服务：HTTP API、Web 界面与后台任务。启动时会自动执行数据库迁移。",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(g)
			if err != nil {
				return err
			}
			if listen != "" {
				cfg.Server.Listen = listen
			}
			log, err := newLogger(cfg)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			a, err := app.New(ctx, cfg, log)
			if err != nil {
				return err
			}
			defer a.Close()
			setupBackground(a)
			return a.Run(ctx)
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "", "覆盖配置中的监听地址，例如 0.0.0.0:8080")
	return cmd
}
