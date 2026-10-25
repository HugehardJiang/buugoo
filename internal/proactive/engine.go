package proactive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/HugehardJiang/buugoo/internal/chat"
	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// Speaker 生成并保存主动消息（chat.Service）。
type Speaker interface {
	Proactive(ctx context.Context, u *store.User, reason string, allowSkip bool) (*chat.ProactiveResult, error)
}

// Deliverer 把主动消息发到用户绑定的聊天软件（消息已经保存在对话里，网页上也能看到）。
type Deliverer interface {
	Deliver(ctx context.Context, u *store.User, res *chat.ProactiveResult)
}

// Mood 返回布谷对用户此刻的心情说明（情绪系统）。
type Mood interface {
	Peek(ctx context.Context, userID uint64) (string, error)
}

// Engine 定时检查到期的提醒，并在合适的时候主动关心用户。
type Engine struct {
	store   store.Store
	speaker Speaker
	deliver Deliverer
	util    llm.ChatProvider
	mood    Mood
	cfg     config.ProactiveConfig
	name    func() string
	loc     *time.Location
	log     *slog.Logger
	now     func() time.Time

	mu       sync.Mutex
	attempts map[uint64]time.Time // 提醒上次尝试发送的时间（失败后稍后重试）
	checked  map[uint64]time.Time // 用户上次判断要不要主动关心的时间
}

// Deps 是 Engine 的依赖。Deliver 与 Mood 可以为空。
type Deps struct {
	Store   store.Store
	Speaker Speaker
	Deliver Deliverer
	Utility llm.ChatProvider
	Mood    Mood
	Config  config.ProactiveConfig
	// Name 返回布谷的名字（人格档案可能随时被修改）。
	Name     func() string
	Location *time.Location
	Logger   *slog.Logger
}

// New 创建 Engine。
func New(d Deps) *Engine {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Location == nil {
		d.Location = time.Local
	}
	return &Engine{store: d.Store, speaker: d.Speaker, deliver: d.Deliver, util: d.Utility, mood: d.Mood, cfg: d.Config,
		name: d.Name, loc: d.Location, log: d.Logger.With("component", "proactive"), now: time.Now,
		attempts: map[uint64]time.Time{}, checked: map[uint64]time.Time{}}
}

// 节奏与重试。
const (
	reminderTick       = 30 * time.Second
	reminderRetryAfter = 2 * time.Minute
	reminderGiveUp     = 30 * time.Minute // 过期太久还没发出去就放弃这一次
	careRecheck        = 6 * time.Hour    // 同一个人多久判断一次要不要主动关心
	unansweredWait     = 72 * time.Hour   // 主动发的消息没人回时，多久后才再次主动
)

// Run 在后台运行，直到 ctx 取消。
func (e *Engine) Run(ctx context.Context) {
	remind := time.NewTicker(reminderTick)
	defer remind.Stop()
	interval := e.cfg.CheckInterval
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	care := time.NewTicker(interval)
	defer care.Stop()
	e.FireReminders(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-remind.C:
			e.FireReminders(ctx)
		case <-care.C:
			if e.cfg.Care {
				e.Care(ctx)
			}
		}
	}
}

// FireReminders 发出所有到期的提醒。
func (e *Engine) FireReminders(ctx context.Context) {
	now := e.now()
	due, err := e.store.Reminders().Due(ctx, now, 50)
	if err != nil {
		e.log.Warn("读取提醒失败", "err", err)
		return
	}
	for _, r := range due {
		if ctx.Err() != nil {
			return
		}
		e.mu.Lock()
		last, tried := e.attempts[r.ID]
		e.mu.Unlock()
		if tried && now.Sub(last) < reminderRetryAfter {
			continue
		}
		err := e.fire(ctx, r)
		e.mu.Lock()
		if err == nil || now.Sub(r.DueAt) > reminderGiveUp {
			delete(e.attempts, r.ID)
		} else {
			e.attempts[r.ID] = now
		}
		e.mu.Unlock()
		if err != nil && now.Sub(r.DueAt) <= reminderGiveUp {
			e.log.Warn("发送提醒失败，稍后重试", "reminder_id", r.ID, "err", err)
			continue
		}
		if err != nil {
			e.log.Warn("提醒过期太久，放弃这一次", "reminder_id", r.ID, "err", err)
		}
		if ferr := e.store.Reminders().Fired(ctx, r.ID, now, NextDue(r.DueAt, r.Repeat, now, e.loc)); ferr != nil {
			e.log.Error("更新提醒失败", "reminder_id", r.ID, "err", ferr)
		}
	}
}

