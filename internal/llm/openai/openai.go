// Package openai 实现 OpenAI 兼容协议的聊天补全与向量化，可接 DeepSeek、通义千问、
// 硅基流动、阿里云百炼、OpenAI 等服务。
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/netguard"
)

const maxRetries = 2

type client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	// sleep 便于测试替换。
	sleep func(context.Context, time.Duration) error
}

// sharedTransport 被所有客户端共用：模型可以在网页中随时增删，每次请求都会构造客户端，
// 共用连接池避免连接泄漏。总超时由 context 控制（流式输出可能持续较久）。
var sharedTransport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	ResponseHeaderTimeout: 180 * time.Second,
	IdleConnTimeout:       90 * time.Second,
	MaxIdleConnsPerHost:   10,
}

// ErrPrivateAddress 表示受限客户端试图连接内网地址。
var ErrPrivateAddress = errors.New("出于安全考虑，自行添加的模型不能连接内网或本机地址")

// publicTransport 只允许连接公网地址，用于多用户模式下普通用户自行添加的模型，
// 防止借模型地址访问服务器内网（SSRF）。
var publicTransport = netguard.PublicTransport()

func newClient(baseURL, apiKey string, publicOnly bool) client {
	t := sharedTransport
	if publicOnly {
		t = publicTransport
	}
	return client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Transport: t},
		sleep:   sleepCtx,
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// do 发送 POST 请求。retry 为 true 时对限流与 5xx 错误做指数退避重试。
func (c client) do(ctx context.Context, path string, body any, retry bool) (*http.Response, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("%w：缺少 API Key", llm.ErrNotConfigured)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("编码请求: %w", err)
	}
	attempts := 1
	if retry {
		attempts += maxRetries
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			if err := c.sleep(ctx, time.Duration(1<<(i-1))*time.Second); err != nil {
				return nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("创建请求: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = fmt.Errorf("请求模型服务: %w", err)
			if errors.Is(err, netguard.ErrPrivateAddress) {
				return nil, fmt.Errorf("请求模型服务: %w", ErrPrivateAddress)
			}
			continue
		}
		if resp.StatusCode/100 == 2 {
			return resp, nil
		}
		apiErr := readAPIError(resp)
		lastErr = apiErr
		if !apiErr.Retryable() {
			return nil, apiErr
		}
	}
	return nil, lastErr
}

func readAPIError(resp *http.Response) *llm.APIError {
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	e := &llm.APIError{StatusCode: resp.StatusCode}
	var parsed struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &parsed) == nil && (parsed.Error.Message != "" || parsed.Message != "") {
		e.Message = parsed.Error.Message
		if e.Message == "" {
			e.Message = parsed.Message
		}
		e.Type = parsed.Error.Type
	} else {
		e.Message = strings.TrimSpace(string(b))
		if e.Message == "" {
			e.Message = http.StatusText(resp.StatusCode)
		}
	}
	return e
}

// ---------- 聊天补全 ----------

// Chat 是 OpenAI 兼容的聊天补全客户端。
type Chat struct {
	client
	cfg config.ModelConfig
}

var _ llm.ChatProvider = (*Chat)(nil)

// NewChat 创建聊天补全客户端。
func NewChat(cfg config.ModelConfig) *Chat {
	return &Chat{client: newClient(cfg.BaseURL, cfg.APIKey, false), cfg: cfg}
}

// NewPublicOnlyChat 创建只能连接公网地址的聊天补全客户端（用于不受信任的用户配置）。
func NewPublicOnlyChat(cfg config.ModelConfig) *Chat {
	return &Chat{client: newClient(cfg.BaseURL, cfg.APIKey, true), cfg: cfg}
}

// Model 实现 llm.ChatProvider。
func (c *Chat) Model() string { return c.cfg.Model }

type chatRequest struct {
	Model           string         `json:"model"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	EnableThinking  *bool          `json:"enable_thinking,omitempty"`
	Thinking        *thinkingParam `json:"thinking,omitempty"`
	ThinkingBudget  int            `json:"thinking_budget,omitempty"`
	Messages        []wireMessage  `json:"messages"`
	Tools           []wireTool     `json:"tools,omitempty"`
	Temperature     *float32       `json:"temperature,omitempty"`
	MaxTokens       int            `json:"max_tokens,omitempty"`
	Stream          bool           `json:"stream,omitempty"`
	StreamOptions   *streamOptions `json:"stream_options,omitempty"`
	ResponseFormat  *respFormat    `json:"response_format,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type respFormat struct {
	Type string `json:"type"`
}

// wireMessage 是 OpenAI 格式的消息。Content 为字符串，或带图片时为内容块数组。
type wireMessage struct {
	Role             string         `json:"role"`
	Content          any            `json:"content"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
}

type wirePart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type wireToolCall struct {
	Index    *int         `json:"index,omitempty"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function wireFunction `json:"function"`
}

type wireFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

type wireTool struct {
	Type     string      `json:"type"`
	Function wireToolDef `json:"function"`
}

type wireToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// wireMessages 转换为 OpenAI 格式。思考过程只在 DeepSeek 格式下回传（DeepSeek 使用工具时要求回传，
// 其他服务可能不接受这个字段）。
func (c *Chat) wireMessages(msgs []llm.Message) []wireMessage {
	out := make([]wireMessage, 0, len(msgs))
	for _, m := range msgs {
		w := wireMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		if len(m.Images) > 0 {
			parts := make([]wirePart, 0, len(m.Images)+1)
			if m.Content != "" {
				parts = append(parts, wirePart{Type: "text", Text: m.Content})
			}
			for _, img := range m.Images {
				parts = append(parts, wirePart{Type: "image_url", ImageURL: &imageURL{URL: img.URL}})
			}
			w.Content = parts
		}
		if c.cfg.ThinkingStyle == "deepseek" {
			w.ReasoningContent = m.Reasoning
		}
		for _, tc := range m.ToolCalls {
			w.ToolCalls = append(w.ToolCalls, wireToolCall{ID: tc.ID, Type: "function",
				Function: wireFunction{Name: tc.Name, Arguments: tc.Arguments}})
		}
		out = append(out, w)
	}
	return out
}

func (c *Chat) build(req llm.ChatRequest, stream bool) chatRequest {
	r := chatRequest{
		Model:       req.Model,
		Messages:    c.wireMessages(req.Messages),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      stream,
	}
	if r.Model == "" {
		r.Model = c.cfg.Model
	}
	if r.Temperature == nil {
		t := c.cfg.Temperature
		r.Temperature = &t
	}
	if r.MaxTokens == 0 {
		r.MaxTokens = c.cfg.MaxTokens
	}
	if stream {
		r.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	if req.JSON {
		r.ResponseFormat = &respFormat{Type: "json_object"}
	}
	for _, t := range req.Tools {
		r.Tools = append(r.Tools, wireTool{Type: "function", Function: wireToolDef{Name: t.Name, Description: t.Description, Parameters: llm.CleanSchema(t.Parameters)}})
	}
	c.applyThinking(&r, req.Thinking)
	return r
}

// thinkingParam 是 DeepSeek 的思考开关：{"type": "enabled" | "disabled"}。
type thinkingParam struct {
	Type string `json:"type"`
}

// deepseekEffort 把思考等级映射为 DeepSeek 的 reasoning_effort（只有 low、high、max 三档）。
var deepseekEffort = map[string]string{"low": "low", "medium": "high", "high": "max"}

// thinkingBudget 是 enable_thinking 方式下各等级的思考 token 预算。
var thinkingBudget = map[string]int{"low": 1024, "medium": 4096, "high": 16384}

// applyThinking 按模型配置的思考参数格式开启或关闭思考。
func (c *Chat) applyThinking(r *chatRequest, level string) {
	on := level != "" && level != "off"
	switch c.cfg.ThinkingStyle {
	case "reasoning_effort":
		if on {
			r.ReasoningEffort = level
			// 推理模型通常不接受 temperature，max_tokens 也会被思考占用，交给服务端默认值。
			r.Temperature = nil
			r.MaxTokens = 0
		}
	case "enable_thinking":
		r.EnableThinking = &on
		if on {
			r.ThinkingBudget = thinkingBudget[level]
			if r.MaxTokens > 0 {
				r.MaxTokens += r.ThinkingBudget
			}
		}
	case "deepseek":
		// DeepSeek 默认开启思考，关闭时必须显式传 disabled。思考模式下 temperature 不生效。
		if !on {
			r.Thinking = &thinkingParam{Type: "disabled"}
			return
		}
		r.Thinking = &thinkingParam{Type: "enabled"}
		r.ReasoningEffort = deepseekEffort[level]
		if r.ReasoningEffort == "" {
			r.ReasoningEffort = "high"
		}
		r.Temperature = nil
		r.MaxTokens = 0
	case "model_switch":
		if on && c.cfg.ThinkingModel != "" {
			r.Model = c.cfg.ThinkingModel
			r.Temperature = nil
			r.MaxTokens = 0
		}
	}
}

type respMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// DeepSeek、通义千问、硅基流动使用 reasoning_content；OpenRouter 等使用 reasoning。
	ReasoningContent string         `json:"reasoning_content"`
	Reasoning        string         `json:"reasoning"`
	ToolCalls        []wireToolCall `json:"tool_calls"`
}

func (m respMessage) reasoning() string {
	if m.ReasoningContent != "" {
		return m.ReasoningContent
	}
	return m.Reasoning
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      respMessage `json:"message"`
		Delta        respMessage `json:"delta"`
		FinishReason *string     `json:"finish_reason"`
	} `json:"choices"`
	Usage *llm.Usage `json:"usage"`
}

// Chat 实现 llm.ChatProvider。
func (c *Chat) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	if c.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.Timeout)
		defer cancel()
	}
	resp, err := c.do(ctx, "/chat/completions", c.build(req, false), true)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("解析模型响应: %w", err)
	}
	if len(out.Choices) == 0 {
		return nil, errors.New("模型响应中没有 choices")
	}
	msg := out.Choices[0].Message
	r := &llm.ChatResponse{Content: msg.Content, Reasoning: msg.reasoning(), Model: out.Model}
	for _, tc := range msg.ToolCalls {
		r.ToolCalls = append(r.ToolCalls, llm.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
	}
	if fr := out.Choices[0].FinishReason; fr != nil {
		r.FinishReason = *fr
	}
	if out.Usage != nil {
		r.Usage = *out.Usage
	}
	return r, nil
}

