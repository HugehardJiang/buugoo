package memstore

import (
	"context"
	"errors"
	"math/rand/v2"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/vector"
)

type fakeDB struct {
	mu    sync.Mutex
	rows  map[uint64]map[string][]store.EmbeddingRow
	loads atomic.Int32
	err   error
}

func (f *fakeDB) add(user uint64, model string, id uint64, v []float32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rows == nil {
		f.rows = map[uint64]map[string][]store.EmbeddingRow{}
	}
	if f.rows[user] == nil {
		f.rows[user] = map[string][]store.EmbeddingRow{}
	}
	n, _ := vector.Normalize(v)
	f.rows[user][model] = append(f.rows[user][model], store.EmbeddingRow{ID: id, Embedding: vector.Encode(n)})
}

func (f *fakeDB) load(_ context.Context, user uint64, model string) ([]store.EmbeddingRow, error) {
	f.loads.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.EmbeddingRow(nil), f.rows[user][model]...), nil
}

func TestSearchTopKAndIsolation(t *testing.T) {
	db := &fakeDB{}
	db.add(1, "m", 1, []float32{1, 0, 0})
	db.add(1, "m", 2, []float32{0.9, 0.1, 0})
	db.add(1, "m", 3, []float32{0, 1, 0})
	db.add(1, "m", 4, []float32{-1, 0, 0})
	db.add(1, "other", 5, []float32{1, 0, 0}) // 不同模型
	db.add(2, "m", 6, []float32{1, 0, 0})     // 不同用户
	s := New(db.load, config.VectorCacheConfig{MaxUsers: 10}, nil)

	hits, err := s.Search(context.Background(), 1, "m", []float32{2, 0, 0}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 || hits[0].ID != 1 || hits[1].ID != 2 || hits[2].ID != 3 {
		t.Fatalf("hits=%v", hits)
	}
	if hits[0].Similarity < 0.999 || hits[2].Similarity > 1e-6 {
		t.Fatalf("相似度错误: %v", hits)
	}
	for _, h := range hits {
		if h.ID == 5 || h.ID == 6 {
			t.Fatal("不应检索到其他模型或其他用户的向量")
		}
	}
	// 第二次检索应命中缓存。
	if _, err := s.Search(context.Background(), 1, "m", []float32{1, 0, 0}, 3); err != nil {
		t.Fatal(err)
	}
	if db.loads.Load() != 1 {
		t.Fatalf("loads=%d, 应命中缓存", db.loads.Load())
	}
	if _, err := s.Search(context.Background(), 1, "m", []float32{0, 0, 0}, 3); !errors.Is(err, vector.ErrZeroVector) {
		t.Fatalf("零查询向量应报错: %v", err)
	}
}

// 与暴力排序的结果对比，验证堆实现的正确性。
func TestSearchMatchesBruteForce(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	db := &fakeDB{}
	var all [][]float32
	for i := 0; i < 500; i++ {
		v := make([]float32, 16)
		for j := range v {
			v[j] = float32(r.NormFloat64())
		}
		db.add(1, "m", uint64(i+1), v)
		n, _ := vector.Normalize(v)
		all = append(all, n)
	}
	q := make([]float32, 16)
	for j := range q {
		q[j] = float32(r.NormFloat64())
	}
	nq, _ := vector.Normalize(q)
	type pair struct {
		id  uint64
		sim float64
	}
	var want []pair
	for i, v := range all {
		d, _ := vector.Dot(nq, v)
		want = append(want, pair{uint64(i + 1), d})
	}
	sort.Slice(want, func(i, j int) bool { return want[i].sim > want[j].sim })

	s := New(db.load, config.VectorCacheConfig{}, nil)
	hits, err := s.Search(context.Background(), 1, "m", q, 20)
	if err != nil {
		t.Fatal(err)
	}
	for i, h := range hits {
		if h.ID != want[i].id {
			t.Fatalf("rank %d: got %d want %d", i, h.ID, want[i].id)
		}
	}
}

func TestUpsertDeleteInvalidate(t *testing.T) {
	db := &fakeDB{}
	db.add(1, "m", 1, []float32{1, 0})
	s := New(db.load, config.VectorCacheConfig{}, nil)
	ctx := context.Background()
	if _, err := s.Search(ctx, 1, "m", []float32{1, 0}, 5); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(ctx, 1, "m", 2, []float32{0, 1}); err != nil {
		t.Fatal(err)
	}
	hits, _ := s.Search(ctx, 1, "m", []float32{0, 1}, 5)
	if len(hits) != 2 || hits[0].ID != 2 {
		t.Fatalf("Upsert 后应能检索到新向量: %v", hits)
	}
	if err := s.Delete(ctx, 1, 2); err != nil {
		t.Fatal(err)
	}
	hits, _ = s.Search(ctx, 1, "m", []float32{0, 1}, 5)
	if len(hits) != 1 || hits[0].ID != 1 {
		t.Fatalf("Delete 后不应再检索到: %v", hits)
	}
	// 未缓存用户的 Upsert 不应触发加载。
	if err := s.Upsert(ctx, 9, "m", 3, []float32{1, 0}); err != nil {
		t.Fatal(err)
	}
	s.Invalidate(1)
	if n, _ := s.Stats(); n != 0 {
		t.Fatalf("Invalidate 后缓存应为空: %d", n)
	}
	if _, err := s.Search(ctx, 1, "m", []float32{1, 0}, 5); err != nil {
		t.Fatal(err)
	}
	if db.loads.Load() != 2 {
		t.Fatalf("Invalidate 后应重新加载: %d", db.loads.Load())
	}
}

func TestEvictionAndTTL(t *testing.T) {
	db := &fakeDB{}
	for u := uint64(1); u <= 3; u++ {
		db.add(u, "m", u, []float32{1, 0})
	}
	s := New(db.load, config.VectorCacheConfig{MaxUsers: 2, TTL: time.Minute}, nil)
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	ctx := context.Background()
	for u := uint64(1); u <= 3; u++ {
		if _, err := s.Search(ctx, u, "m", []float32{1, 0}, 1); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := s.Stats(); n != 2 {
		t.Fatalf("应按 MaxUsers 淘汰: %d", n)
	}
	before := db.loads.Load()
	now = now.Add(2 * time.Minute)
	if _, err := s.Search(ctx, 3, "m", []float32{1, 0}, 1); err != nil {
		t.Fatal(err)
	}
	if db.loads.Load() != before+1 {
		t.Fatal("超过 TTL 应重新加载")
	}
}

func TestConcurrentLoadOnce(t *testing.T) {
	db := &fakeDB{}
	db.add(1, "m", 1, []float32{1, 0})
	s := New(db.load, config.VectorCacheConfig{}, nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Search(context.Background(), 1, "m", []float32{1, 0}, 1); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if db.loads.Load() != 1 {
		t.Fatalf("并发检索应只加载一次: %d", db.loads.Load())
	}
}

func TestLoadError(t *testing.T) {
	db := &fakeDB{err: errors.New("db down")}
	s := New(db.load, config.VectorCacheConfig{}, nil)
	if _, err := s.Search(context.Background(), 1, "m", []float32{1}, 1); err == nil {
		t.Fatal("加载失败应返回错误")
	}
	if n, _ := s.Stats(); n != 0 {
		t.Fatal("失败的加载不应被缓存")
	}
}
