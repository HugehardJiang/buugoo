// Package memstore 实现"内存暴力计算 + 活跃用户缓存"的 vector.Store。
//
// 检索时按 (user_id, model) 从数据库加载该用户的全部向量到内存，计算点积并排序。
// 加载结果缓存在 LRU 中，按用户数、总字节数与空闲时间淘汰。
package memstore

import (
	"container/heap"
	"container/list"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/vector"
)

// Loader 加载某用户由指定模型生成的全部向量。
type Loader func(ctx context.Context, userID uint64, model string) ([]store.EmbeddingRow, error)

type key struct {
	userID uint64
	model  string
}

type entry struct {
	key      key
	mu       sync.RWMutex
	ids      []uint64
	vecs     [][]float32
	bytes    int64
	lastUsed time.Time
	elem     *list.Element
}

// Store 是内存向量检索实现。
type Store struct {
	load Loader
	cfg  config.VectorCacheConfig
	log  *slog.Logger
	now  func() time.Time

	mu      sync.Mutex
	entries map[key]*entry
	lru     *list.List // 头部为最近使用
	total   int64
	loading map[key]*loadCall
}

type loadCall struct {
	done chan struct{}
	e    *entry
	err  error
}

var _ vector.Store = (*Store)(nil)

// New 创建内存向量检索。
func New(load Loader, cfg config.VectorCacheConfig, log *slog.Logger) *Store {
	if log == nil {
		log = slog.Default()
	}
	return &Store{
		load: load, cfg: cfg, log: log.With("component", "vector"), now: time.Now,
		entries: map[key]*entry{}, lru: list.New(), loading: map[key]*loadCall{},
	}
}

