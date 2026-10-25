// Package models 管理可用的聊天模型与全站唯一的向量模型：
//
//   - 内置模型：配置文件 llm.chat 中的模型（ID 为 0），填写了 API Key 时可用；
//   - 公共模型：管理员在网页中添加，所有用户可用，消耗计入每日限额；
//   - 个人模型：用户用自己的 API Key 添加，只有自己可见，不计入限额。
//
// API Key 用 secret.Box 加密后存入数据库，接口只返回打码后的值。
package models

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/llm/openai"
	"github.com/HugehardJiang/buugoo/internal/secret"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// BuiltinID 是配置文件中默认模型的 ID。
const BuiltinID uint64 = 0

// 模型范围。
const (
	ScopeBuiltin  = "builtin"
	ScopePublic   = "public"
	ScopePersonal = "personal"
)

// 错误。
var (
	ErrNoModel   = errors.New("还没有可用的模型，请在「设置 → 模型」中添加")
	ErrForbidden = errors.New("没有权限修改这个模型")
)

// InvalidError 是输入校验错误。
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

// User 是调用方身份（避免依赖 auth 包）。
type User struct {
	ID      uint64
	IsAdmin bool
}

// View 是返回给前端的模型信息（不含明文 Key）。
type View struct {
	ID            uint64  `json:"id"`
	Name          string  `json:"name"`
	BaseURL       string  `json:"base_url"`
	Model         string  `json:"model"`
	ThinkingStyle string  `json:"thinking_style"`
	ThinkingModel string  `json:"thinking_model"`
	Temperature   float64 `json:"temperature"`
	MaxTokens     int     `json:"max_tokens"`
	Vision        bool    `json:"vision"`
	Tools         bool    `json:"tools"`
	Scope         string  `json:"scope"`
	IsDefault     bool    `json:"is_default"`
	KeyMask       string  `json:"key_mask"`
	Editable      bool    `json:"editable"`
}

// Input 是新增或修改模型的参数。修改时 APIKey 为空表示保持不变。
type Input struct {
	Name          string  `json:"name"`
	BaseURL       string  `json:"base_url"`
	APIKey        string  `json:"api_key"`
	Model         string  `json:"model"`
	ThinkingStyle string  `json:"thinking_style"`
	ThinkingModel string  `json:"thinking_model"`
	Temperature   float64 `json:"temperature"`
	MaxTokens     int     `json:"max_tokens"`
	Vision        bool    `json:"vision"`
	Tools         bool    `json:"tools"`
	// Public 为 true 时添加为公共模型（仅管理员；single 模式下总是公共模型）。
	Public bool `json:"public"`
}

// Resolved 是一轮对话实际使用的模型。
type Resolved struct {
	ID            uint64
	Name          string
	Model         string
	ThinkingStyle string
	ThinkingModel string
	Chat          llm.ChatProvider
	// Personal 为 true 表示使用用户自己的 API Key，不计入每日限额。
	Personal bool
	// Vision 表示模型能看图；Tools 表示支持工具调用。
	Vision bool
	Tools  bool
	// BaseURL 与 APIKey 供同一服务商的其他接口使用（例如 DeepSeek 的搜索与抓取），不对外展示。
	BaseURL string
	APIKey  string
}

// ModelFor 返回指定思考等级下实际调用的模型名（「换用推理模型」开启思考时为推理模型）。
func (r *Resolved) ModelFor(thinking string) string {
	if r.ThinkingStyle == store.ThinkingStyleSwitchMod && thinking != "" && thinking != store.ThinkingOff && r.ThinkingModel != "" {
		return r.ThinkingModel
	}
	return r.Model
}

// Service 是模型管理服务。
type Service struct {
	store store.Store
	box   *secret.Box
	cfg   *config.Config
	log   *slog.Logger
	emb   *DynamicEmbedding
	// newChat 便于测试替换。publicOnly 为 true 时只允许连接公网地址。
	newChat func(c config.ModelConfig, publicOnly bool) llm.ChatProvider
}

