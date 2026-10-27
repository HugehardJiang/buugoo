// Package quota 实现每个用户的用量限额（消息数、token 数），防止 API 费用失控。
//
// 没有会员套餐时按配置的每日免费额度；开通了时长套餐时改用套餐的额度（每天或每个自然月）。
// 额度用完后再用余额包（按单位分别扣），余额包也没有了才拒绝。
package quota

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// ErrDisabled 表示账号被管理员停用了。
var ErrDisabled = errors.New("账号已被停用，有疑问请联系管理员")

// ExceededError 表示超出了限额。
type ExceededError struct {
	Kind   string // messages | tokens
	Limit  int
	Used   int
	Reset  time.Time // 限额重置时间
	Period string    // day | month
	// Hint 是怎么继续用的提示（例如去兑换卡密），可以为空。
	Hint string
}

// MTokens 把 token 数写成以百万为单位的数字（最多三位小数，例如 1.5、0.025）。
func MTokens(n int64) string {
	if n == 0 {
		return "0"
	}
	v := float64(n) / 1e6
	if v > -0.001 && v < 0.001 {
		if v > 0 {
			return "<0.001"
		}
		return ">-0.001"
	}
	return strconv.FormatFloat(math.Round(v*1000)/1000, 'f', -1, 64)
}

func (e *ExceededError) Error() string {
	what := "消息数"
	if e.Kind == "tokens" {
		what = "token 用量"
	}
	when := "今日"
	if e.Period == store.CycleMonth {
		when = "本月"
	}
	used, limit := fmt.Sprint(e.Used), fmt.Sprint(e.Limit)
	if e.Kind == "tokens" {
		used, limit = MTokens(int64(e.Used)), MTokens(int64(e.Limit))+" 百万"
	}
	msg := fmt.Sprintf("%s%s已达上限（%s/%s），将于 %s 重置", when, what, used, limit, e.Reset.Format("01-02 15:04"))
	if e.Hint != "" {
		msg += "。" + e.Hint
	}
	return msg
}

// Entitlement 是用户现在的会员权益：时长套餐（Active 为 true 时）和各单位的余额包合计。
type Entitlement struct {
	Active       bool      `json:"active"`
	PlanName     string    `json:"plan_name,omitempty"`
	Unit         string    `json:"unit,omitempty"`
	Cycle        string    `json:"cycle,omitempty"`
	Quota        int64     `json:"quota,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitzero"`
	PackMessages int64     `json:"pack_messages"`
	PackTokens   int64     `json:"pack_tokens"`
}

// Plans 提供会员权益（由会员系统实现）。
type Plans interface {
	Entitlement(ctx context.Context, userID uint64) (Entitlement, error)
	// Deduct 从余额包里扣掉 n（按单位），余额不够时扣到 0 为止。
	Deduct(ctx context.Context, userID uint64, unit string, n int64) error
}

// Status 是某用户当前周期的用量与限额。
type Status struct {
	Day           string `json:"day"`
	Messages      int    `json:"messages"`
	Tokens        int    `json:"tokens"`
	MessagesLimit int    `json:"messages_limit"` // 0 表示不限
	TokensLimit   int    `json:"tokens_limit"`   // 0 表示不限
	Exempt        bool   `json:"exempt"`
	// Period 是限额的周期（day 或 month），ResetAt 是下次重置的时间。
	Period  string      `json:"period"`
	ResetAt time.Time   `json:"reset_at"`
	Member  Entitlement `json:"member"`
}

// Limiter 检查与记录用量。
type Limiter struct {
	cfg   config.QuotaConfig
	usage store.UsageRepo
	loc   *time.Location
	now   func() time.Time
	// plans、users 为空时只按每日免费额度计算（没有会员系统）。
	plans Plans
	users store.UserRepo
	hint  string
}

// New 创建限额器。
func New(cfg config.QuotaConfig, usage store.UsageRepo, loc *time.Location) *Limiter {
	return &Limiter{cfg: cfg, usage: usage, loc: loc, now: time.Now}
}

// SetPlans 接入会员系统。hint 是额度用完时给用户的提示。
func (l *Limiter) SetPlans(p Plans, users store.UserRepo, hint string) {
	l.plans, l.users, l.hint = p, users, hint
}

func (l *Limiter) exempt(u *store.User) bool { return u.IsAdmin && l.cfg.ExemptAdmins }

// state 是用户当前周期的限额与用量。
type state struct {
	day            string
	period         string
	reset          time.Time
	mLimit, tLimit int64
	usedM, usedT   int64
	ent            Entitlement
}

