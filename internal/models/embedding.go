package models

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/llm/openai"
	"github.com/HugehardJiang/buugoo/internal/secret"
	"github.com/HugehardJiang/buugoo/internal/vector"
)

// 全站唯一的向量模型。网页中保存的设置存在 app_meta，优先于配置文件。
const metaEmbedding = "models.embedding"

// DynamicEmbedding 是可在运行时替换的 llm.EmbeddingProvider。
type DynamicEmbedding struct {
	mu    sync.RWMutex
	inner llm.EmbeddingProvider
}

// NewDynamicEmbedding 创建可替换的向量模型。
func NewDynamicEmbedding(p llm.EmbeddingProvider) *DynamicEmbedding {
	return &DynamicEmbedding{inner: p}
}

func (d *DynamicEmbedding) get() llm.EmbeddingProvider {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.inner
}

// Embed 实现 llm.EmbeddingProvider。
func (d *DynamicEmbedding) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return d.get().Embed(ctx, texts)
}

// Model 实现 llm.EmbeddingProvider。
func (d *DynamicEmbedding) Model() string { return d.get().Model() }

func (d *DynamicEmbedding) swap(p llm.EmbeddingProvider) {
	d.mu.Lock()
	d.inner = p
	d.mu.Unlock()
}

// EmbeddingInput 是向量模型设置。APIKey 为空表示保持不变。
type EmbeddingInput struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
	Dim     int    `json:"dim"`
}

// EmbeddingView 是返回给前端的向量模型信息。
type EmbeddingView struct {
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	Dim     int    `json:"dim"`
	KeyMask string `json:"key_mask"`
	Ready   bool   `json:"ready"`
	// Source 为 web（网页中设置）或 config（配置文件）。
	Source string `json:"source"`
}

type storedEmbedding struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key_enc"`
	Model   string `json:"model"`
	Dim     int    `json:"dim"`
}

func (s *Service) storedEmbedding(ctx context.Context) (*storedEmbedding, error) {
	raw, ok, err := s.store.Meta().Get(ctx, metaEmbedding)
	if err != nil || !ok {
		return nil, err
	}
	var st storedEmbedding
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return nil, fmt.Errorf("解析向量模型设置: %w", err)
	}
	return &st, nil
}

// effectiveEmbedding 返回当前生效的向量模型配置与来源。
func (s *Service) effectiveEmbedding(ctx context.Context) (config.EmbeddingConfig, string, error) {
	cfg := s.cfg.LLM.Embedding
	st, err := s.storedEmbedding(ctx)
	if err != nil || st == nil {
		return cfg, "config", err
	}
	key, err := s.box.Open(st.APIKey)
	if err != nil {
		// 无法解密时退回配置文件，并提示管理员重新填写。
		s.log.Warn("无法解密网页中保存的向量模型 Key，暂时使用配置文件中的设置", "err", err)
		return cfg, "config", nil
	}
	cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.Dim = st.BaseURL, key, st.Model, st.Dim
	return cfg, "web", nil
}

func (s *Service) loadEmbedding(ctx context.Context) (llm.EmbeddingProvider, error) {
	cfg, _, err := s.effectiveEmbedding(ctx)
	if err != nil {
		return nil, err
	}
	return openai.NewEmbedding(cfg), nil
}

// Embedding 返回全站共用的向量模型（设置变更后自动切换）。
func (s *Service) Embedding() *DynamicEmbedding { return s.emb }

// GetEmbedding 返回当前的向量模型设置。
func (s *Service) GetEmbedding(ctx context.Context) (EmbeddingView, error) {
	cfg, src, err := s.effectiveEmbedding(ctx)
	if err != nil {
		return EmbeddingView{}, err
	}
	return EmbeddingView{BaseURL: cfg.BaseURL, Model: cfg.Model, Dim: cfg.Dim,
		KeyMask: secret.Mask(cfg.APIKey), Ready: cfg.APIKey != "", Source: src}, nil
}

// EmbeddingReady 报告向量模型是否已配置 Key。
func (s *Service) EmbeddingReady(ctx context.Context) bool {
	v, err := s.GetEmbedding(ctx)
	return err == nil && v.Ready
}

// SetEmbedding 保存向量模型设置（仅管理员）。返回模型是否发生变化（变化后需要重新计算向量）。
func (s *Service) SetEmbedding(ctx context.Context, u User, in EmbeddingInput) (EmbeddingView, bool, error) {
	if !u.IsAdmin {
		return EmbeddingView{}, false, ErrForbidden
	}
	in.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	in.Model = strings.TrimSpace(in.Model)
	in.APIKey = strings.TrimSpace(in.APIKey)
	switch {
	case !validURL(in.BaseURL):
		return EmbeddingView{}, false, &InvalidError{"Base URL 需以 http:// 或 https:// 开头"}
	case in.Model == "":
		return EmbeddingView{}, false, &InvalidError{"请填写向量模型名，例如 BAAI/bge-m3"}
	case in.Dim < 0 || in.Dim > 8192:
		return EmbeddingView{}, false, &InvalidError{"维度需在 0-8192 之间（0 表示模型默认）"}
	}
	cur, _, err := s.effectiveEmbedding(ctx)
	if err != nil {
		return EmbeddingView{}, false, err
	}
	key := in.APIKey
	if key == "" {
		key = cur.APIKey
	}
	if key == "" {
		return EmbeddingView{}, false, &InvalidError{"请填写 API Key"}
	}
	sealed, err := s.box.Seal(key)
	if err != nil {
		return EmbeddingView{}, false, err
	}
	b, _ := json.Marshal(storedEmbedding{BaseURL: in.BaseURL, APIKey: sealed, Model: in.Model, Dim: in.Dim})
	if err := s.store.Meta().Set(ctx, metaEmbedding, string(b)); err != nil {
		return EmbeddingView{}, false, err
	}
	changed := cur.Model != in.Model || cur.Dim != in.Dim
	ecfg := s.cfg.LLM.Embedding
	ecfg.BaseURL, ecfg.APIKey, ecfg.Model, ecfg.Dim = in.BaseURL, key, in.Model, in.Dim
	s.emb.swap(openai.NewEmbedding(ecfg))
	v, err := s.GetEmbedding(ctx)
	return v, changed, err
}

// TestEmbedding 测试向量模型设置（in 为空时测试当前设置）。
func (s *Service) TestEmbedding(ctx context.Context, u User, in *EmbeddingInput) (TestResult, error) {
	if !u.IsAdmin {
		return TestResult{}, ErrForbidden
	}
	cfg, _, err := s.effectiveEmbedding(ctx)
	if err != nil {
		return TestResult{}, err
	}
	if in != nil {
		if strings.TrimSpace(in.BaseURL) != "" {
			cfg.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
		}
		if strings.TrimSpace(in.Model) != "" {
			cfg.Model = strings.TrimSpace(in.Model)
		}
		if k := strings.TrimSpace(in.APIKey); k != "" {
			cfg.APIKey = k
		}
		cfg.Dim = in.Dim
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	start := time.Now()
	vecs, err := openai.NewEmbedding(cfg).Embed(ctx, []string{"布谷"})
	res := TestResult{LatencyMS: time.Since(start).Milliseconds()}
	if err != nil {
		res.Error = err.Error()
		return res, nil
	}
	if _, err := vector.Normalize(vecs[0]); err != nil {
		res.Error = "返回的向量无效：" + err.Error()
		return res, nil
	}
	res.OK = true
	res.Reply = fmt.Sprintf("%d 维", len(vecs[0]))
	return res, nil
}
