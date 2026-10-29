package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/emotion"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/vector"
)

// WriteJob 是一轮对话结束后的记忆写入任务。
type WriteJob struct {
	UserID         uint64
	ConversationID uint64
	MessageID      uint64 // 用户消息 ID，记为记忆来源
	Input          ExtractInput
	// Chat 是本轮对话使用的模型，记忆提取跟随它；为空时使用默认模型。
	Chat llm.ChatProvider
	// Personal 为 true 表示使用用户自己的 API Key，不计入每日限额。
	Personal bool
	// UsageKey 是这次记忆提取的计价项（跟着本轮对话的模型），为空时按辅助模型计。
	UsageKey string
}

// UsageFunc 用于把后台任务消耗的 token 计入用户用量。
type UsageFunc func(ctx context.Context, job WriteJob, u llm.Usage)

// Writer 异步执行记忆写入：LLM 提取 → 按重要性过滤 → 向量化 → 去重 → 入库。
// 写入失败只记录日志，不影响对话。
type Writer struct {
	repo        store.MemoryRepo
	vs          vector.Store
	emb         llm.EmbeddingProvider
	ext         Extractor
	cfg         config.MemoryWriteConfig
	log         *slog.Logger
	onUsage     UsageFunc
	onAppraisal AppraisalFunc
	onWritten   func(userID uint64)

	queue   chan WriteJob
	wg      sync.WaitGroup
	mu      sync.RWMutex // 保护 closed 与 queue 的关闭
	closed  bool
	dropped atomic.Int64
	// userLocks 保证同一用户的写入串行执行，避免并发写入重复记忆。
	userLocks sync.Map
}

// NewWriter 创建写入器，需调用 Start 启动后台 worker。
func NewWriter(repo store.MemoryRepo, vs vector.Store, emb llm.EmbeddingProvider, ext Extractor,
	cfg config.MemoryWriteConfig, log *slog.Logger, onUsage UsageFunc) *Writer {
	if log == nil {
		log = slog.Default()
	}
	return &Writer{
		repo: repo, vs: vs, emb: emb, ext: ext, cfg: cfg,
		log: log.With("component", "memory-writer"), onUsage: onUsage,
		queue: make(chan WriteJob, cfg.QueueSize),
	}
}

// Start 启动后台 worker。ctx 取消后 worker 会处理完队列中剩余任务再退出。
func (w *Writer) Start(ctx context.Context) {
	for i := 0; i < w.cfg.Workers; i++ {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			for job := range w.queue {
				// 每个任务使用独立的超时，不随服务关闭立即中断。
				jctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
				if _, err := w.Process(jctx, job); err != nil {
					w.log.Error("记忆写入失败", "user_id", job.UserID, "message_id", job.MessageID, "err", err)
				}
				cancel()
			}
		}()
	}
}

// Enqueue 提交写入任务。队列已满时丢弃并记录告警，返回 false。
func (w *Writer) Enqueue(job WriteJob) bool {
	if !w.cfg.Enabled {
		return false
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return false
	}
	select {
	case w.queue <- job:
		return true
	default:
		n := w.dropped.Add(1)
		w.log.Warn("记忆写入队列已满，丢弃任务", "user_id", job.UserID, "dropped_total", n)
		return false
	}
}

// Close 停止接收新任务并等待队列处理完毕，ctx 到期则放弃等待。
func (w *Writer) Close(ctx context.Context) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	close(w.queue)
	w.mu.Unlock()
	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("等待记忆写入完成: %w", ctx.Err())
	}
}

func (w *Writer) lockUser(userID uint64) func() {
	v, _ := w.userLocks.LoadOrStore(userID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// AppraisalFunc 接收提取时顺带给出的情绪评价。
type AppraisalFunc func(ctx context.Context, job WriteJob, a emotion.Appraisal)

// OnAppraisal 设置情绪评价的回调（在 Start 之前调用）。
func (w *Writer) OnAppraisal(fn AppraisalFunc) { w.onAppraisal = fn }

// OnWritten 设置写入新记忆后的回调（检查记忆是不是太多了、需要压缩）。
func (w *Writer) OnWritten(fn func(userID uint64)) { w.onWritten = fn }

// Process 同步执行一个写入任务，返回新写入的记忆。
func (w *Writer) Process(ctx context.Context, job WriteJob) ([]store.Memory, error) {
	ctx = quota.WithUser(ctx, job.UserID) // 向量模型的用量记到这个用户身上
	unlock := w.lockUser(job.UserID)
	defer unlock()

	ex, err := w.ext.Extract(ctx, job.Chat, job.Input)
	if w.onUsage != nil && ex.Usage.Total() > 0 {
		w.onUsage(ctx, job, ex.Usage)
	}
	if err != nil {
		return nil, err
	}
	if ex.Appraisal != nil && w.onAppraisal != nil {
		w.onAppraisal(ctx, job, *ex.Appraisal)
	}
	cands := ex.Candidates
	kept := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		if c.Importance >= w.cfg.ImportanceThreshold {
			kept = append(kept, c)
		}
	}
	if len(kept) == 0 {
		return nil, nil
	}
	texts := make([]string, len(kept))
	for i, c := range kept {
		texts[i] = c.Content
	}
	vecs, err := w.emb.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("计算记忆向量: %w", err)
	}
	model := w.emb.Model()
	var written []store.Memory
	var errs []error
	for i, c := range kept {
		vec, err := vector.Normalize(vecs[i])
		if err != nil {
			errs = append(errs, fmt.Errorf("记忆 %q 的向量无效: %w", c.Content, err))
			continue
		}
		dup, err := w.isDuplicate(ctx, job.UserID, model, vec)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if dup {
			w.log.Debug("跳过重复记忆", "user_id", job.UserID, "content", c.Content)
			continue
		}
		m := store.Memory{
			UserID: job.UserID, Type: c.Type, Content: c.Content,
			Embedding: vector.Encode(vec), EmbeddingModel: model, EmbeddingDim: len(vec),
			Importance: c.Importance, Emotion: c.Emotion, Strength: 1, Source: c.Source,
			Metadata: "{}",
		}
		if job.MessageID != 0 {
			id := job.MessageID
			m.SourceMessageID = &id
		}
		if job.ConversationID != 0 {
			id := job.ConversationID
			m.ConversationID = &id
		}
		if err := w.repo.Create(ctx, &m); err != nil {
			errs = append(errs, fmt.Errorf("保存记忆: %w", err))
			continue
		}
		if err := w.vs.Upsert(ctx, job.UserID, model, m.ID, vec); err != nil {
			// 数据库已写入，下次缓存失效后会重新加载，这里只需让缓存失效。
			w.vs.Invalidate(job.UserID)
			w.log.Warn("更新向量缓存失败", "err", err)
		}
		written = append(written, m)
		w.log.Info("写入记忆", "user_id", job.UserID, "memory_id", m.ID, "type", m.Type,
			"importance", m.Importance, "source", m.Source)
	}
	if len(written) > 0 && w.onWritten != nil {
		w.onWritten(job.UserID)
	}
	return written, errors.Join(errs...)
}

func (w *Writer) isDuplicate(ctx context.Context, userID uint64, model string, vec []float32) (bool, error) {
	if w.cfg.DedupSimilarity <= 0 || w.cfg.DedupSimilarity > 1 {
		return false, nil
	}
	hits, err := w.vs.Search(ctx, userID, model, vec, 1)
	if err != nil {
		return false, fmt.Errorf("记忆去重检索: %w", err)
	}
	return len(hits) > 0 && hits[0].Similarity >= w.cfg.DedupSimilarity, nil
}
