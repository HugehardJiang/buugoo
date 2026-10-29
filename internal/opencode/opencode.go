// Package opencode 接入 OpenCode Go 套餐：模型目录（模型名、官方价格、每个模型在各档套餐里的月额度），
// 站长一键导入（按「官方价 × 套餐月费 ÷ 这个模型的月额度 × 汇率 × 加价倍数」折算成实际成本写进计费），
// 用户用自己的 Key 一键添加，以及定时从官网文档同步模型和价格。
//
// 内置的目录来自 https://opencode.ai/docs/go/（Builtin.Date 那天）；同步成功后用官网最新的。
package opencode

import (
	"math"
	"net/url"
	"strings"
	"time"
)

// BaseURL 是 OpenCode Go 的 OpenAI 兼容接口（所有文本模型都能走这个接口，平台会自动转换）。
const BaseURL = "https://opencode.ai/inference/go/openai/v1"

// DocsURL 是官方文档（同步模型和价格用）。
const DocsURL = "https://opencode.ai/docs/go/"

// 套餐档位（和官网文档里的 data-plan-panel 一致）。
const (
	TierGo    = "go"
	TierPlus  = "go-plus"
	TierTeams = "go-teams"
)

// NormalizeTier 把旧版本保存的档位名改成现在的。
func NormalizeTier(t string) string {
	if t == "plus" {
		return TierPlus
	}
	return t
}

// Plan 是一档套餐。
type Plan struct {
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	// PerMember 表示按成员收费（Go Teams：每个成员每月 $20，每个成员有自己的额度）。
	PerMember bool `json:"per_member,omitempty"`
}

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
	// OffPeak 不为空时，Rate 是高峰价，OffPeak 是空闲价。
	OffPeak *Rate `json:"off_peak,omitempty"`
	// Limits 是各档的月额度（美元），免费模型为 0。
	Limits map[string]float64 `json:"limits"`
	Free   bool               `json:"free,omitempty"`
	Vision bool               `json:"vision,omitempty"`
	// Note 是要注意的事（分段计价只取了低档、需要改隐私设置等）。
	Note string `json:"note,omitempty"`
}

// Snapshot 是某一天的完整目录。
type Snapshot struct {
	Date string `json:"date"`
	// Source 是 builtin（随程序发布的）或 synced（从官网同步的）。
	Source    string          `json:"source"`
	FetchedAt *time.Time      `json:"fetched_at,omitempty"`
	Plans     map[string]Plan `json:"plans"`
	// Tiers 是档位的顺序。
	Tiers  []string `json:"tiers"`
	Models []Model  `json:"models"`
}

// Find 按模型名找模型。
func (s *Snapshot) Find(id string) (Model, bool) {
	for _, m := range s.Models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// Factor 是这个模型在某一档里的成本折算系数：套餐月费 ÷ 这个模型的月额度（免费模型为 0）。
// 例如 Go 档 GLM-5.2 的月额度是 $60，系数是 10/60，相当于官方价的六分之一。
func (s *Snapshot) Factor(m Model, tier string) float64 {
	limit := m.Limits[tier]
	if m.Free || limit <= 0 {
		return 0
	}
	return s.Plans[tier].Price / limit
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

const (
	tieredNote  = "上下文超过一定长度后官方价格更高，这里按较低一档计"
	privacyNote = "默认被隐私设置挡住，要在 OpenCode 控制台 Settings › Privacy 的 Regions 里选上 Global"
	trainNote   = "会用请求数据训练模型，要在 OpenCode 控制台 Settings › Privacy 里打开 Allow paid endpoints that train on request data；部分地区不可用"
)

func lim(goLimit, plusLimit float64) map[string]float64 {
	return map[string]float64{TierGo: goLimit, TierPlus: plusLimit}
}

// Builtin 是随程序发布的目录（官网有变化时以同步到的为准）。
var Builtin = Snapshot{
	Date: "2026-10-28", Source: "builtin",
	Plans: map[string]Plan{TierGo: {Name: "Go", Price: 10}, TierPlus: {Name: "Go Plus", Price: 40}},
	Tiers: []string{TierGo, TierPlus},
	Models: []Model{
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
		{ID: "muse-spark-1.3-contributor", Name: "Muse Spark 1.3 Contributor", Rate: Rate{0.10, 0.20, 0.002}, Limits: lim(60, 120), Note: trainNote},
		{ID: "muse-spark-1.2-contributor", Name: "Muse Spark 1.2 Contributor", Rate: Rate{0.10, 0.20, 0.002}, Limits: lim(60, 120), Note: trainNote},
		{ID: "qwen3.8-max", Name: "Qwen3.8 Max", Rate: Rate{2.00, 6.00, 0.25}, Limits: lim(15, 60)},
		{ID: "qwen3.8-flash", Name: "Qwen3.8 Flash", Rate: Rate{0.15, 0.47, 0.016}, Limits: lim(30, 90)},
		{ID: "qwen3.7-plus", Name: "Qwen3.7 Plus", Rate: Rate{0.40, 1.60, 0.04}, Limits: lim(60, 180), Note: tieredNote},
		{ID: "deepseek-v4.1-flash", Name: "DeepSeek V4.1 Flash", Rate: Rate{0.30, 1.20, 0.006}, OffPeak: &Rate{0.15, 0.60, 0.003}, Limits: lim(80, 160), Note: privacyNote},
		{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", Rate: Rate{1.32, 3.96, 0.044}, OffPeak: &Rate{0.66, 1.98, 0.022}, Limits: lim(15, 60), Note: privacyNote},
		{ID: "hy4-preview", Name: "Hy4 preview", Rate: Rate{0.834, 2.501, 0.042}, Limits: lim(30, 120)},
		{ID: "hy3", Name: "Hy3", Rate: Rate{0.14, 0.58, 0.035}, Limits: lim(60, 240)},
		{ID: "grok-4.7", Name: "Grok 4.7", Rate: Rate{2.00, 6.00, 0.50}, Limits: lim(15, 60), Note: tieredNote},
		{ID: "gpt-6-luna", Name: "GPT 6 Luna", Rate: Rate{0.10, 0.50, 0.01}, Limits: lim(15, 60), Vision: true, Note: tieredNote},
		{ID: "gpt-5.6-luna", Name: "GPT 5.6 Luna", Rate: Rate{0.20, 1.20, 0.02}, Limits: lim(15, 60), Vision: true, Note: tieredNote},
		{ID: "claude-haiku-5-5", Name: "Claude Haiku 5.5", Rate: Rate{0.10, 0.50, 0.01}, Limits: lim(15, 60), Vision: true, Note: tieredNote},
		{ID: "gemini-4-argon", Name: "Gemini 4 Argon", Rate: Rate{2.00, 10.00, 0.10}, Limits: lim(30, 120), Vision: true, Note: "按优惠价计（官方写明是限时优惠）"},
		{ID: "beam-free", Name: "Beam Free", Free: true, Limits: lim(0, 0), Note: "限时免费；中国大陆、港澳等地区不可用"},
		{ID: "iceman-contributor-free", Name: "Iceman Contributor Free", Free: true, Limits: lim(0, 0), Note: "限时免费"},
	},
}
