// Package opencode 是 OpenCode Go 套餐的模型目录：模型名、官方价格（美元 / 百万 token）和每个模型在
// Go、Go Plus 两档里的月额度（美元）。管理后台一键导入时，按「官方价 × 套餐月费 ÷ 这个模型的月额度 × 汇率」
// 折算成站长的实际成本价（元 / 百万 token），写进计费设置。
//
// 数据来自 https://opencode.ai/docs/go/（CatalogDate 那天），官方会调整，导入前可以在页面上改。
package opencode

import (
	"math"
	"net/url"
	"strings"
)

// CatalogDate 是价格表的日期。
const CatalogDate = "2026-10-28"

// BaseURL 是 OpenCode Go 的 OpenAI 兼容接口（所有文本模型都能走这个接口，平台会自动转换）。
const BaseURL = "https://opencode.ai/inference/go/openai/v1"

// 套餐档位。
const (
	TierGo   = "go"
	TierPlus = "plus"
)

// PlanPrice 是两档的月费（美元）。
var PlanPrice = map[string]float64{TierGo: 10, TierPlus: 40}

// Rate 是一组价格（美元 / 百万 token）。
type Rate struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
	Cached float64 `json:"cached"`
}

// Model 是目录里的一个模型。
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Rate Rate   `json:"rate"`
	// OffPeak 不为空时，Rate 是高峰价，OffPeak 是空闲价（DeepSeek：UTC 周一至周五 01:00-04:00、06:00-10:00 是高峰）。
	OffPeak *Rate `json:"off_peak,omitempty"`
	// Limits 是两档的月额度（美元），0 表示免费、不限。
	Limits map[string]float64 `json:"limits"`
	Free   bool               `json:"free,omitempty"`
	Vision bool               `json:"vision,omitempty"`
	// Note 是要注意的事（分段计价只取了低档、需要改隐私设置等）。
	Note string `json:"note,omitempty"`
}

func lim(goLimit, plusLimit float64) map[string]float64 {
	return map[string]float64{TierGo: goLimit, TierPlus: plusLimit}
}

const tiered = "上下文超过一定长度后官方价格更高，这里按较低一档计"

