// Package proactive 让布谷主动找用户：按时提醒（用户让布谷设的提醒），以及在合适的时候主动关心
// （很久没聊、之前提到的事有了结果等）。消息写进用户最近聊天的对话，并通过绑定的聊天软件发出去。
package proactive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/tools"
)

// 工具名。
const (
	NameSetReminder    = "set_reminder"
	NameListReminders  = "list_reminders"
	NameCancelReminder = "cancel_reminder"
)

// maxPendingReminders 是每个用户同时有效的提醒数上限。
const maxPendingReminders = 50

// timeLayout 是模型填写提醒时间的格式（本地时间）。
const timeLayout = "2006-01-02 15:04"

var repeatLabels = map[string]string{
	store.RepeatNone: "一次", store.RepeatDaily: "每天", store.RepeatWeekdays: "工作日", store.RepeatWeekly: "每周",
}

// NextDue 返回重复提醒的下一次时间（严格晚于 after），不重复时返回 nil。按本地时间计算，避开夏令时等问题。
func NextDue(due time.Time, repeat string, after time.Time, loc *time.Location) *time.Time {
	if repeat == store.RepeatNone {
		return nil
	}
	d := due.In(loc)
	for i := 0; i < 800 && !d.After(after); i++ {
		switch repeat {
		case store.RepeatWeekly:
			d = d.AddDate(0, 0, 7)
		default:
			d = d.AddDate(0, 0, 1)
		}
		if repeat == store.RepeatWeekdays {
			for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				d = d.AddDate(0, 0, 1)
			}
		}
	}
	return &d
}

