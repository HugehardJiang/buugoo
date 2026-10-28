package memory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/vector"
)

// Scored 是一条带得分的检索结果。
type Scored struct {
	Memory     store.Memory `json:"memory"`
	Similarity float64      `json:"similarity"`
	Score      Breakdown    `json:"score"`
}

// Retriever 检索与当前话题相关的记忆。
type Retriever struct {
	repo   store.MemoryRepo
	vs     vector.Store
	emb    llm.EmbeddingProvider
	cfg    config.RetrievalConfig
	boost  float64
	scorer Scorer
	now    func() time.Time
}

// NewRetriever 创建检索器。accessBoost 为每次使用后增加的记忆强度。
func NewRetriever(repo store.MemoryRepo, vs vector.Store, emb llm.EmbeddingProvider, cfg config.RetrievalConfig, accessBoost float64) *Retriever {
	return &Retriever{repo: repo, vs: vs, emb: emb, cfg: cfg, boost: accessBoost, scorer: NewScorer(cfg), now: time.Now}
}

// EmbedQuery 计算查询文本的向量。
func (r *Retriever) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	vecs, err := r.emb.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("计算查询向量: %w", err)
	}
	return vecs[0], nil
}

// Retrieve 检索与 query 相关的记忆，按得分降序返回至多 top_k 条。
func (r *Retriever) Retrieve(ctx context.Context, userID uint64, query string) ([]Scored, error) {
	return r.RetrieveMore(ctx, userID, query, 0)
}

// RetrieveMore 和 Retrieve 一样，但多取 extra 条（记忆影响更强的性格用）。
func (r *Retriever) RetrieveMore(ctx context.Context, userID uint64, query string, extra int) ([]Scored, error) {
	q, err := r.EmbedQuery(quota.WithUser(ctx, userID), query) // 向量模型的用量记到这个用户身上
	if err != nil {
		return nil, err
	}
	return r.retrieve(ctx, userID, q, r.cfg.TopK+max(0, extra))
}

// RetrieveByVector 用已计算好的查询向量检索。
//
// 流程：按相似度取出 candidate_k 条候选 → 过滤相似度低于 min_relevance 的 →
// 读取元数据并按 α·recency + β·importance + γ·relevance 打分 → 取前 top_k 条。
func (r *Retriever) RetrieveByVector(ctx context.Context, userID uint64, q []float32) ([]Scored, error) {
	return r.retrieve(ctx, userID, q, r.cfg.TopK)
}

func (r *Retriever) retrieve(ctx context.Context, userID uint64, q []float32, topK int) ([]Scored, error) {
	hits, err := r.vs.Search(ctx, userID, r.emb.Model(), q, max(r.cfg.CandidateK, topK))
	if err != nil {
		return nil, fmt.Errorf("向量检索: %w", err)
	}
	sims := make(map[uint64]float64, len(hits))
	ids := make([]uint64, 0, len(hits))
	for _, h := range hits {
		if h.Similarity < r.cfg.MinRelevance {
			continue
		}
		sims[h.ID] = h.Similarity
		ids = append(ids, h.ID)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	mems, err := r.repo.GetMany(ctx, userID, ids)
	if err != nil {
		return nil, fmt.Errorf("读取记忆: %w", err)
	}
	return r.rank(mems, sims, topK), nil
}

func (r *Retriever) rank(mems []store.Memory, sims map[uint64]float64, topK int) []Scored {
	now := r.now()
	out := make([]Scored, 0, len(mems))
	for i := range mems {
		m := mems[i]
		// 被新记忆取代的旧记忆不再使用（第二阶段冲突更新时写入 superseded_by）。
		if m.SupersededBy != nil {
			continue
		}
		sim := sims[m.ID]
		out = append(out, Scored{Memory: m, Similarity: sim, Score: r.scorer.Score(&m, sim, now)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score.Total != out[j].Score.Total {
			return out[i].Score.Total > out[j].Score.Total
		}
		return out[i].Memory.ID > out[j].Memory.ID
	})
	if len(out) > topK {
		out = out[:topK]
	}
	return out
}

// MarkUsed 记录本轮回复使用了哪些记忆：更新访问时间与次数、增强记忆强度，并写入使用记录。
func (r *Retriever) MarkUsed(ctx context.Context, userID, messageID uint64, used []Scored) error {
	if len(used) == 0 {
		return nil
	}
	ids := make([]uint64, len(used))
	usages := make([]store.MemoryUsage, len(used))
	for i, s := range used {
		ids[i] = s.Memory.ID
		usages[i] = store.MemoryUsage{MessageID: messageID, MemoryID: s.Memory.ID, UserID: userID, Score: s.Score.Total}
	}
	if err := r.repo.TouchAccessed(ctx, userID, ids, r.boost, r.now().UTC()); err != nil {
		return fmt.Errorf("更新记忆访问记录: %w", err)
	}
	if err := r.repo.RecordUsage(ctx, usages); err != nil {
		return fmt.Errorf("写入记忆使用记录: %w", err)
	}
	return nil
}
