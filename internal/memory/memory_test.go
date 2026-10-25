package memory

import (
	"context"
	"errors"
	"hash/fnv"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
	"github.com/HugehardJiang/buugoo/internal/vector"
	"github.com/HugehardJiang/buugoo/internal/vector/memstore"
)

// fakeEmb 把文本映射为"关键词袋"向量：每个关键词占一维，便于构造可预期的相似度。
type fakeEmb struct {
	model string
	words []string
	calls int
	mu    sync.Mutex
}

func (f *fakeEmb) Model() string { return f.model }
func (f *fakeEmb) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, len(f.words)+1)
		for j, w := range f.words {
			if strings.Contains(t, w) {
				v[j] = 1
			}
		}
		h := fnv.New32a()
		_, _ = h.Write([]byte(t))
		v[len(f.words)] = 0.01 + float32(h.Sum32()%100)/10000 // 避免零向量
		out[i] = v
	}
	return out, nil
}

func newDB(t *testing.T) *gormstore.DB {
	t.Helper()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "m.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestScorer(t *testing.T) {
	s := Scorer{Alpha: 1, Beta: 1, Gamma: 1, Decay: 0.5}
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	created := now.Add(-2 * time.Hour)
	m := &store.Memory{Importance: 8, CreatedAt: created}

	b := s.Score(m, 0.6, now)
	if math.Abs(b.Recency-0.25) > 1e-9 || b.Importance != 0.8 || b.Relevance != 0.6 {
		t.Fatalf("b=%+v", b)
	}
	if math.Abs(b.Total-(0.25+0.8+0.6)) > 1e-9 {
		t.Fatalf("total=%v", b.Total)
	}
	// 有访问记录时按最后访问时间计算。
	acc := now.Add(-1 * time.Hour)
	m.LastAccessedAt = &acc
	if r := s.Recency(m, now); math.Abs(r-0.5) > 1e-9 {
		t.Fatalf("recency=%v", r)
	}
	// 时钟回拨时 recency 不超过 1；负相似度截断为 0；权重生效。
	future := now.Add(time.Hour)
	m.LastAccessedAt = &future
	w := Scorer{Alpha: 2, Beta: 0, Gamma: 3, Decay: 0.5}.Score(m, -0.4, now)
	if w.Recency != 1 || w.Relevance != 0 || w.Total != 2 {
		t.Fatalf("w=%+v", w)
	}
}

func TestRetrieverRanking(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	emb := &fakeEmb{model: "fake", words: []string{"猫", "沈阳", "考试", "火锅"}}
	vs := memstore.New(db.Memories().LoadEmbeddings, config.VectorCacheConfig{}, nil)
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

	add := func(user uint64, content string, importance int, age time.Duration, model string) uint64 {
		vecs, _ := emb.Embed(ctx, []string{content})
		v, _ := vector.Normalize(vecs[0])
		m := &store.Memory{UserID: user, Type: store.MemorySemantic, Content: content, Embedding: vector.Encode(v),
			EmbeddingModel: model, EmbeddingDim: len(v), Importance: importance, Source: store.SourceStated, Metadata: "{}",
			CreatedAt: now.Add(-age)}
		if err := db.Memories().Create(ctx, m); err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	catOld := add(1, "用户养了一只猫", 5, 1000*time.Hour, "fake")
	catNew := add(1, "用户的猫叫咪咪，用户很喜欢猫", 9, time.Hour, "fake")
	add(1, "用户住在沈阳", 7, time.Hour, "fake")
	add(1, "用户喜欢猫（旧模型）", 9, time.Hour, "old-model")
	add(2, "别人的猫", 10, time.Hour, "fake")

	cfg := config.RetrievalConfig{Alpha: 1, Beta: 1, Gamma: 1.5, Decay: 0.99, CandidateK: 10, TopK: 2, MinRelevance: 0.3}
	r := NewRetriever(db.Memories(), vs, emb, cfg, 0.2)
	r.now = func() time.Time { return now }

	got, err := r.Retrieve(ctx, 1, "我的猫今天生病了")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("应返回 2 条与猫有关的记忆（沈阳相似度低于阈值被过滤）: %+v", got)
	}
	if got[0].Memory.ID != catNew || got[1].Memory.ID != catOld {
		t.Fatalf("排序错误: %d %d", got[0].Memory.ID, got[1].Memory.ID)
	}
	if got[0].Score.Total <= got[1].Score.Total {
		t.Fatal("得分应降序")
	}
	for _, s := range got {
		if s.Memory.UserID != 1 || s.Memory.EmbeddingModel != "fake" {
			t.Fatalf("不应检索到其他用户或其他模型的记忆: %+v", s.Memory)
		}
	}

	if err := r.MarkUsed(ctx, 1, 42, got[:1]); err != nil {
		t.Fatal(err)
	}
	m, _ := db.Memories().Get(ctx, 1, catNew)
	if m.AccessCount != 1 || math.Abs(m.Strength-1.2) > 1e-9 || m.LastAccessedAt == nil {
		t.Fatalf("MarkUsed 未生效: %+v", m)
	}

	none, err := r.Retrieve(ctx, 1, "今天天气不错")
	if err != nil || len(none) != 0 {
		t.Fatalf("无关话题不应返回记忆: %v %v", none, err)
	}
}

