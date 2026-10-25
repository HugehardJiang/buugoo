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
)

// Message 是一条对话消息。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest 是聊天补全请求。零值字段使用 provider 的默认配置。
type ChatRequest struct {
	Model       string
	Messages    []Message
	Temperature *float32
	MaxTokens   int
	// JSON 为 true 时要求模型输出 JSON 对象（用于记忆提取等结构化任务）。
	JSON bool
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
	Model        string
	FinishReason string
	Usage        Usage
}

// Chunk 是流式输出中的一个片段。最后一个片段可能只携带 Usage 或 FinishReason。
type Chunk struct {
	Delta        string
	FinishReason string
	Usage        *Usage
	Model        string
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
