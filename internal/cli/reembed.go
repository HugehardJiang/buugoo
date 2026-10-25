package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/HugehardJiang/buugoo/internal/memory"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/secret"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func newReembedCmd(g *globalOpts) *cobra.Command {
	var all, dryRun bool
	var batch int
	cmd := &cobra.Command{
		Use:   "reembed",
		Short: "用当前的向量模型重新计算记忆向量",
		Long: `更换 embedding 模型后，旧模型生成的向量无法与新模型比较（检索时只比较同一模型的向量），
需要用新模型重新计算。默认只处理不是由当前模型生成的向量；--all 重新计算全部。

当前向量模型以管理员在网页「设置 → 向量模型」中保存的为准，没有保存过则使用配置文件中的 llm.embedding。
在网页中修改向量模型后也可以直接在网页上重新计算，无需使用本命令。

使用本命令的建议步骤：停止服务 → 修改配置中的 llm.embedding → 执行 buugoo reembed → 启动服务。`,
		Args: cobra.NoArgs,
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
			ctx := cmd.Context()
			if err := db.Migrate(ctx); err != nil {
				return err
			}
			box, _, err := secret.Load(cfg.Security.EncryptionKey, cfg.DataDir)
			if err != nil {
				return err
			}
			svc, err := models.NewService(ctx, db, box, cfg, log)
			if err != nil {
				return err
			}
			emb := svc.Embedding()
			r := memory.NewReembedder(db.Memories(), emb, nil, log)
			out := cmd.OutOrStdout()

			pending, counts, err := r.Pending(ctx, all)
			if err != nil {
				return err
			}
			names := make([]string, 0, len(counts))
			for m := range counts {
				names = append(names, m)
			}
			sort.Strings(names)
			fmt.Fprintf(out, "当前向量模型：%s\n现有记忆分布：\n", emb.Model())
			for _, m := range names {
				name := m
				if name == "" {
					name = "（无向量）"
				}
				fmt.Fprintf(out, "  %-40s %d 条\n", name, counts[m])
			}
			fmt.Fprintf(out, "需要重新计算：%d 条\n", pending)
			if dryRun || pending == 0 {
				return nil
			}

			st, err := r.Run(ctx, all, batch, func(s memory.ReembedStatus) {
				fmt.Fprintf(out, "\r已完成 %d / %d", s.Done, s.Total)
			})
			if err != nil {
				return fmt.Errorf("已完成 %d 条: %w", st.Done, err)
			}
			fmt.Fprintf(out, "\n完成：成功 %d 条，失败 %d 条。如服务正在运行，请重启服务以刷新向量缓存。\n", st.Done, st.Failed)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "重新计算全部记忆的向量（包括当前模型生成的）")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "只统计，不计算")
	cmd.Flags().IntVar(&batch, "batch", 32, "每批处理的记忆数量")
	return cmd
}
