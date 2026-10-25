package proactive

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/HugehardJiang/buugoo/internal/store"
)

// 重复的间隔限制：太频繁的提醒和定时任务会刷屏，也很费用量。
const (
	minRepeatGap = 30 * time.Minute
	maxEvery     = 31 * 24 * 60 // 每隔多少分钟，最多一个月
)

var repeatLabels = map[string]string{
	store.RepeatNone: "一次", store.RepeatDaily: "每天", store.RepeatWeekdays: "工作日", store.RepeatWeekly: "每周",
	store.RepeatMonthly: "每月", store.RepeatInterval: "每隔一段时间", store.RepeatCron: "自定义",
}

// RepeatLabel 返回重复方式的中文说明，例如「每天」「每隔 2 小时」「每月 15 号」。
func RepeatLabel(r *store.Reminder, loc *time.Location) string {
	switch r.Repeat {
	case store.RepeatInterval:
		return "每隔 " + everyText(r.Every)
	case store.RepeatMonthly:
		return fmt.Sprintf("每月 %d 号", r.DueAt.In(loc).Day())
	case store.RepeatCron:
		return "按 " + r.Cron
	}
	return repeatLabels[r.Repeat]
}

func everyText(min int) string {
	switch {
	case min%(24*60) == 0:
		return fmt.Sprintf("%d 天", min/(24*60))
	case min%60 == 0:
		return fmt.Sprintf("%d 小时", min/60)
	case min > 60:
		return fmt.Sprintf("%d 小时 %d 分钟", min/60, min%60)
	}
	return fmt.Sprintf("%d 分钟", min)
}

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// parseCron 解析 5 段 cron 表达式（分 时 日 月 周），并检查两次之间不短于 minRepeatGap。
func parseCron(expr string, loc *time.Location, now time.Time) (cron.Schedule, error) {
	expr = strings.Join(strings.Fields(expr), " ")
	if strings.HasPrefix(expr, "TZ=") || strings.HasPrefix(expr, "CRON_TZ=") {
		return nil, errors.New("cron 表达式不能带时区，按服务器所在时区计算")
	}
	sch, err := cronParser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("cron 表达式不对（格式：分 时 日 月 周）：%v", err)
	}
	t := sch.Next(now.In(loc))
	if t.IsZero() {
		return nil, errors.New("这个 cron 表达式不会触发")
	}
	for i := 0; i < 20; i++ {
		n := sch.Next(t)
		if n.IsZero() {
			break
		}
		if n.Sub(t) < minRepeatGap {
			return nil, fmt.Errorf("太频繁了，两次之间至少要隔 %d 分钟", int(minRepeatGap.Minutes()))
		}
		t = n
	}
	return sch, nil
}

// monthlyCron 把「每月」换成 cron 表达式：每月的这一天这个时刻（没有这一天的月份跳过）。
func monthlyCron(due time.Time, loc *time.Location) string {
	d := due.In(loc)
	return fmt.Sprintf("%d %d %d * *", d.Minute(), d.Hour(), d.Day())
}

