// Package proactive 让布谷主动找用户：按时提醒、到点执行定时任务（用户让布谷设的），以及在合适的时候主动关心
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
	NameScheduleTask   = "schedule_task"
	NameListReminders  = "list_reminders"
	NameCancelReminder = "cancel_reminder"
)

// 每个用户同时有效的提醒与定时任务数上限。
const (
	maxPendingReminders = 50
	maxPendingTasks     = 20
	maxReminderChars    = 200
	maxTaskChars        = 500
)

// timeLayout 是模型填写时间的格式（本地时间）。
const timeLayout = "2006-01-02 15:04"

// scheduleProps 是提醒与定时任务共用的时间参数。
func scheduleProps(props map[string]any) map[string]any {
	props["time"] = map[string]any{"type": "string", "description": "（第一次的）时间，本地时间，格式 YYYY-MM-DD HH:MM"}
	props["after_minutes"] = map[string]any{"type": "integer", "description": "多少分钟后（和 time 二选一）"}
	props["repeat"] = map[string]any{"type": "string", "enum": []string{"none", "daily", "weekdays", "weekly", "monthly", "interval", "cron"},
		"description": "重复：不重复、每天、工作日、每周、每月（同一天）、每隔一段时间（填 every_minutes）、按 cron 表达式（填 cron），默认不重复"}
	props["every_minutes"] = map[string]any{"type": "integer", "description": "repeat 为 interval 时，每隔多少分钟（至少 30）"}
	props["cron"] = map[string]any{"type": "string", "description": "repeat 为 cron 时的 5 段表达式：分 时 日 月 周（本地时间），例如「0 9 * * 1,3,5」是每周一三五早上 9 点；这时 time 可以不填"}
	return props
}

// Kind 的中文名。
func kindLabel(kind string) string {
	if kind == store.KindTask {
		return "定时任务"
	}
	return "提醒"
}

// Describe 返回一行说明：编号、时间、重复、内容。
func Describe(r *store.Reminder, loc *time.Location) string {
	state := ""
	if r.Status == store.ReminderPaused {
		state = "，已暂停"
	}
	return fmt.Sprintf("#%d [%s] 下次 %s（%s%s）%s", r.ID, kindLabel(r.Kind), r.DueAt.In(loc).Format(timeLayout), RepeatLabel(r, loc), state, r.Content)
}

