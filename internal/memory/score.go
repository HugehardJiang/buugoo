// Package memory 实现布谷的记忆系统：写入（LLM 提取 + 重要性打分）、检索（加权打分）与管理。
package memory

import (
	"math"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// Breakdown 是一条记忆的检索得分明细：
//
//	score = α·recency + β·importance + γ·relevance
//	recency    = decay^(距上次访问的小时数)，从未被访问过时从创建时间算起
//	importance = 重要性 / 10
//	relevance  = 查询向量与记忆向量的余弦相似度（负值截断为 0）
type Breakdown struct {
	Recency    float64 `json:"recency"`
	Importance float64 `json:"importance"`
	Relevance  float64 `json:"relevance"`
	Total      float64 `json:"total"`
}

// Scorer 按配置计算检索得分。
type Scorer struct {
	Alpha, Beta, Gamma, Decay float64
}

// NewScorer 从配置创建 Scorer。
func NewScorer(cfg config.RetrievalConfig) Scorer {
	return Scorer{Alpha: cfg.Alpha, Beta: cfg.Beta, Gamma: cfg.Gamma, Decay: cfg.Decay}
}

// Recency 计算时间衰减分量。
func (s Scorer) Recency(m *store.Memory, now time.Time) float64 {
	ref := m.CreatedAt
	if m.LastAccessedAt != nil {
		ref = *m.LastAccessedAt
	}
	hours := now.Sub(ref).Hours()
	if hours < 0 {
		hours = 0
	}
	return math.Pow(s.Decay, hours)
}

// Score 计算一条记忆的得分。
func (s Scorer) Score(m *store.Memory, similarity float64, now time.Time) Breakdown {
	b := Breakdown{
		Recency:    s.Recency(m, now),
		Importance: clamp(float64(m.Importance)/10, 0, 1),
		Relevance:  clamp(similarity, 0, 1),
	}
	b.Total = s.Alpha*b.Recency + s.Beta*b.Importance + s.Gamma*b.Relevance
	return b
}

func clamp(x, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, x))
}
