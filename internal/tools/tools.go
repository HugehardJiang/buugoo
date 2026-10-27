// Package tools 实现聊天中模型可以调用的工具：联网搜索、读取网页（DeepSeek、博查、Tavily 或直接读取）
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
	NameSearch  = "web_search"
	NameFetch   = "web_fetch"
	NamePython  = "run_python"
	NameInstall = "install_package"
	NameSend    = "send_file"
)

// Installer 安装第三方库。
type Installer interface {
	Install(ctx context.Context, pkgs []string) (string, error)
}

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
	// Images 是代码生成的图片，由调用方保存后显示在回复中。
	Images []Image
	// Files 是要发给用户的文件，由调用方保存为回复的附件。
	Files []Image
}

type handler func(ctx context.Context, args json.RawMessage) Result

// Extra 是由其他模块提供的工具（例如提醒）。
type Extra struct {
	Def llm.Tool
	// Label 是界面上显示的中文名。
	Label string
	// Hint 写进系统提示词的工具说明，可以为空。
	Hint string
	Run  func(ctx context.Context, args json.RawMessage) Result
}

// Set 是一轮对话可用的工具。
type Set struct {
	defs     []llm.Tool
	handlers map[string]handler
	labels   map[string]string
	hints    []string
}

// Options 决定提供哪些工具。
type Options struct {
	// Search 不为空时提供联网搜索。
	Search Searcher
	// Fetch 不为空时提供读取网页。
	Fetch Fetcher
	// Python 不为空时提供运行 Python。
	Python Runner
	// Packages 是已经预装的第三方库（写进工具说明）。
	Packages []string
	// Installer 不为空时提供安装第三方库。
	Installer Installer
	// CJKFont 表示运行代码的环境有中文字体，图表中可以写中文。
	CJKFont bool
	// Workspace 是这段对话的文件环境，为空时每次运行都是全新的空目录。
	Workspace Workspace
	// Inputs 是 input/ 中的文件名，Saved 是工作目录里已经保存的文件（写进工具说明）。
	Inputs, Saved []string
	// Extra 是其他模块提供的工具。
	Extra []Extra
}

