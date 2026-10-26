package proactive

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/chat"
	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

var shanghai = time.FixedZone("CST", 8*3600)

func newDB(t *testing.T) *gormstore.DB {
	t.Helper()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "p.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestNextDue(t *testing.T) {
	due := time.Date(2026, 10, 23, 22, 0, 0, 0, shanghai) // 周五
	after := due
	next := func(repeat string, after time.Time) *time.Time {
		return NextDue(&store.Reminder{DueAt: due, Repeat: repeat}, after, shanghai)
	}
	if n := next(store.RepeatDaily, after); !n.Equal(due.AddDate(0, 0, 1)) {
		t.Fatalf("daily %v", n)
	}
	if n := next(store.RepeatWeekdays, after); n.Weekday() != time.Monday || n.Hour() != 22 {
		t.Fatalf("weekdays %v", n)
	}
	if n := next(store.RepeatWeekly, after.Add(10*24*time.Hour)); !n.Equal(due.AddDate(0, 0, 14)) {
		t.Fatalf("weekly 应跳过错过的周期 %v", n)
	}
	if next(store.RepeatNone, after) != nil {
		t.Fatal("不重复")
	}
	// 每隔 90 分钟：错过的周期跳过。
	if n := NextDue(&store.Reminder{DueAt: due, Repeat: store.RepeatInterval, Every: 90}, due.Add(200*time.Minute), shanghai); !n.Equal(due.Add(270 * time.Minute)) {
		t.Fatalf("interval %v", n)
	}
	// 每月 31 号：没有 31 号的月份跳过。
	jan := time.Date(2027, 1, 31, 9, 0, 0, 0, shanghai)
	m := &store.Reminder{DueAt: jan, Repeat: store.RepeatMonthly, Cron: monthlyCron(jan, shanghai)}
	if n := NextDue(m, jan, shanghai); !n.Equal(time.Date(2027, 3, 31, 9, 0, 0, 0, shanghai)) {
		t.Fatalf("monthly %v", n)
	}
	// cron：每周一三五早上 9 点。
	c := &store.Reminder{DueAt: due, Repeat: store.RepeatCron, Cron: "0 9 * * 1,3,5"}
	if n := NextDue(c, due, shanghai); !n.Equal(time.Date(2026, 10, 26, 9, 0, 0, 0, shanghai)) {
		t.Fatalf("cron %v", n)
	}
}

func TestScheduleApply(t *testing.T) {
	now := time.Date(2026, 10, 25, 20, 0, 0, 0, shanghai)
	ok := func(s Schedule) *store.Reminder {
		t.Helper()
		r := &store.Reminder{}
		if err := s.Apply(r, now, shanghai); err != nil {
			t.Fatalf("%+v: %v", s, err)
		}
		return r
	}
	if r := ok(Schedule{Repeat: "cron", Cron: "30 8 * * *"}); !r.DueAt.Equal(time.Date(2026, 10, 26, 8, 30, 0, 0, shanghai)) || r.Cron != "30 8 * * *" {
		t.Fatalf("cron 不填时间从下一次开始 %+v", r)
	}
	if r := ok(Schedule{Repeat: "monthly", Time: "2026-11-15 09:00"}); r.Cron != "0 9 15 * *" {
		t.Fatalf("%+v", r)
	}
	if r := ok(Schedule{Repeat: "interval", EveryMinutes: 120, AfterMinutes: 10}); r.Every != 120 || !r.DueAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("%+v", r)
	}
	for _, bad := range []Schedule{
		{Repeat: "cron", Cron: "*/5 * * * *"}, // 太频繁
		{Repeat: "cron", Cron: "0 9 * *"},     // 少一段
		{Repeat: "cron", Cron: "CRON_TZ=UTC 0 9 * * *"},
		{Repeat: "interval", EveryMinutes: 10, AfterMinutes: 1},
		{Repeat: "yearly", AfterMinutes: 1},
		{Time: "2026-10-25 08:00"}, // 已经过去
	} {
		if err := bad.Apply(&store.Reminder{}, now, shanghai); err == nil {
			t.Errorf("%+v 应失败", bad)
		}
	}
}

