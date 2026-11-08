package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HugehardJiang/buugoo/internal/netguard"
	"github.com/HugehardJiang/buugoo/internal/secret"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// 供应商：一个 OpenAI 兼容接口的地址和 API Key，下面挂多个模型（类似 OpenCode 的 provider）。
// 模型表里照旧存着地址和 Key（和供应商保持一致），所以聊天、计费等其他代码不用管供应商。

// 限制。
const (
	// MaxBatchModels 是一次最多添加多少个模型。
	MaxBatchModels = 100
	remoteTimeout  = 15 * time.Second
)

// ProviderView 是返回给前端的供应商（不含明文 Key）。
type ProviderView struct {
	ID       uint64 `json:"id"`
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
	KeyMask  string `json:"key_mask"`
	Scope    string `json:"scope"`
	Editable bool   `json:"editable"`
}

// ProviderInput 是新增或修改供应商的参数。修改时 APIKey 为空表示保持不变。
type ProviderInput struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	// Public 为 true 时添加为公共供应商（仅管理员；single 模式下总是公共的）。
	Public bool `json:"public"`
}

func (in *ProviderInput) validate(requireKey bool) error {
	in.Name = strings.TrimSpace(in.Name)
	in.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	in.APIKey = strings.TrimSpace(in.APIKey)
	if in.Name == "" && validURL(in.BaseURL) {
		in.Name = GuessProviderName(in.BaseURL)
	}
	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > 64:
		return &InvalidError{"名称需为 1-64 个字"}
	case !validURL(in.BaseURL):
		return &InvalidError{"Base URL 需以 http:// 或 https:// 开头，例如 https://api.deepseek.com/v1"}
	case requireKey && in.APIKey == "":
		return &InvalidError{"请填写 API Key"}
	case len(in.APIKey) > 1024:
		return &InvalidError{"API Key 过长"}
	}
	return nil
}

// knownHosts 按地址认出常见的供应商。
var knownHosts = []struct{ match, name string }{
	{"opencode.ai/zen/go", "OpenCode Go"}, {"opencode.ai/inference/go", "OpenCode Go"}, {"opencode.ai", "OpenCode Zen"},
	{"api.deepseek.com", "DeepSeek"}, {"dashscope", "阿里云百炼"}, {"siliconflow", "硅基流动"},
	{"api.openai.com", "OpenAI"}, {"openrouter.ai", "OpenRouter"}, {"api.anthropic.com", "Anthropic"},
	{"generativelanguage.googleapis.com", "Google Gemini"}, {"moonshot", "Moonshot"}, {"bigmodel.cn", "智谱"},
	{"volces.com", "火山方舟"}, {"hunyuan", "腾讯混元"}, {"minimax", "MiniMax"}, {"x.ai", "xAI"},
	{"groq.com", "Groq"}, {"together", "Together"}, {"mistral.ai", "Mistral"}, {"localhost", "本机"}, {"127.0.0.1", "本机"},
	{":11434", "Ollama"},
}

// GuessProviderName 按地址猜供应商的名字，认不出时用域名。
func GuessProviderName(baseURL string) string {
	low := strings.ToLower(baseURL)
	for _, k := range knownHosts {
		if strings.Contains(low, k.match) {
			return k.name
		}
	}
	if u, err := url.Parse(baseURL); err == nil && u.Hostname() != "" {
		return strings.TrimPrefix(u.Hostname(), "api.")
	}
	return "自定义"
}

// guessThinking 按供应商地址猜模型的思考参数格式。
func guessThinking(baseURL, model string) string {
	low, m := strings.ToLower(baseURL), strings.ToLower(model)
	switch {
	case strings.Contains(low, "api.deepseek.com"):
		return store.ThinkingStyleDeepSeek
	case strings.Contains(low, "dashscope"), strings.Contains(low, "siliconflow"):
		if strings.Contains(m, "qwen3") || strings.Contains(m, "qwq") {
			return store.ThinkingStyleEnable
		}
		return store.ThinkingStyleNone
	case strings.Contains(low, "opencode.ai"), strings.Contains(low, "api.openai.com"), strings.Contains(low, "openrouter.ai"):
		return store.ThinkingStyleEffort
	}
	if regexp.MustCompile(`(^|[/-])(o[134]|gpt-5)`).MatchString(m) {
		return store.ThinkingStyleEffort
	}
	return store.ThinkingStyleNone
}

