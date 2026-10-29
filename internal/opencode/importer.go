package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/HugehardJiang/buugoo/internal/billing"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// 保存在 app_meta 里的键。
const (
	metaOptions = "opencode.import"
	metaCatalog = "opencode.catalog"
	metaSync    = "opencode.sync"
)

// syncEvery 是自动同步官网目录的间隔（只在添加过 OpenCode 模型时才同步）。
const syncEvery = 12 * time.Hour

// UserError 是填写的内容不对。
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

// Options 是站长导入时的选项（下次打开页面时带出来，自动同步时也用它）。
type Options struct {
	Tier       string  `json:"tier"`
	Rate       float64 `json:"rate"`
	Markup     float64 `json:"markup"`
	Restricted bool    `json:"restricted"`
	// AutoApply 为 true 时，同步到官网价格变了就自动更新已添加模型的计费价格。
	AutoApply bool `json:"auto_apply"`
}

func (o *Options) validate(snap *Snapshot) error {
	o.Tier = NormalizeTier(o.Tier)
	switch {
	case snap.Plans[o.Tier].Price <= 0:
		return &UserError{"套餐档位不对"}
	case o.Rate <= 0 || o.Rate > 100 || math.IsNaN(o.Rate):
		return &UserError{"汇率要在 0 到 100 之间"}
	case o.Markup <= 0 || o.Markup > 100 || math.IsNaN(o.Markup):
		return &UserError{"加价倍数要在 0 到 100 之间（1 表示按成本价）"}
	}
	return nil
}

// SyncReport 是一次同步的结果。
type SyncReport struct {
	At    time.Time `json:"at"`
	Error string    `json:"error,omitempty"`
	// Added、Removed 是官网新增和去掉的模型；Changes 是已添加模型的价格或额度变化（中文描述）。
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Changes []string `json:"changes,omitempty"`
	// Applied 表示已经按新价格更新了计费；Pending 表示有变化还没应用。
	Applied bool `json:"applied,omitempty"`
	Pending bool `json:"pending,omitempty"`
}

// Importer 负责导入、个人添加和同步。
type Importer struct {
	db      *gorm.DB
	meta    store.MetaRepo
	models  *models.Service
	billing *billing.Service // 单用户版为空
	loc     *time.Location
	log     *slog.Logger
	http    *http.Client
	docsURL string
	now     func() time.Time

	mu   sync.Mutex
	snap *Snapshot
}

// NewImporter 创建 Importer。bill 可以为空（没有计费）。
func NewImporter(db *gorm.DB, meta store.MetaRepo, ms *models.Service, bill *billing.Service, loc *time.Location, log *slog.Logger) *Importer {
	if log == nil {
		log = slog.Default()
	}
	return &Importer{db: db, meta: meta, models: ms, billing: bill, loc: loc, log: log.With("component", "opencode"),
		http: &http.Client{Timeout: 30 * time.Second}, docsURL: DocsURL, now: time.Now}
}

// Catalog 返回现在用的目录：同步到的官网目录，没有时用内置的。
func (i *Importer) Catalog(ctx context.Context) *Snapshot {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.snap != nil {
		return i.snap
	}
	snap := &Builtin
	if v, ok, err := i.meta.Get(ctx, metaCatalog); err == nil && ok {
		var s Snapshot
		if json.Unmarshal([]byte(v), &s) == nil && s.validate() == nil {
			snap = &s
		}
	}
	i.snap = snap
	return snap
}

// Options 返回上次导入的选项。
func (i *Importer) Options(ctx context.Context) Options {
	o := Options{Tier: TierGo, Rate: 7.2, Markup: 1, Restricted: true}
	if v, ok, err := i.meta.Get(ctx, metaOptions); err == nil && ok {
		_ = json.Unmarshal([]byte(v), &o)
	}
	o.Tier = NormalizeTier(o.Tier)
	return o
}

// SaveOptions 只保存选项（例如切换「自动应用新价格」）。
func (i *Importer) SaveOptions(ctx context.Context, o Options) error {
	if err := o.validate(i.Catalog(ctx)); err != nil {
		return err
	}
	b, _ := json.Marshal(o)
	return i.meta.Set(ctx, metaOptions, string(b))
}

