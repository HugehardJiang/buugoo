// Package quota 实现每个用户的用量限额和按量扣费。
//
// 额度：没有会员套餐时按配置的每日免费额度（扣费方式为「全部按量扣费」时没有免费额度）；
// 开通了时长套餐时改用套餐的额度（每天或每个自然月，消息、token、联网搜索各自限，0 表示不限）。
// 额度内的用量不花钱；超出后每次调用（聊天、后台的记忆和摘要、向量、联网搜索）按管理员设的价格从余额里扣，
// 余额用完就停。联网搜索的额度单独算：搜索用完只是不能再搜，聊天照常。
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

// 扣费方式。
const (
	ModeQuotaFirst = "quota_first" // 先用免费额度和套餐额度，用完再扣余额
	ModePay        = "pay"         // 没有免费额度：套餐额度之外全部按量扣余额
)

// 计价项。模型按 ModelKey，搜索和读网页按服务商（例如 search:deepseek）。
const (
	KeyUtility   = "utility"   // 辅助模型（记忆提取、摘要、整理记忆、起标题）
	KeyEmbedding = "embedding" // 向量模型
)

// ModelKey 是模型的计价项。
func ModelKey(id uint64) string { return "model:" + strconv.FormatUint(id, 10) }

// SearchKey、FetchKey 是联网搜索、读网页的计价项。
func SearchKey(provider string) string { return "search:" + provider }
func FetchKey(provider string) string  { return "fetch:" + provider }

// Use 是一次调用的用量。
type Use struct {
	// Key 是计价项；为空时只记用量不扣费。
	Key              string
	Messages         int
	PromptTokens     int
	CompletionTokens int
	// Search 为 true 表示这是一次联网搜索（计入搜索次数）；Calls 是按次计价的次数。
	Search bool
	Calls  int
	// Uncounted 为 true 时不计入消息和 token 用量（例如向量模型），只在超出额度后按价格扣钱。
	Uncounted bool
}

