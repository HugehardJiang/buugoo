package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/vector"
)

// 记忆压缩：记忆越攒越多，每轮检索、提取时的开销也越大，而且有很多其实是重复或相近的。
// 压缩分两步：
//  1. 合并相近的记忆：向量相似度超过阈值的归成一组，请模型合成一条（信息不丢，冲突以新的为准）；
//  2. 记忆还是太多时（只在自动压缩时做），把最不重要、最久没用过的零碎记忆按类型分批概括成几条。
//
// 被合并的旧记忆删除，新记忆在 Metadata 里记下合并自哪些。

// ErrCompactBusy 表示这个人的记忆正在整理。
var ErrCompactBusy = errors.New("记忆正在整理，稍等一会儿")

// 每次请模型处理的记忆条数上限，每组最多几条。
const (
	compactBatch    = 60
	compactGroupMax = 10
	compactMinCount = 10
)

// CompactReport 是一次压缩的结果。
type CompactReport struct {
	Before  int `json:"before"`
	After   int `json:"after"`
	Merged  int `json:"merged"`  // 被合并掉的旧记忆
	Created int `json:"created"` // 合并出的新记忆
}

// CompactUsageFunc 记录压缩用掉的 token（计入用户的每日用量）。
type CompactUsageFunc func(ctx context.Context, userID uint64, u llm.Usage)

// Compactor 压缩记忆。
type Compactor struct {
	repo    store.MemoryRepo
	vs      vector.Store
	emb     llm.EmbeddingProvider
	llm     llm.ChatProvider
	cfg     config.MemoryCompactConfig
	log     *slog.Logger
	onUsage CompactUsageFunc
	now     func() time.Time

	locks sync.Map // userID → *sync.Mutex
}

// NewCompactor 创建记忆压缩器。chat 是用来合并记忆的模型（工具模型）。
func NewCompactor(repo store.MemoryRepo, vs vector.Store, emb llm.EmbeddingProvider, chat llm.ChatProvider,
	cfg config.MemoryCompactConfig, log *slog.Logger, onUsage CompactUsageFunc) *Compactor {
	if log == nil {
		log = slog.Default()
	}
	return &Compactor{repo: repo, vs: vs, emb: emb, llm: chat, cfg: cfg, log: log.With("component", "memory-compact"),
		onUsage: onUsage, now: time.Now}
}

// Auto 在记忆超过上限时在后台压缩（写入新记忆后调用）。
func (c *Compactor) Auto(userID uint64) {
	if c.cfg.MaxMemories <= 0 {
		return
	}
	ctx := context.Background()
	_, total, err := c.repo.List(ctx, userID, store.MemoryFilter{}, 1, 0)
	if err != nil || int(total) <= c.cfg.MaxMemories {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		defer cancel()
		r, err := c.Compact(ctx, userID, false)
		switch {
		case errors.Is(err, ErrCompactBusy):
		case err != nil:
			c.log.Warn("自动压缩记忆失败", "user_id", userID, "err", err)
		default:
			c.log.Info("自动压缩了记忆", "user_id", userID, "before", r.Before, "after", r.After)
		}
	}()
}

// Compact 压缩一个人的记忆。manual 为 true 时（设置页手动整理）只合并相近的记忆；
// 自动压缩时如果还超过上限，再把零碎的旧记忆概括成几条，压到上限的七成左右。
func (c *Compactor) Compact(ctx context.Context, userID uint64, manual bool) (CompactReport, error) {
	ctx = quota.WithUser(ctx, userID) // 向量模型的用量记到这个用户身上
	mu, _ := c.locks.LoadOrStore(userID, &sync.Mutex{})
	if !mu.(*sync.Mutex).TryLock() {
		return CompactReport{}, ErrCompactBusy
	}
	defer mu.(*sync.Mutex).Unlock()

	mems, _, err := c.repo.List(ctx, userID, store.MemoryFilter{}, 100000, 0)
	if err != nil {
		return CompactReport{}, err
	}
	mems = active(mems)
	rep := CompactReport{Before: len(mems), After: len(mems)}
	if manual && len(mems) < compactMinCount {
		return rep, nil // 没几条，不值得整理
	}
	if !manual && (c.cfg.MaxMemories <= 0 || len(mems) <= c.cfg.MaxMemories) {
		return rep, nil
	}
	vecs, err := c.vectors(ctx, userID)
	if err != nil {
		return rep, err
	}

	// 第一步：合并相近的记忆。
	groups := c.similarGroups(mems, vecs)
	if err := c.mergeGroups(ctx, userID, groups, mergeSimilar, &rep); err != nil {
		return rep, err
	}

	// 第二步：自动压缩时还超过上限，就概括旧的零碎记忆。
	if !manual && c.cfg.MaxMemories > 0 && rep.After > c.cfg.MaxMemories {
		target := c.cfg.MaxMemories * 7 / 10
		left, _, err := c.repo.List(ctx, userID, store.MemoryFilter{}, 100000, 0)
		if err != nil {
			return rep, err
		}
		left = active(left)
		if excess := len(left) - target; excess > 0 {
			if err := c.mergeGroups(ctx, userID, staleGroups(left, excess*2), mergeStale, &rep); err != nil {
				return rep, err
			}
		}
	}
	return rep, nil
}