// LastSync 返回上次同步的结果，没同步过时为空。
func (i *Importer) LastSync(ctx context.Context) *SyncReport {
	v, ok, err := i.meta.Get(ctx, metaSync)
	if err != nil || !ok {
		return nil
	}
	var r SyncReport
	if json.Unmarshal([]byte(v), &r) != nil {
		return nil
	}
	return &r
}

var admin = models.User{IsAdmin: true}

// Imported 返回已经添加的 OpenCode 公共模型（按模型名）。
func (i *Importer) Imported(ctx context.Context) (map[string]models.View, error) {
	return i.list(ctx, admin, models.ScopePublic)
}

func (i *Importer) list(ctx context.Context, u models.User, scope string) (map[string]models.View, error) {
	list, err := i.models.List(ctx, u)
	if err != nil {
		return nil, err
	}
	out := map[string]models.View{}
	for _, m := range list {
		if m.Scope == scope && IsOpenCode(m.BaseURL) {
			out[m.Model] = m
		}
	}
	return out, nil
}

func input(spec Model, key string) models.Input {
	return models.Input{Name: spec.Name + "（OpenCode）", BaseURL: BaseURL, APIKey: key, Model: spec.ID,
		ThinkingStyle: "none", Temperature: 0.8, Vision: spec.Vision, Tools: true}
}

// keep 保留已有模型上改过的名字和参数，只换 Key（填了的话）。
func keep(in models.Input, old models.View) models.Input {
	in.Name, in.ThinkingStyle, in.ThinkingModel = old.Name, old.ThinkingStyle, old.ThinkingModel
	in.Temperature, in.MaxTokens, in.Vision, in.Tools, in.ContextTokens = old.Temperature, old.MaxTokens, old.Vision, old.Tools, old.ContextTokens
	return in
}

// ImportResult 是站长导入的结果。
type ImportResult struct {
	Added, Updated, Priced int
	PeakNote               string
}

// Import 用站长的 Key 添加（或更新）公共模型，并写好计费价格。key 为空时沿用已保存的。
func (i *Importer) Import(ctx context.Context, key string, ids []string, o Options) (*ImportResult, error) {
	snap := i.Catalog(ctx)
	if err := o.validate(snap); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, &UserError{"至少选一个模型"}
	}
	imported, err := i.Imported(ctx)
	if err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)
	res := &ImportResult{}
	targets := map[uint64]Model{}
	for _, id := range ids {
		spec, ok := snap.Find(id)
		if !ok {
			return nil, &UserError{"目录里没有模型 " + id}
		}
		in := input(spec, key)
		in.Public, in.Restricted = true, o.Restricted
		if old, ok := imported[spec.ID]; ok {
			if _, err := i.models.Update(ctx, admin, old.ID, keep(in, old)); err != nil {
				return nil, err
			}
			targets[old.ID] = spec
			res.Updated++
			continue
		}
		if key == "" {
			return nil, &UserError{"第一次添加要填 OpenCode Go 的 API Key"}
		}
		v, err := i.models.Create(ctx, admin, in)
		if err != nil {
			return nil, err
		}
		targets[v.ID] = spec
		res.Added++
	}
	if res.Priced, res.PeakNote, err = i.applyPrices(ctx, snap, o, targets); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(o)
	_ = i.meta.Set(ctx, metaOptions, string(b))
	return res, nil
}

// applyPrices 按目录给这些公共模型写计费价格（没有计费时什么都不做）。
func (i *Importer) applyPrices(ctx context.Context, snap *Snapshot, o Options, targets map[uint64]Model) (int, string, error) {
	if i.billing == nil || len(targets) == 0 {
		return 0, "", nil
	}
	p := i.billing.Pricing(ctx)
	if p.Prices == nil {
		p.Prices = map[string]billing.Price{}
	}
	needPeak := false
	for id, spec := range targets {
		f := snap.Factor(spec, o.Tier)
		r := spec.Rate.CNY(f, o.Rate, o.Markup)
		price := billing.Price{Input: r.Input, CacheInput: r.Cached, Output: r.Output}
		if spec.OffPeak != nil {
			off := spec.OffPeak.CNY(f, o.Rate, o.Markup)
			price.OffPeak = &billing.Price{Input: off.Input, CacheInput: off.Cached, Output: off.Output}
			needPeak = true
		}
		if price == (billing.Price{}) {
			delete(p.Prices, quota.ModelKey(id)) // 免费模型
		} else {
			p.Prices[quota.ModelKey(id)] = price
		}
	}
	note := ""
	if needPeak && p.Peak == nil {
		if rule := PeakRule(i.loc); rule != nil {
			p.Peak = rule
			note = "已按 OpenCode 的 DeepSeek 高峰时段（工作日 " + strings.Join(rule.Ranges, "、") + "）打开分时段计价。"
		} else {
			// 换算到本地时间会跨天，没法用一套每天的时段表示：全天按高峰价收。
			for id := range targets {
				if pr, ok := p.Prices[quota.ModelKey(id)]; ok && pr.OffPeak != nil {
					pr.OffPeak = nil
					p.Prices[quota.ModelKey(id)] = pr
				}
			}
			note = "系统时区下 DeepSeek 的高峰时段跨天，DeepSeek 全天按高峰价收。"
		}
	}
	return len(targets), note, i.billing.SetPricing(ctx, p)
}