// ExceededError 表示超出了限额（并且没有余额可以扣）。
type ExceededError struct {
	Kind   string // messages | tokens | searches | balance
	Limit  int
	Used   int
	Reset  time.Time // 限额重置时间
	Period string    // day | month
	// Hint 是怎么继续用的提示（例如去充值），可以为空。
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

// Yuan 把微元写成元（最多四位小数，例如 12.5、0.0035）。
func Yuan(micro int64) string {
	return strconv.FormatFloat(math.Round(float64(micro)/100)/10000, 'f', -1, 64)
}

func (e *ExceededError) Error() string {
	if e.Kind == "balance" {
		msg := "余额不足"
		if e.Hint != "" {
			msg += "。" + e.Hint
		}
		return msg
	}
	what := map[string]string{"messages": "消息数", "tokens": "token 用量", "searches": "联网搜索次数"}[e.Kind]
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

// Entitlement 是用户现在的会员权益：时长套餐（Active 为 true 时）和余额。
type Entitlement struct {
	Active        bool      `json:"active"`
	PlanName      string    `json:"plan_name,omitempty"`
	Cycle         string    `json:"cycle,omitempty"`
	QuotaMessages int64     `json:"quota_messages,omitempty"`
	QuotaTokens   int64     `json:"quota_tokens,omitempty"`
	QuotaSearches int64     `json:"quota_searches,omitempty"`
	ExpiresAt     time.Time `json:"expires_at,omitzero"`
	// Balance 是余额（微元）。
	Balance int64 `json:"balance"`
}

// Billing 是会员和计费系统（由 billing 包实现）。
type Billing interface {
	Entitlement(ctx context.Context, userID uint64) (Entitlement, error)
	// Mode 是扣费方式（ModeQuotaFirst 或 ModePay）。
	Mode() string
	// Cost 按价格算出这次调用要扣多少钱（微元），没有设价格时为 0。
	Cost(u Use) int64
	// Charge 从余额里扣 amount（微元）并记进当天的消费。
	Charge(ctx context.Context, userID uint64, u Use, amount int64) error
}

// Status 是某用户当前周期的用量与限额。
type Status struct {
	Day           string `json:"day"`
	Messages      int    `json:"messages"`
	Tokens        int    `json:"tokens"`
	Searches      int    `json:"searches"`
	MessagesLimit int    `json:"messages_limit"` // 0 表示不限
	TokensLimit   int    `json:"tokens_limit"`   // 0 表示不限
	SearchesLimit int    `json:"searches_limit"` // 0 表示不限
	Exempt        bool   `json:"exempt"`
	// Free 为 false 表示没有任何免费额度（按量扣费模式下没有套餐的用户）。
	Free bool `json:"free"`
	// Paying 为 true 表示额度用完了，现在每次使用都在扣余额。
	Paying bool `json:"paying"`
	// Period 是限额的周期（day 或 month），ResetAt 是下次重置的时间。
	Period  string      `json:"period"`
	ResetAt time.Time   `json:"reset_at"`
	Mode    string      `json:"mode,omitempty"`
	Member  Entitlement `json:"member"`
}

// Limiter 检查与记录用量。
type Limiter struct {
	cfg   config.QuotaConfig
	usage store.UsageRepo
	loc   *time.Location
	now   func() time.Time
	// billing、users 为空时只按每日免费额度计算（没有会员和计费系统）。
	billing Billing
	users   store.UserRepo
	hint    string
}

// New 创建限额器。
func New(cfg config.QuotaConfig, usage store.UsageRepo, loc *time.Location) *Limiter {
	return &Limiter{cfg: cfg, usage: usage, loc: loc, now: time.Now}
}

// SetBilling 接入会员和计费系统。hint 是额度用完时给用户的提示。
func (l *Limiter) SetBilling(b Billing, users store.UserRepo, hint string) {
	l.billing, l.users, l.hint = b, users, hint
}

func (l *Limiter) exempt(u *store.User) bool { return u.IsAdmin && l.cfg.ExemptAdmins }

// state 是用户当前周期的限额与用量。
type state struct {
	day     string
	period  string
	reset   time.Time
	free    bool // 有没有免费额度（套餐或每日免费额度）
	limM    int64
	limT    int64
	limS    int64
	usedM   int64
	usedT   int64
	usedS   int64
	ent     Entitlement
	hasBill bool
}

// chatCovered 报告聊天（消息和 token）还在额度内。
func (s state) chatCovered() bool {
	return s.free && (s.limM <= 0 || s.usedM < s.limM) && (s.limT <= 0 || s.usedT < s.limT)
}

// searchCovered 报告联网搜索还在额度内。
func (s state) searchCovered() bool { return s.free && (s.limS <= 0 || s.usedS < s.limS) }

func (l *Limiter) state(ctx context.Context, userID uint64) (state, error) {
	now := l.now().In(l.loc)
	y, m, d := now.Date()
	st := state{day: now.Format("2006-01-02"), period: store.CycleDay, reset: time.Date(y, m, d+1, 0, 0, 0, 0, l.loc), free: true,
		limM: int64(l.cfg.DailyMessages), limT: int64(l.cfg.DailyTokens), limS: int64(l.cfg.DailySearches)}
	if l.billing != nil {
		ent, err := l.billing.Entitlement(ctx, userID)
		if err != nil {
			return st, fmt.Errorf("读取会员: %w", err)
		}
		st.ent, st.hasBill = ent, true
		switch {
		case ent.Active:
			st.limM, st.limT, st.limS = ent.QuotaMessages, ent.QuotaTokens, ent.QuotaSearches
			if ent.Cycle == store.CycleMonth {
				st.period, st.reset = store.CycleMonth, time.Date(y, m+1, 1, 0, 0, 0, 0, l.loc)
			}
		case l.billing.Mode() == ModePay:
			// 没有免费额度：限额都不适用（显示为 0），每次使用都扣余额。
			st.free, st.limM, st.limT, st.limS = false, 0, 0, 0
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
	st.usedM, st.usedT, st.usedS = int64(used.Messages), int64(used.TotalTokens()), int64(used.Searches)
	return st, nil
}

// Status 返回用户当前周期的用量。
func (l *Limiter) Status(ctx context.Context, u *store.User) (Status, error) {
	st, err := l.state(ctx, u.ID)
	if err != nil {
		return Status{}, err
	}
	out := Status{
		Day: st.day, Messages: int(st.usedM), Tokens: int(st.usedT), Searches: int(st.usedS),
		MessagesLimit: int(st.limM), TokensLimit: int(st.limT), SearchesLimit: int(st.limS),
		Exempt: l.exempt(u), Free: st.free, Period: st.period, ResetAt: st.reset, Member: st.ent,
	}
	if l.billing != nil {
		out.Mode = l.billing.Mode()
		out.Paying = !out.Exempt && !st.chatCovered()
	}
	return out, nil
}

// exceeded 返回聊天额度用完时的错误。
func (l *Limiter) exceeded(st state) error {
	switch {
	case !st.free:
		return &ExceededError{Kind: "balance", Hint: l.hint}
	case st.limM > 0 && st.usedM >= st.limM:
		return &ExceededError{Kind: "messages", Limit: int(st.limM), Used: int(st.usedM), Reset: st.reset, Period: st.period, Hint: l.hint}
	default:
		return &ExceededError{Kind: "tokens", Limit: int(st.limT), Used: int(st.usedT), Reset: st.reset, Period: st.period, Hint: l.hint}
	}
}

// Check 在发送消息前检查：额度内或者还有余额才可以。超限时返回 *ExceededError；账号被停用时返回 ErrDisabled。
func (l *Limiter) Check(ctx context.Context, u *store.User) error {
	if u.DisabledAt != nil {
		return ErrDisabled
	}
	if l.exempt(u) {
		return nil
	}
	if l.billing == nil && l.cfg.DailyMessages == 0 && l.cfg.DailyTokens == 0 {
		return nil
	}
	st, err := l.state(ctx, u.ID)
	if err != nil {
		return err
	}
	if st.chatCovered() || st.ent.Balance > 0 {
		return nil
	}
	return l.exceeded(st)
}

// CheckSearch 在联网搜索前检查：搜索额度内或者还有余额才可以。用完时只是不能搜索，不影响聊天。
func (l *Limiter) CheckSearch(ctx context.Context, userID uint64) error {
	if l.billing == nil && l.cfg.DailySearches == 0 {
		return nil
	}
	if l.users != nil {
		if u, err := l.users.GetByID(ctx, userID); err == nil && l.exempt(u) {
			return nil
		}
	}
	st, err := l.state(ctx, userID)
	if err != nil {
		return err
	}
	if st.searchCovered() || st.ent.Balance > 0 {
		return nil
	}
	if !st.free {
		return &ExceededError{Kind: "balance", Hint: l.hint}
	}
	return &ExceededError{Kind: "searches", Limit: int(st.limS), Used: int(st.usedS), Reset: st.reset, Period: st.period, Hint: l.hint}
}

// Record 记录一次用量。后台任务（记忆提取等）的用量也计入该用户。
// 记录前已经超出额度的部分，按价格从余额里扣。
func (l *Limiter) Record(ctx context.Context, userID uint64, u Use) error {
	var before state
	charge := false
	if l.billing != nil && l.users != nil && u.Key != "" {
		if usr, err := l.users.GetByID(ctx, userID); err == nil && !l.exempt(usr) {
			if st, err := l.state(ctx, userID); err == nil {
				before, charge = st, true
			}
		}
	}
	day := l.now().In(l.loc).Format("2006-01-02")
	if !u.Uncounted && (u.Messages > 0 || u.PromptTokens > 0 || u.CompletionTokens > 0) {
		if err := l.usage.Add(ctx, userID, day, u.Messages, u.PromptTokens, u.CompletionTokens); err != nil {
			return fmt.Errorf("记录用量: %w", err)
		}
	}
	if u.Search {
		if err := l.usage.AddSearches(ctx, userID, day, 1); err != nil {
			return fmt.Errorf("记录用量: %w", err)
		}
	}
	if !charge {
		return nil
	}
	covered := before.chatCovered()
	if u.Search {
		covered = before.searchCovered()
	}
	if covered {
		return nil
	}
	if cost := l.billing.Cost(u); cost > 0 {
		if err := l.billing.Charge(ctx, userID, u, cost); err != nil {
			return fmt.Errorf("扣除余额: %w", err)
		}
	}
	return nil
}

type userKey struct{}

// WithUser 把用量要记到哪个用户身上放进 ctx（给向量模型这类拿不到用户的调用用）。
func WithUser(ctx context.Context, userID uint64) context.Context {
	return context.WithValue(ctx, userKey{}, userID)
}

// UserFrom 取出 WithUser 放进去的用户，没有时返回 0。
func UserFrom(ctx context.Context) uint64 {
	id, _ := ctx.Value(userKey{}).(uint64)
	return id
}
