package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/billing"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/opencode"
	"github.com/HugehardJiang/buugoo/internal/quota"
)

// 管理后台 → OpenCode Go：用站长的 Key 一键添加 OpenCode Go 的模型（默认只给授权用户用），
// 并按「官方价 × 套餐月费 ÷ 模型月额度 × 汇率 × 加价倍数」写好计费价格。

const opencodeMetaKey = "opencode.import"

// opencodeOptions 是上次导入用的选项（下次打开页面时带出来）。
type opencodeOptions struct {
	Tier       string  `json:"tier"`
	Rate       float64 `json:"rate"`
	Markup     float64 `json:"markup"`
	Restricted bool    `json:"restricted"`
}

func (s *server) opencodeOptions(ctx context.Context) opencodeOptions {
	o := opencodeOptions{Tier: opencode.TierGo, Rate: 7.2, Markup: 1, Restricted: true}
	if v, ok, err := s.Store.Meta().Get(ctx, opencodeMetaKey); err == nil && ok {
		_ = json.Unmarshal([]byte(v), &o)
	}
	return o
}

// importedModel 是已经添加过的 OpenCode 公共模型。
type importedModel struct {
	ID         uint64 `json:"id"`
	Model      string `json:"model"`
	Name       string `json:"name"`
	Restricted bool   `json:"restricted"`
}

func (s *server) opencodeImported(ctx context.Context) (map[string]models.View, error) {
	list, err := s.Models.List(ctx, models.User{IsAdmin: true})
	if err != nil {
		return nil, err
	}
	out := map[string]models.View{}
	for _, m := range list {
		if m.Scope == models.ScopePublic && opencode.IsOpenCode(m.BaseURL) {
			out[m.Model] = m
		}
	}
	return out, nil
}

func (s *server) consoleOpenCode(c echo.Context) error {
	ctx := c.Request().Context()
	imported, err := s.opencodeImported(ctx)
	if err != nil {
		return err
	}
	var list []importedModel
	for _, m := range imported {
		list = append(list, importedModel{ID: m.ID, Model: m.Model, Name: m.Name, Restricted: m.Restricted})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"catalog": opencode.Catalog, "catalog_date": opencode.CatalogDate, "plans": opencode.PlanPrice,
		"base_url": opencode.BaseURL, "options": s.opencodeOptions(ctx), "imported": list,
		"billing": s.Billing != nil, "multi": s.Config.Mode != "single",
	})
}