// visionHint 按模型名猜能不能看图。
var visionHint = regexp.MustCompile(`(?i)(vl|vision|omni|gpt-4o|gpt-4\.1|gpt-5|claude|gemini|glm-4v|glm-5v|qvq|pixtral|llava|kimi-k2\.5|kimi-k3|deepseek-flash|mimo-v)`)

// noChat 是一看就不是聊天模型的名字（向量、语音、画图、排序），从列表里藏到后面。
var noChat = regexp.MustCompile(`(?i)(embed|rerank|whisper|tts|speech|audio|dall-e|image|moderation|stable-diffusion|flux|sora|bge-|text-embedding)`)

func (s *Service) providerView(p *store.ModelProvider, u User) ProviderView {
	scope := ScopePersonal
	if p.Public() {
		scope = ScopePublic
	}
	mask := "已加密保存"
	if key, err := s.box.Open(p.APIKey); err == nil {
		mask = secret.Mask(key)
	} else if p.APIKey != "" {
		mask = "无法解密，请重新填写"
	}
	return ProviderView{ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, KeyMask: mask, Scope: scope, Editable: s.canEditProvider(p, u)}
}

func (s *Service) canEditProvider(p *store.ModelProvider, u User) bool {
	if p.Public() {
		return u.IsAdmin
	}
	return p.OwnerID == u.ID
}

