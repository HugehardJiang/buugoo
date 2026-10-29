// Package httpapi 提供 Buugoo 的 HTTP API（/api/v1）与内嵌的 Web 界面。
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"github.com/HugehardJiang/buugoo/internal/accounts"
	"github.com/HugehardJiang/buugoo/internal/agent"
	"github.com/HugehardJiang/buugoo/internal/billing"
	"github.com/HugehardJiang/buugoo/internal/dbmove"
	"github.com/HugehardJiang/buugoo/internal/mcp"
	"github.com/HugehardJiang/buugoo/internal/memo"
	"github.com/HugehardJiang/buugoo/internal/opencode"
	"github.com/HugehardJiang/buugoo/internal/skills"
	"github.com/HugehardJiang/buugoo/internal/todo"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"

	"github.com/HugehardJiang/buugoo/internal/auth"
	"github.com/HugehardJiang/buugoo/internal/buildinfo"
	"github.com/HugehardJiang/buugoo/internal/channels"
	"github.com/HugehardJiang/buugoo/internal/chat"
	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/emotion"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/llm/openai"
	"github.com/HugehardJiang/buugoo/internal/memory"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/persona"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/secret"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/uploads"
)

// Deps 是 HTTP 层的依赖。
type Deps struct {
	Config   *config.Config
	Store    store.Store
	Auth     *auth.Service
	Chat     *chat.Service
	Memories *memory.Manager
	// Compactor 为空时不提供手动整理记忆。
	Compactor *memory.Compactor
	Quota     *quota.Limiter
	Models    *models.Service
	Reembed   *memory.Reembedder
	// Uploads 为空时不提供上传接口。
	Uploads *uploads.Service
	Emotion *emotion.Engine
	Persona *persona.Manager
	// Channels 为空表示没有开启任何聊天软件接入。
	Channels *channels.Hub
	// Skills、Plugins、Agent 为空时不提供对应接口。
	Skills  *skills.Manager
	Plugins *mcp.Manager
	Agent   *agent.Service
	Memos   *memo.Service
	// DBMove 不为空时管理后台可以把 SQLite 一键迁移到 MySQL；Restart 让服务重启，Supervised 表示重启后会被自动拉起。
	DBMove     *dbmove.Mover
	Restart    func()
	Supervised bool
	Todos      *todo.Service
	// OpenCode 是 OpenCode Go 的导入、个人添加和官网同步。
	OpenCode *opencode.Importer
	// Billing、Accounts 为空时不提供会员和用户管理（single 模式）。
	Billing  *billing.Service
	Accounts *accounts.Service
	Logger   *slog.Logger
}

type server struct {
	Deps
	log         *slog.Logger
	console     *consoleSessions
	redeemGuard redeemGuard
	live        *liveHub
}

const ctxUserKey = "buugoo.user"

