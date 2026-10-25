// Package app 组装 Buugoo 的全部组件并管理服务生命周期。
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/HugehardJiang/buugoo/internal/auth"
	"github.com/HugehardJiang/buugoo/internal/buildinfo"
	"github.com/HugehardJiang/buugoo/internal/chat"
	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/emotion"
	"github.com/HugehardJiang/buugoo/internal/httpapi"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/llm/provider"
	"github.com/HugehardJiang/buugoo/internal/memory"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/persona"
	"github.com/HugehardJiang/buugoo/internal/prompt"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/scheduler"
	"github.com/HugehardJiang/buugoo/internal/secret"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
	"github.com/HugehardJiang/buugoo/internal/uploads"
	"github.com/HugehardJiang/buugoo/internal/vector/memstore"
)

// App 持有所有组件。
type App struct {
	Config *config.Config
	Log    *slog.Logger
	DB     *gormstore.DB
	Auth   *auth.Service
	Chat   *chat.Service
	Models *models.Service
	Writer *memory.Writer
	// Scheduler 为空表示配置中关闭了定时任务。
	Scheduler *scheduler.Scheduler
	Handler   http.Handler

	uploads *uploads.Service
	tools   *toolProvider

	// UpdateCheck 在服务启动后于后台执行（检查新版本），可为空。
	UpdateCheck func(ctx context.Context)
	// OnStart 在后台组件启动后、HTTP 服务开始监听前调用（注册定时任务等），可为空。
	OnStart func(ctx context.Context, a *App) error
}

// New 打开数据库、执行迁移并组装所有组件。
func New(ctx context.Context, cfg *config.Config, log *slog.Logger) (*App, error) {
	db, err := gormstore.Open(cfg.Database, log)
	if err != nil {
		return nil, err
	}
	a, err := build(ctx, cfg, log, db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return a, nil
}

func build(ctx context.Context, cfg *config.Config, log *slog.Logger, db *gormstore.DB) (*App, error) {
	if err := db.Migrate(ctx); err != nil {
		return nil, err
	}
	personas, err := persona.NewManager(cfg.Persona.File, cfg.DataDir)
	if err != nil {
		return nil, err
	}
	p := personas.Current()
	utilLLM, err := provider.NewChat(cfg.LLM.Utility)
	if err != nil {
		return nil, err
	}
	box, keySource, err := secret.Load(cfg.Security.EncryptionKey, cfg.DataDir)
	if err != nil {
		return nil, err
	}
	if keySource != "config" {
		log.Info("网页中添加的 API Key 使用本地密钥文件加密，请与数据库一起备份", "file", keySource)
	}
	modelSvc, err := models.NewService(ctx, db, box, cfg, log)
	if err != nil {
		return nil, err
	}
	emb := modelSvc.Embedding()
	authSvc, err := auth.NewService(ctx, cfg, db)
	if err != nil {
		return nil, err
	}

	loc := cfg.Location()
	limiter := quota.New(cfg.Quota, db.Usage(), loc)
	vs := memstore.New(db.Memories().LoadEmbeddings, cfg.Memory.Cache, log)
	retriever := memory.NewRetriever(db.Memories(), vs, emb, cfg.Memory.Retrieval, cfg.Memory.Write.AccessBoost)
	writer := memory.NewWriter(db.Memories(), vs, emb, memory.NewLLMExtractor(utilLLM), cfg.Memory.Write, log,
		func(ctx context.Context, job memory.WriteJob, u llm.Usage) {
			if job.Personal {
				return // 用户自带的 Key，不计入限额
			}
			if err := limiter.Record(ctx, job.UserID, 0, u.PromptTokens, u.CompletionTokens); err != nil {
				log.Error("记录后台用量失败", "user_id", job.UserID, "err", err)
			}
		})
	if cfg.Uploads.Dir == "" {
		cfg.Uploads.Dir = filepath.Join(cfg.DataDir, "uploads")
	}
	uploadSvc := uploads.New(db.Attachments(), cfg.Uploads, log)
	if cfg.Tools.Python.WorkspaceMB > 0 {
		uploadSvc.SetWorkspaceRoot(cfg.Tools.Python.WorkspaceDir)
	}
	toolsProv := newToolProvider(cfg, log)
	toolsProv.files, toolsProv.attachments = uploadSvc, db.Attachments()
	emo := emotion.New(db.Emotions(), cfg.Emotion, loc, log)
	writer.OnAppraisal(func(ctx context.Context, job memory.WriteJob, a emotion.Appraisal) {
		if err := emo.Appraise(ctx, job.UserID, a); err != nil {
			log.Warn("更新情绪失败", "user_id", job.UserID, "err", err)
		}
	})
	builder := prompt.NewBuilder(p, cfg.Chat.MaxContextTokens)
	// 管理员在网页上修改人格后立即生效。
	personas.OnChange(builder.SetPersona)
	var summarizer chat.Summarizer
	if cfg.Chat.Summarize {
		summarizer = chat.LLMSummarizer{LLM: utilLLM}
	}
	chatSvc := chat.New(chat.Deps{
		Store: db, Models: modelSvc, Tools: toolsProv, Emotion: emo, Uploads: uploadSvc, Retriever: retriever, Writer: writer,
		Builder: builder, Quota: limiter,
		Config: cfg.Chat, Location: loc, Logger: log, DisabledPresets: cfg.Persona.DisabledPresets,
		Summarizer: summarizer,
	})
	handler := httpapi.New(httpapi.Deps{
		Config: cfg, Store: db, Auth: authSvc, Chat: chatSvc, Memories: memory.NewManager(db.Memories(), vs),
		Quota: limiter, Models: modelSvc, Reembed: memory.NewReembedder(db.Memories(), emb, vs, log),
		Uploads: uploadSvc, Emotion: emo, Persona: personas, Logger: log,
	})
	a := &App{Config: cfg, Log: log, DB: db, Auth: authSvc, Chat: chatSvc, Models: modelSvc, Writer: writer, Handler: handler,
		uploads: uploadSvc, tools: toolsProv}
	if cfg.Scheduler.Enabled {
		a.Scheduler = scheduler.New(db.Jobs(), loc, log)
		// 第二阶段在此注入 Consolidator 实现。
		if err := a.Scheduler.Register(memory.NewConsolidationJob(cfg.Scheduler.ConsolidationSpec, nil, log)); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// Run 启动后台组件与 HTTP 服务，阻塞直到 ctx 取消，然后优雅关闭。
func (a *App) Run(ctx context.Context) error {
	cfg := a.Config
	a.warnings()

	bgCtx, cancelBg := context.WithCancel(ctx)
	defer cancelBg()
	a.Writer.Start(bgCtx)
	go a.uploads.RunCleanup(bgCtx)
	go a.tools.prepare(bgCtx)
	if a.Scheduler != nil {
		a.Scheduler.Start(bgCtx)
	}
	if a.OnStart != nil {
		if err := a.OnStart(bgCtx, a); err != nil {
			return err
		}
	}
	if a.UpdateCheck != nil {
		go a.UpdateCheck(bgCtx)
	}

	ln, err := net.Listen("tcp", cfg.Server.Listen)
	if err != nil {
		return fmt.Errorf("监听 %s: %w", cfg.Server.Listen, err)
	}
	srv := &http.Server{
		Handler:           a.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       2 * time.Minute,
	}
	a.Log.Info("布谷已启动", "version", buildinfo.Version, "mode", cfg.Mode, "listen", ln.Addr().String(),
		"database", cfg.Database.Driver, "config", cfg.Path())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-errCh:
	}

	a.Log.Info("正在停止服务…")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var errs []error
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		errs = append(errs, fmt.Errorf("HTTP 服务: %w", serveErr))
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, fmt.Errorf("关闭 HTTP 服务: %w", err))
	}
	if a.Scheduler != nil {
		if err := a.Scheduler.Stop(shutdownCtx); err != nil {
			errs = append(errs, err)
		}
	}
	cancelBg()
	a.Chat.Wait()
	if err := a.Writer.Close(shutdownCtx); err != nil {
		errs = append(errs, err)
	}
	a.Log.Info("服务已停止")
	return errors.Join(errs...)
}