// PeakRule 把 OpenCode 的 DeepSeek 高峰时段（UTC 周一至周五 01-04、06-10 点）换算成系统时区的规则；换算后跨天时返回 nil。
func PeakRule(loc *time.Location) *billing.PeakRule {
	ref := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC) // 一个星期一
	var ranges []string
	for _, r := range PeakUTC {
		a := ref.Add(time.Duration(r[0]) * time.Hour).In(loc)
		b := ref.Add(time.Duration(r[1]) * time.Hour).In(loc)
		if a.Weekday() != time.Monday || b.Weekday() != time.Monday || a.Minute() != 0 || b.Minute() != 0 {
			return nil
		}
		ranges = append(ranges, fmt.Sprintf("%02d:00-%02d:00", a.Hour(), b.Hour()))
	}
	return &billing.PeakRule{Days: []int{1, 2, 3, 4, 5}, Ranges: ranges}
}

// AddPersonal 用用户自己的 Key 添加（或更新）个人模型（不计费）。key 为空时只能更新已有的。
func (i *Importer) AddPersonal(ctx context.Context, u models.User, key string, ids []string) (added, updated int, err error) {
	if len(ids) == 0 {
		return 0, 0, &UserError{"至少选一个模型"}
	}
	snap := i.Catalog(ctx)
	mine, err := i.list(ctx, u, models.ScopePersonal)
	if err != nil {
		return 0, 0, err
	}
	key = strings.TrimSpace(key)
	for _, id := range ids {
		spec, ok := snap.Find(id)
		if !ok {
			return added, updated, &UserError{"没有模型 " + id}
		}
		in := input(spec, key)
		if old, ok := mine[spec.ID]; ok {
			if _, err := i.models.Update(ctx, u, old.ID, keep(in, old)); err != nil {
				return added, updated, err
			}
			updated++
			continue
		}
		if key == "" {
			return added, updated, &UserError{"请填写你的 OpenCode Go API Key"}
		}
		if _, err := i.models.Create(ctx, u, in); err != nil {
			return added, updated, err
		}
		added++
	}
	return added, updated, nil
}

