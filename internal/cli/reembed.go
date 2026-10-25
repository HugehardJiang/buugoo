package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/HugehardJiang/buugoo/internal/llm/provider"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
	"github.com/HugehardJiang/buugoo/internal/vector"
)

func newReembedCmd(g *globalOpts) *cobra.Command {
	var all, dryRun bool
	var batch int
	cmd := &cobra.Command{
		Use:   "reembed",
		Short: "用当前配置的向量模型重新计算记忆向量",
		Long: `更换 embedding 模型后，旧模型生成的向量无法与新模型比较（检索时只比较同一模型的向量），
需要用新模型重新计算。默认只处理不是由当前模型生成的向量；--all 重新计算全部。

建议步骤：停止服务 → 修改配置中的 llm.embedding → 执行 buugoo reembed → 启动服务。`,
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
			emb, err := provider.NewEmbedding(cfg.LLM.Embedding)
			if err != nil {
				return err
			}
			model := emb.Model()
			out := cmd.OutOrStdout()

			counts, err := db.Memories().CountByModel(ctx)
			if err != nil {
				return err
			}
			models := make([]string, 0, len(counts))
			for m := range counts {
				models = append(models, m)
			}
			sort.Strings(models)
			fmt.Fprintf(out, "当前向量模型：%s\n现有记忆分布：\n", model)
			var pending int64
			for _, m := range models {
				name := m
				if name == "" {
					name = "（无向量）"
				}
				fmt.Fprintf(out, "  %-40s %d 条\n", name, counts[m])
				if all || m != model {
					pending += counts[m]
				}
			}
			fmt.Fprintf(out, "需要重新计算：%d 条\n", pending)
			if dryRun || pending == 0 {
				return nil
			}

			var afterID uint64
			done, failed := 0, 0
			for {
				mems, err := db.Memories().ListForReembed(ctx, afterID, model, !all, batch)
				if err != nil {
					return err
				}
				if len(mems) == 0 {
					break
				}
				afterID = mems[len(mems)-1].ID
				texts := make([]string, len(mems))
				for i, m := range mems {
					texts[i] = m.Content
				}
				vecs, err := emb.Embed(ctx, texts)
				if err != nil {
					return fmt.Errorf("计算向量（已完成 %d 条，可重新执行继续）: %w", done, err)
				}
				for i, m := range mems {
					v, err := vector.Normalize(vecs[i])
					if err != nil {
						failed++
						log.Warn("向量无效，跳过", "memory_id", m.ID, "err", err)
						continue
					}
					if err := db.Memories().UpdateEmbedding(ctx, m.ID, vector.Encode(v), model, len(v)); err != nil {
						return err
					}
					done++
				}
				fmt.Fprintf(out, "\r已完成 %d / %d", done, pending)
			}
			fmt.Fprintf(out, "\n完成：成功 %d 条，失败 %d 条。如服务正在运行，请重启服务以刷新向量缓存。\n", done, failed)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "重新计算全部记忆的向量（包括当前模型生成的）")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "只统计，不计算")
	cmd.Flags().IntVar(&batch, "batch", 32, "每批处理的记忆数量")
	return cmd
}