// New 创建 HTTP 处理器。
func New(d Deps) http.Handler {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	s := &server{Deps: d, log: d.Logger.With("component", "http"), console: &consoleSessions{tokens: map[string]time.Time{}}, live: newLiveHub()}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	// 信任来自本机与内网的反向代理（宝塔/Nginx）设置的 X-Forwarded-For。
	e.IPExtractor = echo.ExtractIPFromXFFHeader()
	e.HTTPErrorHandler = s.errorHandler

	e.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{
		LogErrorFunc: func(c echo.Context, err error, stack []byte) error {
			s.log.Error("panic", "err", err, "path", c.Request().URL.Path, "stack", string(stack))
			return err
		},
	}))
	e.Use(middleware.RequestID())
	e.Use(s.requestLogger())
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		XSSProtection:      "0",
		ContentTypeNosniff: "nosniff",
		XFrameOptions:      "DENY",
		ReferrerPolicy:     "same-origin",
	}))
	if origins := d.Config.Server.CORSOrigins; len(origins) > 0 {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins: origins,
			AllowHeaders: []string{echo.HeaderAuthorization, echo.HeaderContentType, "X-Console-Token"},
			AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete, http.MethodOptions},
		}))
	}

	e.GET("/healthz", s.healthz)

	api := e.Group("/api/v1", middleware.BodyLimit("1M"), s.maintenance)
	api.GET("/meta", s.meta)

	// 登录与注册按 IP 限速，防止暴力破解。
	authLimiter := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{
			Rate: rate.Every(6 * time.Second), Burst: 10, ExpiresIn: 10 * time.Minute,
		}),
		DenyHandler: func(c echo.Context, _ string, _ error) error {
			return &apiError{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "尝试次数过多，请稍后再试"}
		},
	})
	api.POST("/auth/login", s.login, authLimiter)
	api.POST("/auth/register", s.register, authLimiter)

	// 管理后台：用管理员密码登录（单独限速，比普通登录更严格）。
	consoleLimiter := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{
			Rate: rate.Every(15 * time.Second), Burst: 5, ExpiresIn: 30 * time.Minute,
		}),
		DenyHandler: func(c echo.Context, _ string, _ error) error {
			return &apiError{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "尝试次数过多，请稍后再试"}
		},
	})
	api.POST("/console/login", s.consoleLogin, consoleLimiter)
	console := api.Group("/console", s.requireConsole)
	console.POST("/logout", s.consoleLogout)
	console.GET("/settings", s.consoleSettings)
	console.PUT("/settings", s.consoleSave)
	console.POST("/settings/discard", s.consoleDiscard)
	console.GET("/persona", s.consoleGetPersona)
	console.PUT("/persona", s.consoleSavePersona)
	console.DELETE("/persona", s.consoleResetPersona)
	console.GET("/channels", s.consoleChannels)
	console.GET("/diagnostics", s.consoleDiagnostics)
	console.POST("/channels/weixin/:id/logout", s.consoleWeixinKick)
	if s.OpenCode != nil {
		console.GET("/opencode", s.consoleOpenCode)
		console.POST("/opencode/import", s.consoleOpenCodeImport)
		console.POST("/opencode/sync", s.consoleOpenCodeSync)
		console.POST("/opencode/apply", s.consoleOpenCodeApply)
		console.PUT("/opencode/options", s.consoleOpenCodeOptions)
		console.PUT("/opencode/key", s.consoleOpenCodeKey)
		console.POST("/opencode/remove", s.consoleOpenCodeRemove)
	}
	if s.DBMove != nil {
		console.GET("/database", s.consoleDatabase)
		console.POST("/database/test", s.consoleDatabaseTest)
		console.POST("/database/migrate", s.consoleDatabaseMigrate)
	}

	priv := api.Group("", s.requireUser)
	priv.POST("/auth/logout-all", s.logoutAll)
	priv.POST("/auth/password", s.changePassword)
	priv.GET("/me", s.me)
	priv.GET("/diagnostics", s.myDiagnostics)
	priv.PUT("/me/proactive", s.setProactive)
	priv.GET("/me/profile", s.profile)
	priv.PUT("/me/profile", s.updateProfile)
	priv.POST("/me/avatar", s.regenerateAvatar)
	priv.GET("/channels", s.myChannels)
	priv.POST("/channels/code", s.newBindCode)
	priv.DELETE("/channels/bindings/:id", s.unbind)
	priv.POST("/channels/weixin/login", s.weixinLogin)
	priv.POST("/channels/weixin/logout", s.weixinLogout)
	priv.GET("/reminders", s.listReminders)
	priv.POST("/reminders", s.createReminder)
	priv.PUT("/reminders/:id", s.updateReminder)
	priv.POST("/reminders/:id/pause", s.pauseReminder(true))
	priv.POST("/reminders/:id/resume", s.pauseReminder(false))
	priv.DELETE("/reminders/:id", s.cancelReminder)
	priv.GET("/conversations/:id/latest", s.latestMessage)
	priv.GET("/conversations/:id/context", s.conversationContext)
	priv.POST("/conversations/:id/compact", s.compactConversation)
	priv.GET("/persona/presets", s.listPresets)
	priv.PUT("/persona/preset", s.setPreset)
	priv.GET("/conversations", s.listConversations)
	priv.POST("/conversations", s.createConversation)
	priv.PATCH("/conversations/:id", s.updateConversation)
	priv.DELETE("/conversations/:id", s.deleteConversation)
	priv.GET("/conversations/:id/messages", s.listConversationMessages)
	priv.DELETE("/conversations/:id/messages/:mid", s.deleteMessage)
	priv.POST("/conversations/:id/regenerate", s.regenerate)
	priv.GET("/conversations/:id/live", s.liveReply)
	priv.POST("/conversations/:id/stop", s.stopReply)
	priv.GET("/messages", s.listMessages)
	priv.POST("/chat", s.chat)
	priv.GET("/models", s.listModels)
	priv.POST("/models", s.createModel)
	priv.POST("/models/test", s.testModelInput)
	priv.DELETE("/models/default", s.clearDefaultModel)
	priv.PUT("/models/:id", s.updateModel)
	priv.DELETE("/models/:id", s.deleteModel)
	priv.POST("/models/:id/default", s.setDefaultModel)
	priv.POST("/models/:id/test", s.testModel)
	priv.GET("/memories", s.listMemories)
	priv.GET("/memories/:id", s.getMemory)
	priv.DELETE("/memories/:id", s.deleteMemory)
	if s.Compactor != nil {
		priv.POST("/memories/compact", s.compactMemories)
	}
	priv.GET("/debug/prompt", s.debugPrompt)
	priv.GET("/emotion", s.getEmotion)
	priv.DELETE("/emotion", s.resetEmotion)
	if s.Uploads != nil {
		// 上传接口单独设置请求体上限（单个文件上限 + 表单开销）。
		limit := fmt.Sprintf("%dK", s.Uploads.MaxFileBytes()/1024+512)
		e.POST("/api/v1/uploads", s.uploadFile, middleware.BodyLimit(limit), s.requireUser)
		priv.GET("/uploads/:id", s.getUpload)
		priv.DELETE("/uploads/:id", s.deleteUpload)
		priv.GET("/files", s.listFiles)
		priv.GET("/files/usage", s.usage)
		priv.POST("/files/delete", s.deleteFiles)
	}

	admin := priv.Group("/admin", s.requireAdmin)
	admin.GET("/users", s.adminUsers)
	admin.GET("/invites", s.adminListInvites)
	admin.POST("/invites", s.adminCreateInvite)
	admin.DELETE("/invites/:id", s.adminDeleteInvite)
	admin.GET("/utility", s.adminGetUtility)
	admin.PUT("/utility", s.adminSetUtility)
	admin.GET("/embedding", s.adminGetEmbedding)
	admin.PUT("/embedding", s.adminSetEmbedding)
	admin.POST("/embedding/test", s.adminTestEmbedding)
	admin.GET("/embedding/reembed", s.adminReembedStatus)
	admin.POST("/embedding/reembed", s.adminStartReembed)

	s.routeExtensions(e, api, priv, console)
	s.routeBilling(api, priv, console)
	if s.Memos != nil {
		priv.GET("/memos", s.listMemos)
		priv.POST("/memos", s.createMemo)
		priv.GET("/memos/:id", s.getMemo)
		priv.PUT("/memos/:id", s.updateMemo)
		priv.DELETE("/memos/:id", s.deleteMemo)
	}
	if s.OpenCode != nil {
		priv.GET("/opencode/catalog", s.openCodeCatalog)
		priv.POST("/opencode/mine", s.addMyOpenCode)
		priv.POST("/opencode/mine/remove", s.removeMyOpenCode)
	}
	if s.Todos != nil {
		priv.GET("/todos", s.listTodos)
		priv.POST("/todos", s.createTodo)
		priv.PUT("/todos/:id", s.updateTodo)
		priv.DELETE("/todos/:id", s.deleteTodo)
		priv.POST("/todos/clear-done", s.clearDoneTodos)
	}

	// QQ（NapCat）的反向 WebSocket 连接，用访问令牌鉴权。
	if s.Channels != nil {
		if ob, ok := s.Channels.Channel(channels.QQ).(*channels.OneBotChannel); ok {
			e.GET(channels.OneBotPath, echo.WrapHandler(ob))
		}
	}

	// 未匹配的 /api 路径返回 JSON 404，其余交给前端。
	api.Any("/*", func(echo.Context) error { return echo.ErrNotFound })
	s.mountWeb(e)
	return e
}

