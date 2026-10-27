package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/auth"
	"github.com/HugehardJiang/buugoo/internal/chat"
	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/memory"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/store"
)

func modelUser(u *store.User) models.User { return models.User{ID: u.ID, IsAdmin: u.IsAdmin} }

// ---------- DTO ----------

type userDTO struct {
	ID          uint64    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	IsAdmin     bool      `json:"is_admin"`
	Proactive   bool      `json:"proactive"`
	CreatedAt   time.Time `json:"created_at"`
	// AvatarSeed 决定随机头像的样子，网页按它画出小鸟。
	AvatarSeed string `json:"avatar_seed"`
}

func toUser(u *store.User) userDTO {
	seed := u.AvatarSeed
	if seed == "" {
		seed = fmt.Sprintf("user-%d", u.ID)
	}
	return userDTO{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, IsAdmin: u.IsAdmin, Proactive: u.Proactive,
		CreatedAt: u.CreatedAt, AvatarSeed: seed}
}

type sessionDTO struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      userDTO   `json:"user"`
}

func toSession(s *auth.Session) sessionDTO {
	return sessionDTO{Token: s.Token, ExpiresAt: s.ExpiresAt, User: toUser(s.User)}
}

type messageDTO struct {
	ID          uint64           `json:"id"`
	Role        string           `json:"role"`
	Content     string           `json:"content"`
	Reasoning   string           `json:"reasoning,omitempty"`
	Model       string           `json:"model,omitempty"`
	Status      string           `json:"status"`
	Attachments []attachmentDTO  `json:"attachments,omitempty"`
	Tools       []chat.ToolEvent `json:"tools,omitempty"`
	Error       string           `json:"error,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
}

func toMessage(m *store.Message) messageDTO {
	d := messageDTO{ID: m.ID, Role: m.Role, Content: m.Content, Reasoning: m.Reasoning, Model: m.Model, Error: m.Error,
		Status: m.Status, CreatedAt: m.CreatedAt}
	if m.Role == store.RoleAssistant {
		d.Content = chat.StripToolNotes(d.Content) // 以前的回复里可能照抄了工具记录
	}
	if m.ToolLog != "" {
		_ = json.Unmarshal([]byte(m.ToolLog), &d.Tools)
	}
	return d
}

type attachmentDTO struct {
	ID       uint64 `json:"id"`
	Kind     string `json:"kind"`
	Filename string `json:"filename"`
	MIME     string `json:"mime"`
	Size     int64  `json:"size"`
	// Deleted 表示文件已经在文件管理中删除。
	Deleted bool `json:"deleted,omitempty"`
}

func toAttachment(a *store.Attachment) attachmentDTO {
	return attachmentDTO{ID: a.ID, Kind: a.Kind, Filename: a.Filename, MIME: a.MIME, Size: a.Size, Deleted: a.DeletedAt != nil}
}

func toAttachments(list []store.Attachment) []attachmentDTO {
	out := make([]attachmentDTO, len(list))
	for i := range list {
		out[i] = toAttachment(&list[i])
	}
	return out
}

type conversationDTO struct {
	ID            uint64     `json:"id"`
	Title         string     `json:"title"`
	ModelID       *uint64    `json:"model_id"`
	Thinking      string     `json:"thinking"`
	CreatedAt     time.Time  `json:"created_at"`
	LastMessageAt *time.Time `json:"last_message_at"`
	// Channel 是对话来自哪个聊天软件（weixin、qq、telegram），网页上的对话为空。
	Channel string `json:"channel,omitempty"`
}

func toConversation(c *store.Conversation) conversationDTO {
	return conversationDTO{ID: c.ID, Title: c.Title, ModelID: c.ModelID, Thinking: c.Thinking,
		CreatedAt: c.CreatedAt, LastMessageAt: c.LastMessageAt, Channel: c.Channel}
}

type memoryDTO struct {
	ID             uint64     `json:"id"`
	Type           string     `json:"type"`
	Content        string     `json:"content"`
	Importance     int        `json:"importance"`
	Emotion        float64    `json:"emotion"`
	Strength       float64    `json:"strength"`
	Source         string     `json:"source"`
	AccessCount    int        `json:"access_count"`
	CreatedAt      time.Time  `json:"created_at"`
	LastAccessedAt *time.Time `json:"last_accessed_at"`
}

func toMemory(m *store.Memory) memoryDTO {
	return memoryDTO{ID: m.ID, Type: m.Type, Content: m.Content, Importance: m.Importance, Emotion: m.Emotion,
		Strength: m.Strength, Source: m.Source, AccessCount: m.AccessCount, CreatedAt: m.CreatedAt, LastAccessedAt: m.LastAccessedAt}
}

type usedMemoryDTO struct {
	ID      uint64           `json:"id"`
	Content string           `json:"content"`
	Source  string           `json:"source"`
	Score   memory.Breakdown `json:"score"`
}

func toUsedMemories(ms []memory.Scored) []usedMemoryDTO {
	out := make([]usedMemoryDTO, len(ms))
	for i, m := range ms {
		out[i] = usedMemoryDTO{ID: m.Memory.ID, Content: m.Memory.Content, Source: m.Memory.Source, Score: m.Score}
	}
	return out
}

func bind(c echo.Context, v any) error {
	if err := c.Bind(v); err != nil {
		return badRequest("请求格式错误")
	}
	return nil
}

func queryInt(c echo.Context, name string, def, lo, hi int) (int, error) {
	raw := c.QueryParam(name)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo || n > hi {
		return 0, badRequest(name + " 参数无效")
	}
	return n, nil
}

func pathID(c echo.Context) (uint64, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, badRequest("id 无效")
	}
	return id, nil
}

// ---------- 鉴权 ----------

func (s *server) login(c echo.Context) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	sess, err := s.Auth.Login(c.Request().Context(), req.Username, req.Password)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toSession(sess))
}

func (s *server) register(c echo.Context) error {
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		InviteCode  string `json:"invite_code"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	sess, err := s.Auth.Register(c.Request().Context(), auth.RegisterInput{
		Username: req.Username, Password: req.Password, DisplayName: req.DisplayName, InviteCode: req.InviteCode,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, toSession(sess))
}

func (s *server) logoutAll(c echo.Context) error {
	if s.Config.Mode == config.ModeSingle && !s.Auth.PasswordRequired() {
		return &apiError{Status: http.StatusForbidden, Code: "not_allowed", Message: "未设置访问密码，无需退出"}
	}
	if err := s.Auth.LogoutAll(c.Request().Context(), currentUser(c).ID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *server) changePassword(c echo.Context) error {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	sess, err := s.Auth.ChangePassword(c.Request().Context(), currentUser(c), req.OldPassword, req.NewPassword)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toSession(sess))
}

func (s *server) me(c echo.Context) error {
	u := currentUser(c)
	st, err := s.Quota.Status(c.Request().Context(), u)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	ms, err := s.Models.List(ctx, modelUser(u))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{
		"user": toUser(u), "quota": st,
		"has_model":       len(ms) > 0,
		"embedding_ready": s.Models.EmbeddingReady(ctx),
	})
}

