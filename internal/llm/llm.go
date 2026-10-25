// Package llm 定义大模型调用的抽象接口：聊天补全（支持流式）与文本向量化。
package llm

import (
	"context"
	"errors"
	"fmt"
)

// 消息角色。
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message 是一条对话消息。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Images 是随用户消息发送的图片（只有能看图的模型才会收到）。
	Images []Image `json:"images,omitempty"`
	// Reasoning 是助手消息的思考过程。使用工具时 DeepSeek 要求在后续请求中回传。
	Reasoning string `json:"reasoning,omitempty"`
	// ToolCalls 是助手消息中的工具调用。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID 是工具结果消息（Role 为 tool）对应的调用 ID。
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// Image 是一张图片。
type Image struct {
	// Name 是文件名，用于调试展示。
	Name string `json:"name"`
	// URL 是 data: URL（base64），不在调试输出中展示。
	URL string `json:"-"`
}

// Tool 是提供给模型的工具（函数）定义。
type Tool struct {
	Name        string
	Description string
	// Parameters 是 JSON Schema。
	Parameters map[string]any
}

// ToolCall 是模型发起的一次工具调用。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCallDelta 是流式输出中工具调用的增量，按 Index 归并。
type ToolCallDelta struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

// ChatRequest 是聊天补全请求。零值字段使用 provider 的默认配置。
type ChatRequest struct {
	Model       string
	Messages    []Message
	Temperature *float32
	MaxTokens   int
	// JSON 为 true 时要求模型输出 JSON 对象（用于记忆提取等结构化任务）。
	JSON bool
	// Thinking 是思考等级：off（或空）| low | medium | high。如何开启思考由模型配置的
	// thinking_style 决定，不支持思考的模型忽略此项。
	Thinking string
	// Tools 是本次请求可用的工具，为空表示不使用工具。
	Tools []Tool
}

// Usage 是 token 用量。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Total 返回总 token 数。
func (u Usage) Total() int { return u.PromptTokens + u.CompletionTokens }

// ChatResponse 是非流式聊天补全结果。
type ChatResponse struct {
	Content      string
	Reasoning    string
	ToolCalls    []ToolCall
	Model        string
	FinishReason string
	Usage        Usage
}

// Chunk 是流式输出中的一个片段。最后一个片段可能只携带 Usage 或 FinishReason。
type Chunk struct {
	Delta string
	// Reasoning 是思考过程的增量文本（DeepSeek、通义千问等返回的 reasoning_content）。
	Reasoning    string
	FinishReason string
	Usage        *Usage
	Model        string
	ToolCalls    []ToolCallDelta
}

// ChatStream 是流式输出。Recv 在正常结束时返回 io.EOF；调用方必须调用 Close。
type ChatStream interface {
	Recv() (Chunk, error)
	Close() error
}

// ChatProvider 是聊天补全接口。
type ChatProvider interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	ChatStream(ctx context.Context, req ChatRequest) (ChatStream, error)
	// Model 返回默认模型名。
	Model() string
}

// EmbeddingProvider 是文本向量化接口。
type EmbeddingProvider interface {
	// Embed 返回与 texts 一一对应的向量。
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Model 返回模型名，写入记忆的 embedding_model 字段，检索时只比较同一模型的向量。
	Model() string
}

// ErrNotConfigured 表示未配置 API Key 等必要参数。
var ErrNotConfigured = errors.New("模型未配置")

// APIError 是模型服务返回的错误。
type APIError struct {
	StatusCode int
	Type       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Type != "" {
		return fmt.Sprintf("模型服务返回错误（HTTP %d，%s）：%s", e.StatusCode, e.Type, e.Message)
	}
	return fmt.Sprintf("模型服务返回错误（HTTP %d）：%s", e.StatusCode, e.Message)
}

// Retryable 报告该错误是否值得重试（限流或服务端错误）。
func (e *APIError) Retryable() bool {
	return e.StatusCode == 429 || e.StatusCode >= 500
}

// ToolCallBuilder 把流式输出中的工具调用增量归并为完整的调用。
type ToolCallBuilder struct {
	calls   []ToolCall
	byIndex map[int]int
}

// Add 归并一批增量。
func (b *ToolCallBuilder) Add(deltas []ToolCallDelta) {
	if b.byIndex == nil {
		b.byIndex = map[int]int{}
	}
	for _, d := range deltas {
		i, ok := b.byIndex[d.Index]
		if !ok {
			i = len(b.calls)
			b.byIndex[d.Index] = i
			b.calls = append(b.calls, ToolCall{})
		}
		c := &b.calls[i]
		if d.ID != "" {
			c.ID = d.ID
		}
		if d.Name != "" {
			c.Name += d.Name
		}
		c.Arguments += d.Arguments
	}
}

// Calls 返回归并后的工具调用。
func (b *ToolCallBuilder) Calls() []ToolCall { return b.calls }
