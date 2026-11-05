package apiserve

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/HugehardJiang/buugoo/internal/llm"
)

// ---------- OpenAI 格式（/v1/chat/completions）----------

// OpenAIRequest 是 OpenAI 格式的请求（只取常用字段，其余忽略）。
type OpenAIRequest struct {
	Model               string          `json:"model"`
	Messages            []openAIMessage `json:"messages"`
	Tools               []openAITool    `json:"tools"`
	Temperature         *float32        `json:"temperature"`
	MaxTokens           int             `json:"max_tokens"`
	MaxCompletionTokens int             `json:"max_completion_tokens"`
	Stream              bool            `json:"stream"`
	StreamOptions       *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
	ReasoningEffort string `json:"reasoning_effort"`
	ResponseFormat  *struct {
		Type string `json:"type"`
	} `json:"response_format"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    json.RawMessage  `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls"`
	ToolCallID string           `json:"tool_call_id"`
}

type openAIToolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

// RequestError 是请求格式不对（返回 400）。
type RequestError struct{ Msg string }

func (e *RequestError) Error() string { return e.Msg }

func badReq(format string, a ...any) error { return &RequestError{fmt.Sprintf(format, a...)} }

// openAIContent 解析 content：字符串，或者 [{type:text},{type:image_url}] 数组。
func openAIContent(raw json.RawMessage) (string, []llm.Image, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil, nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil, nil
	}
	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL *struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", nil, badReq("content 格式不对")
	}
	var text []string
	var imgs []llm.Image
	for _, p := range parts {
		switch p.Type {
		case "text", "input_text":
			text = append(text, p.Text)
		case "image_url":
			if p.ImageURL == nil || !validImageURL(p.ImageURL.URL) {
				return "", nil, badReq("image_url 要是 data: 或 https:// 地址")
			}
			imgs = append(imgs, llm.Image{Name: "image", URL: p.ImageURL.URL})
		default:
			return "", nil, badReq("不支持的内容类型 %q", p.Type)
		}
	}
	return strings.Join(text, "\n"), imgs, nil
}

func validImageURL(u string) bool {
	return strings.HasPrefix(u, "data:image/") || strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")
}

// effortLevel 把 reasoning_effort 换成布谷的思考等级。
func effortLevel(e string) string {
	switch strings.ToLower(e) {
	case "minimal", "low":
		return "low"
	case "medium":
		return "medium"
	case "high", "xhigh", "max":
		return "high"
	}
	return "off"
}

// ToChat 把 OpenAI 格式的请求转成布谷内部的请求。
func (r *OpenAIRequest) ToChat() (llm.ChatRequest, error) {
	if len(r.Messages) == 0 {
		return llm.ChatRequest{}, badReq("messages 不能为空")
	}
	req := llm.ChatRequest{Temperature: r.Temperature, MaxTokens: r.MaxTokens, Thinking: effortLevel(r.ReasoningEffort)}
	if r.MaxCompletionTokens > 0 {
		req.MaxTokens = r.MaxCompletionTokens
	}
	if r.ResponseFormat != nil && r.ResponseFormat.Type == "json_object" {
		req.JSON = true
	}
	for _, m := range r.Messages {
		text, imgs, err := openAIContent(m.Content)
		if err != nil {
			return req, err
		}
		msg := llm.Message{Content: text, Images: imgs}
		switch m.Role {
		case "system", "developer":
			msg.Role = llm.RoleSystem
		case "user":
			msg.Role = llm.RoleUser
		case "assistant":
			msg.Role = llm.RoleAssistant
			for _, tc := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
			}
		case "tool":
			msg.Role, msg.ToolCallID = llm.RoleTool, m.ToolCallID
		default:
			return req, badReq("不支持的 role %q", m.Role)
		}
		req.Messages = append(req.Messages, msg)
	}
	for _, t := range r.Tools {
		if t.Type != "" && t.Type != "function" {
			continue
		}
		if t.Function.Name == "" {
			return req, badReq("工具缺少 name")
		}
		params := t.Function.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		req.Tools = append(req.Tools, llm.Tool{Name: t.Function.Name, Description: t.Function.Description, Parameters: params})
	}
	return req, nil
}

func openAIFinish(reason string, hasTools bool) string {
	switch {
	case hasTools:
		return "tool_calls"
	case reason == "length" || reason == "content_filter" || reason == "tool_calls":
		return reason
	}
	return "stop"
}

func openAIUsage(u llm.Usage) map[string]any {
	out := map[string]any{"prompt_tokens": u.PromptTokens, "completion_tokens": u.CompletionTokens, "total_tokens": u.PromptTokens + u.CompletionTokens}
	if u.CachedTokens > 0 {
		out["prompt_tokens_details"] = map[string]any{"cached_tokens": u.CachedTokens}
	}
	return out
}