// ChatStream 实现 llm.ChatProvider。流式请求不重试（避免重复输出）。
func (c *Chat) ChatStream(ctx context.Context, req llm.ChatRequest) (llm.ChatStream, error) {
	resp, err := c.do(ctx, "/chat/completions", c.build(req, true), false)
	if err != nil {
		return nil, err
	}
	return newSSEStream(resp.Body), nil
}

// sseStream 解析 text/event-stream 格式的流式响应。
type sseStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	pending []llm.Chunk
	done    bool
}

func newSSEStream(body io.ReadCloser) *sseStream {
	s := bufio.NewScanner(body)
	s.Buffer(make([]byte, 64<<10), 4<<20)
	return &sseStream{body: body, scanner: s}
}

func (s *sseStream) Recv() (llm.Chunk, error) {
	for {
		if len(s.pending) > 0 {
			c := s.pending[0]
			s.pending = s.pending[1:]
			return c, nil
		}
		if s.done {
			return llm.Chunk{}, io.EOF
		}
		if !s.scanner.Scan() {
			if err := s.scanner.Err(); err != nil {
				return llm.Chunk{}, fmt.Errorf("读取流式响应: %w", err)
			}
			// 部分服务不发送 [DONE]，连接关闭即视为结束。
			s.done = true
			continue
		}
		line := strings.TrimSpace(s.scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue // 忽略 event:、id: 等字段
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			s.done = true
			continue
		}
		var ev chatResponse
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return llm.Chunk{}, fmt.Errorf("解析流式片段: %w", err)
		}
		// 有的服务在流中途以错误对象结束。
		if len(ev.Choices) == 0 && ev.Usage == nil {
			var e struct {
				Error *struct {
					Message string `json:"message"`
					Type    string `json:"type"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(data), &e) == nil && e.Error != nil {
				return llm.Chunk{}, &llm.APIError{StatusCode: http.StatusOK, Type: e.Error.Type, Message: e.Error.Message}
			}
		}
		for _, ch := range ev.Choices {
			c := llm.Chunk{Delta: ch.Delta.Content, Reasoning: ch.Delta.reasoning(), Model: ev.Model}
			if ch.FinishReason != nil {
				c.FinishReason = *ch.FinishReason
			}
			for i, tc := range ch.Delta.ToolCalls {
				idx := i
				if tc.Index != nil {
					idx = *tc.Index
				}
				c.ToolCalls = append(c.ToolCalls, llm.ToolCallDelta{Index: idx, ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
			}
			if c.Delta != "" || c.Reasoning != "" || c.FinishReason != "" || len(c.ToolCalls) > 0 {
				s.pending = append(s.pending, c)
			}
		}
		if ev.Usage != nil {
			s.pending = append(s.pending, llm.Chunk{Usage: ev.Usage, Model: ev.Model})
		}
	}
}

func (s *sseStream) Close() error { return s.body.Close() }

// ---------- 向量化 ----------

// Embedding 是 OpenAI 兼容的向量化客户端。
type Embedding struct {
	client
	cfg config.EmbeddingConfig
}

var _ llm.EmbeddingProvider = (*Embedding)(nil)

// NewEmbedding 创建向量化客户端。
func NewEmbedding(cfg config.EmbeddingConfig) *Embedding {
	return &Embedding{client: newClient(cfg.BaseURL, cfg.APIKey, false), cfg: cfg}
}

// Model 实现 llm.EmbeddingProvider。
func (e *Embedding) Model() string { return e.cfg.Model }

type embeddingRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	Dimensions     int      `json:"dimensions,omitempty"`
	EncodingFormat string   `json:"encoding_format"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed 实现 llm.EmbeddingProvider，按 batch_size 分批请求。
func (e *Embedding) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	batch := e.cfg.BatchSize
	if batch <= 0 {
		batch = 32
	}
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += batch {
		end := min(start+batch, len(texts))
		vecs, err := e.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (e *Embedding) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if e.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.cfg.Timeout)
		defer cancel()
	}
	resp, err := e.do(ctx, "/embeddings", embeddingRequest{
		Model: e.cfg.Model, Input: texts, Dimensions: e.cfg.Dim, EncodingFormat: "float",
	}, true)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("解析向量响应: %w", err)
	}
	if len(r.Data) != len(texts) {
		return nil, fmt.Errorf("向量数量不匹配：请求 %d 条，返回 %d 条", len(texts), len(r.Data))
	}
	out := make([][]float32, len(texts))
	for _, d := range r.Data {
		if d.Index < 0 || d.Index >= len(texts) || out[d.Index] != nil {
			return nil, fmt.Errorf("向量响应中的 index %d 无效", d.Index)
		}
		if len(d.Embedding) == 0 {
			return nil, fmt.Errorf("第 %d 条向量为空", d.Index)
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}