func (s *server) consoleOpenCodeImport(c echo.Context) error {
	var req struct {
		APIKey string   `json:"api_key"`
		Models []string `json:"models"`
		opencodeOptions
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	req.APIKey = strings.TrimSpace(req.APIKey)
	switch {
	case len(req.Models) == 0:
		return badRequest("至少选一个模型")
	case req.Tier != opencode.TierGo && req.Tier != opencode.TierPlus:
		return badRequest("套餐档位不对")
	case req.Rate <= 0 || req.Rate > 100 || math.IsNaN(req.Rate):
		return badRequest("汇率要在 0 到 100 之间")
	case req.Markup <= 0 || req.Markup > 100 || math.IsNaN(req.Markup):
		return badRequest("加价倍数要在 0 到 100 之间（1 表示按成本价）")
	}
	imported, err := s.opencodeImported(ctx)
	if err != nil {
		return err
	}
	admin := models.User{IsAdmin: true}
	type done struct {
		ID   uint64
		Spec opencode.Model
	}
	var added, updated []done
	for _, id := range req.Models {
		spec, ok := opencode.Find(id)
		if !ok {
			return badRequest("目录里没有模型 " + id)
		}
		in := models.Input{
			Name: spec.Name + "（OpenCode）", BaseURL: opencode.BaseURL, APIKey: req.APIKey, Model: spec.ID,
			ThinkingStyle: "none", Temperature: 0.8, MaxTokens: 0, Vision: spec.Vision, Tools: true,
			Public: true, Restricted: req.Restricted,
		}
		if old, ok := imported[spec.ID]; ok {
			// 已经有了：保留管理员改过的名字和参数，只更新 Key（填了的话）和授权范围。
			in.Name, in.ThinkingStyle, in.ThinkingModel = old.Name, old.ThinkingStyle, old.ThinkingModel
			in.Temperature, in.MaxTokens, in.Vision, in.Tools, in.ContextTokens = old.Temperature, old.MaxTokens, old.Vision, old.Tools, old.ContextTokens
			if _, err := s.Models.Update(ctx, admin, old.ID, in); err != nil {
				return err
			}
			updated = append(updated, done{old.ID, spec})
			continue
		}
		if req.APIKey == "" {
			return badRequest("第一次添加要填 OpenCode Go 的 API Key")
		}
		v, err := s.Models.Create(ctx, admin, in)
		if err != nil {
			return err
		}
		added = append(added, done{v.ID, spec})
	}

	// 写计费价格（多用户版才有计费）。
	priced := 0
	peakNote := ""
	if s.Billing != nil {
		p := s.Billing.Pricing(ctx)
		if p.Prices == nil {
			p.Prices = map[string]billing.Price{}
		}
		needPeak := false
		for _, d := range append(added, updated...) {
			f := d.Spec.Factor(req.Tier)
			r := d.Spec.Rate.CNY(f, req.Rate, req.Markup)
			price := billing.Price{Input: r.Input, CacheInput: r.Cached, Output: r.Output}
			if d.Spec.OffPeak != nil {
				o := d.Spec.OffPeak.CNY(f, req.Rate, req.Markup)
				price.OffPeak = &billing.Price{Input: o.Input, CacheInput: o.Cached, Output: o.Output}
				needPeak = true
			}
			if price == (billing.Price{}) {
				delete(p.Prices, quota.ModelKey(d.ID)) // 免费模型
			} else {
				p.Prices[quota.ModelKey(d.ID)] = price
			}
			priced++
		}
		if needPeak && p.Peak == nil {
			if rule := opencodePeak(s.Config.Location()); rule != nil {
				p.Peak = rule
				peakNote = "已按 OpenCode 的 DeepSeek 高峰时段（" + strings.Join(rule.Ranges, "、") + "，工作日）打开分时段计价。"
			} else {
				// 换算到本地时间会跨天，没法用一套每天的时段表示：全天按高峰价收。
				for _, d := range append(added, updated...) {
					if pr, ok := p.Prices[quota.ModelKey(d.ID)]; ok && pr.OffPeak != nil {
						pr.OffPeak = nil
						p.Prices[quota.ModelKey(d.ID)] = pr
					}
				}
				peakNote = "系统时区下 DeepSeek 的高峰时段跨天，DeepSeek 全天按高峰价收。"
			}
		}
		if err := s.Billing.SetPricing(ctx, p); err != nil {
			return err
		}
	}
	b, _ := json.Marshal(req.opencodeOptions)
	_ = s.Store.Meta().Set(ctx, opencodeMetaKey, string(b))
	s.audit(c, "管理后台导入了 OpenCode Go 模型", "added", len(added), "updated", len(updated), "tier", req.Tier)
	msg := fmt.Sprintf("新添加 %d 个，更新 %d 个", len(added), len(updated))
	if priced > 0 {
		msg += fmt.Sprintf("，写好了 %d 个的价格", priced)
	}
	return c.JSON(http.StatusOK, map[string]any{"message": msg, "peak_note": peakNote})
}

// opencodePeak 把 OpenCode 的 DeepSeek 高峰时段（UTC 周一至周五 01-04、06-10 点）换算成系统时区的规则；
// 换算后跨天时返回 nil。
func opencodePeak(loc *time.Location) *billing.PeakRule {
	ref := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC) // 一个星期一
	var ranges []string
	for _, r := range opencode.PeakUTC {
		a := ref.Add(time.Duration(r[0]) * time.Hour).In(loc)
		b := ref.Add(time.Duration(r[1]) * time.Hour).In(loc)
		if a.Weekday() != time.Monday || b.Weekday() != time.Monday || a.Minute() != 0 || b.Minute() != 0 {
			return nil
		}
		ranges = append(ranges, fmt.Sprintf("%02d:00-%02d:00", a.Hour(), b.Hour()))
	}
	return &billing.PeakRule{Days: []int{1, 2, 3, 4, 5}, Ranges: ranges}
}