// Close 关闭数据库连接。
func (a *App) Close() error { return a.DB.Close() }

func (a *App) warnings() {
	cfg := a.Config
	ctx := context.Background()
	for _, n := range cfg.Notices() {
		a.Log.Warn(n)
	}
	if cfg.Admin.Password == config.DefaultAdminPassword {
		a.Log.Warn("管理后台仍在使用默认密码 1228，任何人都能用它修改系统设置！请在配置文件 admin.password 中修改后重启")
	}
	if _, err := os.Stat(cfg.SettingsPath()); err == nil {
		a.Log.Info("已加载网页管理后台保存的设置", "file", cfg.SettingsPath())
	}
	if ms, err := a.Models.List(ctx, models.User{IsAdmin: true}); err == nil && len(ms) == 0 && cfg.Mode == config.ModeSingle {
		a.Log.Warn("还没有可用的聊天模型：请在配置文件 llm.chat 中填写，或登录网页在「设置 → 模型」中添加")
	}
	if !a.Models.EmbeddingReady(ctx) {
		a.Log.Warn("未配置向量模型 API Key，记忆检索与写入将无法使用：请在配置文件 llm.embedding 中填写，或由管理员在网页「设置 → 向量模型」中配置")
	}
	if cfg.Mode == config.ModeSingle && cfg.Auth.AccessPassword == "" && !isLoopback(cfg.Server.Listen) {
		a.Log.Warn("服务对外监听但未设置访问密码，任何能访问该地址的人都可以和布谷聊天并查看记忆！请设置 auth.access_password",
			"listen", cfg.Server.Listen)
	}
}

func isLoopback(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
