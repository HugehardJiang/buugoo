package quota

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
)

type memUsage map[string]store.UsageDaily

func (m memUsage) Get(_ context.Context, userID uint64, day string) (store.UsageDaily, error) {
	return m[day], nil
}

func (m memUsage) Add(_ context.Context, userID uint64, day string, msgs, p, c int) error {
	u := m[day]
	u.Messages += msgs
	u.PromptTokens += p
	u.CompletionTokens += c
	m[day] = u
	return nil
}

func (m memUsage) Sum(_ context.Context, userID uint64, from, to string) (store.UsageDaily, error) {
	var out store.UsageDaily
	for d, u := range m {
		if d >= from && d <= to {
			out.Messages += u.Messages
			out.PromptTokens += u.PromptTokens
			out.CompletionTokens += u.CompletionTokens
		}
	}
	return out, nil
}

func TestLimiter(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	usage := memUsage{}
	l := New(config.QuotaConfig{DailyMessages: 2, DailyTokens: 100, ExemptAdmins: true}, usage, loc)
	// UTC 16:30 在上海已是次日 00:30。
	l.now = func() time.Time { return time.Date(2026, 10, 24, 16, 30, 0, 0, time.UTC) }
	ctx := context.Background()
	user := &store.User{ID: 7}

	for i := 0; i < 2; i++ {
		if err := l.Check(ctx, user); err != nil {
			t.Fatal(err)
		}
		if err := l.Record(ctx, 7, 1, 10, 10); err != nil {
			t.Fatal(err)
		}
	}
	var ex *ExceededError
	if err := l.Check(ctx, user); !errors.As(err, &ex) || ex.Kind != "messages" {
		t.Fatalf("应超出消息数限额: %v", err)
	}
	if _, ok := usage["2026-10-25"]; !ok {
		t.Fatal("应按配置时区计算日期")
	}
	if ex.Reset.Format("2006-01-02 15:04") != "2026-10-26 00:00" {
		t.Fatalf("reset = %v", ex.Reset)
	}
	if err := l.Check(ctx, &store.User{ID: 7, IsAdmin: true}); err != nil {
		t.Fatal("管理员应豁免")
	}

	l2 := New(config.QuotaConfig{DailyTokens: 30}, memUsage{"2026-10-25": {PromptTokens: 20, CompletionTokens: 10}}, loc)
	l2.now = l.now
	if err := l2.Check(ctx, user); !errors.As(err, &ex) || ex.Kind != "tokens" {
		t.Fatalf("应超出 token 限额: %v", err)
	}
	unlimited := New(config.QuotaConfig{}, memUsage{}, loc)
	if err := unlimited.Check(ctx, user); err != nil {
		t.Fatal(err)
	}
}
