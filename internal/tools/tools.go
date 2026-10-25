// Package tools 实现聊天中模型可以调用的工具：联网搜索、读取网页（DeepSeek Search / Fetch 接口）
// 与运行 Python 代码（Docker 沙箱或本机解释器）。
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/HugehardJiang/buugoo/internal/llm"
)

// 工具名。
const (
	NameSearch = "web_search"
	NameFetch  = "web_fetch"
	NamePython = "run_python"
)

// Result 是一次工具调用的结果。
type Result struct {
	// Output 是返回给模型的内容。
	Output string
	// Input 是给用户看的调用参数（搜索词、网址或代码）。
	Input string
	// Summary 是给用户看的一句话结果。
	Summary string
	// Detail 是给用户看的详细结果（搜索结果列表、代码输出），可为空。
	Detail string
	OK     bool
}

type handler func(ctx context.Context, args json.RawMessage) Result

// Set 是一轮对话可用的工具。
type Set struct {
	defs     []llm.Tool
	handlers map[string]handler
}

// Options 决定提供哪些工具。
type Options struct {
	// DeepSeek 不为空时提供联网搜索与读取网页。
	DeepSeek *DeepSeek
	// Python 不为空时提供运行 Python。
	Python Runner
}

// New 按选项组装工具集。
func New(o Options) *Set {
	s := &Set{handlers: map[string]handler{}}
	if o.DeepSeek != nil {
		s.add(llm.Tool{
			Name: NameSearch,
			Description: "联网搜索实时信息（新闻、天气、价格、近期事件、你不确定的事实）。返回标题、链接、日期与摘要。" +
				"支持 site:、\"精确短语\"、after:2025-01-01 等语法。",
			Parameters: object(map[string]any{
				"query": map[string]any{"type": "string", "description": "搜索词，1～128 个字"},
				"count": map[string]any{"type": "integer", "description": "返回条数，默认 5，最多 10"},
			}, "query"),
		}, o.DeepSeek.search)
		s.add(llm.Tool{
			Name:        NameFetch,
			Description: "读取一个网页的正文（Markdown）。用于查看搜索结果或用户给出的链接的详细内容。",
			Parameters: object(map[string]any{
				"url": map[string]any{"type": "string", "description": "http(s) 网址"},
			}, "url"),
		}, o.DeepSeek.fetch)
	}
	if o.Python != nil {
		s.add(llm.Tool{
			Name: NamePython,
			Description: "运行一段 Python 3 代码并返回标准输出与错误输出。用于需要准确结果的计算、换算、日期推算、统计与数据处理；" +
				"不要心算。用 print 输出结果。没有网络，只有标准库；单次运行有时间与内存限制。",
			Parameters: object(map[string]any{
				"code": map[string]any{"type": "string", "description": "完整的 Python 代码"},
			}, "code"),
		}, pythonHandler(o.Python))
	}
	return s
}

func object(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func (s *Set) add(def llm.Tool, h handler) {
	s.defs = append(s.defs, def)
	s.handlers[def.Name] = h
}

// Defs 返回工具定义，没有工具时为空。
func (s *Set) Defs() []llm.Tool {
	if s == nil {
		return nil
	}
	return s.defs
}

// Run 执行一次工具调用。工具出错时结果中的 Output 会说明原因，交给模型自行处理。
func (s *Set) Run(ctx context.Context, call llm.ToolCall) Result {
	h, ok := s.handlers[call.Name]
	if !ok {
		return Result{Output: fmt.Sprintf("没有名为 %s 的工具", call.Name), Summary: "未知工具"}
	}
	args := json.RawMessage(call.Arguments)
	if strings.TrimSpace(call.Arguments) == "" {
		args = json.RawMessage("{}")
	}
	if !json.Valid(args) {
		return Result{Output: "参数不是合法的 JSON：" + call.Arguments, Summary: "参数格式错误"}
	}
	return h(ctx, args)
}

// Label 返回工具的中文名，用于界面展示。
func Label(name string) string {
	switch name {
	case NameSearch:
		return "联网搜索"
	case NameFetch:
		return "读取网页"
	case NamePython:
		return "运行代码"
	}
	return name
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "\n…（已截断）"
}
