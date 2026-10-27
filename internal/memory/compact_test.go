package memory

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/vector"
	"github.com/HugehardJiang/buugoo/internal/vector/memstore"
)

// mergeChat 把每组里的记忆合并成一条「合并：第一条的内容」。
type mergeChat struct {
	mu    sync.Mutex
	calls int
}

func (f *mergeChat) Model() string { return "fake" }
func (f *mergeChat) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	type merged struct {
		IDs        []uint64 `json:"ids"`
		Content    string   `json:"content"`
		Importance int      `json:"importance"`
	}
	var out []merged
	line := regexp.MustCompile(`^\[(\d+)\] [^：]+：(.*)（`)
	for _, g := range strings.Split(req.Messages[1].Content, "组：")[1:] {
		var m merged
		for _, l := range strings.Split(g, "\n") {
			if s := line.FindStringSubmatch(l); s != nil {
				id, _ := strconv.ParseUint(s[1], 10, 64)
				m.IDs = append(m.IDs, id)
				if m.Content == "" {
					m.Content = "合并：" + s[2]
				}
			}
		}
		out = append(out, m)
	}
	b, _ := json.Marshal(map[string]any{"merged": out})
	return &llm.ChatResponse{Content: "```json\n" + string(b) + "\n```", Usage: llm.Usage{PromptTokens: 10, CompletionTokens: 5}}, nil
}
func (f *mergeChat) ChatStream(context.Context, llm.ChatRequest) (llm.ChatStream, error) {
	return nil, errors.New("not used")
}

func seedMemories(t *testing.T, ctx context.Context, repo store.MemoryRepo, emb *fakeEmb) {
	t.Helper()
	items := []struct {
		text   string
		imp    int
		source string
	}{
		{"用户养了一只猫", 6, store.SourceStated}, {"用户的猫叫团子", 7, store.SourceStated},
		{"用户的猫是橘猫", 5, store.SourceInferred}, {"用户的猫三岁了", 5, store.SourceStated},
		{"用户对花生过敏", 9, store.SourceStated}, {"用户不能吃花生", 8, store.SourceStated},
		{"用户住在沈阳", 6, store.SourceStated}, {"用户下周考试", 6, store.SourceStated},
		{"用户经常加班", 5, store.SourceStated}, {"用户喜欢跑步", 4, store.SourceStated},
		{"用户爱喝咖啡", 4, store.SourceStated}, {"用户爱看电影", 4, store.SourceStated},
	}
	for _, it := range items {
		vecs, _ := emb.Embed(ctx, []string{it.text})
		v, _ := vector.Normalize(vecs[0])
		m := store.Memory{UserID: 1, Type: store.MemorySemantic, Content: it.text, Importance: it.imp, Source: it.source,
			Strength: 1, Metadata: "{}", Embedding: vector.Encode(v), EmbeddingModel: emb.model, EmbeddingDim: len(v)}
		if err := repo.Create(ctx, &m); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCompact(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	emb := &fakeEmb{model: "fake", words: []string{"猫", "花生", "沈阳", "考试", "加班", "跑步", "咖啡", "电影"}}
	vs := memstore.New(db.Memories().LoadEmbeddings, config.VectorCacheConfig{}, nil)
	seedMemories(t, ctx, db.Memories(), emb)
	chat := &mergeChat{}
	used := 0
	c := NewCompactor(db.Memories(), vs, emb, chat, config.MemoryCompactConfig{MaxMemories: 0, Similarity: 0.8}, nil,
		func(_ context.Context, uid uint64, u llm.Usage) { used += u.Total() })

	// 手动整理：只合并相近的（4 条猫 → 1，2 条花生 → 1）。
	r, err := c.Compact(ctx, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Before != 12 || r.After != 8 || r.Merged != 6 || r.Created != 2 {
		t.Fatalf("%+v", r)
	}
	if chat.calls != 1 || used != 15 {
		t.Errorf("calls=%d used=%d", chat.calls, used)
	}
	list, total, _ := db.Memories().List(ctx, 1, store.MemoryFilter{}, 100, 0)
	if total != 8 {
		t.Fatalf("total = %d", total)
	}
	var cat *store.Memory
	for i := range list {
		if strings.HasPrefix(list[i].Content, "合并：用户养了一只猫") {
			cat = &list[i]
		}
	}
	if cat == nil || cat.Importance != 7 || cat.Source != store.SourceInferred || !strings.Contains(cat.Metadata, "merged_from") {
		t.Fatalf("merged = %+v", cat)
	}
	// 合并后的记忆能检索到，被合并的检索不到。
	ret := NewRetriever(db.Memories(), vs, emb, config.RetrievalConfig{CandidateK: 20, TopK: 5, MinRelevance: 0.3, Alpha: 1, Beta: 1, Gamma: 1, Decay: 0.99}, 0)
	got, err := ret.Retrieve(ctx, 1, "猫")
	if err != nil || len(got) != 1 || got[0].Memory.ID != cat.ID {
		t.Fatalf("retrieve = %+v %v", got, err)
	}

	// 再整理一次：没有相近的了，不调用模型。
	if r, _ := c.Compact(ctx, 1, true); r.Merged != 0 || chat.calls != 1 {
		t.Errorf("second = %+v calls=%d", r, chat.calls)
	}

	// 自动压缩：超过上限时连零碎的旧记忆一起概括，压到上限的七成以下。
	c.cfg.MaxMemories = 5
	r, err = c.Compact(ctx, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.After > 3 {
		t.Fatalf("auto = %+v", r)
	}
	if _, total, _ := db.Memories().List(ctx, 1, store.MemoryFilter{}, 100, 0); int(total) != r.After {
		t.Errorf("total = %d, report %+v", total, r)
	}
}

func TestCompactAutoThreshold(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	emb := &fakeEmb{model: "fake", words: []string{"猫", "花生", "沈阳", "考试", "加班", "跑步", "咖啡", "电影"}}
	vs := memstore.New(db.Memories().LoadEmbeddings, config.VectorCacheConfig{}, nil)
	seedMemories(t, ctx, db.Memories(), emb)
	chat := &mergeChat{}
	c := NewCompactor(db.Memories(), vs, emb, chat, config.MemoryCompactConfig{MaxMemories: 50, Similarity: 0.8}, nil, nil)
	c.Auto(1) // 12 条没超过 50 条，什么都不做
	if chat.calls != 0 {
		t.Fatal("没超过上限不应压缩")
	}
}
