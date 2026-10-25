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
)

const maxRetries = 2

type client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	// sleep 便于测试替换。
	sleep func(context.Context, time.Duration) error
}

func newClient(baseURL, apiKey string, timeout time.Duration) client {
	return client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		// 超时由 context 控制（流式输出可能持续较久），这里只限制建立连接与响应头。
		http: &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: timeout,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConnsPerHost:   10,
		}},
		sleep: sleepCtx,
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
	return &Chat{client: newClient(cfg.BaseURL, cfg.APIKey, cfg.Timeout), cfg: cfg}
}

// Model 实现 llm.ChatProvider。
func (c *Chat) Model() string { return c.cfg.Model }

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []llm.Message  `json:"messages"`
	Temperature    *float32       `json:"temperature,omitempty"`
	MaxTokens      int            `json:"max_tokens,omitempty"`
	Stream         bool           `json:"stream,omitempty"`
	StreamOptions  *streamOptions `json:"stream_options,omitempty"`
	ResponseFormat *respFormat    `json:"response_format,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type respFormat struct {
	Type string `json:"type"`
}

func (c *Chat) build(req llm.ChatRequest, stream bool) chatRequest {
	r := chatRequest{
		Model:       req.Model,
		Messages:    req.Messages,
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
	return r
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      llm.Message `json:"message"`
		Delta        llm.Message `json:"delta"`
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
	r := &llm.ChatResponse{Content: out.Choices[0].Message.Content, Model: out.Model}
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
			c := llm.Chunk{Delta: ch.Delta.Content, Model: ev.Model}
			if ch.FinishReason != nil {
				c.FinishReason = *ch.FinishReason
			}
			if c.Delta != "" || c.FinishReason != "" {
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
	return &Embedding{client: newClient(cfg.BaseURL, cfg.APIKey, cfg.Timeout), cfg: cfg}
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