func (l *Limiter) state(ctx context.Context, userID uint64) (state, error) {
	now := l.now().In(l.loc)
	y, m, d := now.Date()
	st := state{day: now.Format("2006-01-02"), period: store.CycleDay, reset: time.Date(y, m, d+1, 0, 0, 0, 0, l.loc),
		mLimit: int64(l.cfg.DailyMessages), tLimit: int64(l.cfg.DailyTokens)}
	if l.plans != nil {
		ent, err := l.plans.Entitlement(ctx, userID)
		if err != nil {
			return st, fmt.Errorf("读取会员: %w", err)
		}
		st.ent = ent
		if ent.Active {
			// 会员按套餐的额度，套餐只限一种单位，另一种不限。
			st.mLimit, st.tLimit = 0, 0
			if ent.Unit == store.UnitTokens {
				st.tLimit = ent.Quota
			} else {
				st.mLimit = ent.Quota
			}
			if ent.Cycle == store.CycleMonth {
				st.period, st.reset = store.CycleMonth, time.Date(y, m+1, 1, 0, 0, 0, 0, l.loc)
			}
		}
	}
	var used store.UsageDaily
	var err error
	if st.period == store.CycleMonth {
		used, err = l.usage.Sum(ctx, userID, time.Date(y, m, 1, 0, 0, 0, 0, l.loc).Format("2006-01-02"), st.day)
	} else {
		used, err = l.usage.Get(ctx, userID, st.day)
	}
	if err != nil {
		return st, fmt.Errorf("读取用量: %w", err)
	}
	st.usedM, st.usedT = int64(used.Messages), int64(used.TotalTokens())
	return st, nil
}

// Status 返回用户当前周期的用量。
func (l *Limiter) Status(ctx context.Context, u *store.User) (Status, error) {
	st, err := l.state(ctx, u.ID)
	if err != nil {
		return Status{}, err
	}
	return Status{
		Day: st.day, Messages: int(st.usedM), Tokens: int(st.usedT),
		MessagesLimit: int(st.mLimit), TokensLimit: int(st.tLimit),
		Exempt: l.exempt(u), Period: st.period, ResetAt: st.reset, Member: st.ent,
	}, nil
}

// Check 在发送消息前检查是否超限，超限时返回 *ExceededError；账号被停用时返回 ErrDisabled。
func (l *Limiter) Check(ctx context.Context, u *store.User) error {
	if u.DisabledAt != nil {
		return ErrDisabled
	}
	if l.exempt(u) {
		return nil
	}
	if l.plans == nil && l.cfg.DailyMessages == 0 && l.cfg.DailyTokens == 0 {
		return nil
	}
	st, err := l.state(ctx, u.ID)
	if err != nil {
		return err
	}
	if st.mLimit > 0 && st.usedM >= st.mLimit && st.ent.PackMessages <= 0 {
		return &ExceededError{Kind: "messages", Limit: int(st.mLimit), Used: int(st.usedM), Reset: st.reset, Period: st.period, Hint: l.hint}
	}
	if st.tLimit > 0 && st.usedT >= st.tLimit && st.ent.PackTokens <= 0 {
		return &ExceededError{Kind: "tokens", Limit: int(st.tLimit), Used: int(st.usedT), Reset: st.reset, Period: st.period, Hint: l.hint}
	}
	return nil
}

// over 是超出限额的部分（不限额时为 0）。
func over(used, limit int64) int64 {
	if limit <= 0 || used <= limit {
		return 0
	}
	return used - limit
}

// Record 记录一次用量。后台任务（记忆提取等）消耗的 token 也计入该用户。
// 超出周期额度的部分从对应单位的余额包里扣。
func (l *Limiter) Record(ctx context.Context, userID uint64, messages, promptTokens, completionTokens int) error {
	var before state
	charge := false
	if l.plans != nil && l.users != nil {
		if u, err := l.users.GetByID(ctx, userID); err == nil && !l.exempt(u) {
			if st, err := l.state(ctx, userID); err == nil {
				before, charge = st, true
			}
		}
	}
	day := l.now().In(l.loc).Format("2006-01-02")
	if err := l.usage.Add(ctx, userID, day, messages, promptTokens, completionTokens); err != nil {
		return fmt.Errorf("记录用量: %w", err)
	}
	if !charge {
		return nil
	}
	tokens := int64(promptTokens + completionTokens)
	var errs []error
	if n := over(before.usedM+int64(messages), before.mLimit) - over(before.usedM, before.mLimit); n > 0 && before.ent.PackMessages > 0 {
		errs = append(errs, l.plans.Deduct(ctx, userID, store.UnitMessages, n))
	}
	if n := over(before.usedT+tokens, before.tLimit) - over(before.usedT, before.tLimit); n > 0 && before.ent.PackTokens > 0 {
		errs = append(errs, l.plans.Deduct(ctx, userID, store.UnitTokens, n))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("扣除余额: %w", err)
	}
	return nil
}
