package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"

	"github.com/HugehardJiang/buugoo/internal/apiserve"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/logx"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// API 接入：用户用 API Key 通过 OpenAI（/v1/chat/completions）或 Anthropic（/v1/messages）格式调用模型。
// 只是转发给模型（不经过布谷的人格和记忆），按价格倍率计入额度、扣余额。

// API 调用的限制：每个用户每分钟最多 60 次（可以短时间连发 20 次），同时最多 4 个请求在生成。
const (
	apiRatePerMinute = 60
	apiBurst         = 20
	apiConcurrency   = 4
	apiBodyLimit     = "20M"
)

const (
	formatOpenAI    = "openai"
	formatAnthropic = "anthropic"
	ctxAPIKey       = "buugoo.apikey"
)

type apiUserLimit struct {
	rate   *rate.Limiter
	active int
	seen   time.Time
}

type apiLimits struct {
	mu    sync.Mutex
	users map[uint64]*apiUserLimit
}

// acquire 检查频率和并发，通过时返回释放函数。
func (l *apiLimits) acquire(userID uint64) (func(), string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.users == nil {
		l.users = map[uint64]*apiUserLimit{}
	}
	now := time.Now()
	// 顺手清掉一个小时没来过的。
	if len(l.users) > 1000 {
		for id, u := range l.users {
			if u.active == 0 && now.Sub(u.seen) > time.Hour {
				delete(l.users, id)
			}
		}
	}
	u := l.users[userID]
	if u == nil {
		u = &apiUserLimit{rate: rate.NewLimiter(rate.Every(time.Minute/apiRatePerMinute), apiBurst)}
		l.users[userID] = u
	}
	u.seen = now
	if u.active >= apiConcurrency {
		return nil, "同时进行的请求太多了（最多 4 个），等前面的完成再发"
	}
	if !u.rate.Allow() {
		return nil, "请求太频繁了（每分钟最多 60 次），稍后再试"
	}
	u.active++
	return func() {
		l.mu.Lock()
		u.active--
		l.mu.Unlock()
	}, ""
}

func apiFormat(c echo.Context) string {
	if strings.HasSuffix(c.Path(), "/messages") || c.Request().Header.Get("anthropic-version") != "" {
		return formatAnthropic
	}
	return formatOpenAI
}

// apiFail 按请求的格式写错误。kind 用 OpenAI 的错误类型，Anthropic 格式时换成对应的。
func apiFail(c echo.Context, status int, kind, msg string) error {
	if apiFormat(c) == formatAnthropic {
		akind := map[string]string{
			"invalid_request_error": "invalid_request_error", "authentication_error": "authentication_error",
			"permission_error": "permission_error", "not_found_error": "not_found_error",
			"rate_limit_error": "rate_limit_error", "insufficient_quota": "rate_limit_error", "api_error": "api_error",
		}[kind]
		if akind == "" {
			akind = "api_error"
		}
		return c.JSON(status, apiserve.AnthropicError(akind, msg))
	}
	return c.JSON(status, apiserve.OpenAIError(kind, msg))
}

func (s *server) routeAPI(e *echo.Echo, priv, console *echo.Group) {
	if s.APIKeys == nil || s.APISettings == nil {
		return
	}
	priv.GET("/api-access", s.apiAccess)
	priv.POST("/api-keys", s.createAPIKey, s.notImpersonated)
	priv.DELETE("/api-keys/:id", s.deleteAPIKey, s.notImpersonated)
	m := console.Group("", s.requireStrongConsole)
	m.GET("/api-settings", s.consoleAPISettings)
	m.PUT("/api-settings", s.consoleSetAPISettings)

	// 第三方客户端（网页版聊天工具等）会从浏览器直接调，允许跨域；用 Key 认证，不带 Cookie，跨域是安全的。
	cors := middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{echo.HeaderAuthorization, echo.HeaderContentType, "x-api-key", "anthropic-version", "anthropic-beta", "anthropic-dangerous-direct-browser-access"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
	})
	v1 := e.Group("/v1", cors, middleware.BodyLimit(apiBodyLimit), s.maintenance, s.requireAPIKey)
	v1.GET("/models", s.apiModels)
	v1.POST("/chat/completions", s.apiChatCompletions)
	v1.POST("/messages", s.apiMessages)
	v1.Any("/*", func(c echo.Context) error {
		return apiFail(c, http.StatusNotFound, "not_found_error", "没有这个接口：只支持 /v1/models、/v1/chat/completions（OpenAI 格式）和 /v1/messages（Anthropic 格式）")
	})
}