func active(mems []store.Memory) []store.Memory {
	out := mems[:0]
	for _, m := range mems {
		if m.SupersededBy == nil && strings.TrimSpace(m.Content) != "" {
			out = append(out, m)
		}
	}
	return out
}

func (c *Compactor) vectors(ctx context.Context, userID uint64) (map[uint64][]float32, error) {
	rows, err := c.repo.LoadEmbeddings(ctx, userID, c.emb.Model())
	if err != nil {
		return nil, err
	}
	out := make(map[uint64][]float32, len(rows))
	for _, r := range rows {
		if v, err := vector.Decode(r.Embedding); err == nil {
			out[r.ID] = v
		}
	}
	return out, nil
}

// similarGroups 把相似度超过阈值的记忆归成组（同类型、每组最多 compactGroupMax 条），只返回至少两条的组。
func (c *Compactor) similarGroups(mems []store.Memory, vecs map[uint64][]float32) [][]store.Memory {
	sort.Slice(mems, func(i, j int) bool { return mems[i].ID < mems[j].ID })
	used := make([]bool, len(mems))
	var out [][]store.Memory
	for i := range mems {
		if used[i] || vecs[mems[i].ID] == nil {
			continue
		}
		group := []store.Memory{mems[i]}
		used[i] = true
		for j := i + 1; j < len(mems) && len(group) < compactGroupMax; j++ {
			if used[j] || mems[j].Type != mems[i].Type || vecs[mems[j].ID] == nil {
				continue
			}
			if sim, err := vector.Dot(vecs[mems[i].ID], vecs[mems[j].ID]); err == nil && sim >= c.cfg.Similarity {
				group = append(group, mems[j])
				used[j] = true
			}
		}
		if len(group) > 1 {
			out = append(out, group)
		}
	}
	return out
}

// staleGroups 挑出 n 条最不重要、最久没用过的记忆，按类型每 20 条分一组。
func staleGroups(mems []store.Memory, n int) [][]store.Memory {
	last := func(m *store.Memory) time.Time {
		if m.LastAccessedAt != nil {
			return *m.LastAccessedAt
		}
		return m.CreatedAt
	}
	sort.Slice(mems, func(i, j int) bool {
		if mems[i].Importance != mems[j].Importance {
			return mems[i].Importance < mems[j].Importance
		}
		return last(&mems[i]).Before(last(&mems[j]))
	})
	if n > len(mems) {
		n = len(mems)
	}
	byType := map[string][]store.Memory{}
	for _, m := range mems[:n] {
		byType[m.Type] = append(byType[m.Type], m)
	}
	var out [][]store.Memory
	for _, list := range byType {
		for len(list) > 1 {
			k := min(20, len(list))
			out = append(out, list[:k])
			list = list[k:]
		}
	}
	return out
}

type mergeKind int

const (
	mergeSimilar mergeKind = iota
	mergeStale
)

const compactPrompt = `你在帮一个 AI 朋友整理它关于一个人的记忆，目标是让记忆更少、更精炼，但不丢失有用的信息。
下面每组是一些记忆（编号、类型、内容、记下的日期）。%s
要求：
- 合并后的每条都是一句完整、具体的话，保留人名、数字、日期、地点、喜好等细节
- 内容冲突时以日期更新的为准
- 用户亲口说的事实和 AI 的推测不要混在一起写成事实
- 每条旧记忆只能归到一条新记忆里；没有必要合并的就不要放进结果
只输出 JSON：{"merged":[{"ids":[被合并的编号],"content":"合并后的内容","importance":1-10}]}`