func (e *Engine) fire(ctx context.Context, r store.Reminder) error {
	u, err := e.store.Users().GetByID(ctx, r.UserID)
	if err != nil {
		return err
	}
	late := ""
	if d := e.now().Sub(r.DueAt); d > 5*time.Minute {
		late = fmt.Sprintf("（服务器刚才忙，晚了 %d 分钟）", int(d.Minutes()))
	}
	reason := fmt.Sprintf("对方之前让你在 %s 提醒：「%s」。现在到时间了%s，提醒对方一下。",
		r.DueAt.In(e.loc).Format("01-02 15:04"), r.Content, late)
	res, err := e.speaker.Proactive(ctx, u, reason, false)
	if err != nil {
		return err
	}
	if e.deliver != nil {
		e.deliver.Deliver(ctx, u, res)
	}
	e.log.Info("已发送提醒", "user_id", u.ID, "reminder_id", r.ID)
	return nil
}

// inQuiet 报告现在是否在免打扰时段（主动关心不在这时发，提醒不受影响）。
func (e *Engine) inQuiet(t time.Time) bool {
	start, err1 := time.Parse("15:04", e.cfg.QuietStart)
	end, err2 := time.Parse("15:04", e.cfg.QuietEnd)
	if err1 != nil || err2 != nil || e.cfg.QuietStart == e.cfg.QuietEnd {
		return false
	}
	t = t.In(e.loc)
	m := t.Hour()*60 + t.Minute()
	s, en := start.Hour()*60+start.Minute(), end.Hour()*60+end.Minute()
	if s < en {
		return m >= s && m < en
	}
	return m >= s || m < en
}

// Care 检查每个允许主动消息的用户，在合适的时候主动找对方聊天。
func (e *Engine) Care(ctx context.Context) {
	now := e.now()
	if e.inQuiet(now) || e.util == nil {
		return
	}
	for offset := 0; ; offset += 200 {
		users, err := e.store.Users().List(ctx, 200, offset)
		if err != nil {
			e.log.Warn("读取用户失败", "err", err)
			return
		}
		for i := range users {
			if ctx.Err() != nil {
				return
			}
			e.careFor(ctx, &users[i], now)
		}
		if len(users) < 200 {
			return
		}
	}
}

func (e *Engine) careFor(ctx context.Context, u *store.User, now time.Time) {
	if !u.Proactive {
		return
	}
	last, err := e.store.Messages().LastUserMessage(ctx, u.ID)
	if err != nil {
		return // 从没聊过
	}
	idle := now.Sub(last.CreatedAt)
	if idle < time.Duration(e.cfg.IdleHours*float64(time.Hour)) {
		return
	}
	if p := u.LastProactiveAt; p != nil {
		if now.Sub(*p) < time.Duration(e.cfg.MinGapHours*float64(time.Hour)) {
			return
		}
		// 上次主动发的还没回：别一直追着发。
		if p.After(last.CreatedAt) && now.Sub(*p) < unansweredWait {
			return
		}
	}
	e.mu.Lock()
	if t, ok := e.checked[u.ID]; ok && now.Sub(t) < careRecheck {
		e.mu.Unlock()
		return
	}
	e.checked[u.ID] = now
	e.mu.Unlock()

	reason, ok, err := e.decide(ctx, u, last, idle, now)
	if err != nil {
		e.log.Warn("判断要不要主动关心失败", "user_id", u.ID, "err", err)
		return
	}
	if !ok {
		return
	}
	res, err := e.speaker.Proactive(ctx, u, "你想主动找对方聊聊："+reason, true)
	if errors.Is(err, chat.ErrNothingToSay) {
		return
	}
	if err != nil {
		e.log.Warn("发送主动消息失败", "user_id", u.ID, "err", err)
		return
	}
	if e.deliver != nil {
		e.deliver.Deliver(ctx, u, res)
	}
	e.log.Info("已主动发消息", "user_id", u.ID, "reason", reason)
}