// get 返回缓存条目，未命中时加载（同一 key 的并发加载只执行一次）。
func (s *Store) get(ctx context.Context, k key) (*entry, error) {
	s.mu.Lock()
	if e, ok := s.entries[k]; ok {
		if s.cfg.TTL > 0 && s.now().Sub(e.lastUsed) > s.cfg.TTL {
			s.removeLocked(e)
		} else {
			e.lastUsed = s.now()
			s.lru.MoveToFront(e.elem)
			s.mu.Unlock()
			return e, nil
		}
	}
	if call, ok := s.loading[k]; ok {
		s.mu.Unlock()
		select {
		case <-call.done:
			return call.e, call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &loadCall{done: make(chan struct{})}
	s.loading[k] = call
	s.mu.Unlock()

	call.e, call.err = s.loadEntry(ctx, k)

	s.mu.Lock()
	delete(s.loading, k)
	if call.err == nil {
		call.e.lastUsed = s.now()
		call.e.elem = s.lru.PushFront(call.e)
		s.entries[k] = call.e
		s.total += call.e.bytes
		s.evictLocked()
	}
	s.mu.Unlock()
	close(call.done)
	return call.e, call.err
}

func (s *Store) loadEntry(ctx context.Context, k key) (*entry, error) {
	rows, err := s.load(ctx, k.userID, k.model)
	if err != nil {
		return nil, fmt.Errorf("加载向量: %w", err)
	}
	e := &entry{key: k, ids: make([]uint64, 0, len(rows)), vecs: make([][]float32, 0, len(rows))}
	bad := 0
	for _, r := range rows {
		v, err := vector.Decode(r.Embedding)
		if err != nil || len(v) == 0 {
			bad++
			continue
		}
		e.ids = append(e.ids, r.ID)
		e.vecs = append(e.vecs, v)
		e.bytes += int64(len(r.Embedding)) + 8
	}
	if bad > 0 {
		s.log.Warn("跳过无法解码的向量", "user_id", k.userID, "model", k.model, "count", bad)
	}
	return e, nil
}

func (s *Store) removeLocked(e *entry) {
	s.lru.Remove(e.elem)
	delete(s.entries, e.key)
	s.total -= e.bytes
}

func (s *Store) evictLocked() {
	for s.lru.Len() > 1 &&
		((s.cfg.MaxUsers > 0 && s.lru.Len() > s.cfg.MaxUsers) || (s.cfg.MaxBytes > 0 && s.total > s.cfg.MaxBytes)) {
		s.removeLocked(s.lru.Back().Value.(*entry))
	}
}

// Search 实现 vector.Store。
func (s *Store) Search(ctx context.Context, userID uint64, model string, query []float32, k int) ([]vector.Hit, error) {
	if k <= 0 {
		return nil, nil
	}
	q, err := vector.Normalize(query)
	if err != nil {
		return nil, fmt.Errorf("查询向量: %w", err)
	}
	e, err := s.get(ctx, key{userID, model})
	if err != nil {
		return nil, err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()

	h := make(minHeap, 0, k)
	mismatched := 0
	for i, v := range e.vecs {
		if len(v) != len(q) {
			mismatched++
			continue
		}
		sim, _ := vector.Dot(q, v)
		if len(h) < k {
			heap.Push(&h, vector.Hit{ID: e.ids[i], Similarity: sim})
		} else if sim > h[0].Similarity {
			h[0] = vector.Hit{ID: e.ids[i], Similarity: sim}
			heap.Fix(&h, 0)
		}
	}
	if mismatched > 0 {
		s.log.Warn("存在维度不一致的向量，请执行 buugoo reembed", "user_id", userID, "model", model, "count", mismatched)
	}
	out := make([]vector.Hit, len(h))
	for i := len(h) - 1; i >= 0; i-- {
		out[i] = heap.Pop(&h).(vector.Hit)
	}
	return out, nil
}

// Upsert 实现 vector.Store：只在该用户已缓存时更新缓存，未缓存时下次检索会从数据库加载。
func (s *Store) Upsert(_ context.Context, userID uint64, model string, id uint64, vec []float32) error {
	s.mu.Lock()
	e, ok := s.entries[key{userID, model}]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, existing := range e.ids {
		if existing == id {
			e.vecs[i] = vec
			return nil
		}
	}
	e.ids = append(e.ids, id)
	e.vecs = append(e.vecs, vec)
	s.adjustBytes(e, int64(4*len(vec)+8))
	return nil
}

// adjustBytes 在 s.mu 保护下调整条目与总字节数（e.bytes 只在 s.mu 下读写）。
func (s *Store) adjustBytes(e *entry, delta int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries[e.key] != e {
		return // 条目已被淘汰
	}
	e.bytes += delta
	s.total += delta
	s.evictLocked()
}

// Delete 实现 vector.Store。
func (s *Store) Delete(_ context.Context, userID, id uint64) error {
	s.mu.Lock()
	var targets []*entry
	for k, e := range s.entries {
		if k.userID == userID {
			targets = append(targets, e)
		}
	}
	s.mu.Unlock()
	for _, e := range targets {
		var freed int64
		e.mu.Lock()
		for i, existing := range e.ids {
			if existing == id {
				freed = int64(4*len(e.vecs[i]) + 8)
				e.ids = append(e.ids[:i], e.ids[i+1:]...)
				e.vecs = append(e.vecs[:i], e.vecs[i+1:]...)
				break
			}
		}
		e.mu.Unlock()
		if freed > 0 {
			s.adjustBytes(e, -freed)
		}
	}
	return nil
}

// Invalidate 实现 vector.Store。
func (s *Store) Invalidate(userID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, e := range s.entries {
		if k.userID == userID {
			s.removeLocked(e)
		}
	}
}

// InvalidateAll 实现 vector.Store。
func (s *Store) InvalidateAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		s.removeLocked(e)
	}
}

// Stats 返回缓存统计（用户条目数、总字节数）。
func (s *Store) Stats() (entries int, bytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lru.Len(), s.total
}

// minHeap 按相似度的小顶堆，用于取 Top-K。
type minHeap []vector.Hit

func (h minHeap) Len() int { return len(h) }
func (h minHeap) Less(i, j int) bool {
	if h[i].Similarity == h[j].Similarity {
		return h[i].ID > h[j].ID // 相同相似度时 ID 大（更新）的排前面
	}
	return h[i].Similarity < h[j].Similarity
}
func (h minHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x any)   { *h = append(*h, x.(vector.Hit)) }
func (h *minHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}