// apiAllowed 报告用户能不能用 API：管理员总是可以，其他人要有包含 API 接入的套餐。
func (s *server) apiAllowed(ctx context.Context, u *store.User) bool {
	if u.IsAdmin {
		return true
	}
	if s.Billing == nil {
		return false
	}
	ent, err := s.Billing.Entitlement(ctx, u.ID)
	return err == nil && ent.APIAccess
}

// requireAPIKey 用 API Key 认证（OpenAI 的 Authorization: Bearer，或 Anthropic 的 x-api-key）。
func (s *server) requireAPIKey(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if c.Request().Method == http.MethodOptions {
			return c.NoContent(http.StatusNoContent)
		}
		raw := strings.TrimSpace(c.Request().Header.Get("x-api-key"))
		if raw == "" {
			raw = strings.TrimSpace(strings.TrimPrefix(c.Request().Header.Get(echo.HeaderAuthorization), "Bearer "))
		}
		if raw == "" {
			return apiFail(c, http.StatusUnauthorized, "authentication_error", "缺少 API Key：放在 Authorization: Bearer 或 x-api-key 请求头里")
		}
		ctx := c.Request().Context()
		key, err := s.APIKeys.Lookup(ctx, raw)
		if err != nil {
			if errors.Is(err, apiserve.ErrInvalidKey) {
				return apiFail(c, http.StatusUnauthorized, "authentication_error", "API Key 无效或已经删除")
			}
			return err
		}
		u, err := s.Store.Users().GetByID(ctx, key.UserID)
		if err != nil {
			return apiFail(c, http.StatusUnauthorized, "authentication_error", "API Key 无效或已经删除")
		}
		if u.DisabledAt != nil {
			return apiFail(c, http.StatusForbidden, "permission_error", "账号已停用")
		}
		if !s.apiAllowed(ctx, u) {
			return apiFail(c, http.StatusForbidden, "permission_error", "你现在的套餐不包含 API 接入（或者套餐已到期）")
		}
		c.Set(ctxUserKey, u)
		c.Set(ctxAPIKey, key)
		return next(c)
	}
}

func (s *server) apiCatalog(ctx context.Context, u *store.User) ([]apiserve.Entry, error) {
	views, err := s.Models.List(ctx, modelUser(u))
	if err != nil {
		return nil, err
	}
	return apiserve.Catalog(views, s.APISettings.Get(ctx)), nil
}