// NextDue 返回重复提醒的下一次时间（严格晚于 after），不重复时返回 nil。按本地时间计算，避开夏令时等问题。
func NextDue(r *store.Reminder, after time.Time, loc *time.Location) *time.Time {
	switch r.Repeat {
	case store.RepeatNone:
		return nil
	case store.RepeatInterval:
		if r.Every <= 0 {
			return nil
		}
		step := time.Duration(r.Every) * time.Minute
		d := r.DueAt
		if !d.After(after) {
			n := after.Sub(d)/step + 1
			d = d.Add(n * step)
		}
		return &d
	case store.RepeatMonthly, store.RepeatCron:
		expr := r.Cron
		if expr == "" {
			expr = monthlyCron(r.DueAt, loc)
		}
		sch, err := cronParser.Parse(expr)
		if err != nil {
			return nil
		}
		d := sch.Next(maxTime(after, r.DueAt).In(loc))
		if d.IsZero() {
			return nil
		}
		return &d
	}
	d := r.DueAt.In(loc)
	for i := 0; i < 800 && !d.After(after); i++ {
		switch r.Repeat {
		case store.RepeatWeekly:
			d = d.AddDate(0, 0, 7)
		default:
			d = d.AddDate(0, 0, 1)
		}
		if r.Repeat == store.RepeatWeekdays {
			for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				d = d.AddDate(0, 0, 1)
			}
		}
	}
	return &d
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// Schedule 是新建或修改提醒时填写的时间安排。
type Schedule struct {
	// Time 是本地时间（YYYY-MM-DD HH:MM）；AfterMinutes 表示多少分钟后，两者选一个。
	// 按 cron 重复时都可以不填，从下一次触发开始。
	Time         string `json:"time"`
	AfterMinutes int    `json:"after_minutes"`
	// Repeat：none、daily、weekdays、weekly、monthly、interval、cron。
	Repeat       string `json:"repeat"`
	EveryMinutes int    `json:"every_minutes"`
	Cron         string `json:"cron"`
}

// Apply 检查时间安排并填进提醒（DueAt、Repeat、Every、Cron）。
func (s Schedule) Apply(r *store.Reminder, now time.Time, loc *time.Location) error {
	repeat := strings.TrimSpace(s.Repeat)
	if repeat == "none" {
		repeat = store.RepeatNone
	}
	if _, ok := repeatLabels[repeat]; !ok {
		return errors.New("repeat 只能是 none、daily、weekdays、weekly、monthly、interval、cron")
	}
	r.Repeat, r.Every, r.Cron = repeat, 0, ""
	switch repeat {
	case store.RepeatInterval:
		if s.EveryMinutes < int(minRepeatGap.Minutes()) || s.EveryMinutes > maxEvery {
			return fmt.Errorf("every_minutes 要在 %d 分钟到 31 天之间", int(minRepeatGap.Minutes()))
		}
		r.Every = s.EveryMinutes
	case store.RepeatCron:
		sch, err := parseCron(s.Cron, loc, now)
		if err != nil {
			return err
		}
		r.Cron = strings.Join(strings.Fields(s.Cron), " ")
		if strings.TrimSpace(s.Time) == "" && s.AfterMinutes <= 0 {
			r.DueAt = sch.Next(now.In(loc)).UTC()
			return nil
		}
	}
	var due time.Time
	switch {
	case s.AfterMinutes > 0:
		if s.AfterMinutes > 366*24*60 {
			return errors.New("最多只能设一年以内")
		}
		due = now.Add(time.Duration(s.AfterMinutes) * time.Minute)
	case strings.TrimSpace(s.Time) != "":
		t, err := time.ParseInLocation(timeLayout, strings.TrimSpace(s.Time), loc)
		if err != nil {
			return errors.New("时间格式不对，应为 YYYY-MM-DD HH:MM")
		}
		due = t
	default:
		return errors.New("需要填写 time 或 after_minutes")
	}
	r.DueAt = due
	if repeat == store.RepeatMonthly {
		r.Cron = monthlyCron(due, loc)
	}
	if !due.After(now) {
		if repeat == store.RepeatNone {
			return errors.New("这个时间已经过去了")
		}
		if repeat == store.RepeatCron {
			// 按 cron 重复时，时间只是起点。
			sch, _ := cronParser.Parse(r.Cron)
			due = sch.Next(now.In(loc))
		} else if n := NextDue(r, now, loc); n != nil {
			due = *n
		}
	} else if repeat == store.RepeatCron {
		sch, _ := cronParser.Parse(r.Cron)
		due = sch.Next(due.Add(-time.Second).In(loc))
	}
	if due.After(now.AddDate(1, 0, 1)) {
		return errors.New("最多只能设一年以内")
	}
	r.DueAt = due.UTC()
	return nil
}