const decidePrompt = `你是 AI 朋友"%s"的内心。根据下面的信息，判断现在要不要主动给对方发一条消息，就像真正的朋友那样：
- 适合主动的情况：对方之前提到的事现在可能有结果了（面试、考试、看病、出行、约会、项目上线等），想问问；对方之前心情不好，想关心一下；很久没聊了，有点想念
- 不适合的情况：没什么特别想说的；对方之前明确说要忙、想安静；上次聊天刚结束不久；只是为了找话说
- 不要编造对方没说过的事
只输出 JSON：{"reach_out": true 或 false, "reason": "如果要发，一句话说明想聊什么（例如：想问问周三面试结果怎么样）"}`

func (e *Engine) decide(ctx context.Context, u *store.User, last *store.Message, idle time.Duration, now time.Time) (string, bool, error) {
	name := "布谷"
	if e.name != nil {
		name = e.name()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "现在是 %s。对方上次发消息是 %s（%.0f 小时前）。\n", now.In(e.loc).Format("2006-01-02 15:04 Mon"),
		last.CreatedAt.In(e.loc).Format("01-02 15:04"), idle.Hours())
	if u.LastProactiveAt != nil {
		fmt.Fprintf(&b, "你上次主动发消息是 %s。\n", u.LastProactiveAt.In(e.loc).Format("01-02 15:04"))
	}
	if e.mood != nil {
		if mood, err := e.mood.Peek(ctx, u.ID); err == nil && mood != "" {
			b.WriteString("\n【你现在的心情】\n" + mood + "\n")
		}
	}
	if mems, _, err := e.store.Memories().List(ctx, u.ID, store.MemoryFilter{}, 15, 0); err == nil && len(mems) > 0 {
		b.WriteString("\n【你记得的关于对方的事（最近的在前）】\n")
		for _, m := range mems {
			fmt.Fprintf(&b, "- %s（%s）\n", m.Content, m.CreatedAt.In(e.loc).Format("01-02"))
		}
	}
	if msgs, err := e.store.Messages().Recent(ctx, u.ID, last.ConversationID, 12); err == nil && len(msgs) > 0 {
		b.WriteString("\n【最近的聊天】\n")
		for _, m := range msgs {
			who := "对方"
			if m.Role == store.RoleAssistant {
				who = name
			}
			text := []rune(strings.TrimSpace(m.Content))
			if len(text) > 200 {
				text = append(text[:200], '…')
			}
			fmt.Fprintf(&b, "%s（%s）：%s\n", who, m.CreatedAt.In(e.loc).Format("01-02 15:04"), string(text))
		}
	}
	cctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	resp, err := e.util.Chat(cctx, llm.ChatRequest{Thinking: "off", Messages: []llm.Message{
		{Role: llm.RoleSystem, Content: fmt.Sprintf(decidePrompt, name)},
		{Role: llm.RoleUser, Content: b.String()},
	}})
	if err != nil {
		return "", false, err
	}
	var out struct {
		ReachOut bool   `json:"reach_out"`
		Reason   string `json:"reason"`
	}
	raw := strings.TrimSpace(resp.Content)
	if i, j := strings.Index(raw, "{"), strings.LastIndex(raw, "}"); i >= 0 && j > i {
		raw = raw[i : j+1]
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return "", false, fmt.Errorf("解析判断结果: %w", err)
	}
	return strings.TrimSpace(out.Reason), out.ReachOut && strings.TrimSpace(out.Reason) != "", nil
}
