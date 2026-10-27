package memo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/tools"
)

// maxRead 是读一页时交给模型的最大字数。
const maxRead = 20000

const hint = "备忘录：对方明确要你「记到备忘录」、查备忘录、改或删备忘录里的内容时才用 memo_ 开头的工具。" +
	"备忘录的内容不会自动出现在对话里，平时不要主动去读。往里写之前先用 memo_list 看看有哪些页，" +
	"选一个合适的页写进去，没有合适的就起一个简短的新标题（例如「购物清单」「读书笔记」）。" +
	"备忘录是对方要长期保存的清单、笔记、资料；到点提醒用提醒工具，不要写进备忘录。" +
	"删除要先问对方：memo_delete 会给一个确认码，对方明确同意后，下一次回复时带上确认码再调用。"

// Tools 返回某个用户这一轮对话可用的备忘录工具。turn 每轮不同，用来保证删除是在对方确认之后。
func Tools(s *Service, userID uint64, turn string) []tools.Extra {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	obj := func(props map[string]any, required ...string) map[string]any {
		m := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			m["required"] = required
		}
		return m
	}
	type pageArgs struct {
		Page string `json:"page"`
	}
	find := func(ctx context.Context, title string) (*pageRef, error) {
		p, err := s.Find(ctx, userID, title)
		if errors.Is(err, ErrNotFound) {
			return nil, &UserError{"备忘录里没有「" + strings.TrimSpace(title) + "」这一页，先用 memo_list 看看有哪些页"}
		}
		if err != nil {
			return nil, err
		}
		return &pageRef{id: p.ID, title: p.Title, content: p.Content}, nil
	}
	fail := func(res tools.Result, err error) tools.Result {
		res.Output, res.Summary = "失败："+err.Error(), "失败"
		return res
	}
	return []tools.Extra{
		{
			Def:   llm.Tool{Name: "memo_list", Description: "列出对方备忘录里的所有页面（标题、字数、开头一句、最后修改时间）。", Parameters: obj(map[string]any{})},
			Label: "查看备忘录", Hint: hint,
			Run: func(ctx context.Context, _ json.RawMessage) tools.Result {
				res := tools.Result{OK: true}
				list, err := s.List(ctx, userID)
				if err != nil {
					return fail(res, err)
				}
				if len(list) == 0 {
					res.Output, res.Summary = "备忘录还是空的，一页都没有。", "还是空的"
					return res
				}
				var b strings.Builder
				for _, p := range list {
					fmt.Fprintf(&b, "- 「%s」%d 字，%s 修改：%s\n", p.Title, p.Size, p.UpdatedAt.Format("01-02 15:04"), p.Preview)
				}
				res.Output, res.Summary = b.String(), fmt.Sprintf("%d 页", len(list))
				return res
			},
		},
		{
			Def:   llm.Tool{Name: "memo_read", Description: "读备忘录里的一页（完整内容）。", Parameters: obj(map[string]any{"page": str("页面标题")}, "page")},
			Label: "读备忘录",
			Run: func(ctx context.Context, args json.RawMessage) tools.Result {
				var a pageArgs
				_ = json.Unmarshal(args, &a)
				res := tools.Result{Input: a.Page}
				p, err := find(ctx, a.Page)
				if err != nil {
					return fail(res, err)
				}
				res.OK = true
				res.Output = clip(p.content, maxRead)
				if strings.TrimSpace(p.content) == "" {
					res.Output = "（这一页是空的）"
				}
				res.Summary, res.Detail = "读了「"+p.title+"」", clip(p.content, 2000)
				return res
			},
		},
		{
			Def: llm.Tool{Name: "memo_write", Description: "往备忘录的某一页写内容，这一页不存在时自动新建。mode 为 append（默认，加在末尾）、prepend（加在开头）或 replace（整页换成新内容，只在对方要求重写整页时用）。" +
				"内容用 Markdown，清单用「- 」开头的列表。",
				Parameters: obj(map[string]any{"page": str("页面标题"), "content": str("要写的内容"),
					"mode": map[string]any{"type": "string", "enum": []string{"append", "prepend", "replace"}}}, "page", "content")},
			Label: "写备忘录",
			Run: func(ctx context.Context, args json.RawMessage) tools.Result {
				var a struct {
					Page, Content, Mode string
				}
				_ = json.Unmarshal(args, &a)
				res := tools.Result{Input: a.Page}
				text := strings.TrimSpace(a.Content)
				if text == "" {
					return fail(res, &UserError{"没有要写的内容"})
				}
				p, err := s.Find(ctx, userID, a.Page)
				if errors.Is(err, ErrNotFound) {
					np, err := s.Create(ctx, userID, a.Page, text)
					if err != nil {
						return fail(res, err)
					}
					res.OK, res.Output, res.Summary, res.Detail = true, "新建了「"+np.Title+"」并写入", "新建「"+np.Title+"」", clip(text, 2000)
					return res
				}
				if err != nil {
					return fail(res, err)
				}
				content := text
				switch a.Mode {
				case "replace":
				case "prepend":
					content = text + "\n" + strings.TrimLeft(p.Content, "\n")
				default:
					if c := strings.TrimRight(p.Content, "\n"); c != "" {
						content = c + "\n" + text
					}
				}
				if _, err := s.Update(ctx, userID, p.ID, nil, &content); err != nil {
					return fail(res, err)
				}
				res.OK, res.Output, res.Summary, res.Detail = true, "已写进「"+p.Title+"」", "写进「"+p.Title+"」", clip(text, 2000)
				return res
			},
		},
		{
			Def: llm.Tool{Name: "memo_edit", Description: "修改备忘录某一页里的一段文字：把 find（原文中唯一的一段，要一字不差）换成 replace。也可以用 new_title 给这一页改名。删除内容不要用这个，用 memo_delete。",
				Parameters: obj(map[string]any{"page": str("页面标题"), "find": str("要改的原文"), "replace": str("改成的内容"), "new_title": str("新标题（可选）")}, "page")},
			Label: "改备忘录",
			Run: func(ctx context.Context, args json.RawMessage) tools.Result {
				var a struct {
					Page, Find, Replace string
					NewTitle            string `json:"new_title"`
				}
				_ = json.Unmarshal(args, &a)
				res := tools.Result{Input: a.Page}
				p, err := find(ctx, a.Page)
				if err != nil {
					return fail(res, err)
				}
				var content, title *string
				if a.Find != "" {
					if strings.TrimSpace(a.Replace) == "" {
						return fail(res, &UserError{"replace 为空就是删除，删除要用 memo_delete，先问过对方"})
					}
					switch strings.Count(p.content, a.Find) {
					case 0:
						return fail(res, &UserError{"这一页里没有这段原文，先用 memo_read 看准确的内容"})
					case 1:
					default:
						return fail(res, &UserError{"这段原文出现了不止一次，多带几个字让它唯一"})
					}
					c := strings.Replace(p.content, a.Find, a.Replace, 1)
					content = &c
				}
				if strings.TrimSpace(a.NewTitle) != "" {
					title = &a.NewTitle
				}
				if content == nil && title == nil {
					return fail(res, &UserError{"没有要改的"})
				}
				np, err := s.Update(ctx, userID, p.id, title, content)
				if err != nil {
					return fail(res, err)
				}
				res.OK, res.Output, res.Summary = true, "已修改「"+np.Title+"」", "改了「"+np.Title+"」"
				if content != nil {
					res.Detail = clip(a.Find+" → "+a.Replace, 2000)
				}
				return res
			},
		},
		{
			Def: llm.Tool{Name: "memo_delete", Description: "删除备忘录里的一段文字（text 填一字不差的原文），或者不填 text 删除整页。" +
				"第一次调用不要填 confirm_code：会返回要删的内容和一个确认码，你要先问对方；对方明确同意后，在下一次回复时带上 confirm_code 再调用才会真的删除。",
				Parameters: obj(map[string]any{"page": str("页面标题"), "text": str("要删的原文（不填表示删除整页）"), "confirm_code": str("确认码")}, "page")},
			Label: "删备忘录",
			Run: func(ctx context.Context, args json.RawMessage) tools.Result {
				var a struct {
					Page, Text  string
					ConfirmCode string `json:"confirm_code"`
				}
				_ = json.Unmarshal(args, &a)
				res := tools.Result{Input: a.Page}
				if a.ConfirmCode != "" {
					d, err := s.take(a.ConfirmCode, userID, turn)
					if err != nil {
						return fail(res, err)
					}
					p, err := s.Get(ctx, userID, d.PageID)
					if err != nil {
						return fail(res, err)
					}
					if d.Text == "" {
						if err := s.Delete(ctx, userID, p.ID); err != nil {
							return fail(res, err)
						}
						res.OK, res.Output, res.Summary = true, "已删除整页「"+p.Title+"」", "删除了「"+p.Title+"」"
						return res
					}
					if !strings.Contains(p.Content, d.Text) {
						return fail(res, &UserError{"这段内容已经不在了"})
					}
					c := removeText(p.Content, d.Text)
					if _, err := s.Update(ctx, userID, p.ID, nil, &c); err != nil {
						return fail(res, err)
					}
					res.OK, res.Output, res.Summary, res.Detail = true, "已从「"+p.Title+"」删除", "从「"+p.Title+"」删除了内容", clip(d.Text, 2000)
					return res
				}
				p, err := find(ctx, a.Page)
				if err != nil {
					return fail(res, err)
				}
				what := "整页「" + p.title + "」（" + fmt.Sprint(utf8.RuneCountInString(p.content)) + " 字）"
				if a.Text != "" {
					switch strings.Count(p.content, a.Text) {
					case 0:
						return fail(res, &UserError{"这一页里没有这段原文，先用 memo_read 看准确的内容"})
					case 1:
					default:
						return fail(res, &UserError{"这段原文出现了不止一次，多带几个字让它唯一"})
					}
					what = "「" + p.title + "」里的：" + clip(a.Text, 300)
				}
				code := s.propose(pendingDelete{UserID: userID, PageID: p.id, Text: a.Text, Turn: turn})
				res.OK = true
				res.Output = "还没有删除。要删的是" + what + "。请先问对方确认；对方明确同意后，下一次回复时调用 memo_delete 并带上 confirm_code=" + code + "。"
				res.Summary = "等对方确认"
				res.Detail = res.Output // 下一轮靠历史里的工具记录拿到确认码
				return res
			},
		},
	}
}

type pageRef struct {
	id      uint64
	title   string
	content string
}

// removeText 删掉一段文字，连带它独占的那一行，并去掉多余的空行。
func removeText(content, text string) string {
	i := strings.Index(content, text)
	if i < 0 {
		return content
	}
	start, end := i, i+len(text)
	lineStart := strings.LastIndex(content[:start], "\n") + 1
	lineEnd := len(content)
	if j := strings.Index(content[end:], "\n"); j >= 0 {
		lineEnd = end + j
	}
	// 这一行除了要删的内容只剩列表符号或空白时，整行删掉。
	rest := strings.TrimSpace(content[lineStart:start] + content[end:lineEnd])
	if rest == "" || rest == "-" || rest == "*" || rest == "- [ ]" || rest == "- [x]" {
		start = lineStart
		end = lineEnd
		if end < len(content) {
			end++ // 连换行一起删
		}
	}
	out := content[:start] + content[end:]
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimRight(out, "\n")
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