// Providers 列出用户看得到的供应商：公共的（只列出下面有自己能用的模型的，管理员全列）和自己的。
func (s *Service) Providers(ctx context.Context, u User) ([]ProviderView, error) {
	ps, err := s.store.Providers().ListVisible(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	var usable map[uint64]bool
	if !u.IsAdmin && !s.singleMode() {
		ms, err := s.store.Models().ListVisible(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		usable = map[uint64]bool{}
		for _, m := range s.visibleTo(ms, u) {
			if m.ProviderID != nil {
				usable[*m.ProviderID] = true
			}
		}
	}
	out := []ProviderView{}
	for i := range ps {
		if ps[i].Public() && usable != nil && !usable[ps[i].ID] {
			continue
		}
		out = append(out, s.providerView(&ps[i], u))
	}
	return out, nil
}

// editableProvider 返回用户能修改的供应商。
func (s *Service) editableProvider(ctx context.Context, u User, id uint64) (*store.ModelProvider, error) {
	p, err := s.store.Providers().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !p.Public() && p.OwnerID != u.ID {
		return nil, store.ErrNotFound
	}
	if !s.canEditProvider(p, u) {
		return nil, ErrForbidden
	}
	return p, nil
}

func (s *Service) ownerFor(u User, public bool) (uint64, error) {
	if s.singleMode() || public {
		if !s.singleMode() && !u.IsAdmin {
			return 0, ErrForbidden
		}
		return store.PublicOwner, nil
	}
	return u.ID, nil
}

// CreateProvider 添加供应商。
func (s *Service) CreateProvider(ctx context.Context, u User, in ProviderInput) (ProviderView, error) {
	if err := in.validate(true); err != nil {
		return ProviderView{}, err
	}
	owner, err := s.ownerFor(u, in.Public)
	if err != nil {
		return ProviderView{}, err
	}
	sealed, err := s.box.Seal(in.APIKey)
	if err != nil {
		return ProviderView{}, err
	}
	p := &store.ModelProvider{OwnerID: owner, Name: in.Name, BaseURL: in.BaseURL, APIKey: sealed}
	if err := s.store.Providers().Create(ctx, p); err != nil {
		return ProviderView{}, err
	}
	return s.providerView(p, u), nil
}

// UpdateProvider 修改供应商，地址和 Key 同步到它下面的所有模型。APIKey 为空时保留原来的 Key。
func (s *Service) UpdateProvider(ctx context.Context, u User, id uint64, in ProviderInput) (ProviderView, error) {
	p, err := s.editableProvider(ctx, u, id)
	if err != nil {
		return ProviderView{}, err
	}
	if err := in.validate(false); err != nil {
		return ProviderView{}, err
	}
	if in.APIKey != "" {
		if p.APIKey, err = s.box.Seal(in.APIKey); err != nil {
			return ProviderView{}, err
		}
	}
	p.Name, p.BaseURL = in.Name, in.BaseURL
	if err := s.store.Providers().Update(ctx, p); err != nil {
		return ProviderView{}, err
	}
	return s.providerView(p, u), nil
}

// DeleteProvider 删除供应商和它下面的所有模型（用这些模型的对话改回默认模型）。
func (s *Service) DeleteProvider(ctx context.Context, u User, id uint64) (int, error) {
	p, err := s.editableProvider(ctx, u, id)
	if err != nil {
		return 0, err
	}
	ms, err := s.store.Models().ListByProvider(ctx, p.ID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range ms {
		if err := s.Delete(ctx, u, m.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return n, err
		}
		n++
	}
	return n, s.store.Providers().Delete(ctx, p.ID)
}

// RemoteModel 是供应商接口列出的一个模型，带上猜的能力，添加前可以改。
type RemoteModel struct {
	ID            string `json:"id"`
	Added         bool   `json:"added"`
	Chat          bool   `json:"chat"`
	Vision        bool   `json:"vision"`
	Tools         bool   `json:"tools"`
	ThinkingStyle string `json:"thinking_style"`
	ContextTokens int    `json:"context_tokens,omitempty"`
}

// RemoteModels 请求供应商的 GET /models，列出它有哪些模型，标出已经添加过的。
func (s *Service) RemoteModels(ctx context.Context, u User, id uint64) ([]RemoteModel, error) {
	p, err := s.editableProvider(ctx, u, id)
	if err != nil {
		return nil, err
	}
	key, err := s.box.Open(p.APIKey)
	if err != nil {
		return nil, &InvalidError{"这个供应商的 Key 解不开了，请重新填写"}
	}
	items, err := fetchModelList(ctx, p.BaseURL, key, !p.Public() && s.untrusted(u))
	if err != nil {
		return nil, &InvalidError{"没读到这个供应商的模型列表（" + err.Error() + "），可以手动填模型名"}
	}
	have, err := s.store.Models().ListByProvider(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	added := map[string]bool{}
	for _, m := range have {
		added[m.Model] = true
	}
	out := make([]RemoteModel, 0, len(items))
	seen := map[string]bool{}
	for _, it := range items {
		id, _ := it["id"].(string)
		id = strings.TrimPrefix(strings.TrimSpace(id), "models/")
		if id == "" || len(id) > 128 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, RemoteModel{ID: id, Added: added[id], Chat: !noChat.MatchString(id), Vision: visionHint.MatchString(id),
			Tools: true, ThinkingStyle: guessThinking(p.BaseURL, id), ContextTokens: apiContext(it)})
	}
	// 聊天模型在前，各自按名字排。
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Chat != out[j].Chat {
			return out[i].Chat
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// fetchModelList 请求 OpenAI 兼容的 GET /models。
func fetchModelList(ctx context.Context, baseURL, key string, publicOnly bool) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, errors.New("地址不对")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
	if publicOnly {
		client.Transport = netguard.PublicTransport()
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("超时")
		}
		return nil, errors.New("连不上")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&body) != nil {
		return nil, errors.New("返回的不是模型列表")
	}
	return body.Data, nil
}

// AddItem 是批量添加时的一个模型。
type AddItem struct {
	Model         string `json:"model"`
	Name          string `json:"name"`
	Vision        bool   `json:"vision"`
	Tools         bool   `json:"tools"`
	ThinkingStyle string `json:"thinking_style"`
	ContextTokens int    `json:"context_tokens"`
}

// AddModels 在供应商下面一次添加多个模型（已经有的跳过）。restricted 只对公共供应商有效。
func (s *Service) AddModels(ctx context.Context, u User, id uint64, items []AddItem, restricted bool) (added int, err error) {
	p, err := s.editableProvider(ctx, u, id)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, &InvalidError{"至少选一个模型"}
	}
	if len(items) > MaxBatchModels {
		return 0, &InvalidError{fmt.Sprintf("一次最多添加 %d 个", MaxBatchModels)}
	}
	have, err := s.store.Models().ListByProvider(ctx, p.ID)
	if err != nil {
		return 0, err
	}
	exists := map[string]bool{}
	for _, m := range have {
		exists[m.Model] = true
	}
	for _, it := range items {
		it.Model = strings.TrimSpace(it.Model)
		if exists[it.Model] {
			continue
		}
		name := strings.TrimSpace(it.Name)
		if name == "" {
			name = it.Model
		}
		if r := []rune(name); len(r) > 64 {
			name = string(r[:64])
		}
		if it.ThinkingStyle == "" {
			it.ThinkingStyle = guessThinking(p.BaseURL, it.Model)
		}
		if it.ContextTokens < 0 || it.ContextTokens > 20_000_000 {
			it.ContextTokens = 0
		}
		in := Input{Name: name, Model: it.Model, ProviderID: p.ID, ThinkingStyle: it.ThinkingStyle, Vision: it.Vision, Tools: it.Tools,
			Temperature: 0.8, ContextTokens: it.ContextTokens, Public: p.Public(), Restricted: restricted}
		if _, err := s.Create(ctx, u, in); err != nil {
			return added, fmt.Errorf("添加「%s」: %w", it.Model, err)
		}
		exists[it.Model] = true
		added++
	}
	return added, nil
}

// fillFromProvider 用供应商的地址和 Key 填好模型输入，返回供应商。
func (s *Service) fillFromProvider(ctx context.Context, u User, in *Input) (*store.ModelProvider, error) {
	p, err := s.editableProvider(ctx, u, in.ProviderID)
	if err != nil {
		return nil, err
	}
	key, err := s.box.Open(p.APIKey)
	if err != nil {
		return nil, &InvalidError{"这个供应商的 Key 解不开了，请重新填写"}
	}
	in.BaseURL, in.APIKey, in.Public = p.BaseURL, key, p.Public()
	return p, nil
}

// attach 把没有供应商的模型归到同一个所有者、同样地址和 Key 的供应商下面，没有就新建一个。
// key 是明文 Key。
func (s *Service) attach(ctx context.Context, m *store.ModelConfig, key string) error {
	ps, err := s.store.Providers().ListOwned(ctx, m.OwnerID)
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for i := range ps {
		names[ps[i].Name] = true
		if ps[i].BaseURL != m.BaseURL {
			continue
		}
		if k, err := s.box.Open(ps[i].APIKey); (err == nil && k == key) || ps[i].APIKey == m.APIKey {
			m.ProviderID = &ps[i].ID
			return s.store.Models().Update(ctx, m)
		}
	}
	name := GuessProviderName(m.BaseURL)
	for i := 2; names[name]; i++ {
		name = fmt.Sprintf("%s %d", GuessProviderName(m.BaseURL), i)
	}
	p := &store.ModelProvider{OwnerID: m.OwnerID, Name: name, BaseURL: m.BaseURL, APIKey: m.APIKey}
	if err := s.store.Providers().Create(ctx, p); err != nil {
		return err
	}
	m.ProviderID = &p.ID
	return s.store.Models().Update(ctx, m)
}

// GroupModels 把还没有供应商的模型（升级前添加的）按「所有者 + 地址 + Key」归组。启动时调用，可以重复调用。
func (s *Service) GroupModels(ctx context.Context) (int, error) {
	ms, err := s.store.Models().ListUngrouped(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range ms {
		key, err := s.box.Open(ms[i].APIKey)
		if err != nil {
			key = "\x00" + ms[i].APIKey // 解不开的按密文归组
		}
		if err := s.attach(ctx, &ms[i], key); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// dropIfEmpty 在供应商下面没有模型时删掉它（模型换到别的供应商之后调用）。
func (s *Service) dropIfEmpty(ctx context.Context, id uint64) {
	ms, err := s.store.Models().ListByProvider(ctx, id)
	if err == nil && len(ms) == 0 {
		_ = s.store.Providers().Delete(ctx, id)
	}
}