// NewService 创建服务，并按网页中保存的设置（没有则用配置文件）初始化向量模型。
func NewService(ctx context.Context, s store.Store, box *secret.Box, cfg *config.Config, log *slog.Logger) (*Service, error) {
	if log == nil {
		log = slog.Default()
	}
	svc := &Service{store: s, box: box, cfg: cfg, log: log.With("component", "models"),
		newChat: func(c config.ModelConfig, publicOnly bool) llm.ChatProvider {
			if publicOnly {
				return openai.NewPublicOnlyChat(c)
			}
			return openai.NewChat(c)
		}}
	emb, err := svc.loadEmbedding(ctx)
	if err != nil {
		return nil, err
	}
	svc.emb = NewDynamicEmbedding(emb)
	return svc, nil
}

func (s *Service) singleMode() bool { return s.cfg.Mode == config.ModeSingle }

// untrusted 报告该用户填写的模型地址是否需要限制为公网地址：
// 多用户模式下普通用户自行添加的模型不能用来访问服务器内网。
func (s *Service) untrusted(u User) bool {
	return !s.singleMode() && !u.IsAdmin && !s.cfg.Security.AllowPrivateModelURLs
}

// builtinAvailable 报告配置文件中的默认模型是否可用。
func (s *Service) builtinAvailable() bool { return s.cfg.LLM.Chat.APIKey != "" }

func (s *Service) builtinView(isDefault bool) View {
	c := s.cfg.LLM.Chat
	style := c.ThinkingStyle
	if style == "" {
		style = store.ThinkingStyleNone
	}
	return View{
		ID: BuiltinID, Name: c.Model, BaseURL: c.BaseURL, Model: c.Model,
		ThinkingStyle: style, ThinkingModel: c.ThinkingModel, Temperature: float64(c.Temperature), MaxTokens: c.MaxTokens,
		Vision: c.Vision, Tools: c.Tools, Scope: ScopeBuiltin, IsDefault: isDefault, KeyMask: secret.Mask(c.APIKey),
	}
}

func (s *Service) view(m *store.ModelConfig, u User, isDefault bool) View {
	scope := ScopePersonal
	if m.Public() {
		scope = ScopePublic
	}
	mask := "已加密保存"
	if key, err := s.box.Open(m.APIKey); err == nil {
		mask = secret.Mask(key)
	} else if m.APIKey != "" {
		mask = "无法解密，请重新填写"
	}
	return View{
		ID: m.ID, Name: m.Name, BaseURL: m.BaseURL, Model: m.Model, ThinkingStyle: m.ThinkingStyle,
		ThinkingModel: m.ThinkingModel, Temperature: m.Temperature, MaxTokens: m.MaxTokens,
		Vision: m.Vision, Tools: m.Tools, Scope: scope, IsDefault: isDefault, KeyMask: mask, Editable: s.canEdit(m, u),
	}
}

func (s *Service) canEdit(m *store.ModelConfig, u User) bool {
	if m.Public() {
		return u.IsAdmin
	}
	return m.OwnerID == u.ID
}

