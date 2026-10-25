// Package anthropic 预留 Anthropic Messages API 的 llm.ChatProvider 实现。
//
// 第一阶段只实现 OpenAI 兼容协议；此处保留构造函数与接口约束，
// 后续实现时只需补全 Chat / ChatStream，无需改动调用方。
package anthropic

import (
	"context"
	"errors"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
)

// ErrNotImplemented 表示尚未实现。
var ErrNotImplemented = errors.New("anthropic provider 尚未实现，请暂时使用 OpenAI 兼容协议")

// Client 是 Anthropic 客户端（未实现）。
type Client struct{ cfg config.ModelConfig }

var _ llm.ChatProvider = (*Client)(nil)

// New 创建客户端。
func New(cfg config.ModelConfig) (*Client, error) { return &Client{cfg: cfg}, nil }

// Chat 实现 llm.ChatProvider。
func (c *Client) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, ErrNotImplemented
}

// ChatStream 实现 llm.ChatProvider。
func (c *Client) ChatStream(context.Context, llm.ChatRequest) (llm.ChatStream, error) {
	return nil, ErrNotImplemented
}

// Model 实现 llm.ChatProvider。
func (c *Client) Model() string { return c.cfg.Model }
