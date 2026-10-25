package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/vector"
)

// ReembedStatus 是重新计算向量任务的状态。
type ReembedStatus struct {
	Running    bool       `json:"running"`
	Model      string     `json:"model"`
	Total      int64      `json:"total"`
	Done       int        `json:"done"`
	Failed     int        `json:"failed"`
	Error      string     `json:"error,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// ErrReembedRunning 表示已有任务在运行。
var ErrReembedRunning = errors.New("正在重新计算向量，请稍后再试")

// Reembedder 用当前的向量模型重新计算记忆向量（更换向量模型后使用）。
type Reembedder struct {
	repo store.MemoryRepo
	emb  llm.EmbeddingProvider
	vs   vector.Store
	log  *slog.Logger

	mu     sync.Mutex
	status ReembedStatus
}

// NewReembedder 创建重新计算向量的任务。vs 可为空（命令行中没有缓存）。
func NewReembedder(repo store.MemoryRepo, emb llm.EmbeddingProvider, vs vector.Store, log *slog.Logger) *Reembedder {
	if log == nil {
		log = slog.Default()
	}
	return &Reembedder{repo: repo, emb: emb, vs: vs, log: log.With("component", "reembed")}
}

// Pending 返回需要重新计算的记忆数量（all 为 true 时为全部未删除记忆）以及按模型的分布。
func (r *Reembedder) Pending(ctx context.Context, all bool) (int64, map[string]int64, error) {
	counts, err := r.repo.CountByModel(ctx)
	if err != nil {
		return 0, nil, err
	}
	var n int64
	for m, c := range counts {
		if all || m != r.emb.Model() {
			n += c
		}
	}
	return n, counts, nil
}

// Status 返回最近一次任务的状态。
func (r *Reembedder) Status() ReembedStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// Start 在后台运行任务。
func (r *Reembedder) Start(ctx context.Context, all bool) error {
	r.mu.Lock()
	if r.status.Running {
		r.mu.Unlock()
		return ErrReembedRunning
	}
	r.status = ReembedStatus{Running: true, Model: r.emb.Model()}
	r.mu.Unlock()
	go func() {
		if _, err := r.Run(context.WithoutCancel(ctx), all, 32, nil); err != nil {
			r.log.Error("重新计算向量失败", "err", err)
		}
	}()
	return nil
}

// Run 同步运行任务。progress 在每批完成后调用（可为空）。可以中断后重新运行，已完成的不会重复计算。
func (r *Reembedder) Run(ctx context.Context, all bool, batch int, progress func(ReembedStatus)) (ReembedStatus, error) {
	model := r.emb.Model()
	total, _, err := r.Pending(ctx, all)
	now := time.Now()
	r.update(func(s *ReembedStatus) {
		*s = ReembedStatus{Running: true, Model: model, Total: total, StartedAt: &now}
	})
	if err != nil {
		return r.finish(err), err
	}
	var afterID uint64
	for {
		mems, err := r.repo.ListForReembed(ctx, afterID, model, !all, batch)
		if err != nil {
			return r.finish(err), err
		}
		if len(mems) == 0 {
			break
		}
		afterID = mems[len(mems)-1].ID
		texts := make([]string, len(mems))
		for i, m := range mems {
			texts[i] = m.Content
		}
		vecs, err := r.emb.Embed(ctx, texts)
		if err != nil {
			err = fmt.Errorf("计算向量（可重新运行继续）: %w", err)
			return r.finish(err), err
		}
		done, failed := 0, 0
		for i, m := range mems {
			v, err := vector.Normalize(vecs[i])
			if err != nil {
				failed++
				r.log.Warn("向量无效，跳过", "memory_id", m.ID, "err", err)
				continue
			}
			if err := r.repo.UpdateEmbedding(ctx, m.ID, vector.Encode(v), model, len(v)); err != nil {
				return r.finish(err), err
			}
			done++
		}
		st := r.update(func(s *ReembedStatus) { s.Done += done; s.Failed += failed })
		if progress != nil {
			progress(st)
		}
	}
	if r.vs != nil {
		r.vs.InvalidateAll()
	}
	return r.finish(nil), nil
}

func (r *Reembedder) update(fn func(*ReembedStatus)) ReembedStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(&r.status)
	return r.status
}

func (r *Reembedder) finish(err error) ReembedStatus {
	return r.update(func(s *ReembedStatus) {
		now := time.Now()
		s.Running, s.FinishedAt = false, &now
		if err != nil {
			s.Error = err.Error()
		}
	})
}
