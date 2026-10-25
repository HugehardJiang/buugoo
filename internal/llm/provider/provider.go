// Package provider 根据配置构造具体的 llm.ChatProvider / llm.EmbeddingProvider 实现。
package provider

import (
	"fmt"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/llm/anthropic"
	"github.com/HugehardJiang/buugoo/internal/llm/openai"
)

// NewChat 根据 provider 字段创建聊天补全实现。
func NewChat(cfg config.ModelConfig) (llm.ChatProvider, error) {
	switch cfg.Provider {
	case "openai", "":
		return openai.NewChat(cfg), nil
	case "anthropic":
		return anthropic.New(cfg)
	default:
		return nil, fmt.Errorf("不支持的 LLM provider %q", cfg.Provider)
	}
}

// NewEmbedding 根据 provider 字段创建向量化实现。
func NewEmbedding(cfg config.EmbeddingConfig) (llm.EmbeddingProvider, error) {
	switch cfg.Provider {
	case "openai", "":
		return openai.NewEmbedding(cfg), nil
	default:
		return nil, fmt.Errorf("不支持的 embedding provider %q", cfg.Provider)
	}
}
