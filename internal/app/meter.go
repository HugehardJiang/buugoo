package app

import (
	"context"
	"log/slog"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/prompt"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/tools"
)

// 计量：向量模型、联网搜索、读网页的调用记到用户身上，超出额度后按价格扣余额。

// meteredEmbedding 给向量模型计量。接口不返回用量，按文字长度估算 token；用户从 ctx 里取（quota.WithUser）。
type meteredEmbedding struct {
	llm.EmbeddingProvider
	lim *quota.Limiter
	log *slog.Logger
}

func (m *meteredEmbedding) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	vecs, err := m.EmbeddingProvider.Embed(ctx, texts)
	if err != nil {
		return vecs, err
	}
	if uid := quota.UserFrom(ctx); uid != 0 {
		n := 0
		for _, t := range texts {
			n += prompt.EstimateTokens(t)
		}
		if err := m.lim.Record(context.WithoutCancel(ctx), uid, quota.Use{Key: quota.KeyEmbedding, PromptTokens: n, Uncounted: true}); err != nil {
			m.log.Error("记录向量模型用量失败", "user_id", uid, "err", err)
		}
	}
	return vecs, nil
}

// providerName 是搜索服务的名字（计价项用），服务器直接读网页时为空（不花钱）。
func providerName(v any) string {
	switch v.(type) {
	case *tools.DeepSeek:
		return "deepseek"
	case *tools.Bocha:
		return "bocha"
	case *tools.Tavily:
		return "tavily"
	}
	return ""
}

// meteredSearcher 在搜索前检查搜索额度和余额，搜索后记次数、按价格扣钱。
type meteredSearcher struct {
	inner  tools.Searcher
	name   string
	userID uint64
	lim    *quota.Limiter
	log    *slog.Logger
}

func (m *meteredSearcher) Search(ctx context.Context, query string, count int) ([]tools.SearchHit, error) {
	if err := m.lim.CheckSearch(ctx, m.userID); err != nil {
		return nil, err
	}
	hits, err := m.inner.Search(ctx, query, count)
	if err != nil {
		return hits, err
	}
	key := ""
	if m.name != "" {
		key = quota.SearchKey(m.name)
	}
	if err := m.lim.Record(context.WithoutCancel(ctx), m.userID, quota.Use{Key: key, Search: true, Calls: 1}); err != nil {
		m.log.Error("记录搜索用量失败", "user_id", m.userID, "err", err)
	}
	return hits, nil
}

// meteredFetcher 读网页后按价格扣钱（读网页算在聊天的额度里，不占搜索次数）。
type meteredFetcher struct {
	inner  tools.Fetcher
	name   string
	userID uint64
	lim    *quota.Limiter
	log    *slog.Logger
}

func (m *meteredFetcher) Fetch(ctx context.Context, url string) (tools.Page, error) {
	page, err := m.inner.Fetch(ctx, url)
	if err != nil {
		return page, err
	}
	if err := m.lim.Record(context.WithoutCancel(ctx), m.userID, quota.Use{Key: quota.FetchKey(m.name), Calls: 1, Uncounted: true}); err != nil {
		m.log.Error("记录读网页用量失败", "user_id", m.userID, "err", err)
	}
	return page, nil
}

// meterSearch 给这一轮对话的搜索和读网页加上计量。lim 为空时原样返回。
func meterSearch(s tools.Searcher, f tools.Fetcher, userID uint64, lim *quota.Limiter, log *slog.Logger) (tools.Searcher, tools.Fetcher) {
	if lim == nil {
		return s, f
	}
	if s != nil {
		s = &meteredSearcher{inner: s, name: providerName(s), userID: userID, lim: lim, log: log}
	}
	if f != nil {
		if name := providerName(f); name != "" {
			f = &meteredFetcher{inner: f, name: name, userID: userID, lim: lim, log: log}
		}
	}
	return s, f
}