// ReminderTools 返回某个用户可用的提醒工具。
func ReminderTools(repo store.ReminderRepo, userID uint64, loc *time.Location, now func() time.Time) []tools.Extra {
	return []tools.Extra{
		{
			Def: llm.Tool{
				Name: NameSetReminder,
				Description: "设一个提醒：到时间后你会主动给对方发消息。对方说「X 分钟后叫我」「明天早上八点提醒我开会」「每天晚上十点提醒我吃药」时使用。" +
					"time 填本地时间（格式 2006-01-02 15:04），或者用 after_minutes 表示多少分钟后。设好后简单确认一下就行。",
				Parameters: map[string]any{"type": "object", "properties": map[string]any{
					"content":       map[string]any{"type": "string", "description": "到时候要提醒的事，例如「起来活动一下」「九点的组会」"},
					"time":          map[string]any{"type": "string", "description": "提醒时间，本地时间，格式 YYYY-MM-DD HH:MM"},
					"after_minutes": map[string]any{"type": "integer", "description": "多少分钟后提醒（和 time 二选一）"},
					"repeat":        map[string]any{"type": "string", "enum": []string{"none", "daily", "weekdays", "weekly"}, "description": "重复：不重复、每天、工作日、每周，默认不重复"},
				}, "required": []string{"content"}},
			},
			Label: "设提醒",
			Hint:  "set_reminder / list_reminders / cancel_reminder：对方让你到点提醒、或者想看和取消提醒时使用；到时间你会主动发消息",
			Run: func(ctx context.Context, args json.RawMessage) tools.Result {
				return setReminder(ctx, repo, userID, loc, now(), args)
			},
		},
		{
			Def: llm.Tool{
				Name:        NameListReminders,
				Description: "列出对方现在有效的提醒（编号、时间、内容、是否重复）。",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
			Label: "查看提醒",
			Run: func(ctx context.Context, _ json.RawMessage) tools.Result {
				list, err := repo.ListPending(ctx, userID)
				if err != nil {
					return tools.Result{Output: "读取提醒失败：" + err.Error(), Summary: "读取失败"}
				}
				if len(list) == 0 {
					return tools.Result{OK: true, Output: "现在没有提醒。", Summary: "没有提醒"}
				}
				var b strings.Builder
				for _, r := range list {
					fmt.Fprintf(&b, "#%d %s（%s）%s\n", r.ID, r.DueAt.In(loc).Format(timeLayout), repeatLabels[r.Repeat], r.Content)
				}
				return tools.Result{OK: true, Output: strings.TrimSpace(b.String()), Summary: fmt.Sprintf("%d 个提醒", len(list)), Detail: strings.TrimSpace(b.String())}
			},
		},
		{
			Def: llm.Tool{
				Name:        NameCancelReminder,
				Description: "取消一个提醒。先用 list_reminders 找到编号。",
				Parameters: map[string]any{"type": "object", "properties": map[string]any{
					"id": map[string]any{"type": "integer", "description": "提醒编号"},
				}, "required": []string{"id"}},
			},
			Label: "取消提醒",
			Run: func(ctx context.Context, args json.RawMessage) tools.Result {
				var in struct {
					ID uint64 `json:"id"`
				}
				_ = json.Unmarshal(args, &in)
				res := tools.Result{Input: fmt.Sprintf("#%d", in.ID)}
				if err := repo.Cancel(ctx, userID, in.ID); err != nil {
					if errors.Is(err, store.ErrNotFound) {
						res.Output, res.Summary = "没有这个提醒（可能已经提醒过或取消了）", "没有这个提醒"
					} else {
						res.Output, res.Summary = "取消失败："+err.Error(), "取消失败"
					}
					return res
				}
				res.OK, res.Output, res.Summary = true, "已取消。", "已取消"
				return res
			},
		},
	}
}

func setReminder(ctx context.Context, repo store.ReminderRepo, userID uint64, loc *time.Location, now time.Time, args json.RawMessage) tools.Result {
	var in struct {
		Content      string `json:"content"`
		Time         string `json:"time"`
		AfterMinutes int    `json:"after_minutes"`
		Repeat       string `json:"repeat"`
	}
	_ = json.Unmarshal(args, &in)
	in.Content = strings.TrimSpace(in.Content)
	res := tools.Result{Input: in.Content}
	fail := func(msg string) tools.Result {
		res.Output, res.Summary = msg, msg
		return res
	}
	if in.Content == "" {
		return fail("提醒内容不能为空")
	}
	if utf8.RuneCountInString(in.Content) > 200 {
		in.Content = string([]rune(in.Content)[:200])
	}
	repeat := strings.TrimSpace(in.Repeat)
	if repeat == "none" {
		repeat = store.RepeatNone
	}
	if _, ok := repeatLabels[repeat]; !ok {
		return fail("repeat 只能是 none、daily、weekdays、weekly")
	}
	var due time.Time
	switch {
	case in.AfterMinutes > 0:
		if in.AfterMinutes > 366*24*60 {
			return fail("最多只能设一年以内的提醒")
		}
		due = now.Add(time.Duration(in.AfterMinutes) * time.Minute)
	case strings.TrimSpace(in.Time) != "":
		t, err := time.ParseInLocation(timeLayout, strings.TrimSpace(in.Time), loc)
		if err != nil {
			return fail("时间格式不对，应为 YYYY-MM-DD HH:MM")
		}
		due = t
		if !due.After(now) {
			if repeat == store.RepeatNone {
				return fail("这个时间已经过去了")
			}
			due = *NextDue(due, repeat, now, loc)
		}
		if due.After(now.AddDate(1, 0, 1)) {
			return fail("最多只能设一年以内的提醒")
		}
	default:
		return fail("需要填写 time 或 after_minutes")
	}
	pending, err := repo.ListPending(ctx, userID)
	if err != nil {
		return fail("设提醒失败：" + err.Error())
	}
	if len(pending) >= maxPendingReminders {
		return fail(fmt.Sprintf("提醒太多了（最多 %d 个），先取消一些", maxPendingReminders))
	}
	r := &store.Reminder{UserID: userID, Content: in.Content, DueAt: due.UTC(), Repeat: repeat}
	if err := repo.Create(ctx, r); err != nil {
		return fail("设提醒失败：" + err.Error())
	}
	when := due.In(loc).Format("01-02 15:04")
	res.OK = true
	res.Summary = fmt.Sprintf("%s %s", when, repeatLabels[repeat])
	res.Output = fmt.Sprintf("已设好提醒 #%d：%s（%s），内容：%s", r.ID, due.In(loc).Format("2006-01-02 15:04 Mon"), repeatLabels[repeat], in.Content)
	return res
}