// OpenAIResponse 是不流式时的完整响应。
func OpenAIResponse(id, model string, created time.Time, r *llm.ChatResponse, u llm.Usage) map[string]any {
	msg := map[string]any{"role": "assistant", "content": r.Content}
	if r.Reasoning != "" {
		msg["reasoning_content"] = r.Reasoning
	}
	if len(r.ToolCalls) > 0 {
		calls := make([]map[string]any, len(r.ToolCalls))
		for i, tc := range r.ToolCalls {
			calls[i] = map[string]any{"id": tc.ID, "type": "function", "function": map[string]any{"name": tc.Name, "arguments": tc.Arguments}}
		}
		msg["tool_calls"] = calls
	}
	return map[string]any{
		"id": id, "object": "chat.completion", "created": created.Unix(), "model": model,
		"choices": []map[string]any{{"index": 0, "message": msg, "finish_reason": openAIFinish(r.FinishReason, len(r.ToolCalls) > 0)}},
		"usage":   openAIUsage(u),
	}
}

// Streamer 把模型的流式输出写成某种格式的 SSE。
type Streamer interface {
	Start() error
	Chunk(c llm.Chunk) error
	// Finish 写结束事件。
	Finish(u llm.Usage) error
	// Fail 在流的中途报错（已经发了 200，只能在流里告诉客户端）。
	Fail(msg string)
}

// OpenAIStream 写 OpenAI 格式的流（chat.completion.chunk）。
type OpenAIStream struct {
	w            io.Writer
	flush        func()
	id, model    string
	created      int64
	includeUsage bool
	finish       string
	tools        bool
}

// NewOpenAIStream 创建 OpenAI 格式的流。
func NewOpenAIStream(w io.Writer, flush func(), id, model string, created time.Time, includeUsage bool) *OpenAIStream {
	return &OpenAIStream{w: w, flush: flush, id: id, model: model, created: created.Unix(), includeUsage: includeUsage}
}

func (s *OpenAIStream) send(choices []map[string]any, usage map[string]any) error {
	ev := map[string]any{"id": s.id, "object": "chat.completion.chunk", "created": s.created, "model": s.model, "choices": choices}
	if usage != nil {
		ev["usage"] = usage
	}
	b, _ := json.Marshal(ev)
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", b); err != nil {
		return err
	}
	s.flush()
	return nil
}

// Start 实现 Streamer。
func (s *OpenAIStream) Start() error {
	return s.send([]map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": ""}, "finish_reason": nil}}, nil)
}

// Chunk 实现 Streamer。
func (s *OpenAIStream) Chunk(c llm.Chunk) error {
	if c.FinishReason != "" {
		s.finish = c.FinishReason
	}
	delta := map[string]any{}
	if c.Delta != "" {
		delta["content"] = c.Delta
	}
	if c.Reasoning != "" {
		delta["reasoning_content"] = c.Reasoning
	}
	if len(c.ToolCalls) > 0 {
		s.tools = true
		calls := make([]map[string]any, len(c.ToolCalls))
		for i, tc := range c.ToolCalls {
			call := map[string]any{"index": tc.Index, "function": map[string]any{"arguments": tc.Arguments}}
			if tc.ID != "" {
				call["id"], call["type"] = tc.ID, "function"
			}
			if tc.Name != "" {
				call["function"].(map[string]any)["name"] = tc.Name
			}
			calls[i] = call
		}
		delta["tool_calls"] = calls
	}
	if len(delta) == 0 {
		return nil
	}
	return s.send([]map[string]any{{"index": 0, "delta": delta, "finish_reason": nil}}, nil)
}

// Finish 实现 Streamer。
func (s *OpenAIStream) Finish(u llm.Usage) error {
	if err := s.send([]map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": openAIFinish(s.finish, s.tools)}}, nil); err != nil {
		return err
	}
	if s.includeUsage {
		if err := s.send([]map[string]any{}, openAIUsage(u)); err != nil {
			return err
		}
	}
	_, err := io.WriteString(s.w, "data: [DONE]\n\n")
	s.flush()
	return err
}

// Fail 实现 Streamer。
func (s *OpenAIStream) Fail(msg string) {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg, "type": "upstream_error"}})
	fmt.Fprintf(s.w, "data: %s\n\ndata: [DONE]\n\n", b)
	s.flush()
}

// OpenAIError 是 OpenAI 格式的错误响应。
func OpenAIError(kind, msg string) map[string]any {
	return map[string]any{"error": map[string]any{"message": msg, "type": kind, "code": kind}}
}