// List 列出用户可用的模型（内置 → 公共 → 个人），并标出该用户当前的默认模型。
func (s *Service) List(ctx context.Context, u User) ([]View, error) {
	ms, err := s.store.Models().ListVisible(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	def := s.defaultID(ms, u)
	var out []View
	if s.builtinAvailable() {
		out = append(out, s.builtinView(def == BuiltinID))
	}
	for i := range ms {
		out = append(out, s.view(&ms[i], u, ms[i].ID == def))
	}
	return out, nil
}

// defaultID 返回用户的默认模型：个人默认 → 公共默认 → 内置 → 第一个公共 → 第一个个人。
// 没有任何模型时返回 BuiltinID（Resolve 会报 ErrNoModel）。
func (s *Service) defaultID(ms []store.ModelConfig, u User) uint64 {
	for _, m := range ms {
		if m.IsDefault && m.OwnerID == u.ID && !m.Public() {
			return m.ID
		}
	}
	for _, m := range ms {
		if m.IsDefault && m.Public() {
			return m.ID
		}
	}
	if s.builtinAvailable() {
		return BuiltinID
	}
	for _, m := range ms {
		if m.Public() {
			return m.ID
		}
	}
	if len(ms) > 0 {
		return ms[0].ID
	}
	return BuiltinID
}

// Visible 报告用户能否使用该模型（BuiltinID 在内置模型可用时可见）。
func (s *Service) Visible(ctx context.Context, u User, id uint64) (bool, error) {
	if id == BuiltinID {
		return s.builtinAvailable(), nil
	}
	m, err := s.store.Models().Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return m.Public() || m.OwnerID == u.ID, nil
}

// Resolve 返回本轮对话使用的模型。modelID 为空、已删除或无权使用时使用默认模型。
func (s *Service) Resolve(ctx context.Context, u User, modelID *uint64) (*Resolved, error) {
	if modelID != nil {
		ok, err := s.Visible(ctx, u, *modelID)
		if err != nil {
			return nil, err
		}
		if ok {
			return s.resolveID(ctx, u, *modelID)
		}
	}
	ms, err := s.store.Models().ListVisible(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return s.resolveID(ctx, u, s.defaultID(ms, u))
}

func (s *Service) resolveID(ctx context.Context, u User, id uint64) (*Resolved, error) {
	if id == BuiltinID {
		if !s.builtinAvailable() {
			return nil, ErrNoModel
		}
		c := s.cfg.LLM.Chat
		return &Resolved{ID: BuiltinID, Name: s.builtinView(false).Name, Model: c.Model,
			ThinkingStyle: c.ThinkingStyle, ThinkingModel: c.ThinkingModel, Chat: s.newChat(c, false),
			Vision: c.Vision, Tools: c.Tools, BaseURL: c.BaseURL, APIKey: c.APIKey}, nil
	}
	m, err := s.store.Models().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	key, err := s.box.Open(m.APIKey)
	if err != nil {
		return nil, fmt.Errorf("模型「%s」: %w", m.Name, err)
	}
	return &Resolved{
		ID: m.ID, Name: m.Name, Model: m.Model, ThinkingStyle: m.ThinkingStyle, ThinkingModel: m.ThinkingModel,
		Chat:     s.newChat(s.chatConfig(m, key), !m.Public() && s.untrusted(u)),
		Personal: !m.Public() && !s.singleMode(),
		Vision:   m.Vision, Tools: m.Tools, BaseURL: m.BaseURL, APIKey: key,
	}, nil
}

func (s *Service) chatConfig(m *store.ModelConfig, key string) config.ModelConfig {
	return config.ModelConfig{
		Provider: "openai", BaseURL: m.BaseURL, APIKey: key, Model: m.Model,
		Temperature: float32(m.Temperature), MaxTokens: m.MaxTokens, Timeout: 180 * time.Second,
		ThinkingStyle: m.ThinkingStyle, ThinkingModel: m.ThinkingModel, Vision: m.Vision, Tools: m.Tools,
	}
}

func validateInput(in *Input, requireKey bool) error {
	in.Name = strings.TrimSpace(in.Name)
	in.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	in.Model = strings.TrimSpace(in.Model)
	in.APIKey = strings.TrimSpace(in.APIKey)
	in.ThinkingModel = strings.TrimSpace(in.ThinkingModel)
	if in.ThinkingStyle == "" {
		in.ThinkingStyle = store.ThinkingStyleNone
	}
	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > 64:
		return &InvalidError{"名称需为 1-64 个字"}
	case in.Model == "" || len(in.Model) > 128:
		return &InvalidError{"请填写模型名，例如 deepseek-chat"}
	case !validURL(in.BaseURL):
		return &InvalidError{"Base URL 需以 http:// 或 https:// 开头，例如 https://api.deepseek.com/v1"}
	case requireKey && in.APIKey == "":
		return &InvalidError{"请填写 API Key"}
	case len(in.APIKey) > 1024:
		return &InvalidError{"API Key 过长"}
	case !store.ValidThinkingStyle(in.ThinkingStyle):
		return &InvalidError{"思考参数格式无效"}
	case in.ThinkingStyle == store.ThinkingStyleSwitchMod && in.ThinkingModel == "":
		return &InvalidError{"「换用推理模型」需要填写推理模型名，例如 deepseek-reasoner"}
	case in.Temperature < 0 || in.Temperature > 2:
		return &InvalidError{"temperature 需在 0-2 之间"}
	case in.MaxTokens < 0 || in.MaxTokens > 65536:
		return &InvalidError{"最大输出长度需在 0-65536 之间（0 表示使用服务默认值）"}
	}
	return nil
}

func validURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// Create 添加模型。single 模式下添加为公共模型；multi 模式下 Public 仅管理员可用。
func (s *Service) Create(ctx context.Context, u User, in Input) (View, error) {
	if err := validateInput(&in, true); err != nil {
		return View{}, err
	}
	owner := u.ID
	if s.singleMode() || in.Public {
		if !s.singleMode() && !u.IsAdmin {
			return View{}, ErrForbidden
		}
		owner = store.PublicOwner
	}
	sealed, err := s.box.Seal(in.APIKey)
	if err != nil {
		return View{}, err
	}
	m := &store.ModelConfig{
		OwnerID: owner, Name: in.Name, BaseURL: in.BaseURL, APIKey: sealed, Model: in.Model,
		ThinkingStyle: in.ThinkingStyle, ThinkingModel: in.ThinkingModel,
		Temperature: in.Temperature, MaxTokens: in.MaxTokens, Vision: in.Vision, Tools: in.Tools,
	}
	if err := s.store.Models().Create(ctx, m); err != nil {
		return View{}, err
	}
	return s.view(m, u, false), nil
}

func (s *Service) editable(ctx context.Context, u User, id uint64) (*store.ModelConfig, error) {
	if id == BuiltinID {
		return nil, &InvalidError{"默认模型来自配置文件，请在配置文件中修改"}
	}
	m, err := s.store.Models().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !m.Public() && m.OwnerID != u.ID {
		return nil, store.ErrNotFound // 不暴露他人模型是否存在
	}
	if !s.canEdit(m, u) {
		return nil, ErrForbidden
	}
	return m, nil
}

// Update 修改模型，APIKey 为空时保留原来的 Key。
func (s *Service) Update(ctx context.Context, u User, id uint64, in Input) (View, error) {
	m, err := s.editable(ctx, u, id)
	if err != nil {
		return View{}, err
	}
	if err := validateInput(&in, false); err != nil {
		return View{}, err
	}
	if in.APIKey != "" {
		if m.APIKey, err = s.box.Seal(in.APIKey); err != nil {
			return View{}, err
		}
	}
	m.Name, m.BaseURL, m.Model = in.Name, in.BaseURL, in.Model
	m.ThinkingStyle, m.ThinkingModel = in.ThinkingStyle, in.ThinkingModel
	m.Temperature, m.MaxTokens = in.Temperature, in.MaxTokens
	m.Vision, m.Tools = in.Vision, in.Tools
	if err := s.store.Models().Update(ctx, m); err != nil {
		return View{}, err
	}
	return s.view(m, u, m.IsDefault), nil
}

// Delete 删除模型，使用它的对话改回默认模型。
func (s *Service) Delete(ctx context.Context, u User, id uint64) error {
	if _, err := s.editable(ctx, u, id); err != nil {
		return err
	}
	if err := s.store.Models().Delete(ctx, id); err != nil {
		return err
	}
	return s.store.Conversations().ClearModel(ctx, id)
}

// SetDefault 设为默认模型：个人模型设为自己的默认，公共模型设为全站默认（仅管理员）。
// 内置模型（配置文件）只能由管理员设为全站默认。
func (s *Service) SetDefault(ctx context.Context, u User, id uint64) error {
	if id == BuiltinID {
		if !u.IsAdmin || !s.builtinAvailable() {
			return ErrForbidden
		}
		if err := s.store.Models().ClearDefault(ctx, u.ID); err != nil {
			return err
		}
		return s.store.Models().ClearDefault(ctx, store.PublicOwner)
	}
	m, err := s.editable(ctx, u, id)
	if err != nil {
		return err
	}
	if m.Public() && u.ID != store.PublicOwner {
		// 管理员设置全站默认时，同时取消自己的个人默认，否则看不到效果。
		if err := s.store.Models().ClearDefault(ctx, u.ID); err != nil {
			return err
		}
	}
	return s.store.Models().SetDefault(ctx, m.OwnerID, m.ID)
}

// ClearDefault 取消用户自己的默认模型，改用全站默认。
func (s *Service) ClearDefault(ctx context.Context, u User) error {
	return s.store.Models().ClearDefault(ctx, u.ID)
}

// TestResult 是连通性测试结果。
type TestResult struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	Reply     string `json:"reply,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Test 用一条简短的消息测试模型。id 为 0 且 in 非空时测试尚未保存的配置；
// 修改已有模型时 in.APIKey 为空则使用已保存的 Key。
func (s *Service) Test(ctx context.Context, u User, id uint64, in *Input) (TestResult, error) {
	var cfg config.ModelConfig
	switch {
	case in != nil:
		if err := validateInput(in, id == 0); err != nil {
			return TestResult{}, err
		}
		key := in.APIKey
		if key == "" && id != 0 {
			m, err := s.editable(ctx, u, id)
			if err != nil {
				return TestResult{}, err
			}
			if key, err = s.box.Open(m.APIKey); err != nil {
				return TestResult{}, err
			}
		}
		cfg = s.chatConfig(&store.ModelConfig{BaseURL: in.BaseURL, Model: in.Model, ThinkingStyle: in.ThinkingStyle,
			ThinkingModel: in.ThinkingModel, Temperature: in.Temperature, MaxTokens: in.MaxTokens}, key)
	default:
		ok, err := s.Visible(ctx, u, id)
		if err != nil {
			return TestResult{}, err
		}
		if !ok {
			return TestResult{}, store.ErrNotFound
		}
		r, err := s.resolveID(ctx, u, id)
		if err != nil {
			return TestResult{}, err
		}
		return s.runTest(ctx, r.Chat), nil
	}
	return s.runTest(ctx, s.newChat(cfg, s.untrusted(u))), nil
}

func (s *Service) runTest(ctx context.Context, chat llm.ChatProvider) TestResult {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	start := time.Now()
	resp, err := chat.Chat(ctx, llm.ChatRequest{MaxTokens: 32, Messages: []llm.Message{{Role: llm.RoleUser, Content: "用一句话打个招呼。"}}})
	res := TestResult{LatencyMS: time.Since(start).Milliseconds()}
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.OK = true
	res.Reply = truncate(strings.TrimSpace(resp.Content), 60)
	return res
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// VisionHelper 返回用来把图片转述成文字的模型：当前用户可用、能看图的模型中，
// 依次优先默认模型、公共模型（含配置文件模型）、个人模型。没有时返回 ErrNoModel。
func (s *Service) VisionHelper(ctx context.Context, u User) (*Resolved, error) {
	ms, err := s.store.Models().ListVisible(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	def := s.defaultID(ms, u)
	var cands []uint64
	builtinVision := s.builtinAvailable() && s.cfg.LLM.Chat.Vision
	if def == BuiltinID && builtinVision {
		cands = append(cands, BuiltinID)
	}
	for _, m := range ms {
		if m.ID == def && m.Vision {
			cands = append(cands, m.ID)
		}
	}
	if builtinVision {
		cands = append(cands, BuiltinID)
	}
	for _, m := range ms {
		if m.Vision && m.Public() {
			cands = append(cands, m.ID)
		}
	}
	for _, m := range ms {
		if m.Vision && !m.Public() {
			cands = append(cands, m.ID)
		}
	}
	for _, id := range cands {
		r, err := s.resolveID(ctx, u, id)
		if err == nil {
			return r, nil
		}
		s.log.Warn("看图模型不可用", "model_id", id, "err", err)
	}
	return nil, ErrNoModel
}