func (c *Compactor) mergeGroups(ctx context.Context, userID uint64, groups [][]store.Memory, kind mergeKind, rep *CompactReport) error {
	instr := "同一组里的记忆说的是相近的事，把重复、相近的合并成一条。"
	if kind == mergeStale {
		instr = "这些是比较零碎、很久没用到的记忆，把能放在一起说的概括成几条（每条可以包含好几件小事），条数越少越好。"
	}
	// 每次请模型处理不超过 compactBatch 条。
	for len(groups) > 0 {
		var batch [][]store.Memory
		n := 0
		for len(groups) > 0 && (n == 0 || n+len(groups[0]) <= compactBatch) {
			batch = append(batch, groups[0])
			n += len(groups[0])
			groups = groups[1:]
		}
		if err := c.mergeBatch(ctx, userID, batch, instr, rep); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compactor) mergeBatch(ctx context.Context, userID uint64, batch [][]store.Memory, instr string, rep *CompactReport) error {
	byID := map[uint64]*store.Memory{}
	var b strings.Builder
	for gi, g := range batch {
		fmt.Fprintf(&b, "\n第 %d 组：\n", gi+1)
		for i := range g {
			m := &g[i]
			byID[m.ID] = m
			kind := "事实"
			if m.Type == store.MemoryEpisodic {
				kind = "经历"
			}
			if m.Source == store.SourceInferred {
				kind += "（推测）"
			}
			fmt.Fprintf(&b, "[%d] %s：%s（%s）\n", m.ID, kind, m.Content, m.CreatedAt.Format("2006-01-02"))
		}
	}
	resp, err := c.llm.Chat(ctx, llm.ChatRequest{JSON: true, Thinking: "off", Messages: []llm.Message{
		{Role: llm.RoleSystem, Content: fmt.Sprintf(compactPrompt, instr)},
		{Role: llm.RoleUser, Content: b.String()},
	}})
	if err != nil {
		return fmt.Errorf("合并记忆: %w", err)
	}
	if c.onUsage != nil && resp.Usage.Total() > 0 {
		c.onUsage(ctx, userID, resp.Usage)
	}
	var out struct {
		Merged []struct {
			IDs        []uint64 `json:"ids"`
			Content    string   `json:"content"`
			Importance int      `json:"importance"`
		} `json:"merged"`
	}
	if err := json.Unmarshal([]byte(extractJSON(resp.Content)), &out); err != nil {
		return fmt.Errorf("合并记忆的结果格式不对: %w", err)
	}

	// 校验：编号必须在这一批里、每条旧记忆只用一次、至少合并两条（概括时也允许一条改写成更短的不算）。
	taken := map[uint64]bool{}
	type plan struct {
		sources    []*store.Memory
		content    string
		importance int
	}
	var plans []plan
	for _, mg := range out.Merged {
		content := strings.TrimSpace(mg.Content)
		var src []*store.Memory
		for _, id := range mg.IDs {
			if m := byID[id]; m != nil && !taken[id] {
				src = append(src, m)
			}
		}
		if len(src) < 2 || content == "" || len([]rune(content)) > 1000 {
			continue
		}
		for _, m := range src {
			taken[m.ID] = true
		}
		plans = append(plans, plan{src, content, mg.Importance})
	}
	if len(plans) == 0 {
		return nil
	}
	texts := make([]string, len(plans))
	for i, p := range plans {
		texts[i] = p.content
	}
	vecs, err := c.emb.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("计算记忆向量: %w", err)
	}
	model := c.emb.Model()
	for i, p := range plans {
		vec, err := vector.Normalize(vecs[i])
		if err != nil {
			continue
		}
		m := mergedMemory(userID, p.sources, p.content, p.importance)
		m.Embedding, m.EmbeddingModel, m.EmbeddingDim = vector.Encode(vec), model, len(vec)
		if err := c.repo.Create(ctx, &m); err != nil {
			return fmt.Errorf("保存合并后的记忆: %w", err)
		}
		if err := c.vs.Upsert(ctx, userID, model, m.ID, vec); err != nil {
			c.vs.Invalidate(userID)
		}
		for _, s := range p.sources {
			if err := c.repo.Delete(ctx, userID, s.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("删除被合并的记忆: %w", err)
			}
			if err := c.vs.Delete(ctx, userID, s.ID); err != nil {
				c.vs.Invalidate(userID)
			}
		}
		rep.Created++
		rep.Merged += len(p.sources)
		rep.After -= len(p.sources) - 1
	}
	return nil
}

// mergedMemory 由几条旧记忆生成合并后的记忆：重要性、情绪、强度取最大，类型取多数，
// 只有全是用户亲口说的才算亲口说的；时间取最新的，检索时的新近度不受影响。
func mergedMemory(userID uint64, src []*store.Memory, content string, importance int) store.Memory {
	m := store.Memory{UserID: userID, Content: content, Source: store.SourceStated, Strength: 1}
	types := map[string]int{}
	ids := make([]uint64, 0, len(src))
	maxImp := 0
	for _, s := range src {
		ids = append(ids, s.ID)
		types[s.Type]++
		maxImp = max(maxImp, s.Importance)
		m.Emotion = max(m.Emotion, s.Emotion)
		m.Strength = max(m.Strength, s.Strength)
		m.AccessCount += s.AccessCount
		if s.Source != store.SourceStated {
			m.Source = store.SourceInferred
		}
		if s.CreatedAt.After(m.CreatedAt) {
			m.CreatedAt = s.CreatedAt
		}
		if s.LastAccessedAt != nil && (m.LastAccessedAt == nil || s.LastAccessedAt.After(*m.LastAccessedAt)) {
			t := *s.LastAccessedAt
			m.LastAccessedAt = &t
		}
	}
	best := 0
	for t, n := range types {
		if n > best || (n == best && t < m.Type) {
			m.Type, best = t, n
		}
	}
	if importance < 1 || importance > 10 {
		importance = maxImp
	}
	m.Importance = max(importance, maxImp)
	meta, _ := json.Marshal(map[string]any{"merged_from": ids})
	m.Metadata = string(meta)
	return m
}

// extractJSON 取出回复里的 JSON 对象（有的模型会包一层代码块）。
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		return s[i : j+1]
	}
	return s
}