// Mine 返回用户自己添加过的 OpenCode 模型名。
func (i *Importer) Mine(ctx context.Context, u models.User) ([]string, error) {
	mine, err := i.list(ctx, u, models.ScopePersonal)
	out := make([]string, 0, len(mine))
	for id := range mine {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, err
}

// fetch 下载官网文档并解析。
func (i *Importer) fetch(ctx context.Context) (*Snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.docsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "buugoo (+https://github.com/HugehardJiang/buugoo)")
	resp, err := i.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("打不开官网文档：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("官网文档返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return Parse(string(body), i.now())
}

// Sync 从官网同步目录，和现在的比较；打开了自动应用时按新价格更新已添加模型的计费。
func (i *Importer) Sync(ctx context.Context) *SyncReport {
	rep := &SyncReport{At: i.now()}
	defer func() {
		b, _ := json.Marshal(rep)
		_ = i.meta.Set(context.WithoutCancel(ctx), metaSync, string(b))
	}()
	next, err := i.fetch(ctx)
	if err != nil {
		rep.Error = err.Error()
		i.log.Warn("同步 OpenCode Go 目录失败，继续用之前的", "err", err)
		return rep
	}
	prev := i.Catalog(ctx)
	o := i.Options(ctx)
	imported, err := i.Imported(ctx)
	if err != nil {
		rep.Error = err.Error()
		return rep
	}
	rep.Added, rep.Removed, rep.Changes = diff(prev, next, o.Tier, imported)
	b, _ := json.Marshal(next)
	if err := i.meta.Set(ctx, metaCatalog, string(b)); err != nil {
		rep.Error = err.Error()
		return rep
	}
	i.mu.Lock()
	i.snap = next
	i.mu.Unlock()
	if len(rep.Changes) > 0 {
		if o.AutoApply {
			if err := i.ApplyPrices(ctx); err != nil {
				rep.Error, rep.Pending = "自动更新价格失败："+err.Error(), true
			} else {
				rep.Applied = true
			}
		} else {
			rep.Pending = true
		}
	}
	i.log.Info("已同步 OpenCode Go 目录", "models", len(next.Models), "added", len(rep.Added), "changes", len(rep.Changes))
	return rep
}

// ApplyPrices 按现在的目录和上次的选项，更新所有已添加公共模型的计费价格。
func (i *Importer) ApplyPrices(ctx context.Context) error {
	snap := i.Catalog(ctx)
	o := i.Options(ctx)
	if err := o.validate(snap); err != nil {
		return err
	}
	imported, err := i.Imported(ctx)
	if err != nil {
		return err
	}
	targets := map[uint64]Model{}
	for id, v := range imported {
		if spec, ok := snap.Find(id); ok {
			targets[v.ID] = spec
		}
	}
	if _, _, err := i.applyPrices(ctx, snap, o, targets); err != nil {
		return err
	}
	if r := i.LastSync(ctx); r != nil && r.Pending {
		r.Pending, r.Applied = false, true
		b, _ := json.Marshal(r)
		_ = i.meta.Set(ctx, metaSync, string(b))
	}
	return nil
}

// diff 比较两份目录：新增、去掉的模型，和已添加模型在所选档位上的价格、额度变化。
func diff(prev, next *Snapshot, tier string, imported map[string]models.View) (added, removed, changes []string) {
	for _, m := range next.Models {
		if _, ok := prev.Find(m.ID); !ok {
			added = append(added, m.Name)
		}
	}
	for _, m := range prev.Models {
		n, ok := next.Find(m.ID)
		if !ok {
			removed = append(removed, m.Name)
			if _, used := imported[m.ID]; used {
				changes = append(changes, fmt.Sprintf("%s：官网已经下架，已添加的模型可能用不了了", m.Name))
			}
			continue
		}
		if _, used := imported[m.ID]; !used {
			continue
		}
		var parts []string
		if m.Rate != n.Rate {
			parts = append(parts, fmt.Sprintf("价格 $%v/%v/%v → $%v/%v/%v", m.Rate.Input, m.Rate.Output, m.Rate.Cached, n.Rate.Input, n.Rate.Output, n.Rate.Cached))
		}
		if (m.OffPeak == nil) != (n.OffPeak == nil) || (m.OffPeak != nil && *m.OffPeak != *n.OffPeak) {
			parts = append(parts, "空闲时段价格变了")
		}
		if a, b := m.Limits[tier], n.Limits[tier]; a != b {
			parts = append(parts, fmt.Sprintf("月额度 $%v → $%v", a, b))
		}
		if pa, pb := prev.Plans[tier].Price, next.Plans[tier].Price; pa != pb && pa > 0 {
			parts = append(parts, fmt.Sprintf("套餐月费 $%v → $%v", pa, pb))
		}
		if len(parts) > 0 {
			changes = append(changes, m.Name+"："+strings.Join(parts, "，"))
		}
	}
	return added, removed, changes
}

// hasModels 报告有没有添加过 OpenCode 模型（公共或个人），没有时不去官网同步。
func (i *Importer) hasModels(ctx context.Context) bool {
	var n int64
	i.db.WithContext(ctx).Model(&store.ModelConfig{}).Where("base_url LIKE ?", "%opencode.ai%").Count(&n)
	return n > 0
}

// Run 在后台定时同步，直到 ctx 取消。只在添加过 OpenCode 模型时才去官网。
func (i *Importer) Run(ctx context.Context) {
	t := time.NewTimer(2 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if i.hasModels(ctx) {
			last := i.LastSync(ctx)
			if last == nil || i.now().Sub(last.At) >= syncEvery {
				cctx, cancel := context.WithTimeout(ctx, time.Minute)
				i.Sync(cctx)
				cancel()
			}
		}
		t.Reset(time.Hour)
	}
}
