package app

import (
	"context"
	"log/slog"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/tools"
)

// toolProvider 按模型与用户决定一轮对话可用的工具：
//   - 联网搜索与读取网页：当前模型是 DeepSeek 时提供，使用该模型的 Key；
//   - 运行 Python：Docker 沙箱可用时所有用户可用；本机 Python 只在 single 模式或管理员可用；
//   - 安装 Python 库：开启 allow_install 时，single 模式或管理员的对话可用。
type toolProvider struct {
	cfg    *config.Config
	log    *slog.Logger
	docker *tools.Docker
	local  *tools.Local
}

func newToolProvider(cfg *config.Config, log *slog.Logger) *toolProvider {
	p := &toolProvider{cfg: cfg, log: log}
	switch cfg.Tools.Python.Runner {
	case config.PythonDocker:
		p.docker = tools.NewDocker(cfg.Tools.Python, log)
	case config.PythonLocal:
		p.local = tools.NewLocal(cfg.Tools.Python, log)
	}
	return p
}

// prepare 在启动时检查运行环境（拉取镜像），在后台执行。
func (p *toolProvider) prepare(ctx context.Context) {
	switch {
	case p.docker != nil:
		p.docker.Prepare(ctx)
	case p.local != nil && !p.local.Available():
		p.log.Warn("找不到 Python 解释器，运行代码功能不可用", "python", p.cfg.Tools.Python.Python)
	case p.local != nil:
		p.local.Prepare(ctx)
	}
}

func (p *toolProvider) python(ctx context.Context, u *store.User) tools.Runner {
	switch {
	case p.docker != nil && p.docker.Available(ctx):
		return p.docker
	case p.local != nil && (p.cfg.Mode == config.ModeSingle || u.IsAdmin) && p.local.Available():
		return p.local
	}
	return nil
}

// For 实现 chat.ToolProvider。
func (p *toolProvider) For(ctx context.Context, u *store.User, model *models.Resolved) *tools.Set {
	opts := tools.Options{DeepSeek: tools.NewDeepSeek(model.BaseURL, model.APIKey)}
	if r := p.python(ctx, u); r != nil {
		opts.Python = r
		opts.Packages = p.cfg.Tools.Python.Packages
		opts.CJKFont = tools.HasCJKFont()
		// 安装库需要联网：single 模式或管理员的对话才可以。
		if p.cfg.Tools.Python.AllowInstall && (p.cfg.Mode == config.ModeSingle || u.IsAdmin) {
			if in, ok := r.(tools.Installer); ok {
				opts.Installer = in
			}
		}
	}
	if opts.DeepSeek == nil && opts.Python == nil {
		return nil
	}
	return tools.New(opts)
}