// ---------- 对话 ----------

func (s *server) listConversations(c echo.Context) error {
	ctx := c.Request().Context()
	u := currentUser(c)
	// 确保至少有一段对话，前端总能拿到一条对话。
	if _, err := s.Store.Conversations().Primary(ctx, u.ID); err != nil {
		return err
	}
	convs, err := s.Chat.ListConversations(ctx, u)
	if err != nil {
		return err
	}
	out := make([]conversationDTO, len(convs))
	for i := range convs {
		out[i] = toConversation(&convs[i])
	}
	return c.JSON(http.StatusOK, map[string]any{"conversations": out})
}

// conversationPatch 解析 {"title", "model_id", "thinking"}；model_id 为 null 表示改回默认模型，
// 不出现表示不修改。
func conversationPatch(c echo.Context) (chat.ConversationPatch, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(c.Request().Body).Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return chat.ConversationPatch{}, badRequest("请求格式错误")
	}
	var p chat.ConversationPatch
	if v, ok := raw["title"]; ok {
		var t string
		if err := json.Unmarshal(v, &t); err != nil {
			return p, badRequest("title 无效")
		}
		p.Title = &t
	}
	if v, ok := raw["thinking"]; ok {
		var t string
		if err := json.Unmarshal(v, &t); err != nil {
			return p, badRequest("thinking 无效")
		}
		p.Thinking = &t
	}
	if v, ok := raw["model_id"]; ok {
		p.SetModel = true
		if string(v) != "null" {
			var id uint64
			if err := json.Unmarshal(v, &id); err != nil {
				return p, badRequest("model_id 无效")
			}
			p.ModelID = &id
		}
	}
	return p, nil
}