// New 按选项组装工具集。
func New(o Options) *Set {
	s := &Set{handlers: map[string]handler{}, labels: map[string]string{}}
	for _, e := range o.Extra {
		s.add(e.Def, e.Run)
		s.labels[e.Def.Name] = e.Label
		if e.Hint != "" {
			s.hints = append(s.hints, e.Hint)
		}
	}
	if o.Search != nil {
		s.add(llm.Tool{
			Name:        NameSearch,
			Description: "联网搜索实时信息（新闻、天气、价格、近期事件、你不确定的事实）。返回标题、链接、日期与摘要。",
			Parameters: object(map[string]any{
				"query": map[string]any{"type": "string", "description": "搜索词，1～128 个字"},
				"count": map[string]any{"type": "integer", "description": "返回条数，默认 5，最多 10"},
			}, "query"),
		}, searchHandler(o.Search))
	}
	if o.Fetch != nil {
		s.add(llm.Tool{
			Name:        NameFetch,
			Description: "读取一个网页的正文。用于查看搜索结果或用户给出的链接的详细内容。",
			Parameters: object(map[string]any{
				"url": map[string]any{"type": "string", "description": "http(s) 网址"},
			}, "url"),
		}, fetchHandler(o.Fetch))
	}
	if o.Python != nil {
		libs := "只有标准库"
		if len(o.Packages) > 0 {
			libs = "除标准库外已预装：" + strings.Join(o.Packages, "、")
		}
		if o.Installer != nil {
			libs += "；缺少别的库时先用 install_package 安装"
		}
		files := ""
		if o.Workspace != nil {
			files = "。当前目录是这段对话专属的工作目录，保存在这里的文件之后的运行还能用"
			if len(o.Saved) > 0 {
				files += "（现在有：" + strings.Join(o.Saved, "、") + "）"
			}
			if len(o.Inputs) > 0 {
				files += "；用户在这段对话里发的文件在 input/ 目录（只读）：" + strings.Join(o.Inputs, "、")
			}
		}
		// 画图：保存到当前目录的图片或没有关闭的 matplotlib 图表会直接显示给用户。
		plot := "需要画图时用 matplotlib 画好即可（不用 plt.show），或用 savefig 保存为 png/jpg 到当前目录，图片会自动显示给用户，一次最多 6 张"
		if o.CJKFont {
			plot += "；图中可以写中文"
		} else {
			plot += "；图中的标题、坐标轴和图例请用英文（没有中文字体，中文会显示成方框）"
		}
		s.add(llm.Tool{
			Name: NamePython,
			Description: "运行一段 Python 3 代码并返回标准输出与错误输出。用于需要准确结果的计算、换算、日期推算、统计与数据处理；" +
				"不要心算。用 print 输出结果。运行时没有网络；" + libs + files + "。" + plot + "。单次运行有时间与内存限制。",
			Parameters: object(map[string]any{
				"code": map[string]any{"type": "string", "description": "完整的 Python 代码"},
			}, "code"),
		}, pythonHandler(o.Python, o.Workspace))
		if fs, ok := o.Workspace.(FileSource); ok && o.Workspace != nil {
			s.add(llm.Tool{
				Name: NameSend,
				Description: "把工作目录里的文件发给对方（例如代码生成的 Excel、CSV、PDF、压缩包），对方可以下载；在聊天软件里聊天时会直接发过去。" +
					"先用 run_python 把文件保存到当前目录，再用这个工具发送。图片不用发，画的图会自动显示。一次最多 5 个文件。",
				Parameters: object(map[string]any{
					"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
						"description": "文件在工作目录中的路径，例如 [\"结果.xlsx\"]"},
				}, "paths"),
			}, sendHandler(fs))
		}
		if o.Installer != nil {
			s.add(llm.Tool{
				Name: NameInstall,
				Description: "从 PyPI 安装 Python 第三方库，装好后 run_python 就能 import。只能安装有预编译包（wheel）的库，" +
					"一次最多 10 个；已经装好的不会重复安装。只在确实需要、而预装库做不到时使用。",
				Parameters: object(map[string]any{
					"packages": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
						"description": "PyPI 包名，可带版本号，例如 [\"networkx\", \"statsmodels==0.14.4\"]"},
				}, "packages"),
			}, installHandler(o.Installer))
		}
	}
	return s
}

func installHandler(in Installer) handler {
	return func(ctx context.Context, args json.RawMessage) Result {
		var req struct {
			Packages []string `json:"packages"`
		}
		_ = json.Unmarshal(args, &req)
		res := Result{Input: strings.Join(req.Packages, " ")}
		out, err := in.Install(ctx, req.Packages)
		if err != nil {
			res.Summary = err.Error()
			res.Output = "安装失败：" + err.Error()
			if out != "" {
				res.Output += "\n" + truncate(out, 3000)
			}
			res.Detail = truncate(out, 3000)
			return res
		}
		res.OK, res.Summary = true, "已安装 "+strings.Join(req.Packages, "、")
		res.Output = "安装完成，现在可以在 run_python 中 import。\n" + truncate(out, 1500)
		res.Detail = truncate(out, 3000)
		return res
	}
}

func object(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func (s *Set) add(def llm.Tool, h handler) {
	s.defs = append(s.defs, def)
	s.handlers[def.Name] = h
}

// Label 返回工具在界面上显示的中文名。
func (s *Set) Label(name string) string {
	if s != nil {
		if l, ok := s.labels[name]; ok {
			return l
		}
	}
	return Label(name)
}

// Hints 返回其他模块提供的工具说明（写进系统提示词）。
func (s *Set) Hints() []string {
	if s == nil {
		return nil
	}
	return s.hints
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
	case NameInstall:
		return "安装 Python 库"
	case NameSend:
		return "发送文件"
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
