package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/HugehardJiang/buugoo/internal/app"
	"github.com/HugehardJiang/buugoo/internal/buildinfo"
	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/paths"
	"github.com/HugehardJiang/buugoo/internal/selfupdate"
	"github.com/HugehardJiang/buugoo/internal/service"
)

type updateOpts struct {
	selfupdate.Options
	rollback bool
	yes      bool
	mirror   string
}

func newUpdateCmd(g *globalOpts) *cobra.Command {
	o := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update",
		Short: "更新到最新版本（或指定版本），失败自动回滚",
		Long: `检查并更新 buugoo。

流程：下载新版本并校验 SHA256 → 备份数据库 → 停止服务 → 替换二进制（旧版本保留为 .prev）
→ 执行数据库迁移 → 启动服务并检查健康状态。任一步失败会自动恢复旧二进制和数据库备份。

下载地址默认是 GitHub Releases；国内网络可用 --mirror 或环境变量 BUUGOO_MIRROR 指定镜像。
Docker 部署请使用 docker compose pull && docker compose up -d。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runUpdate(cmd, g, o) },
	}
	f := cmd.Flags()
	f.BoolVar(&o.CheckOnly, "check", false, "只检查是否有新版本，不更新")
	f.StringVar(&o.Version, "version", "", "更新到指定版本，例如 v0.2.0")
	f.BoolVar(&o.Force, "force", false, "即使已是该版本也重新安装")
	f.BoolVar(&o.rollback, "rollback", false, "回滚到上一个版本")
	f.BoolVar(&o.RestoreDB, "restore-db", false, "回滚时同时恢复更新前的数据库备份（不再询问）")
	f.BoolVarP(&o.yes, "yes", "y", false, "非交互模式，自动确认")
	f.StringVar(&o.mirror, "mirror", "", "下载镜像地址（默认 GitHub Releases）")
	return cmd
}

// installContext 汇总安装清单、配置与服务信息。
type installContext struct {
	manifest *paths.Manifest
	cfg      *config.Config
	binary   string
	svc      *service.Manager
}

func loadInstall(g *globalOpts) (*installContext, error) {
	ic := &installContext{}
	cfgPath := g.configPath
	m, err := paths.FindManifest(cfgPath)
	switch {
	case err == nil:
		ic.manifest = m
		if cfgPath == "" {
			cfgPath = m.ConfigFile()
		}
		ic.binary = m.Binary
		if m.Service {
			ic.svc = service.New(paths.ServiceName)
		}
	case errors.Is(err, paths.ErrNoManifest):
	default:
		return nil, err
	}
	if ic.binary == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		if ic.binary, err = filepath.EvalSymlinks(exe); err != nil {
			return nil, err
		}
	}
	cfg, err := loadConfig(&globalOpts{configPath: existing(cfgPath)})
	if err != nil {
		return nil, err
	}
	ic.cfg = cfg
	return ic, nil
}

func existing(p string) string {
	if p == "" {
		return ""
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

func runUpdate(cmd *cobra.Command, g *globalOpts, o *updateOpts) error {
	ic, err := loadInstall(g)
	if err != nil {
		return err
	}
	mirror := o.mirror
	if mirror == "" {
		mirror = ic.cfg.Update.Mirror
	}
	if mirror == "" && ic.manifest != nil {
		mirror = ic.manifest.Mirror
	}
	u := &selfupdate.Updater{
		Source:   selfupdate.NewSource(ic.cfg.Update.Repo, mirror),
		Current:  buildinfo.Version,
		Binary:   ic.binary,
		Manifest: ic.manifest,
		Config:   ic.cfg,
		Service:  ic.svc,
		Out:      cmd.OutOrStdout(),
		Confirm:  selfupdate.TerminalConfirm(cmd.InOrStdin(), cmd.ErrOrStderr(), o.yes),
		// 恢复数据库会丢失数据：-y 时不自动同意，需要显式加 --restore-db。
		ConfirmRestore: selfupdate.TerminalConfirm(cmd.InOrStdin(), cmd.ErrOrStderr(), false),
	}
	if o.yes {
		u.ConfirmRestore = func(msg string) bool {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s：非交互模式下不恢复（如需恢复请加 --restore-db）\n", msg)
			return false
		}
	}
	if ic.manifest == nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "提示：未找到安装清单，将直接替换 %s，且不会管理系统服务。\n", ic.binary)
	}
	if o.rollback {
		return u.Rollback(cmd.Context(), o.Options)
	}
	return u.Update(cmd.Context(), o.Options)
}

// setupBackground 挂接启动时的新版本检查。
func setupBackground(a *app.App) {
	cfg := a.Config
	if !cfg.Update.CheckOnStart || buildinfo.IsDev() {
		return
	}
	a.UpdateCheck = func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		latest, err := selfupdate.NewSource(cfg.Update.Repo, cfg.Update.Mirror).Latest(ctx)
		if err != nil {
			a.Log.Debug("检查新版本失败", "err", err)
			return
		}
		if selfupdate.Newer(latest, buildinfo.Version) {
			a.Log.Info("有新版本可用，运行 buugoo update 更新（可在配置中关闭此提示：update.check_on_start）",
				slog.String("current", buildinfo.Version), slog.String("latest", latest))
		}
	}
}