func (s *server) createConversation(c echo.Context) error {
	p, err := conversationPatch(c)
	if err != nil {
		return err
	}
	conv, err := s.Chat.CreateConversation(c.Request().Context(), currentUser(c), p)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, toConversation(conv))
}

func (s *server) updateConversation(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	p, err := conversationPatch(c)
	if err != nil {
		return err
	}
	conv, err := s.Chat.UpdateConversation(c.Request().Context(), currentUser(c), id, p)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toConversation(conv))
}

func (s *server) deleteConversation(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Chat.DeleteConversation(c.Request().Context(), currentUser(c), id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *server) listConversationMessages(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	return s.messages(c, id)
}

// listMessages 读取最近一段对话的消息（兼容只有一条对话的客户端，例如终端聊天）。
func (s *server) listMessages(c echo.Context) error {
	var convID uint64
	if raw := c.QueryParam("conversation_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return badRequest("conversation_id 无效")
		}
		convID = id
	}
	return s.messages(c, convID)
}

func (s *server) messages(c echo.Context, convID uint64) error {
	limit, err := queryInt(c, "limit", 50, 1, 200)
	if err != nil {
		return err
	}
	var before uint64
	if raw := c.QueryParam("before_id"); raw != "" {
		if before, err = strconv.ParseUint(raw, 10, 64); err != nil {
			return badRequest("before_id 无效")
		}
	}
	ctx, u := c.Request().Context(), currentUser(c)
	conv, msgs, err := s.Chat.History(ctx, u, convID, before, limit)
	if err != nil {
		return err
	}
	atts := s.Chat.MessageAttachments(ctx, u, msgs)
	out := make([]messageDTO, len(msgs))
	for i := range msgs {
		out[i] = toMessage(&msgs[i])
		list := atts[msgs[i].ID]
		if len(list) == 0 {
			continue
		}
		if msgs[i].Role == store.RoleUser {
			out[i].Attachments = toAttachments(list)
			continue
		}
		// 助手回复的附件是代码生成的图片和发送的文件，随工具记录展示；标出已经删除的。
		deleted := map[uint64]bool{}
		for _, a := range list {
			deleted[a.ID] = a.DeletedAt != nil
		}
		for j := range out[i].Tools {
			for k := range out[i].Tools[j].Images {
				img := &out[i].Tools[j].Images[k]
				img.Deleted = deleted[img.ID]
			}
			for k := range out[i].Tools[j].Files {
				f := &out[i].Tools[j].Files[k]
				f.Deleted = deleted[f.ID]
			}
		}
	}
	return c.JSON(http.StatusOK, map[string]any{
		"conversation_id": conv.ID,
		"messages":        out,
		"has_more":        len(msgs) == limit,
	})
}

// ---------- 记忆 ----------

func (s *server) listMemories(c echo.Context) error {
	limit, err := queryInt(c, "limit", 50, 1, 200)
	if err != nil {
		return err
	}
	offset, err := queryInt(c, "offset", 0, 0, 1<<30)
	if err != nil {
		return err
	}
	f := store.MemoryFilter{
		Type:   c.QueryParam("type"),
		Source: c.QueryParam("source"),
		Query:  strings.TrimSpace(c.QueryParam("q")),
	}
	if f.Type != "" && f.Type != store.MemoryEpisodic && f.Type != store.MemorySemantic {
		return badRequest("type 参数无效")
	}
	if f.Source != "" && f.Source != store.SourceStated && f.Source != store.SourceInferred {
		return badRequest("source 参数无效")
	}
	mems, total, err := s.Memories.List(c.Request().Context(), currentUser(c).ID, f, limit, offset)
	if err != nil {
		return err
	}
	out := make([]memoryDTO, len(mems))
	for i := range mems {
		out[i] = toMemory(&mems[i])
	}
	return c.JSON(http.StatusOK, map[string]any{"memories": out, "total": total})
}

func (s *server) getMemory(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	m, err := s.Memories.Get(c.Request().Context(), currentUser(c).ID, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toMemory(m))
}