// apiModels 处理 GET /v1/models：OpenAI 和 Anthropic 的格式不一样，按请求头区分。
func (s *server) apiModels(c echo.Context) error {
	entries, err := s.apiCatalog(c.Request().Context(), currentUser(c))
	if err != nil {
		return err
	}
	if apiFormat(c) == formatAnthropic {
		data := make([]map[string]any, len(entries))
		for i, e := range entries {
			data[i] = map[string]any{"type": "model", "id": e.Name, "display_name": e.DisplayName, "created_at": "2025-01-01T00:00:00Z"}
		}
		first, last := "", ""
		if len(entries) > 0 {
			first, last = entries[0].Name, entries[len(entries)-1].Name
		}
		return c.JSON(http.StatusOK, map[string]any{"data": data, "has_more": false, "first_id": first, "last_id": last})
	}
	data := make([]map[string]any, len(entries))
	for i, e := range entries {
		data[i] = map[string]any{"id": e.Name, "object": "model", "created": 1735689600, "owned_by": "buugoo", "name": e.DisplayName}
	}
	return c.JSON(http.StatusOK, map[string]any{"object": "list", "data": data})
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *server) apiChatCompletions(c echo.Context) error {
	var r apiserve.OpenAIRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&r); err != nil {
		return apiFail(c, http.StatusBadRequest, "invalid_request_error", "请求不是有效的 JSON")
	}
	req, err := r.ToChat()
	if err != nil {
		return apiFail(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	include := r.StreamOptions != nil && r.StreamOptions.IncludeUsage
	return s.apiCall(c, r.Model, req, r.Stream, func(w io.Writer, flush func(), name string) apiserve.Streamer {
		return apiserve.NewOpenAIStream(w, flush, "chatcmpl-"+randomHex(12), name, time.Now(), include)
	}, func(name string, resp *llm.ChatResponse, u llm.Usage) any {
		return apiserve.OpenAIResponse("chatcmpl-"+randomHex(12), name, time.Now(), resp, u)
	})
}

func (s *server) apiMessages(c echo.Context) error {
	var r apiserve.AnthropicRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&r); err != nil {
		return apiFail(c, http.StatusBadRequest, "invalid_request_error", "请求不是有效的 JSON")
	}
	req, err := r.ToChat()
	if err != nil {
		return apiFail(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	return s.apiCall(c, r.Model, req, r.Stream, func(w io.Writer, flush func(), name string) apiserve.Streamer {
		return apiserve.NewAnthropicStream(w, flush, "msg_"+randomHex(12), name)
	}, func(name string, resp *llm.ChatResponse, u llm.Usage) any {
		return apiserve.AnthropicResponse("msg_"+randomHex(12), name, resp, u)
	})
}

// apiCall 是两种格式共用的流程：找模型 → 检查额度 → 调用（流式或一次返回）→ 按倍率记用量、扣费。
func (s *server) apiCall(c echo.Context, modelName string, req llm.ChatRequest, stream bool,
	newStream func(w io.Writer, flush func(), name string) apiserve.Streamer,
	full func(name string, resp *llm.ChatResponse, u llm.Usage) any) error {
	ctx := c.Request().Context()
	u := currentUser(c)
	key, _ := c.Get(ctxAPIKey).(*store.APIKey)
	entries, err := s.apiCatalog(ctx, u)
	if err != nil {
		return err
	}
	entry, ok := apiserve.Find(entries, modelName)
	if !ok {
		return apiFail(c, http.StatusNotFound, "not_found_error", "没有这个模型（或者你不能用）："+modelName+"。可用的模型见 GET /v1/models")
	}
	model, err := s.Models.Resolve(ctx, modelUser(u), &entry.ID)
	if err != nil {
		if errors.Is(err, models.ErrNoModel) {
			return apiFail(c, http.StatusNotFound, "not_found_error", "这个模型现在不能用")
		}
		return err
	}
	if apiserve.HasImages(req) && !model.Vision {
		return apiFail(c, http.StatusBadRequest, "invalid_request_error", "这个模型不能看图，请换一个支持图片的模型")
	}
	if !model.Personal {
		if err := s.Quota.Check(ctx, u); err != nil {
			var qe *quota.ExceededError
			switch {
			case errors.As(err, &qe):
				return apiFail(c, http.StatusTooManyRequests, "insufficient_quota", qe.Error())
			case errors.Is(err, quota.ErrDisabled):
				return apiFail(c, http.StatusForbidden, "permission_error", err.Error())
			}
			return err
		}
	}
	release, why := s.apiLimit.acquire(u.ID)
	if release == nil {
		return apiFail(c, http.StatusTooManyRequests, "rate_limit_error", why)
	}
	defer release()

	// 同一个 Key 用同一个会话编号（OpenCode Go 据此路由和缓存提示词）。
	if key != nil {
		ctx = llm.WithSession(ctx, "buugoo-api-"+randomSession(u.ID, key.ID))
	}
	req.Model = ""
	record := func(usage llm.Usage, output string) {
		if usage.PromptTokens == 0 && usage.CompletionTokens == 0 {
			usage = apiserve.Estimate(req, output)
		}
		if model.Personal {
			return
		}
		use := quota.Use{Key: quota.ModelKey(model.ID), Messages: 1, PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
			CachedTokens: usage.CachedTokens, Scale: entry.Scale, Via: quota.ViaAPI}
		if err := s.Quota.Record(context.WithoutCancel(ctx), u.ID, use); err != nil {
			s.log.Error("记录 API 用量失败", "user_id", u.ID, "err", err)
		}
	}
	upstreamErr := func(err error) error {
		ae := s.llmError(u.ID, err)
		return apiFail(c, http.StatusBadGateway, "api_error", logx.Redact(ae.Message))
	}

	if !stream {
		resp, err := model.Chat.Chat(ctx, req)
		if err != nil {
			return upstreamErr(err)
		}
		// DeepSeek 偶尔把工具调用写成 DSML 混在正文里：还原成真正的工具调用。
		if len(req.Tools) > 0 && len(resp.ToolCalls) == 0 {
			if clean, calls, found := llm.ExtractDSML(resp.Content); found {
				resp.Content, resp.ToolCalls = clean, calls
			}
		}
		usage := resp.Usage
		if usage.PromptTokens == 0 && usage.CompletionTokens == 0 {
			usage = apiserve.Estimate(req, resp.Content+resp.Reasoning)
		}
		record(usage, "")
		return c.JSON(http.StatusOK, full(entry.Name, resp, usage))
	}

	st, err := model.Chat.ChatStream(ctx, req)
	if err != nil {
		return upstreamErr(err)
	}
	defer st.Close()
	h := c.Response().Header()
	h.Set(echo.HeaderContentType, "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	c.Response().WriteHeader(http.StatusOK)
	w := c.Response()
	out := newStream(w, w.Flush, entry.Name)
	var usage llm.Usage
	var text strings.Builder
	// 客户端断开时写入会失败：停止生成，已经生成的部分照样计费。
	if err := out.Start(); err != nil {
		record(usage, "")
		return nil
	}
	for {
		ch, err := st.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if ctx.Err() == nil {
				out.Fail(logx.Redact(s.llmError(u.ID, err).Message))
			}
			record(usage, text.String())
			return nil
		}
		if ch.Usage != nil {
			usage = *ch.Usage
		}
		text.WriteString(ch.Delta)
		text.WriteString(ch.Reasoning)
		for _, tc := range ch.ToolCalls {
			text.WriteString(tc.Name + tc.Arguments)
		}
		if err := out.Chunk(ch); err != nil {
			record(usage, text.String())
			return nil
		}
	}
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 {
		usage = apiserve.Estimate(req, text.String())
	}
	record(usage, "")
	_ = out.Finish(usage)
	return nil
}

// randomSession 是一个 Key 固定的会话编号（不暴露 Key 本身）。
func randomSession(userID, keyID uint64) string {
	return apiserve.HashKey(fmt.Sprintf("api-session/%d/%d", userID, keyID))[:16]
}

// ---------- 用户：管理自己的 API Key ----------

func (s *server) apiAccess(c echo.Context) error {
	ctx := c.Request().Context()
	u := currentUser(c)
	allowed := s.apiAllowed(ctx, u)
	out := map[string]any{"allowed": allowed}
	if !allowed {
		return c.JSON(http.StatusOK, out)
	}
	keys, err := s.APIKeys.List(ctx, u.ID)
	if err != nil {
		return err
	}
	entries, err := s.apiCatalog(ctx, u)
	if err != nil {
		return err
	}
	out["keys"], out["models"] = keys, entries
	return c.JSON(http.StatusOK, out)
}

func (s *server) createAPIKey(c echo.Context) error {
	ctx := c.Request().Context()
	u := currentUser(c)
	if !s.apiAllowed(ctx, u) {
		return &apiError{Status: http.StatusForbidden, Code: "forbidden", Message: "你现在的套餐不包含 API 接入"}
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	raw, view, err := s.APIKeys.Create(ctx, u.ID, req.Name)
	if err != nil {
		return err
	}
	s.log.Info("新建了 API Key", "user_id", u.ID, "key_id", view.ID, "ip", c.RealIP())
	return c.JSON(http.StatusOK, map[string]any{"key": raw, "info": view})
}

func (s *server) deleteAPIKey(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.APIKeys.Delete(c.Request().Context(), currentUser(c).ID, id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// ---------- 管理后台：价格倍率 ----------

func (s *server) consoleAPISettings(c echo.Context) error {
	ctx := c.Request().Context()
	views, err := s.Models.List(ctx, models.User{IsAdmin: true})
	if err != nil {
		return err
	}
	type row struct {
		ID    uint64 `json:"id"`
		Name  string `json:"name"`
		Model string `json:"model"`
		Scope string `json:"scope"`
	}
	var rows []row
	for _, v := range views {
		if v.Scope != models.ScopePersonal {
			rows = append(rows, row{ID: v.ID, Name: v.Name, Model: v.Model, Scope: v.Scope})
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"settings": s.APISettings.Get(ctx), "models": rows})
}

func (s *server) consoleSetAPISettings(c echo.Context) error {
	var in apiserve.Settings
	if err := bind(c, &in); err != nil {
		return err
	}
	st, err := s.APISettings.Set(c.Request().Context(), in)
	if err != nil {
		return err
	}
	s.audit(c, "管理后台修改了 API 价格倍率", "default", st.Default, "models", len(st.Models))
	return c.JSON(http.StatusOK, map[string]any{"settings": st})
}
