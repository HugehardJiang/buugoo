package models

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HugehardJiang/buugoo/internal/netguard"
)

// 模型上下文长度（token）。可以手动填写；没有填写时自动识别：先问服务商的 /models 接口，
// 再查内置的常见模型表，都没有时按 DefaultContextTokens 计算。

// DefaultContextTokens 是识别不出时使用的上下文长度。
const DefaultContextTokens = 64000

// 上下文长度的来源。
const (
	ContextManual  = "manual"  // 手动填写
	ContextAPI     = "api"     // 服务商接口返回
	ContextTable   = "table"   // 内置的常见模型表
	ContextDefault = "default" // 识别不出，用默认值
)

// ContextInfo 是一个模型的上下文长度及其来源。
type ContextInfo struct {
	Tokens int    `json:"tokens"`
	Source string `json:"source"`
}

// knownContexts 是常见模型的上下文长度（按名称包含的关键字匹配，越具体的越靠前）。仅供参考，以服务商文档为准。
var knownContexts = []struct {
	key    string
	tokens int
}{
	{"gpt-4.1", 1047576}, {"gpt-4o", 128000}, {"gpt-5", 400000}, {"o4-mini", 200000}, {"o3", 200000},
	{"claude", 200000}, {"gemini", 1048576},
	{"qwen-long", 1000000}, {"qwen-turbo", 1000000}, {"qwen-max", 32768}, {"qwen-plus", 131072}, {"qwen", 131072},
	{"deepseek", 128000}, {"glm", 128000}, {"kimi", 131072}, {"moonshot", 131072}, {"doubao", 128000},
	{"llama", 128000}, {"mistral", 128000}, {"yi-", 16384},
}

// sizeSuffix 匹配模型名中的「-32k」「-128K」这类上下文标注。
var sizeSuffix = regexp.MustCompile(`(?i)[-_](\d{1,4})k\b`)

// tableContext 按模型名查内置表，查不到返回 0。
func tableContext(model string) int {
	m := strings.ToLower(model)
	if s := sizeSuffix.FindStringSubmatch(m); s != nil {
		if n, err := strconv.Atoi(s[1]); err == nil && n >= 4 {
			return n * 1024
		}
	}
	for _, k := range knownContexts {
		if strings.Contains(m, k.key) {
			return k.tokens
		}
	}
	return 0
}

// contextFields 是各家 /models 接口里表示上下文长度的字段。
var contextFields = []string{"context_length", "context_window", "max_context_length", "max_model_len", "max_input_tokens", "input_token_limit"}

// apiContext 从 /models 接口返回的一项中读取上下文长度（也看一层嵌套，例如 OpenRouter 的 top_provider）。
func apiContext(item map[string]any) int {
	for _, f := range contextFields {
		if n := positive(item[f]); n > 0 {
			return n
		}
	}
	for _, v := range item {
		if sub, ok := v.(map[string]any); ok {
			for _, f := range contextFields {
				if n := positive(sub[f]); n > 0 {
					return n
				}
			}
		}
	}
	return 0
}

func positive(v any) int {
	switch x := v.(type) {
	case float64:
		if x > 0 && x < 1e9 {
			return int(x)
		}
	case string:
		if n, err := strconv.Atoi(x); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// contextCache 缓存自动识别的结果（含识别失败），避免每次对话都请求服务商接口。
type contextCache struct {
	mu       sync.Mutex
	items    map[string]contextEntry
	inflight map[string]bool
	// fetch 便于测试替换。
	fetch func(ctx context.Context, baseURL, key, model string, publicOnly bool) int
}

type contextEntry struct {
	tokens  int
	expires time.Time
}

const (
	contextCacheTTL  = 24 * time.Hour
	contextFailedTTL = time.Hour
	contextTimeout   = 4 * time.Second
)

func newContextCache() *contextCache {
	return &contextCache{items: map[string]contextEntry{}, inflight: map[string]bool{}, fetch: fetchContext}
}

// warm 在后台识别还没有缓存的模型（列出模型时调用，之后列表里就能显示识别结果）。
func (c *contextCache) warm(baseURL, key, model string, publicOnly bool) {
	ck := baseURL + "\x00" + model
	c.mu.Lock()
	e, ok := c.items[ck]
	if (ok && time.Now().Before(e.expires)) || c.inflight[ck] || baseURL == "" {
		c.mu.Unlock()
		return
	}
	c.inflight[ck] = true
	c.mu.Unlock()
	go func() {
		c.detect(context.Background(), baseURL, key, model, publicOnly)
		c.mu.Lock()
		delete(c.inflight, ck)
		c.mu.Unlock()
	}()
}

// detect 返回模型的上下文长度：服务商接口 → 内置表 → 默认值。
func (c *contextCache) detect(ctx context.Context, baseURL, key, model string, publicOnly bool) ContextInfo {
	ck := baseURL + "\x00" + model
	c.mu.Lock()
	e, ok := c.items[ck]
	c.mu.Unlock()
	if !ok || time.Now().After(e.expires) {
		n := 0
		if baseURL != "" {
			n = c.fetch(ctx, baseURL, key, model, publicOnly)
		}
		ttl := contextCacheTTL
		if n == 0 {
			ttl = contextFailedTTL
		}
		e = contextEntry{tokens: n, expires: time.Now().Add(ttl)}
		c.mu.Lock()
		c.items[ck] = e
		c.mu.Unlock()
	}
	if e.tokens > 0 {
		return ContextInfo{Tokens: e.tokens, Source: ContextAPI}
	}
	if n := tableContext(model); n > 0 {
		return ContextInfo{Tokens: n, Source: ContextTable}
	}
	return ContextInfo{Tokens: DefaultContextTokens, Source: ContextDefault}
}

// fetchContext 请求 OpenAI 兼容的 GET /models，找到这个模型并读出上下文长度；失败返回 0。
func fetchContext(ctx context.Context, baseURL, key, model string, publicOnly bool) int {
	ctx, cancel := context.WithTimeout(ctx, contextTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return 0
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
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&body) != nil {
		return 0
	}
	for _, item := range body.Data {
		if id, _ := item["id"].(string); id == model || strings.TrimPrefix(id, "models/") == model {
			return apiContext(item)
		}
	}
	return 0
}

// peek 和 detect 一样，但不请求接口（列表展示用）：缓存里没有时按内置表或默认值。
func (c *contextCache) peek(baseURL, model string) ContextInfo {
	c.mu.Lock()
	e, ok := c.items[baseURL+"\x00"+model]
	c.mu.Unlock()
	if ok && e.tokens > 0 {
		return ContextInfo{Tokens: e.tokens, Source: ContextAPI}
	}
	if n := tableContext(model); n > 0 {
		return ContextInfo{Tokens: n, Source: ContextTable}
	}
	return ContextInfo{Tokens: DefaultContextTokens, Source: ContextDefault}
}

func peekOr(manual int, c ContextInfo) ContextInfo {
	if manual > 0 {
		return ContextInfo{Tokens: manual, Source: ContextManual}
	}
	return c
}

// contextOf 返回模型的上下文长度：手动填写的优先，否则自动识别。
func (s *Service) contextOf(ctx context.Context, manual int, baseURL, key, model string, publicOnly bool) ContextInfo {
	if manual > 0 {
		return ContextInfo{Tokens: manual, Source: ContextManual}
	}
	return s.contexts.detect(ctx, baseURL, key, model, publicOnly)
}
