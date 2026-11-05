package apiserve

import (
	"strconv"
	"strings"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/prompt"
)

// Entry 是 API 里能用的一个模型。
type Entry struct {
	// Name 是请求里填的 model：一般就是模型名（例如 deepseek-flash）；
	// 几个模型同名时写成「模型名@编号」区分。
	Name        string  `json:"name"`
	ID          uint64  `json:"id"`
	DisplayName string  `json:"display_name"`
	Scope       string  `json:"scope"`
	Vision      bool    `json:"vision"`
	Tools       bool    `json:"tools"`
	Default     bool    `json:"is_default"`
	Scale       float64 `json:"scale"`
	// Personal 表示用户自己添加的模型（用自己的 Key，不计费）。
	Personal bool `json:"personal"`
}

// Catalog 把用户能用的模型列成 API 的模型列表。
func Catalog(views []models.View, st Settings) []Entry {
	count := map[string]int{}
	for _, v := range views {
		count[strings.ToLower(v.Model)]++
	}
	out := make([]Entry, 0, len(views))
	for _, v := range views {
		name := v.Model
		if count[strings.ToLower(v.Model)] > 1 {
			name = v.Model + "@" + strconv.FormatUint(v.ID, 10)
		}
		e := Entry{Name: name, ID: v.ID, DisplayName: v.Name, Scope: v.Scope, Vision: v.Vision, Tools: v.Tools,
			Default: v.IsDefault, Personal: v.Scope == models.ScopePersonal}
		if !e.Personal {
			e.Scale = st.Scale(v.ID)
		}
		out = append(out, e)
	}
	return out
}

// Find 按请求里的 model 找模型：先按 API 名字，再按显示名、编号；忽略大小写。为空时用默认模型。
func Find(entries []Entry, model string) (Entry, bool) {
	model = strings.TrimSpace(model)
	if model == "" || model == "default" {
		for _, e := range entries {
			if e.Default {
				return e, true
			}
		}
		if len(entries) > 0 {
			return entries[0], true
		}
		return Entry{}, false
	}
	for _, match := range []func(Entry) bool{
		func(e Entry) bool { return strings.EqualFold(e.Name, model) },
		// 同名模型里没写 @编号 时，用第一个（列表里公共模型在个人模型前面）。
		func(e Entry) bool { n, _, _ := strings.Cut(e.Name, "@"); return strings.EqualFold(n, model) },
		func(e Entry) bool { return strings.EqualFold(e.DisplayName, model) },
		func(e Entry) bool { return strconv.FormatUint(e.ID, 10) == model },
	} {
		for _, e := range entries {
			if match(e) {
				return e, true
			}
		}
	}
	return Entry{}, false
}

// Estimate 在服务商没返回用量时估算 token 数（不让没有用量的调用白用）。
func Estimate(req llm.ChatRequest, output string) llm.Usage {
	in := 0
	for _, m := range req.Messages {
		in += prompt.EstimateTokens(m.Content) + 4 + prompt.ImageTokens*len(m.Images)
		for _, tc := range m.ToolCalls {
			in += prompt.EstimateTokens(tc.Name + tc.Arguments)
		}
	}
	for _, t := range req.Tools {
		in += prompt.EstimateTokens(t.Name+t.Description) + 50
	}
	return llm.Usage{PromptTokens: in, CompletionTokens: prompt.EstimateTokens(output)}
}

// HasImages 报告请求里有没有图片。
func HasImages(req llm.ChatRequest) bool {
	for _, m := range req.Messages {
		if len(m.Images) > 0 {
			return true
		}
	}
	return false
}