func (s *server) requestLogger() echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus: true, LogURI: true, LogMethod: true, LogLatency: true, LogRemoteIP: true,
		LogRequestID: true, LogError: true, HandleError: true,
		Skipper: func(c echo.Context) bool {
			p := c.Request().URL.Path
			return p == "/healthz" || strings.HasPrefix(p, "/assets/")
		},
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			attrs := []any{"method", v.Method, "uri", v.URI, "status", v.Status,
				"latency", v.Latency.Round(time.Millisecond), "ip", v.RemoteIP, "request_id", v.RequestID}
			if u, ok := c.Get(ctxUserKey).(*store.User); ok {
				attrs = append(attrs, "user_id", u.ID)
			}
			if v.Error != nil {
				attrs = append(attrs, "err", v.Error)
			}
			level := slog.LevelInfo
			if v.Status >= 500 {
				level = slog.LevelError
			}
			s.log.Log(c.Request().Context(), level, "request", attrs...)
			return nil
		},
	})
}

func (s *server) healthz(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), 3*time.Second)
	defer cancel()
	status, code := "ok", http.StatusOK
	if err := s.Store.Ping(ctx); err != nil {
		status, code = "db_unavailable", http.StatusServiceUnavailable
	}
	return c.JSON(code, map[string]string{"status": status, "version": buildinfo.Version})
}

