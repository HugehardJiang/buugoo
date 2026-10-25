// Package quota 实现每个用户的每日用量限额（消息数、token 数），防止 API 费用失控。
package quota

import (
	"context"
	"fmt"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// ExceededError 表示超出了每日限额。
type ExceededError struct {
	Kind  string // messages | tokens
	Limit int
	Used  int
	Reset time.Time // 限额重置时间（下一个自然日 0 点）
}

func (e *ExceededError) Error() string {
	what := "消息数"
	if e.Kind == "tokens" {
		what = "token 用量"
	}
	return fmt.Sprintf("今日%s已达上限（%d/%d），将于 %s 重置", what, e.Used, e.Limit, e.Reset.Format("01-02 15:04"))
}

// Status 是某用户当天的用量与限额。
type Status struct {
	Day           string `json:"day"`
	Messages      int    `json:"messages"`
	Tokens        int    `json:"tokens"`
	MessagesLimit int    `json:"messages_limit"` // 0 表示不限
	TokensLimit   int    `json:"tokens_limit"`   // 0 表示不限
	Exempt        bool   `json:"exempt"`
}

// Limiter 检查与记录用量。
type Limiter struct {
	cfg   config.QuotaConfig
	usage store.UsageRepo
	loc   *time.Location
	now   func() time.Time
}

// New 创建限额器。
func New(cfg config.QuotaConfig, usage store.UsageRepo, loc *time.Location) *Limiter {
	return &Limiter{cfg: cfg, usage: usage, loc: loc, now: time.Now}
}

func (l *Limiter) day() (string, time.Time) {
	now := l.now().In(l.loc)
	y, m, d := now.Date()
	reset := time.Date(y, m, d+1, 0, 0, 0, 0, l.loc)
	return now.Format("2006-01-02"), reset
}

func (l *Limiter) exempt(u *store.User) bool { return u.IsAdmin && l.cfg.ExemptAdmins }

// Status 返回用户当天的用量。
func (l *Limiter) Status(ctx context.Context, u *store.User) (Status, error) {
	day, _ := l.day()
	used, err := l.usage.Get(ctx, u.ID, day)
	if err != nil {
		return Status{}, fmt.Errorf("读取用量: %w", err)
	}
	return Status{
		Day: day, Messages: used.Messages, Tokens: used.TotalTokens(),
		MessagesLimit: l.cfg.DailyMessages, TokensLimit: l.cfg.DailyTokens,
		Exempt: l.exempt(u),
	}, nil
}

// Check 在发送消息前检查是否超限，超限时返回 *ExceededError。
func (l *Limiter) Check(ctx context.Context, u *store.User) error {
	if l.exempt(u) || (l.cfg.DailyMessages == 0 && l.cfg.DailyTokens == 0) {
		return nil
	}
	day, reset := l.day()
	used, err := l.usage.Get(ctx, u.ID, day)
	if err != nil {
		return fmt.Errorf("读取用量: %w", err)
	}
	if l.cfg.DailyMessages > 0 && used.Messages >= l.cfg.DailyMessages {
		return &ExceededError{Kind: "messages", Limit: l.cfg.DailyMessages, Used: used.Messages, Reset: reset}
	}
	if l.cfg.DailyTokens > 0 && used.TotalTokens() >= l.cfg.DailyTokens {
		return &ExceededError{Kind: "tokens", Limit: l.cfg.DailyTokens, Used: used.TotalTokens(), Reset: reset}
	}
	return nil
}

// Record 记录一次用量。后台任务（记忆提取等）消耗的 token 也计入该用户。
func (l *Limiter) Record(ctx context.Context, userID uint64, messages, promptTokens, completionTokens int) error {
	day, _ := l.day()
	if err := l.usage.Add(ctx, userID, day, messages, promptTokens, completionTokens); err != nil {
		return fmt.Errorf("记录用量: %w", err)
	}
	return nil
}
