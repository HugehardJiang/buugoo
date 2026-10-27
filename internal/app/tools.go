package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/mcp"
	"github.com/HugehardJiang/buugoo/internal/memo"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/proactive"
	"github.com/HugehardJiang/buugoo/internal/skills"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/tools"
	"github.com/HugehardJiang/buugoo/internal/uploads"
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
	// files 与 attachments 用于准备对话的文件环境（运行代码时的 input/ 与工作目录），可以为空。
	files       *uploads.Service
	attachments store.AttachmentRepo
	// reminders 不为空时提供设提醒的工具。
	reminders store.ReminderRepo
	loc       *time.Location
	// skills 不为空时提供已安装的 skill。
	skills *skills.Manager
	// plugins 不为空时提供管理员安装的 MCP 插件。
	plugins *mcp.Manager
	// memos 不为空时提供读写备忘录的工具。
	memos *memo.Service
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

// workspace 准备这段对话运行代码的文件环境：上传过的文件（没有删除的）放进 input/，
// 工作目录保存在 <workspace_dir>/<用户>/<对话>，计入用户的文件空间，用满后不再保存改动。
func (p *toolProvider) workspace(ctx context.Context, u *store.User, conv *store.Conversation, pending []store.Attachment) (*tools.DirWorkspace, []string) {
	if p.files == nil || conv == nil || conv.ID == 0 {
		return nil, nil
	}
	dir := p.files.WorkspaceDir(u.ID, conv.ID)
	if dir == "" {
		return nil, nil
	}
	ws := &tools.DirWorkspace{Dir: dir, Staging: p.files.StagingDir(), Max: p.cfg.Tools.Python.WorkspaceMB << 20,
		CanSave: func(ctx context.Context) error {
			usage, err := p.files.Usage(ctx, u.ID)
			if err == nil && usage.Full() {
				err = errors.New("文件空间已经用满")
			}
			return err
		}}
	if list, err := p.attachments.ListByConversation(ctx, u.ID, conv.ID); err == nil {
		list = append(list, pending...)
		for i := range list {
			if list[i].DeletedAt == nil {
				ws.Inputs = append(ws.Inputs, tools.InputFile{Name: list[i].Filename, Path: p.files.Path(&list[i])})
			}
		}
	}
	return ws, tools.InputNames(ws.Inputs)
}

// For 实现 chat.ToolProvider。
func (p *toolProvider) For(ctx context.Context, u *store.User, conv *store.Conversation, model *models.Resolved, pending []store.Attachment) *tools.Set {
	opts := tools.Options{}
	opts.Search, opts.Fetch = p.search(model)
	var runner tools.Runner
	var ws tools.Workspace
	if r := p.python(ctx, u); r != nil {
		runner = r
		opts.Python = r
		opts.Packages = p.cfg.Tools.Python.Packages
		opts.CJKFont = tools.HasCJKFont()
		if dws, inputs := p.workspace(ctx, u, conv, pending); dws != nil {
			ws = dws
			opts.Workspace, opts.Inputs, opts.Saved = dws, inputs, tools.ListWorkspace(dws.Dir, 30)
		}
		// 安装库需要联网：single 模式或管理员的对话才可以。
		if p.cfg.Tools.Python.AllowInstall && (p.cfg.Mode == config.ModeSingle || u.IsAdmin) {
			if in, ok := r.(tools.Installer); ok {
				opts.Installer = in
			}
		}
	}
	if p.reminders != nil {
		opts.Extra = append(opts.Extra, proactive.ReminderTools(p.reminders, u.ID, p.loc, time.Now)...)
	}
	if p.skills != nil {
		opts.Extra = append(opts.Extra, skills.Tools(p.skills, p.skills.For(ctx, u), runner, ws)...)
	}
	if p.memos != nil {
		opts.Extra = append(opts.Extra, memo.Tools(p.memos, u.ID, turnID())...)
	}
	if p.plugins != nil {
		opts.Extra = append(opts.Extra, p.plugins.Tools(ctx, p.plugins.For(ctx, skills.SplitNames(u.DisabledPlugins)))...)
	}
	if opts.Search == nil && opts.Fetch == nil && opts.Python == nil && len(opts.Extra) == 0 {
		return nil
	}
	return tools.New(opts)
}

// turnID 是每一轮对话不同的编号（备忘录的删除要在提出之后的回复里确认）。
func turnID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