func (s *server) meta(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{
		"name":              s.Persona.Current().Name,
		"version":           buildinfo.Version,
		"mode":              s.Config.Mode,
		"password_required": s.Auth.PasswordRequired(),
		"registration":      s.Auth.Registration(),
		"embedding_ready":   s.Models.EmbeddingReady(c.Request().Context()),
		"uploads":           s.uploadLimits(),
		// reminders 为 true 时显示「定时任务」入口（提醒或主动关心至少开了一个）。
		"reminders": s.Config.Proactive.Reminders || s.Config.Proactive.Care,
		// channels 为 true 时显示「聊天软件」入口。
		"channels": s.Channels != nil && len(s.Channels.Enabled()) > 0,
		// home 是多用户版未登录时介绍主页的标题和标语，空字符串表示用内置的。
		"home": map[string]string{"title": s.Config.Home.Title, "tagline": s.Config.Home.Tagline},
		// billing 为 true 时设置页显示「会员」（多用户版）。
		"billing": s.Billing != nil,
		// maintenance 为 true 时正在迁移数据库，网页只能查看。
		"maintenance": s.DBMove != nil && s.DBMove.Paused(),
	})
}

// requireUser 校验 Authorization: Bearer <token>，并把用户放入上下文。
func (s *server) requireUser(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := ""
		if h := c.Request().Header.Get(echo.HeaderAuthorization); h != "" {
			t, ok := strings.CutPrefix(h, "Bearer ")
			if !ok {
				return auth.ErrUnauthorized
			}
			token = strings.TrimSpace(t)
		}
		u, err := s.Auth.Authenticate(c.Request().Context(), token)
		if err != nil {
			return err
		}
		c.Set(ctxUserKey, u)
		return next(c)
	}
}

func (s *server) requireAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if !currentUser(c).IsAdmin {
			return auth.ErrForbidden
		}
		return next(c)
	}
}

func currentUser(c echo.Context) *store.User {
	u, _ := c.Get(ctxUserKey).(*store.User)
	return u
}

// apiError 是返回给客户端的错误。
type apiError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  any    `json:"detail,omitempty"`
}

func (e *apiError) Error() string { return e.Message }

func badRequest(msg string) error {
	return &apiError{Status: http.StatusBadRequest, Code: "bad_request", Message: msg}
}