func TestReminderTools(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	now := time.Date(2026, 10, 25, 20, 0, 0, 0, shanghai)
	tools := ReminderTools(db.Reminders(), 1, shanghai, func() time.Time { return now })
	call := func(name, args string) (bool, string) {
		for _, x := range tools {
			if x.Def.Name == name {
				r := x.Run(ctx, json.RawMessage(args))
				return r.OK, r.Output
			}
		}
		t.Fatalf("no tool %s", name)
		return false, ""
	}
	if ok, out := call(NameSetReminder, `{"content":"起来活动","after_minutes":30}`); !ok || !strings.Contains(out, "2026-10-25 20:30") {
		t.Fatalf("%v %s", ok, out)
	}
	if ok, out := call(NameSetReminder, `{"content":"吃药","time":"2026-10-25 08:00","repeat":"daily"}`); !ok || !strings.Contains(out, "2026-10-26 08:00") {
		t.Fatalf("已过去的重复提醒应顺延到下一次: %v %s", ok, out)
	}
	for _, bad := range []string{`{"content":"x","time":"2026-10-25 08:00"}`, `{"content":"x","time":"明天"}`, `{"content":"","after_minutes":5}`,
		`{"content":"x"}`, `{"content":"x","after_minutes":5,"repeat":"yearly"}`} {
		if ok, _ := call(NameSetReminder, bad); ok {
			t.Errorf("%s 应失败", bad)
		}
	}
	if ok, out := call(NameScheduleTask, `{"content":"搜一下今天的科技新闻，挑三条说说","time":"2026-10-26 08:00","repeat":"weekdays"}`); !ok || !strings.Contains(out, "定时任务") {
		t.Fatalf("%v %s", ok, out)
	}
	if ok, out := call(NameListReminders, `{}`); !ok || !strings.Contains(out, "起来活动") || !strings.Contains(out, "每天") || !strings.Contains(out, "[定时任务]") {
		t.Fatalf("%s", out)
	}
	list, _ := db.Reminders().ListPending(ctx, 1)
	if ok, _ := call(NameCancelReminder, `{"id":`+itoa(list[0].ID)+`}`); !ok {
		t.Fatal("cancel")
	}
	if ok, _ := call(NameCancelReminder, `{"id":`+itoa(list[0].ID)+`}`); ok {
		t.Fatal("重复取消应失败")
	}
	// 别人的提醒看不到。
	other := ReminderTools(db.Reminders(), 2, shanghai, func() time.Time { return now })
	if r := other[2].Run(ctx, nil); !strings.Contains(r.Output, "没有提醒") {
		t.Fatal(r.Output)
	}
}

func itoa(n uint64) string { b, _ := json.Marshal(n); return string(b) }

type fakeSpeaker struct {
	mu      sync.Mutex
	reasons []string
	tasks   []string
	fail    error
	skip    bool
}

func (f *fakeSpeaker) RunTask(_ context.Context, u *store.User, task string) (*chat.ProactiveResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	f.tasks = append(f.tasks, task)
	return &chat.ProactiveResult{Message: &store.Message{UserID: u.ID, Content: "今天的新闻"}}, nil
}

func (f *fakeSpeaker) Proactive(_ context.Context, u *store.User, reason string, allowSkip bool) (*chat.ProactiveResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	if f.skip && allowSkip {
		return nil, chat.ErrNothingToSay
	}
	f.reasons = append(f.reasons, reason)
	return &chat.ProactiveResult{Message: &store.Message{UserID: u.ID, Content: "嘿"}}, nil
}

type fakeDeliver struct {
	mu sync.Mutex
	n  int
}

func (f *fakeDeliver) Deliver(context.Context, *store.User, *chat.ProactiveResult, bool) {
	f.mu.Lock()
	f.n++
	f.mu.Unlock()
}