func (s *server) deleteMemory(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Memories.Delete(c.Request().Context(), currentUser(c).ID, id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// debugPrompt 返回本轮会发送给模型的完整提示词（不调用模型、不保存数据）。
// single 模式总是可用；multi 模式需在配置中开启 debug.prompt_endpoint，且仅管理员可用。
func (s *server) debugPrompt(c echo.Context) error {
	if s.Config.Mode == config.ModeMulti && (!s.Config.Debug.PromptEndpoint || !currentUser(c).IsAdmin) {
		return echo.ErrNotFound
	}
	q := c.QueryParam("q")
	var convID uint64
	if raw := c.QueryParam("conversation_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return badRequest("conversation_id 无效")
		}
		convID = id
	}
	p, err := s.Chat.Preview(c.Request().Context(), currentUser(c), convID, q)
	if err != nil {
		return err
	}
	if c.QueryParam("format") == "text" {
		return c.String(http.StatusOK, p.Render())
	}
	return c.JSON(http.StatusOK, map[string]any{
		"messages":         p.Messages,
		"sections":         p.Sections,
		"memories":         toUsedMemories(p.UsedMemories),
		"dropped_memories": p.DroppedMemories,
		"history_included": p.HistoryIncluded,
		"dropped_history":  p.DroppedHistory,
		"estimated_tokens": p.EstimatedTokens,
		"token_budget":     p.ContextTokenSize,
	})
}

// ---------- 管理 ----------

func (s *server) adminUsers(c echo.Context) error {
	limit, err := queryInt(c, "limit", 100, 1, 1000)
	if err != nil {
		return err
	}
	offset, err := queryInt(c, "offset", 0, 0, 1<<30)
	if err != nil {
		return err
	}
	us, err := s.Store.Users().List(c.Request().Context(), limit, offset)
	if err != nil {
		return err
	}
	out := make([]userDTO, len(us))
	for i := range us {
		out[i] = toUser(&us[i])
	}
	return c.JSON(http.StatusOK, map[string]any{"users": out})
}

type inviteDTO struct {
	ID        uint64     `json:"id"`
	Code      string     `json:"code"`
	MaxUses   int        `json:"max_uses"`
	UsedCount int        `json:"used_count"`
	ExpiresAt *time.Time `json:"expires_at"`
	Note      string     `json:"note"`
	CreatedAt time.Time  `json:"created_at"`
}

func toInvite(i *store.Invite) inviteDTO {
	return inviteDTO{ID: i.ID, Code: i.Code, MaxUses: i.MaxUses, UsedCount: i.UsedCount, ExpiresAt: i.ExpiresAt, Note: i.Note, CreatedAt: i.CreatedAt}
}

func (s *server) adminListInvites(c echo.Context) error {
	invs, err := s.Store.Invites().List(c.Request().Context(), 500, 0)
	if err != nil {
		return err
	}
	out := make([]inviteDTO, len(invs))
	for i := range invs {
		out[i] = toInvite(&invs[i])
	}
	return c.JSON(http.StatusOK, map[string]any{"invites": out})
}

func (s *server) adminCreateInvite(c echo.Context) error {
	var req struct {
		MaxUses  *int   `json:"max_uses"`
		TTLHours int    `json:"ttl_hours"`
		Note     string `json:"note"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	in := auth.InviteInput{MaxUses: 1, TTL: time.Duration(req.TTLHours) * time.Hour, Note: strings.TrimSpace(req.Note)}
	if req.MaxUses != nil {
		in.MaxUses = *req.MaxUses
	}
	inv, err := s.Auth.CreateInvite(c.Request().Context(), currentUser(c).ID, in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, toInvite(inv))
}

func (s *server) adminDeleteInvite(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Store.Invites().Delete(c.Request().Context(), id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// ---------- 情绪 ----------

func (s *server) getEmotion(c echo.Context) error {
	if s.Emotion == nil {
		return c.JSON(http.StatusOK, map[string]any{"enabled": false})
	}
	v, err := s.Emotion.Get(c.Request().Context(), currentUser(c).ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}

// resetEmotion 让布谷对当前用户的情绪与关系从头开始。
func (s *server) resetEmotion(c echo.Context) error {
	if s.Emotion == nil {
		return c.NoContent(http.StatusNoContent)
	}
	if err := s.Emotion.Reset(c.Request().Context(), currentUser(c).ID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
