package apiserve

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/HugehardJiang/buugoo/internal/llm"
)

// ---------- Anthropic 格式（/v1/messages）----------

// AnthropicRequest 是 Anthropic Messages API 格式的请求（只取常用字段）。
type AnthropicRequest struct {
	Model       string             `json:"model"`
	System      json.RawMessage    `json:"system"`
	Messages    []anthropicMessage `json:"messages"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature *float32           `json:"temperature"`
	Stream      bool               `json:"stream"`
	Tools       []struct {
		Type        string         `json:"type"`
		Name        string         `json:"name"`
		Description string         `json:"description"`
		InputSchema map[string]any `json:"input_schema"`
	} `json:"tools"`
	Thinking *struct {
		Type         string `json:"type"`
		BudgetTokens int    `json:"budget_tokens"`
	} `json:"thinking"`
}

type anthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type anthropicBlock struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Source *struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
		URL       string `json:"url"`
	} `json:"source"`
	// tool_use
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	// tool_result
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// blocks 解析 content：字符串，或者内容块数组。
func blocks(raw json.RawMessage) ([]anthropicBlock, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []anthropicBlock{{Type: "text", Text: s}}, nil
	}
	var out []anthropicBlock
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, badReq("content 格式不对")
	}
	return out, nil
}

func blockImage(b anthropicBlock) (llm.Image, error) {
	if b.Source == nil {
		return llm.Image{}, badReq("image 缺少 source")
	}
	switch b.Source.Type {
	case "base64":
		if !strings.HasPrefix(b.Source.MediaType, "image/") || b.Source.Data == "" {
			return llm.Image{}, badReq("image 的 media_type 或 data 不对")
		}
		return llm.Image{Name: "image", URL: "data:" + b.Source.MediaType + ";base64," + b.Source.Data}, nil
	case "url":
		if !validImageURL(b.Source.URL) {
			return llm.Image{}, badReq("image 的 url 要是 https:// 地址")
		}
		return llm.Image{Name: "image", URL: b.Source.URL}, nil
	}
	return llm.Image{}, badReq("不支持的图片来源 %q", b.Source.Type)
}

// resultText 是 tool_result 的内容（字符串或 text 块）。
func resultText(raw json.RawMessage) (string, error) {
	bs, err := blocks(raw)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, b := range bs {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// budgetLevel 把 thinking.budget_tokens 换成思考等级。
func budgetLevel(budget int) string {
	switch {
	case budget <= 0:
		return "medium"
	case budget < 4096:
		return "low"
	case budget < 16384:
		return "medium"
	}
	return "high"
}

// ToChat 把 Anthropic 格式的请求转成布谷内部的请求。
func (r *AnthropicRequest) ToChat() (llm.ChatRequest, error) {
	if len(r.Messages) == 0 {
		return llm.ChatRequest{}, badReq("messages 不能为空")
	}
	if r.MaxTokens <= 0 {
		return llm.ChatRequest{}, badReq("max_tokens 必须大于 0")
	}
	req := llm.ChatRequest{Temperature: r.Temperature, MaxTokens: r.MaxTokens, Thinking: "off"}
	if r.Thinking != nil && r.Thinking.Type == "enabled" {
		req.Thinking = budgetLevel(r.Thinking.BudgetTokens)
	}
	if len(r.System) > 0 {
		sys, err := resultText(r.System)
		if err != nil {
			return req, badReq("system 格式不对")
		}
		if sys != "" {
			req.Messages = append(req.Messages, llm.Message{Role: llm.RoleSystem, Content: sys})
		}
	}
	for _, m := range r.Messages {
		bs, err := blocks(m.Content)
		if err != nil {
			return req, err
		}
		switch m.Role {
		case "user":
			// 工具结果在前（OpenAI 格式里是单独的 tool 消息），然后是文字和图片。
			var text []string
			var imgs []llm.Image
			for _, b := range bs {
				switch b.Type {
				case "tool_result":
					t, err := resultText(b.Content)
					if err != nil {
						return req, err
					}
					if b.IsError {
						t = "（工具报错）" + t
					}
					req.Messages = append(req.Messages, llm.Message{Role: llm.RoleTool, ToolCallID: b.ToolUseID, Content: t})
				case "text":
					text = append(text, b.Text)
				case "image":
					img, err := blockImage(b)
					if err != nil {
						return req, err
					}
					imgs = append(imgs, img)
				case "document":
					return req, badReq("暂不支持 document 内容块，请先转成文字")
				}
			}
			if len(text) > 0 || len(imgs) > 0 {
				req.Messages = append(req.Messages, llm.Message{Role: llm.RoleUser, Content: strings.Join(text, "\n"), Images: imgs})
			}
		case "assistant":
			msg := llm.Message{Role: llm.RoleAssistant}
			var text []string
			for _, b := range bs {
				switch b.Type {
				case "text":
					text = append(text, b.Text)
				case "tool_use":
					args := string(b.Input)
					if args == "" || args == "null" {
						args = "{}"
					}
					msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{ID: b.ID, Name: b.Name, Arguments: args})
				}
				// thinking / redacted_thinking 是之前的思考过程，不回传。
			}
			msg.Content = strings.Join(text, "\n")
			req.Messages = append(req.Messages, msg)
		default:
			return req, badReq("不支持的 role %q", m.Role)
		}
	}
	for _, t := range r.Tools {
		// 只支持自定义工具；web_search 这类服务端工具忽略。
		if t.Type != "" && t.Type != "custom" {
			continue
		}
		if t.Name == "" {
			return req, badReq("工具缺少 name")
		}
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		req.Tools = append(req.Tools, llm.Tool{Name: t.Name, Description: t.Description, Parameters: schema})
	}
	return req, nil
}

func anthropicStop(reason string, hasTools bool) string {
	switch {
	case hasTools:
		return "tool_use"
	case reason == "length":
		return "max_tokens"
	}
	return "end_turn"
}

func anthropicUsage(u llm.Usage) map[string]any {
	cached := min(max(u.CachedTokens, 0), u.PromptTokens)
	return map[string]any{"input_tokens": u.PromptTokens - cached, "output_tokens": u.CompletionTokens,
		"cache_read_input_tokens": cached, "cache_creation_input_tokens": 0}
}

func toolInput(args string) json.RawMessage {
	if json.Valid([]byte(args)) && strings.HasPrefix(strings.TrimSpace(args), "{") {
		return json.RawMessage(args)
	}
	return json.RawMessage("{}")
}

// AnthropicResponse 是不流式时的完整响应。
func AnthropicResponse(id, model string, r *llm.ChatResponse, u llm.Usage) map[string]any {
	content := []map[string]any{}
	if r.Reasoning != "" {
		content = append(content, map[string]any{"type": "thinking", "thinking": r.Reasoning, "signature": ""})
	}
	if r.Content != "" || len(r.ToolCalls) == 0 {
		content = append(content, map[string]any{"type": "text", "text": r.Content})
	}
	for _, tc := range r.ToolCalls {
		content = append(content, map[string]any{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": toolInput(tc.Arguments)})
	}
	return map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": model, "content": content,
		"stop_reason": anthropicStop(r.FinishReason, len(r.ToolCalls) > 0), "stop_sequence": nil, "usage": anthropicUsage(u),
	}
}

// AnthropicStream 写 Anthropic 格式的流（message_start、content_block_*、message_delta、message_stop）。
type AnthropicStream struct {
	w         io.Writer
	flush     func()
	id, model string
	// open 是正在写的内容块：kind 是 thinking / text / tool，tool 是工具调用的序号。
	open   bool
	kind   string
	tool   int
	index  int
	finish string
	tools  bool
}

// NewAnthropicStream 创建 Anthropic 格式的流。
func NewAnthropicStream(w io.Writer, flush func(), id, model string) *AnthropicStream {
	return &AnthropicStream{w: w, flush: flush, id: id, model: model, index: -1}
}

func (s *AnthropicStream) event(name string, data map[string]any) error {
	data["type"] = name
	b, _ := json.Marshal(data)
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, b); err != nil {
		return err
	}
	s.flush()
	return nil
}

// Start 实现 Streamer。
func (s *AnthropicStream) Start() error {
	return s.event("message_start", map[string]any{"message": map[string]any{
		"id": s.id, "type": "message", "role": "assistant", "model": s.model, "content": []any{},
		"stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 0, "output_tokens": 0},
	}})
}

func (s *AnthropicStream) closeBlock() error {
	if !s.open {
		return nil
	}
	s.open = false
	return s.event("content_block_stop", map[string]any{"index": s.index})
}

// begin 开始一个新的内容块（先关掉上一个）。
func (s *AnthropicStream) begin(kind string, block map[string]any) error {
	if err := s.closeBlock(); err != nil {
		return err
	}
	s.index++
	s.open, s.kind = true, kind
	return s.event("content_block_start", map[string]any{"index": s.index, "content_block": block})
}

// Chunk 实现 Streamer。
func (s *AnthropicStream) Chunk(c llm.Chunk) error {
	if c.FinishReason != "" {
		s.finish = c.FinishReason
	}
	if c.Reasoning != "" {
		if !s.open || s.kind != "thinking" {
			if err := s.begin("thinking", map[string]any{"type": "thinking", "thinking": "", "signature": ""}); err != nil {
				return err
			}
		}
		if err := s.event("content_block_delta", map[string]any{"index": s.index, "delta": map[string]any{"type": "thinking_delta", "thinking": c.Reasoning}}); err != nil {
			return err
		}
	}
	if c.Delta != "" {
		if !s.open || s.kind != "text" {
			if err := s.begin("text", map[string]any{"type": "text", "text": ""}); err != nil {
				return err
			}
		}
		if err := s.event("content_block_delta", map[string]any{"index": s.index, "delta": map[string]any{"type": "text_delta", "text": c.Delta}}); err != nil {
			return err
		}
	}
	for _, tc := range c.ToolCalls {
		s.tools = true
		// 换了一个工具调用（或者第一次出现）：开一个 tool_use 块。
		if !s.open || s.kind != "tool" || s.tool != tc.Index {
			id := tc.ID
			if id == "" {
				id = fmt.Sprintf("toolu_%s_%d", s.id, tc.Index)
			}
			if err := s.begin("tool", map[string]any{"type": "tool_use", "id": id, "name": tc.Name, "input": map[string]any{}}); err != nil {
				return err
			}
			s.tool = tc.Index
		}
		if tc.Arguments != "" {
			if err := s.event("content_block_delta", map[string]any{"index": s.index, "delta": map[string]any{"type": "input_json_delta", "partial_json": tc.Arguments}}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Finish 实现 Streamer。
func (s *AnthropicStream) Finish(u llm.Usage) error {
	if s.index < 0 {
		// 什么都没输出：也给一个空的文字块，免得客户端拿不到内容。
		if err := s.begin("text", map[string]any{"type": "text", "text": ""}); err != nil {
			return err
		}
	}
	if err := s.closeBlock(); err != nil {
		return err
	}
	if err := s.event("message_delta", map[string]any{
		"delta": map[string]any{"stop_reason": anthropicStop(s.finish, s.tools), "stop_sequence": nil},
		"usage": anthropicUsage(u),
	}); err != nil {
		return err
	}
	return s.event("message_stop", map[string]any{})
}

// Fail 实现 Streamer。
func (s *AnthropicStream) Fail(msg string) {
	_ = s.event("error", map[string]any{"error": map[string]any{"type": "api_error", "message": msg}})
}

// AnthropicError 是 Anthropic 格式的错误响应。
func AnthropicError(kind, msg string) map[string]any {
	return map[string]any{"type": "error", "error": map[string]any{"type": kind, "message": msg}}
}