// toAPIError 把领域错误映射为 HTTP 错误。未知错误统一返回 500，详细信息只写日志。
func toAPIError(err error) *apiError {
	var ae *apiError
	var he *echo.HTTPError
	var ve *auth.ValidationError
	var qe *quota.ExceededError
	var le *llm.APIError
	var mi *models.InvalidError
	var ue *uploads.UserError
	var accErr *accounts.UserError
	if errors.As(err, &accErr) {
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_input", Message: accErr.Msg}
	}
	if errors.Is(err, accounts.ErrNotFound) {
		return &apiError{Status: http.StatusNotFound, Code: "not_found", Message: err.Error()}
	}
	var billErr *billing.UserError
	if errors.As(err, &billErr) {
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_input", Message: billErr.Msg}
	}
	var memoErr *memo.UserError
	switch {
	case errors.As(err, &memoErr):
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_memo", Message: memoErr.Msg}
	case errors.Is(err, memo.ErrNotFound):
		return &apiError{Status: http.StatusNotFound, Code: "not_found", Message: err.Error()}
	}
	if errors.Is(err, dbmove.ErrMaintenance) {
		return &apiError{Status: http.StatusServiceUnavailable, Code: "maintenance", Message: dbmove.ErrMaintenance.Error()}
	}
	var moveErr *dbmove.UserError
	if errors.As(err, &moveErr) {
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_input", Message: moveErr.Msg}
	}
	var ocErr *opencode.UserError
	if errors.As(err, &ocErr) {
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_input", Message: ocErr.Msg}
	}
	var todoErr *todo.UserError
	switch {
	case errors.As(err, &todoErr):
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_todo", Message: todoErr.Msg}
	case errors.Is(err, todo.ErrNotFound):
		return &apiError{Status: http.StatusNotFound, Code: "not_found", Message: err.Error()}
	}
	if ee := extensionError(err); ee != nil {
		return ee
	}
	switch {
	case errors.As(err, &ue):
		status, code := http.StatusBadRequest, "invalid_file"
		switch {
		case errors.Is(err, uploads.ErrQuota):
			status, code = http.StatusRequestEntityTooLarge, "quota_exceeded"
		case errors.Is(err, uploads.ErrTooLarge):
			status = http.StatusRequestEntityTooLarge
		}
		return &apiError{Status: status, Code: code, Message: ue.Msg}
	case errors.Is(err, chat.ErrCannotRegenerate):
		return &apiError{Status: http.StatusConflict, Code: "cannot_regenerate", Message: err.Error()}
	case errors.Is(err, chat.ErrNothingToCompact), errors.Is(err, chat.ErrCompacting), errors.Is(err, chat.ErrSummaryDisabled):
		return &apiError{Status: http.StatusConflict, Code: "cannot_compact", Message: err.Error()}
	case errors.Is(err, chat.ErrAttachmentNotFound), errors.Is(err, chat.ErrTooManyFiles),
		errors.Is(err, chat.ErrUploadsDisabled), errors.Is(err, chat.ErrImagesUnsupported):
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_attachment", Message: err.Error()}
	case errors.As(err, &ae):
		return ae
	case errors.As(err, &ve):
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_input", Message: ve.Msg}
	case errors.As(err, &qe):
		return &apiError{Status: http.StatusTooManyRequests, Code: "quota_exceeded", Message: qe.Error(),
			Detail: map[string]any{"kind": qe.Kind, "limit": qe.Limit, "used": qe.Used, "reset_at": qe.Reset}}
	case errors.Is(err, auth.ErrAccountDisabled), errors.Is(err, quota.ErrDisabled):
		return &apiError{Status: http.StatusForbidden, Code: "account_disabled", Message: err.Error()}
	case errors.Is(err, billing.ErrCardInvalid):
		return &apiError{Status: http.StatusBadRequest, Code: "card_invalid", Message: err.Error()}
	case errors.Is(err, billing.ErrNotFound):
		return &apiError{Status: http.StatusNotFound, Code: "not_found", Message: err.Error()}
	case errors.Is(err, auth.ErrUnauthorized):
		return &apiError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: auth.ErrUnauthorized.Error()}
	case errors.Is(err, auth.ErrInvalidCredentials):
		return &apiError{Status: http.StatusUnauthorized, Code: "invalid_credentials", Message: err.Error()}
	case errors.Is(err, auth.ErrForbidden):
		return &apiError{Status: http.StatusForbidden, Code: "forbidden", Message: err.Error()}
	case errors.Is(err, auth.ErrRegistrationClosed), errors.Is(err, auth.ErrNotSupported):
		return &apiError{Status: http.StatusForbidden, Code: "not_allowed", Message: err.Error()}
	case errors.Is(err, auth.ErrInviteRequired), errors.Is(err, store.ErrInviteInvalid):
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_invite", Message: err.Error()}
	case errors.Is(err, auth.ErrUsernameTaken):
		return &apiError{Status: http.StatusConflict, Code: "username_taken", Message: err.Error()}
	case errors.As(err, &mi):
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_input", Message: mi.Msg}
	case errors.Is(err, models.ErrForbidden):
		return &apiError{Status: http.StatusForbidden, Code: "forbidden", Message: err.Error()}
	case errors.Is(err, models.ErrNoModel):
		return &apiError{Status: http.StatusServiceUnavailable, Code: "no_model", Message: err.Error()}
	case errors.Is(err, secret.ErrDecrypt):
		return &apiError{Status: http.StatusConflict, Code: "key_unreadable", Message: err.Error()}
	case errors.Is(err, memory.ErrCompactBusy):
		return &apiError{Status: http.StatusConflict, Code: "busy", Message: err.Error()}
	case errors.Is(err, memory.ErrReembedRunning):
		return &apiError{Status: http.StatusConflict, Code: "busy", Message: err.Error()}
	case errors.Is(err, chat.ErrInvalidThinking), errors.Is(err, chat.ErrModelNotFound), errors.Is(err, chat.ErrTitleTooLong):
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_input", Message: err.Error()}
	case errors.Is(err, chat.ErrEmptyMessage), errors.Is(err, chat.ErrMessageTooLong):
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_message", Message: err.Error()}
	case errors.Is(err, openai.ErrPrivateAddress):
		return &apiError{Status: http.StatusBadRequest, Code: "private_address", Message: openai.ErrPrivateAddress.Error()}
	case errors.Is(err, llm.ErrNotConfigured):
		return &apiError{Status: http.StatusServiceUnavailable, Code: "llm_not_configured",
			Message: "模型缺少 API Key：请在「设置 → 模型」中填写"}
	case errors.As(err, &le):
		return &apiError{Status: http.StatusFailedDependency, Code: "llm_error", Message: "生成回复失败：" + le.Error()}
	case errors.Is(err, store.ErrNotFound):
		return &apiError{Status: http.StatusNotFound, Code: "not_found", Message: "记录不存在"}
	case errors.As(err, &he):
		msg := http.StatusText(he.Code)
		if m, ok := he.Message.(string); ok && m != "" {
			msg = m
		}
		return &apiError{Status: he.Code, Code: strings.ToLower(strings.ReplaceAll(http.StatusText(he.Code), " ", "_")), Message: msg}
	case errors.Is(err, context.Canceled):
		return &apiError{Status: 499, Code: "canceled", Message: "请求已取消"}
	default:
		return &apiError{Status: http.StatusInternalServerError, Code: "internal", Message: "服务器内部错误"}
	}
}

// errorHandler 把错误写为 JSON 响应。错误本身由请求日志中间件记录。
func (s *server) errorHandler(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}
	ae := toAPIError(err)
	var werr error
	if c.Request().Method == http.MethodHead {
		werr = c.NoContent(ae.Status)
	} else {
		werr = c.JSON(ae.Status, map[string]any{"error": ae})
	}
	if werr != nil {
		s.log.Error("写入错误响应失败", "err", werr)
	}
}
