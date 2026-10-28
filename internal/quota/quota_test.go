package quota

import (
	"context"
	"errors"
	"strings"
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

func (m memUsage) AddSearches(_ context.Context, userID uint64, day string, n int) error {
	u := m[day]
	u.Searches += n
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
			out.Searches += u.Searches
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
		if err := l.Record(ctx, 7, Use{Messages: 1, PromptTokens: 10, CompletionTokens: 10}); err != nil {
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

// fakeBilling 是测试用的计费系统：每个 token 1 微元，每次搜索 5000 微元。
type fakeBilling struct {
	mode    string
	ent     Entitlement
	charged []int64
}

func (f *fakeBilling) Entitlement(context.Context, uint64) (Entitlement, error) { return f.ent, nil }
func (f *fakeBilling) Mode() string                                             { return f.mode }
func (f *fakeBilling) Cost(u Use) int64 {
	return int64(u.PromptTokens+u.CompletionTokens) + int64(u.Calls)*5000
}
func (f *fakeBilling) Charge(_ context.Context, _ uint64, _ Use, amount int64) error {
	f.charged = append(f.charged, amount)
	f.ent.Balance -= amount
	return nil
}

type fakeUsers struct{ store.UserRepo }

func (fakeUsers) GetByID(_ context.Context, id uint64) (*store.User, error) {
	return &store.User{ID: id}, nil
}

func TestBilling(t *testing.T) {
	ctx := context.Background()
	user := &store.User{ID: 7}
	day := time.Now().Format("2006-01-02")

	// 先用额度再扣余额：免费额度内不扣钱，用完后按价格扣；余额也没了就停。
	usage := memUsage{}
	b := &fakeBilling{mode: ModeQuotaFirst}
	l := New(config.QuotaConfig{DailyMessages: 1, DailySearches: 1}, usage, time.Local)
	l.SetBilling(b, fakeUsers{}, "去充值")
	if err := l.Check(ctx, user); err != nil {
		t.Fatal(err)
	}
	_ = l.Record(ctx, 7, Use{Key: "model:1", Messages: 1, PromptTokens: 100, CompletionTokens: 20})
	if len(b.charged) != 0 {
		t.Fatalf("额度内不应扣钱: %v", b.charged)
	}
	var ex *ExceededError
	if err := l.Check(ctx, user); !errors.As(err, &ex) || ex.Kind != "messages" || !strings.Contains(err.Error(), "去充值") {
		t.Fatalf("没余额时应超限: %v", err)
	}
	b.ent.Balance = 1000
	if err := l.Check(ctx, user); err != nil {
		t.Fatalf("有余额就可以继续: %v", err)
	}
	_ = l.Record(ctx, 7, Use{Key: "model:1", Messages: 1, PromptTokens: 300, CompletionTokens: 50})
	if len(b.charged) != 1 || b.charged[0] != 350 || b.ent.Balance != 650 {
		t.Fatalf("超出额度应按价格扣: %v %d", b.charged, b.ent.Balance)
	}
	// 向量模型不计入 token 用量，但超额后照样扣钱。
	_ = l.Record(ctx, 7, Use{Key: KeyEmbedding, PromptTokens: 50, Uncounted: true})
	if usage[day].TotalTokens() != 470 || b.ent.Balance != 600 {
		t.Fatalf("向量模型: tokens=%d balance=%d", usage[day].TotalTokens(), b.ent.Balance)
	}

	// 搜索单独算：第一次在免费额度内，第二次扣余额；余额没了只是不能搜索。
	if err := l.CheckSearch(ctx, 7); err != nil {
		t.Fatal(err)
	}
	_ = l.Record(ctx, 7, Use{Key: "search:deepseek", Search: true, Calls: 1})
	if b.ent.Balance != 600 || usage[day].Searches != 1 {
		t.Fatalf("免费搜索不应扣钱: %d", b.ent.Balance)
	}
	b.ent.Balance = 0
	if err := l.CheckSearch(ctx, 7); !errors.As(err, &ex) || ex.Kind != "searches" {
		t.Fatalf("搜索额度用完且没余额: %v", err)
	}

	// 全部按量扣费：没有套餐时没有免费额度，第一条就要有余额。
	b2 := &fakeBilling{mode: ModePay}
	l2 := New(config.QuotaConfig{DailyMessages: 100}, memUsage{}, time.Local)
	l2.SetBilling(b2, fakeUsers{}, "")
	if err := l2.Check(ctx, user); !errors.As(err, &ex) || ex.Kind != "balance" {
		t.Fatalf("按量扣费、没余额: %v", err)
	}
	if st, _ := l2.Status(ctx, user); st.Free || st.MessagesLimit != 0 || !st.Paying {
		t.Fatalf("按量扣费时不应显示免费额度: %+v", st)
	}
	b2.ent.Balance = 100
	_ = l2.Record(ctx, 7, Use{Key: "model:1", Messages: 1, PromptTokens: 30, CompletionTokens: 10})
	if b2.ent.Balance != 60 {
		t.Fatalf("按量扣费应每条都扣: %d", b2.ent.Balance)
	}
	// 有时长套餐时先用套餐额度（这里只限搜索次数，聊天不限）。
	b2.ent = Entitlement{Active: true, QuotaSearches: 1, Balance: 60}
	_ = l2.Record(ctx, 7, Use{Key: "model:1", Messages: 1, PromptTokens: 1000})
	if b2.ent.Balance != 60 {
		t.Fatalf("套餐没限聊天时聊天不应扣钱: %d", b2.ent.Balance)
	}
	// 管理员豁免时什么都不扣。
	if err := (&Limiter{cfg: config.QuotaConfig{ExemptAdmins: true}, usage: memUsage{}, loc: time.Local, now: time.Now, billing: b2}).
		Check(ctx, &store.User{ID: 1, IsAdmin: true}); err != nil {
		t.Fatal(err)
	}
}