func TestFireReminders(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	u, _ := db.Users().EnsureOwner(ctx)
	now := time.Date(2026, 10, 25, 22, 0, 0, 0, shanghai)
	_ = db.Reminders().Create(ctx, &store.Reminder{UserID: u.ID, Content: "吃药", DueAt: now.Add(-time.Minute), Repeat: store.RepeatDaily})
	_ = db.Reminders().Create(ctx, &store.Reminder{UserID: u.ID, Content: "开会", DueAt: now.Add(-time.Minute)})
	_ = db.Reminders().Create(ctx, &store.Reminder{UserID: u.ID, Content: "以后", DueAt: now.Add(time.Hour)})
	sp, dl := &fakeSpeaker{}, &fakeDeliver{}
	e := New(Deps{Store: db, Speaker: sp, Deliver: dl, Location: shanghai})
	e.now = func() time.Time { return now }
	e.FireReminders(ctx)
	if len(sp.reasons) != 2 || dl.n != 2 || !strings.Contains(sp.reasons[0], "吃药") {
		t.Fatalf("reasons=%v deliver=%d", sp.reasons, dl.n)
	}
	pending, _ := db.Reminders().ListPending(ctx, u.ID)
	if len(pending) != 2 || pending[0].Content != "以后" || pending[1].Content != "吃药" || !pending[1].DueAt.Equal(now.Add(-time.Minute).AddDate(0, 0, 1)) {
		t.Fatalf("一次性的结束、每天的顺延到明天: %+v", pending)
	}
	// 发送失败：稍后重试，不会每 30 秒都打扰模型；过期太久就放弃。
	_ = db.Reminders().Create(ctx, &store.Reminder{UserID: u.ID, Content: "失败", DueAt: now})
	sp.fail = errors.New("模型不可用")
	e.FireReminders(ctx)
	e.now = func() time.Time { return now.Add(time.Minute) }
	e.FireReminders(ctx)
	if p, _ := db.Reminders().ListPending(ctx, u.ID); len(p) != 3 {
		t.Fatal("失败的提醒应保留")
	}
	e.now = func() time.Time { return now.Add(40 * time.Minute) }
	e.FireReminders(ctx)
	if p, _ := db.Reminders().ListPending(ctx, u.ID); len(p) != 2 {
		t.Fatal("过期太久应放弃")
	}

	// 定时任务在后台执行；暂停的不执行。
	sp.fail = nil
	e.now = func() time.Time { return now }
	task := &store.Reminder{UserID: u.ID, Kind: store.KindTask, Content: "搜新闻", DueAt: now.Add(-time.Minute), Repeat: store.RepeatDaily}
	paused := &store.Reminder{UserID: u.ID, Kind: store.KindTask, Content: "暂停的", DueAt: now.Add(-time.Minute), Status: store.ReminderPaused}
	_ = db.Reminders().Create(ctx, task)
	_ = db.Reminders().Create(ctx, paused)
	e.FireReminders(ctx)
	e.Wait()
	if len(sp.tasks) != 1 || sp.tasks[0] != "搜新闻" || dl.n != 3 {
		t.Fatalf("tasks=%v deliver=%d", sp.tasks, dl.n)
	}
	if r, _ := db.Reminders().Get(ctx, u.ID, task.ID); !r.DueAt.Equal(task.DueAt.AddDate(0, 0, 1)) {
		t.Fatalf("每天的任务顺延到明天 %v", r.DueAt)
	}
	// 额度用完重试也没用：跳过这一次。
	_ = db.Reminders().Create(ctx, &store.Reminder{UserID: u.ID, Content: "额度", DueAt: now})
	sp.fail = &quota.ExceededError{}
	e.FireReminders(ctx)
	if p, _ := db.Reminders().ListPending(ctx, u.ID); len(p) != 4 {
		t.Fatalf("%d", len(p))
	}
}

type fakeUtil struct {
	reply string
	reqs  int
}

func (f *fakeUtil) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	f.reqs++
	return &llm.ChatResponse{Content: f.reply}, nil
}
func (f *fakeUtil) ChatStream(context.Context, llm.ChatRequest) (llm.ChatStream, error) {
	return nil, errors.New("no")
}

func TestCare(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	u, _ := db.Users().EnsureOwner(ctx)
	conv, _ := db.Conversations().Primary(ctx, u.ID)
	talked := time.Date(2026, 10, 23, 18, 0, 0, 0, shanghai)
	_ = db.Messages().Create(ctx, &store.Message{ConversationID: conv.ID, UserID: u.ID, Role: store.RoleUser, Content: "周三面试", Status: store.MessageComplete, CreatedAt: talked})
	util := &fakeUtil{reply: "```json\n{\"reach_out\": true, \"reason\": \"想问问面试结果\"}\n```"}
	sp := &fakeSpeaker{}
	cfg := config.Default().Proactive
	e := New(Deps{Store: db, Speaker: sp, Utility: util, Config: cfg, Location: shanghai})

	e.now = func() time.Time { return time.Date(2026, 10, 24, 2, 0, 0, 0, shanghai) }
	e.Care(ctx)
	if util.reqs != 0 {
		t.Fatal("免打扰时段不应判断")
	}
	e.now = func() time.Time { return time.Date(2026, 10, 23, 22, 0, 0, 0, shanghai) }
	e.Care(ctx)
	if util.reqs != 0 {
		t.Fatal("刚聊过不应判断")
	}
	now := time.Date(2026, 10, 25, 10, 0, 0, 0, shanghai)
	e.now = func() time.Time { return now }
	e.Care(ctx)
	if util.reqs != 1 || len(sp.reasons) != 1 || !strings.Contains(sp.reasons[0], "面试结果") {
		t.Fatalf("reqs=%d reasons=%v", util.reqs, sp.reasons)
	}
	// 主动发过还没回：不再追着发。
	_ = db.Users().MarkProactive(ctx, u.ID, now)
	e.checked = map[uint64]time.Time{}
	e.now = func() time.Time { return now.Add(30 * time.Hour) }
	e.Care(ctx)
	if util.reqs != 1 {
		t.Fatal("没回复时不应再主动")
	}
	// 用户关掉主动消息。
	_ = db.Users().SetProactive(ctx, u.ID, false)
	e.now = func() time.Time { return now.Add(100 * time.Hour) }
	e.Care(ctx)
	if util.reqs != 1 {
		t.Fatal("关掉后不应主动")
	}
}

func (f *fakeUtil) Model() string { return "fake" }