// Catalog 是 OpenCode Go 的模型（按官方文档的顺序）。
var Catalog = []Model{
	{ID: "glm-5.3-flash", Name: "GLM-5.3-Flash", Rate: Rate{0.15, 0.50, 0.03}, Limits: lim(60, 180)},
	{ID: "glm-5.3", Name: "GLM-5.3", Rate: Rate{1.40, 4.40, 0.26}, Limits: lim(15, 120)},
	{ID: "glm-5.2", Name: "GLM-5.2", Rate: Rate{1.40, 4.40, 0.26}, Limits: lim(60, 180)},
	{ID: "kimi-k3.1-pro", Name: "Kimi K3.1 Pro", Rate: Rate{2.00, 10.00, 0.20}, Limits: lim(15, 60), Vision: true},
	{ID: "kimi-k3", Name: "Kimi K3", Rate: Rate{3.00, 15.00, 0.30}, Limits: lim(15, 60), Vision: true},
	{ID: "kimi-k2.7-code", Name: "Kimi K2.7 Code", Rate: Rate{0.95, 4.00, 0.19}, Limits: lim(60, 180)},
	{ID: "longcat-2.0", Name: "LongCat-2.0", Rate: Rate{0.30, 1.20, 0.006}, Limits: lim(60, 240)},
	{ID: "step-5-preview", Name: "Step 5 Preview", Rate: Rate{1.00, 2.70, 0.10}, Limits: lim(15, 60)},
	{ID: "mimo-v2.6-flash", Name: "MiMo-V2.6-Flash", Rate: Rate{0.14, 0.28, 0.0028}, Limits: lim(60, 120)},
	{ID: "mimo-v2.6-pro", Name: "MiMo-V2.6-Pro", Rate: Rate{0.435, 0.87, 0.003625}, Limits: lim(15, 60)},
	{ID: "mimo-v2.5", Name: "MiMo-V2.5", Rate: Rate{0.14, 0.28, 0.0028}, Limits: lim(60, 120)},
	{ID: "mimo-v2.5-pro", Name: "MiMo-V2.5-Pro", Rate: Rate{0.435, 0.87, 0.003625}, Limits: lim(15, 60)},
	{ID: "minimax-m3.1-flash", Name: "MiniMax M3.1 Flash", Rate: Rate{0.15, 0.60, 0.015}, Limits: lim(20, 80)},
	{ID: "minimax-m3", Name: "MiniMax M3", Rate: Rate{0.30, 1.20, 0.06}, Limits: lim(60, 180)},
	{ID: "muse-spark-1.3-contributor", Name: "Muse Spark 1.3 Contributor", Rate: Rate{0.10, 0.20, 0.002}, Limits: lim(60, 120),
		Note: "会用请求数据训练模型，要在 OpenCode 控制台 Settings › Privacy 里打开 Allow paid endpoints that train on request data；部分地区不可用"},
	{ID: "muse-spark-1.2-contributor", Name: "Muse Spark 1.2 Contributor", Rate: Rate{0.10, 0.20, 0.002}, Limits: lim(60, 120),
		Note: "会用请求数据训练模型，要在 OpenCode 控制台 Settings › Privacy 里打开 Allow paid endpoints that train on request data；部分地区不可用"},
	{ID: "qwen3.8-max", Name: "Qwen3.8 Max", Rate: Rate{2.00, 6.00, 0.25}, Limits: lim(15, 60)},
	{ID: "qwen3.8-flash", Name: "Qwen3.8 Flash", Rate: Rate{0.15, 0.47, 0.016}, Limits: lim(30, 90)},
	{ID: "qwen3.7-plus", Name: "Qwen3.7 Plus", Rate: Rate{0.40, 1.60, 0.04}, Limits: lim(60, 180), Note: tiered + "（256K）"},
	{ID: "deepseek-v4.1-flash", Name: "DeepSeek V4.1 Flash", Rate: Rate{0.30, 1.20, 0.006}, OffPeak: &Rate{0.15, 0.60, 0.003}, Limits: lim(80, 160),
		Note: "默认被隐私设置挡住，要在 OpenCode 控制台 Settings › Privacy 的 Regions 里选上 Global"},
	{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", Rate: Rate{1.32, 3.96, 0.044}, OffPeak: &Rate{0.66, 1.98, 0.022}, Limits: lim(15, 60),
		Note: "默认被隐私设置挡住，要在 OpenCode 控制台 Settings › Privacy 的 Regions 里选上 Global"},
	{ID: "hy4-preview", Name: "Hy4 preview", Rate: Rate{0.834, 2.501, 0.042}, Limits: lim(30, 120)},
	{ID: "hy3", Name: "Hy3", Rate: Rate{0.14, 0.58, 0.035}, Limits: lim(60, 240)},
	{ID: "grok-4.7", Name: "Grok 4.7", Rate: Rate{2.00, 6.00, 0.50}, Limits: lim(15, 60), Note: tiered + "（200K）"},
	{ID: "gpt-6-luna", Name: "GPT 6 Luna", Rate: Rate{0.10, 0.50, 0.01}, Limits: lim(15, 60), Vision: true, Note: tiered + "（272K）"},
	{ID: "gpt-5.6-luna", Name: "GPT 5.6 Luna", Rate: Rate{0.20, 1.20, 0.02}, Limits: lim(15, 60), Vision: true, Note: tiered + "（272K）"},
	{ID: "claude-haiku-5-5", Name: "Claude Haiku 5.5", Rate: Rate{0.10, 0.50, 0.01}, Limits: lim(15, 60), Vision: true, Note: tiered + "（100K）"},
	{ID: "gemini-4-argon", Name: "Gemini 4 Argon", Rate: Rate{2.00, 10.00, 0.10}, Limits: lim(30, 120), Vision: true,
		Note: "按 5 折优惠价计（优惠到 2026-11-30，之后原价是两倍）"},
	{ID: "beam-free", Name: "Beam Free", Free: true, Limits: lim(0, 0), Note: "限时免费；中国大陆、港澳等地区不可用"},
	{ID: "iceman-contributor-free", Name: "Iceman Contributor Free", Free: true, Limits: lim(0, 0), Note: "限时免费"},
}

// Find 按模型名找目录里的模型。
func Find(id string) (Model, bool) {
	for _, m := range Catalog {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// Factor 是这个模型在某一档里的成本折算系数：套餐月费 ÷ 这个模型的月额度（免费模型为 0）。
// 例如 Go 档 GLM-5.2 的月额度是 $60，系数是 10/60，相当于官方价的六分之一。
func (m Model) Factor(tier string) float64 {
	limit := m.Limits[tier]
	if m.Free || limit <= 0 {
		return 0
	}
	return PlanPrice[tier] / limit
}

// CNY 把一组美元价格按系数、汇率和加价倍数换算成元 / 百万 token（保留 4 位小数）。
func (r Rate) CNY(factor, rate, markup float64) Rate {
	k := factor * rate * markup
	return Rate{Input: round4(r.Input * k), Output: round4(r.Output * k), Cached: round4(r.Cached * k)}
}

func round4(x float64) float64 { return math.Round(x*10000) / 10000 }

// IsOpenCode 报告一个接口地址是不是 OpenCode 的。
func IsOpenCode(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	h := u.Hostname()
	return h == "opencode.ai" || strings.HasSuffix(h, ".opencode.ai")
}

// PeakUTC 是 DeepSeek 在 OpenCode 上的高峰时段（UTC，周一至周五）。
var PeakUTC = [][2]int{{1, 4}, {6, 10}}