type fakeExtractor struct {
	cands []Candidate
	usage llm.Usage
	err   error
}

func (f fakeExtractor) Extract(context.Context, ExtractInput) ([]Candidate, llm.Usage, error) {
	return f.cands, f.usage, f.err
}

func TestWriterProcess(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	emb := &fakeEmb{model: "fake", words: []string{"猫", "沈阳", "花生"}}
	vs := memstore.New(db.Memories().LoadEmbeddings, config.VectorCacheConfig{}, nil)
	var usedTokens int
	ext := fakeExtractor{
		cands: []Candidate{
			{Type: store.MemorySemantic, Content: "用户对花生过敏", Importance: 9, Emotion: 0.3, Source: store.SourceStated},
			{Type: store.MemoryEpisodic, Content: "用户说了声你好", Importance: 1, Source: store.SourceStated},
			{Type: store.MemorySemantic, Content: "用户可能住在沈阳", Importance: 5, Source: store.SourceInferred},
		},
		usage: llm.Usage{PromptTokens: 100, CompletionTokens: 20},
	}
	cfg := config.MemoryWriteConfig{Enabled: true, ImportanceThreshold: 4, DedupSimilarity: 0.95, Workers: 1, QueueSize: 4}
	w := NewWriter(db.Memories(), vs, emb, ext, cfg, nil, func(_ context.Context, _ uint64, u llm.Usage) { usedTokens += u.Total() })

	written, err := w.Process(ctx, WriteJob{UserID: 1, ConversationID: 3, MessageID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 2 {
		t.Fatalf("低于阈值的应被丢弃: %+v", written)
	}
	if usedTokens != 120 {
		t.Fatalf("应记录后台 token 用量: %d", usedTokens)
	}
	if written[0].Source != store.SourceStated || written[1].Source != store.SourceInferred ||
		*written[0].SourceMessageID != 7 || written[0].EmbeddingModel != "fake" {
		t.Fatalf("字段错误: %+v", written[0])
	}
	// 再次写入相同内容应被去重。
	again, err := w.Process(ctx, WriteJob{UserID: 1, MessageID: 8})
	if err != nil || len(again) != 0 {
		t.Fatalf("重复内容应被跳过: %v %v", again, err)
	}
	_, total, _ := db.Memories().List(ctx, 1, store.MemoryFilter{}, 10, 0)
	if total != 2 {
		t.Fatalf("total=%d", total)
	}

	failing := NewWriter(db.Memories(), vs, emb, fakeExtractor{err: errors.New("llm down")}, cfg, nil, nil)
	if _, err := failing.Process(ctx, WriteJob{UserID: 1}); err == nil {
		t.Fatal("提取失败应返回错误")
	}
}

func TestWriterQueue(t *testing.T) {
	db := newDB(t)
	emb := &fakeEmb{model: "fake", words: []string{"猫"}}
	vs := memstore.New(db.Memories().LoadEmbeddings, config.VectorCacheConfig{}, nil)
	ext := fakeExtractor{cands: []Candidate{{Type: store.MemorySemantic, Content: "用户养猫", Importance: 6, Source: store.SourceStated}}}
	cfg := config.MemoryWriteConfig{Enabled: true, ImportanceThreshold: 4, Workers: 2, QueueSize: 8}
	w := NewWriter(db.Memories(), vs, emb, ext, cfg, nil, nil)
	w.Start(context.Background())
	for u := uint64(1); u <= 3; u++ {
		if !w.Enqueue(WriteJob{UserID: u}) {
			t.Fatal("入队失败")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if w.Enqueue(WriteJob{UserID: 1}) {
		t.Fatal("关闭后不应再接收任务")
	}
	for u := uint64(1); u <= 3; u++ {
		if _, n, _ := db.Memories().List(context.Background(), u, store.MemoryFilter{}, 10, 0); n != 1 {
			t.Fatalf("user %d: 关闭前应处理完队列, n=%d", u, n)
		}
	}
}

func TestParseCandidates(t *testing.T) {
	raw := "```json\n" + `{"memories":[
		{"type":"semantic","content":"用户对花生过敏","importance":9,"emotion":0.4,"source":"stated"},
		{"type":"EPISODIC","content":"  用户 10 月 24 日考完了期中考试 ","importance":"6.6","emotion":1.7,"source":"Stated"},
		{"type":"semantic","content":"用户可能比较内向","importance":4,"emotion":0,"source":"guess"},
		{"type":"opinion","content":"无效类型","importance":5,"source":"stated"},
		{"type":"semantic","content":"","importance":5,"source":"stated"},
		{"type":"semantic","content":"重要性越界","importance":42,"emotion":-1,"source":"stated"}
	]}` + "\n```"
	got, err := ParseCandidates(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("应保留 4 条合法记忆: %+v", got)
	}
	if got[1].Type != store.MemoryEpisodic || got[1].Content != "用户 10 月 24 日考完了期中考试" || got[1].Importance != 7 || got[1].Emotion != 1 {
		t.Fatalf("规范化错误: %+v", got[1])
	}
	if got[2].Source != store.SourceInferred {
		t.Fatal("来源不明确时应视为推断")
	}
	if got[3].Importance != 10 || got[3].Emotion != 0 {
		t.Fatalf("越界值应截断: %+v", got[3])
	}
	if _, err := ParseCandidates("我觉得没什么好记的"); err == nil {
		t.Fatal("非 JSON 应报错")
	}
	if got, err := ParseCandidates(`{"memories":[]}`); err != nil || len(got) != 0 {
		t.Fatal(err)
	}
}

// fakeChat 记录收到的请求并返回固定内容。
type fakeChat struct {
	reply string
	got   llm.ChatRequest
}

func (f *fakeChat) Model() string { return "fake" }
func (f *fakeChat) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.got = req
	return &llm.ChatResponse{Content: f.reply, Usage: llm.Usage{PromptTokens: 1}}, nil
}
func (f *fakeChat) ChatStream(context.Context, llm.ChatRequest) (llm.ChatStream, error) {
	return nil, errors.New("not used")
}

func TestLLMExtractorPrompt(t *testing.T) {
	fc := &fakeChat{reply: `{"memories":[{"type":"semantic","content":"用户对花生过敏","importance":9,"emotion":0.2,"source":"stated"}]}`}
	cands, usage, err := NewLLMExtractor(fc).Extract(context.Background(), ExtractInput{
		UserMessage: "我对花生过敏", AssistantReply: "记住啦", Known: []string{"用户住在沈阳"},
		Now: time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC),
	})
	if err != nil || len(cands) != 1 || usage.PromptTokens != 1 {
		t.Fatalf("cands=%v err=%v", cands, err)
	}
	if !fc.got.JSON || len(fc.got.Messages) != 2 {
		t.Fatal("应以 JSON 模式调用")
	}
	user := fc.got.Messages[1].Content
	for _, want := range []string{"2026-10-25 09:00 星期日", "用户住在沈阳", "用户：我对花生过敏", "布谷：记住啦"} {
		if !strings.Contains(user, want) {
			t.Errorf("提示词缺少 %q:\n%s", want, user)
		}
	}
}
