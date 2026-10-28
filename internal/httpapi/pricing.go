package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/billing"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/quota"
)

// pricingItem 是计费页上的一行：一个计价项。
type pricingItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Kind 是 tokens（按输入、输出 token 计价）或 calls（按次计价）。
	Kind  string `json:"kind"`
	Group string `json:"group"`
	Hint  string `json:"hint,omitempty"`
}

// pricingItems 列出要设价格的项：公共模型、辅助模型、向量模型、各家联网搜索和读网页。
func (s *server) pricingItems(c echo.Context) ([]pricingItem, error) {
	var out []pricingItem
	list, err := s.Models.List(c.Request().Context(), models.User{IsAdmin: true})
	if err != nil {
		return nil, err
	}
	for _, m := range list {
		if m.Scope != models.ScopeBuiltin && m.Scope != models.ScopePublic {
			continue
		}
		label := m.Name
		if m.Name != m.Model {
			label += "（" + m.Model + "）"
		}
		hint := ""
		if m.Scope == models.ScopeBuiltin {
			hint = "配置文件里的默认模型"
		}
		out = append(out, pricingItem{Key: quota.ModelKey(m.ID), Label: label, Kind: "tokens", Group: "聊天模型", Hint: hint})
	}
	cfg := s.Config.LLM
	out = append(out,
		pricingItem{Key: quota.KeyUtility, Label: "辅助模型（" + cfg.Utility.Model + "）", Kind: "tokens", Group: "后台调用",
			Hint: "记忆提取、长对话摘要、整理记忆、起标题"},
		pricingItem{Key: quota.KeyEmbedding, Label: "向量模型（" + cfg.Embedding.Model + "）", Kind: "tokens", Group: "后台调用",
			Hint: "记忆检索和写入；接口不返回用量，按文字长度估算 token（只算输入价格）"},
	)
	for _, p := range []struct{ key, name string }{{"deepseek", "DeepSeek"}, {"bocha", "博查"}, {"tavily", "Tavily"}} {
		out = append(out, pricingItem{Key: quota.SearchKey(p.key), Label: p.name + " 联网搜索", Kind: "calls", Group: "联网"})
		if p.key != "bocha" {
			out = append(out, pricingItem{Key: quota.FetchKey(p.key), Label: p.name + " 读网页", Kind: "calls", Group: "联网"})
		}
	}
	return out, nil
}

// costLabels 是计价项的名字（消费明细里显示），出错时返回空表（网页上显示计价项本身）。
func (s *server) costLabels(c echo.Context) map[string]string {
	out := map[string]string{}
	items, err := s.pricingItems(c)
	if err != nil {
		return out
	}
	for _, it := range items {
		out[it.Key] = it.Label
	}
	return out
}

// consolePricing 处理 GET /api/v1/console/pricing：扣费方式和每一项的价格。
func (s *server) consolePricing(c echo.Context) error {
	items, err := s.pricingItems(c)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"pricing": s.Billing.Pricing(c.Request().Context()), "items": items})
}

// consoleSetPricing 处理 PUT /api/v1/console/pricing：保存后立即生效。
func (s *server) consoleSetPricing(c echo.Context) error {
	var p billing.Pricing
	if err := bind(c, &p); err != nil {
		return err
	}
	if err := s.Billing.SetPricing(c.Request().Context(), p); err != nil {
		return err
	}
	s.audit(c, "管理后台修改了计费设置", "mode", p.Mode, "items", len(p.Prices))
	return c.JSON(http.StatusOK, map[string]any{"pricing": s.Billing.Pricing(c.Request().Context())})
}