// ReminderTools 返回某个用户可用的提醒与定时任务工具。
func ReminderTools(repo store.ReminderRepo, userID uint64, loc *time.Location, now func() time.Time) []tools.Extra {
	return []tools.Extra{
		{
			Def: llm.Tool{
				Name: NameSetReminder,
				Description: "设一个提醒：到时间后你会主动给对方发消息提醒一句。对方说「X 分钟后叫我」「明天早上八点提醒我开会」「每天晚上十点提醒我吃药」时使用。" +
					"设好后简单确认一下就行。",
				Parameters: map[string]any{"type": "object", "properties": scheduleProps(map[string]any{
					"content": map[string]any{"type": "string", "description": "到时候要提醒的事，例如「起来活动一下」「九点的组会」"},
				}), "required": []string{"content"}},
			},
			Label: "设提醒",
			Hint: "set_reminder / schedule_task / list_reminders / cancel_reminder：对方让你到点提醒用 set_reminder。" +
				"只要你答应了「到时候提醒/叫/喊/催你」，就必须在这一轮真的调用 set_reminder，光嘴上答应提醒是设不上的；对方说的时间含糊（「下午」「晚点」）就自己定一个具体时间并告诉对方；" +
				"让你到点去做一件事（「每天早上 8 点搜一下科技新闻发我」「每周五晚上帮我总结这周的汇率」）用 schedule_task，到时间你会真的去做（可以搜索、运行代码）再把结果发给对方",
			Run: func(ctx context.Context, args json.RawMessage) tools.Result {
				return create(ctx, repo, userID, store.KindRemind, loc, now(), args)
			},
		},
		{
			Def: llm.Tool{
				Name: NameScheduleTask,
				Description: "设一个定时任务：到时间后你会去执行这件事（需要时搜索、读网页、运行代码），然后把结果主动发给对方。" +
					"适合「每天早上 8 点告诉我今天的天气和新闻」「每周一早上帮我看看比特币价格」这类要求。只是提醒一句的用 set_reminder。",
				Parameters: map[string]any{"type": "object", "properties": scheduleProps(map[string]any{
					"content": map[string]any{"type": "string", "description": "到时候要做的事，写清楚具体要求，例如「搜索今天的科技新闻，挑 3 条最重要的简单说说」"},
				}), "required": []string{"content"}},
			},
			Label: "设定时任务",
			Run: func(ctx context.Context, args json.RawMessage) tools.Result {
				return create(ctx, repo, userID, store.KindTask, loc, now(), args)
			},
		},
		{
			Def: llm.Tool{
				Name:        NameListReminders,
				Description: "列出对方现在有效的提醒和定时任务（编号、下次时间、重复方式、内容）。",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
			Label: "查看提醒",
			Run: func(ctx context.Context, _ json.RawMessage) tools.Result {
				list, err := repo.ListPending(ctx, userID)
				if err != nil {
					return tools.Result{Output: "读取失败：" + err.Error(), Summary: "读取失败"}
				}
				if len(list) == 0 {
					return tools.Result{OK: true, Output: "现在没有提醒和定时任务。", Summary: "没有提醒"}
				}
				var b strings.Builder
				for i := range list {
					b.WriteString(Describe(&list[i], loc) + "\n")
				}
				out := strings.TrimSpace(b.String())
				return tools.Result{OK: true, Output: out, Summary: fmt.Sprintf("%d 个", len(list)), Detail: out}
			},
		},
		{
			Def: llm.Tool{
				Name:        NameCancelReminder,
				Description: "取消一个提醒或定时任务。先用 list_reminders 找到编号。",
				Parameters: map[string]any{"type": "object", "properties": map[string]any{
					"id": map[string]any{"type": "integer", "description": "编号"},
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
						res.Output, res.Summary = "没有这个提醒（可能已经结束或取消了）", "没有这个提醒"
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

// Input 是新建或修改提醒、定时任务时填写的内容。
type Input struct {
	Content string `json:"content"`
	Schedule
}

// Build 检查输入并填进提醒（内容与时间安排）。
func (in Input) Build(r *store.Reminder, now time.Time, loc *time.Location) error {
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return errors.New("内容不能为空")
	}
	limit := maxReminderChars
	if r.Kind == store.KindTask {
		limit = maxTaskChars
	}
	if utf8.RuneCountInString(content) > limit {
		return fmt.Errorf("内容太长了，最多 %d 个字", limit)
	}
	r.Content = content
	return in.Schedule.Apply(r, now, loc)
}

// Create 新建提醒或定时任务（检查数量上限）。
func Create(ctx context.Context, repo store.ReminderRepo, userID uint64, kind string, in Input, now time.Time, loc *time.Location) (*store.Reminder, error) {
	r := &store.Reminder{UserID: userID, Kind: kind}
	if err := in.Build(r, now, loc); err != nil {
		return nil, err
	}
	pending, err := repo.ListPending(ctx, userID)
	if err != nil {
		return nil, err
	}
	tasks := 0
	for _, p := range pending {
		if p.Kind == store.KindTask {
			tasks++
		}
	}
	if len(pending) >= maxPendingReminders {
		return nil, fmt.Errorf("提醒太多了（最多 %d 个），先取消一些", maxPendingReminders)
	}
	if kind == store.KindTask && tasks >= maxPendingTasks {
		return nil, fmt.Errorf("定时任务太多了（最多 %d 个），先取消一些", maxPendingTasks)
	}
	if err := repo.Create(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

func create(ctx context.Context, repo store.ReminderRepo, userID uint64, kind string, loc *time.Location, now time.Time, args json.RawMessage) tools.Result {
	var in Input
	_ = json.Unmarshal(args, &in)
	res := tools.Result{Input: strings.TrimSpace(in.Content)}
	r, err := Create(ctx, repo, userID, kind, in, now, loc)
	if err != nil {
		res.Output, res.Summary = err.Error(), err.Error()
		return res
	}
	res.OK = true
	res.Summary = fmt.Sprintf("%s %s", r.DueAt.In(loc).Format("01-02 15:04"), RepeatLabel(r, loc))
	res.Output = fmt.Sprintf("已设好%s #%d：下次 %s（%s），内容：%s。对方可以在网页「定时任务」页里查看、修改或暂停。"+
		"到时间系统会自动发给对方，现在不要把提醒的话说出来，也不要假装时间已经到了。",
		kindLabel(kind), r.ID, r.DueAt.In(loc).Format("2006-01-02 15:04 Mon"), RepeatLabel(r, loc), r.Content)
	return res
}
