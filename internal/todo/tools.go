package todo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/tools"
)

const hint = "待办清单：对方提到要去做、要你帮忙记着的事（「帮我记一下要写大物实验报告」「下周五前要交论文」），用 todo_add 加进待办，" +
	"有截止时间就带上；对方问「我还有哪些没做」「待办里有什么」用 todo_list；对方说做完了就用 todo_done 打勾。" +
	"待办是持续要做的事；只是到点提醒一句用 set_reminder，长期保存的笔记资料用备忘录。有截止时间的待办快到期还没做完时，你会主动问对方进展。" +
	"删除要先问对方：todo_delete 会给一个确认码，对方明确同意后，下一次回复时带上确认码再调用。"

// Tools 返回某个用户这一轮对话可用的待办工具。turn 每轮不同，用来保证删除是在对方确认之后。
func Tools(s *Service, userID uint64, turn string, now func() time.Time) []tools.Extra {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	obj := func(props map[string]any, required ...string) map[string]any {
		m := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			m["required"] = required
		}
		return m
	}
	dueDesc := "截止时间（本地时间），格式 YYYY-MM-DD HH:MM，只知道日期时写 YYYY-MM-DD"
	fail := func(res tools.Result, err error) tools.Result {
		res.Output, res.Summary = "失败："+err.Error(), "失败"
		return res
	}
	line := func(t *store.Todo) string {
		var b strings.Builder
		fmt.Fprintf(&b, "#%d %s", t.ID, t.Title)
		if t.DueAt != nil {
			b.WriteString("（截止 " + s.FormatDue(t, now()) + "）")
		}
		if t.Done {
			b.WriteString("〔已完成〕")
		}
		if t.Note != "" {
			b.WriteString(" 备注：" + clip(strings.Join(strings.Fields(t.Note), " "), 100))
		}
		return b.String()
	}
	// find 按编号（「#12」「12」）或内容找一项：先找一模一样的，再找唯一包含这段文字的没做完的。
	find := func(ctx context.Context, ref string) (*store.Todo, error) {
		ref = strings.TrimSpace(ref)
		if id, err := strconv.ParseUint(strings.TrimPrefix(ref, "#"), 10, 64); err == nil {
			t, err := s.Get(ctx, userID, id)
			if errors.Is(err, ErrNotFound) {
				return nil, &UserError{"没有编号 #" + strconv.FormatUint(id, 10) + " 的待办，先用 todo_list 看看"}
			}
			return t, err
		}
		list, err := s.List(ctx, userID)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(strings.Join(strings.Fields(ref), " "))
		if key == "" {
			return nil, &UserError{"要说明是哪一项（编号或内容）"}
		}
		var hits []*store.Todo
		for i := range list {
			if strings.ToLower(list[i].Title) == key {
				return &list[i], nil
			}
			if strings.Contains(strings.ToLower(list[i].Title), key) {
				hits = append(hits, &list[i])
			}
		}
		if len(hits) > 1 {
			var open []*store.Todo
			for _, h := range hits {
				if !h.Done {
					open = append(open, h)
				}
			}
			if len(open) == 1 {
				return open[0], nil
			}
		}
		switch len(hits) {
		case 0:
			return nil, &UserError{"待办里没有「" + ref + "」，先用 todo_list 看看"}
		case 1:
			return hits[0], nil
		}
		return nil, &UserError{"有好几项都包含「" + ref + "」，用编号（例如 #12）指明是哪一项"}
	}
	return []tools.Extra{
		{
			Def: llm.Tool{Name: "todo_add", Description: "往对方的待办清单里加一件要做的事。",
				Parameters: obj(map[string]any{"title": str("要做的事，简短一句，例如「写大物实验报告」"), "due": str(dueDesc + "（可选）"),
					"note": str("备注（可选），例如要求、地点、链接")}, "title")},
			Label: "加待办", Hint: hint,
			Run: func(ctx context.Context, args json.RawMessage) tools.Result {
				var a struct{ Title, Due, Note string }
				_ = json.Unmarshal(args, &a)
				res := tools.Result{Input: a.Title}
				t, err := s.Create(ctx, userID, a.Title, a.Note, a.Due)
				if err != nil {
					return fail(res, err)
				}
				res.OK, res.Output, res.Summary = true, "已加进待办："+line(t), "加了「"+t.Title+"」"
				return res
			},
		},
		{
			Def: llm.Tool{Name: "todo_list", Description: "查看对方的待办清单：没做完的（按截止时间排）和最近做完的。",
				Parameters: obj(map[string]any{"include_done": map[string]any{"type": "boolean", "description": "是否也列出已经做完的，默认不列"}})},
			Label: "查看待办",
			Run: func(ctx context.Context, args json.RawMessage) tools.Result {
				var a struct {
					IncludeDone bool `json:"include_done"`
				}
				_ = json.Unmarshal(args, &a)
				res := tools.Result{OK: true}
				list, err := s.List(ctx, userID)
				if err != nil {
					return fail(res, err)
				}
				var b strings.Builder
				open, done := 0, 0
				for i := range list {
					t := &list[i]
					if t.Done {
						done++
						if !a.IncludeDone || done > 20 {
							continue
						}
					} else {
						open++
					}
					b.WriteString("- " + line(t))
					if !t.Done && t.DueAt != nil && t.DueAt.Before(now()) {
						b.WriteString("〔已过截止时间〕")
					}
					b.WriteString("\n")
				}
				switch {
				case open == 0 && b.Len() == 0:
					res.Output = "没有没做完的待办。"
					if done > 0 {
						res.Output += fmt.Sprintf("（做完的有 %d 项）", done)
					}
				case open == 0:
					res.Output = "没有没做完的待办。做完的：\n" + b.String()
				default:
					res.Output = fmt.Sprintf("没做完的有 %d 项（现在是 %s）：\n", open, now().In(s.loc).Format("2006-01-02 15:04")) + b.String()
					if done > 0 && !a.IncludeDone {
						res.Output += fmt.Sprintf("另外已经做完 %d 项。", done)
					}
				}
				res.Summary = fmt.Sprintf("%d 项没做完", open)
				return res
			},
		},
		{
			Def: llm.Tool{Name: "todo_done", Description: "把一项待办标记为做完（打勾）；done 填 false 可以取消打勾。",
				Parameters: obj(map[string]any{"item": str("编号（例如 #12）或待办的内容"), "done": map[string]any{"type": "boolean", "description": "默认 true"}}, "item")},
			Label: "完成待办",
			Run: func(ctx context.Context, args json.RawMessage) tools.Result {
				var a struct {
					Item string `json:"item"`
					Done *bool  `json:"done"`
				}
				_ = json.Unmarshal(args, &a)
				res := tools.Result{Input: a.Item}
				t, err := find(ctx, a.Item)
				if err != nil {
					return fail(res, err)
				}
				done := a.Done == nil || *a.Done
				if t, err = s.SetDone(ctx, userID, t.ID, done); err != nil {
					return fail(res, err)
				}
				res.OK = true
				if done {
					res.Output, res.Summary = "已打勾："+line(t), "完成「"+t.Title+"」"
				} else {
					res.Output, res.Summary = "已取消打勾："+line(t), "重新打开「"+t.Title+"」"
				}
				return res
			},
		},
		{
			Def: llm.Tool{Name: "todo_update", Description: "修改一项待办的内容、截止时间或备注（不填的不改）。due 填 none 表示去掉截止时间。",
				Parameters: obj(map[string]any{"item": str("编号（例如 #12）或待办的内容"), "title": str("新的内容"), "due": str(dueDesc + "，或 none"),
					"note": str("新的备注（整段替换）")}, "item")},
			Label: "改待办",
			Run: func(ctx context.Context, args json.RawMessage) tools.Result {
				var a struct {
					Item             string
					Title, Due, Note *string
				}
				_ = json.Unmarshal(args, &a)
				res := tools.Result{Input: a.Item}
				t, err := find(ctx, a.Item)
				if err != nil {
					return fail(res, err)
				}
				p := Patch{Title: a.Title, Note: a.Note, Due: a.Due}
				if p.Title != nil && strings.TrimSpace(*p.Title) == "" {
					p.Title = nil
				}
				if p.Due != nil && strings.EqualFold(strings.TrimSpace(*p.Due), "none") {
					empty := ""
					p.Due = &empty
				} else if p.Due != nil && strings.TrimSpace(*p.Due) == "" {
					p.Due = nil
				}
				if p.Title == nil && p.Note == nil && p.Due == nil {
					return fail(res, &UserError{"没有要改的"})
				}
				if t, err = s.Update(ctx, userID, t.ID, p); err != nil {
					return fail(res, err)
				}
				res.OK, res.Output, res.Summary = true, "已修改："+line(t), "改了「"+t.Title+"」"
				return res
			},
		},
		{
			Def: llm.Tool{Name: "todo_delete", Description: "从待办清单里删掉一项（做完的应该用 todo_done 打勾，不是删除）。" +
				"第一次调用不要填 confirm_code：会返回要删的是哪一项和一个确认码，你要先问对方；对方明确同意后，在下一次回复时带上 confirm_code 再调用才会真的删除。",
				Parameters: obj(map[string]any{"item": str("编号（例如 #12）或待办的内容"), "confirm_code": str("确认码")}, "item")},
			Label: "删待办",
			Run: func(ctx context.Context, args json.RawMessage) tools.Result {
				var a struct {
					Item        string
					ConfirmCode string `json:"confirm_code"`
				}
				_ = json.Unmarshal(args, &a)
				res := tools.Result{Input: a.Item}
				if a.ConfirmCode != "" {
					d, err := s.take(a.ConfirmCode, userID, turn)
					if err != nil {
						return fail(res, err)
					}
					t, err := s.Get(ctx, userID, d.ItemID)
					if err != nil {
						return fail(res, err)
					}
					if err := s.Delete(ctx, userID, t.ID); err != nil {
						return fail(res, err)
					}
					res.OK, res.Output, res.Summary = true, "已删除："+line(t), "删除了「"+t.Title+"」"
					return res
				}
				t, err := find(ctx, a.Item)
				if err != nil {
					return fail(res, err)
				}
				code := s.propose(pendingDelete{UserID: userID, ItemID: t.ID, Turn: turn})
				res.OK = true
				res.Output = "还没有删除。要删的是：" + line(t) + "。请先问对方确认；对方明确同意后，下一次回复时调用 todo_delete 并带上 confirm_code=" + code + "。"
				res.Summary = "等对方确认"
				res.Detail = res.Output // 下一轮靠历史里的工具记录拿到确认码
				return res
			},
		},
	}
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
