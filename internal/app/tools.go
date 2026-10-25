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
//   - 联网搜索与读取网页：按 tools.search 选择 DeepSeek（使用当前模型的 Key）、博查或 Tavily；
//     搜索服务不能读网页时由服务器直接读取（direct_fetch）；
//   - 运行 Python：Docker 沙箱可用时所有用户可用；本机 Python 只在 single 模式或管理员可用；
//   - 安装 Python 库：开启 allow_install 时，single 模式或管理员的对话可用。
type toolProvider struct {
	cfg    *config.Config
	log    *slog.Logger
	docker *tools.Docker
	local  *tools.Local
	direct *tools.Direct
}

func newToolProvider(cfg *config.Config, log *slog.Logger) *toolProvider {
	p := &toolProvider{cfg: cfg, log: log, direct: tools.NewDirect()}
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

// search 选择联网搜索与读取网页的服务。
func (p *toolProvider) search(model *models.Resolved) (tools.Searcher, tools.Fetcher) {
	sc := p.cfg.Tools.Search
	var searcher tools.Searcher
	var fetcher tools.Fetcher
	use := func(s tools.Searcher) {
		if s == nil || searcher != nil {
			return
		}
		searcher = s
		if f, ok := s.(tools.Fetcher); ok {
			fetcher = f
		}
	}
	// 接口的值为 nil 指针时也不能放进 interface，所以逐个判断。
	deepseek := func() {
		if ds := tools.NewDeepSeek(model.BaseURL, model.APIKey); ds != nil {
			use(ds)
		}
	}
	bocha := func() {
		if b := tools.NewBocha(sc.BochaAPIKey); b != nil {
			use(b)
		}
	}
	tavily := func() {
		if t := tools.NewTavily(sc.TavilyAPIKey); t != nil {
			use(t)
		}
	}
	switch sc.Provider {
	case config.SearchOff:
	case config.SearchDeepSeek:
		deepseek()
	case config.SearchBocha:
		bocha()
	case config.SearchTavily:
		tavily()
	default:
		deepseek()
		bocha()
		tavily()
	}
	// 博查不能读网页：如果也配了 Tavily，用 Tavily 读。
	if fetcher == nil && sc.Provider != config.SearchOff {
		if t := tools.NewTavily(sc.TavilyAPIKey); t != nil {
			fetcher = t
		}
	}
	if fetcher == nil && sc.DirectFetch && sc.Provider != config.SearchOff {
		fetcher = p.direct
	}
	return searcher, fetcher
}

// For 实现 chat.ToolProvider。
func (p *toolProvider) For(ctx context.Context, u *store.User, model *models.Resolved) *tools.Set {
	opts := tools.Options{}
	opts.Search, opts.Fetch = p.search(model)
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
	if opts.Search == nil && opts.Fetch == nil && opts.Python == nil {
		return nil
	}
	return tools.New(opts)
}
