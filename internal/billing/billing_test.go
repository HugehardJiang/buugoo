package billing

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/secret"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func setup(t *testing.T) (*Service, *gormstore.DB) {
	t.Helper()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "b.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	box, _ := secret.New(make([]byte, 32))
	return New(db.Gorm(), box), db
}

func newUser(t *testing.T, db *gormstore.DB, name string) *store.User {
	t.Helper()
	u := &store.User{Username: name}
	if err := db.Users().Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestPlanValidation(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	bad := []PlanInput{
		{Name: "", Kind: store.PlanPeriod, Unit: store.UnitMessages, Days: 30, Cycle: store.CycleDay, Quota: 100},
		{Name: "月卡", Kind: store.PlanPeriod, Unit: store.UnitMessages, Days: 0, Cycle: store.CycleDay, Quota: 100},
		{Name: "月卡", Kind: store.PlanPeriod, Unit: "yuan", Days: 30, Cycle: store.CycleDay, Quota: 100},
		{Name: "包", Kind: store.PlanPack, Unit: store.UnitTokens, Quota: 0},
		{Name: "包", Kind: store.PlanPack, Unit: store.UnitTokens, Quota: 10, BuyURL: "javascript:alert(1)"},
	}
	for _, in := range bad {
		if _, err := s.CreatePlan(ctx, in); err == nil {
			t.Errorf("%+v should fail", in)
		}
	}
}

func TestRedeemAndQuota(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	u := newUser(t, db, "alice")
	month, err := s.CreatePlan(ctx, PlanInput{Name: "月卡", Kind: store.PlanPeriod, Unit: store.UnitMessages, Days: 30, Cycle: store.CycleDay, Quota: 3, Visible: true})
	if err != nil {
		t.Fatal(err)
	}
	pack, _ := s.CreatePlan(ctx, PlanInput{Name: "加油包", Kind: store.PlanPack, Unit: store.UnitMessages, Quota: 2, ValidDays: 7})

	gen, err := s.Generate(ctx, GenerateInput{PlanID: month.ID, Count: 3, Prefix: "bg", Note: "测试渠道"})
	if err != nil {
		t.Fatal(err)
	}
	if len(gen.Codes) != 3 || !strings.HasPrefix(gen.Codes[0], "BG-") || len(normalize(gen.Codes[0])) != 18 {
		t.Fatalf("codes = %v", gen.Codes)
	}
	// 数据库里没有卡密原文。
	var rows []store.CardKey
	db.Gorm().Find(&rows)
	for _, r := range rows {
		if strings.Contains(r.CodeHash, normalize(gen.Codes[0])[2:]) || strings.Contains(r.Hint, normalize(gen.Codes[0])[2:10]) {
			t.Fatal("卡密原文不应该存进数据库")
		}
	}

	// 兑换：大小写和短横线都可以随便写。
	code := strings.ToLower(strings.ReplaceAll(gen.Codes[0], "-", " "))
	msg, err := s.Redeem(ctx, u.ID, code)
	if err != nil || !strings.Contains(msg, "月卡") {
		t.Fatalf("redeem = %q %v", msg, err)
	}
	if _, err := s.Redeem(ctx, u.ID, gen.Codes[0]); !errors.Is(err, ErrCardInvalid) {
		t.Errorf("用过的卡密 = %v", err)
	}
	if _, err := s.Redeem(ctx, u.ID, "BG-AAAA-BBBB-CCCC-DDDD"); !errors.Is(err, ErrCardInvalid) {
		t.Errorf("不存在的卡密 = %v", err)
	}
	// 同一张卡并发兑换只成功一次。
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Redeem(ctx, u.ID, gen.Codes[1]); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("并发兑换成功了 %d 次", ok)
	}
	// 续费：两次月卡一共 60 天。
	a, _ := s.Account(ctx, u.ID, 10)
	if a.Subscription == nil || a.Subscription.ExpiresAt.Sub(time.Now()) < 59*24*time.Hour || len(a.Logs) != 2 {
		t.Fatalf("account = %+v", a)
	}

	// 接到限额器：会员每天 3 条，用完后用余额包。
	limiter := quota.New(config.QuotaConfig{DailyMessages: 1}, db.Usage(), time.Local)
	limiter.SetPlans(s, db.Users(), "去兑换卡密")
	for i := 0; i < 3; i++ {
		if err := limiter.Check(ctx, u); err != nil {
			t.Fatalf("第 %d 条：%v", i+1, err)
		}
		if err := limiter.Record(ctx, u.ID, 1, 10, 10); err != nil {
			t.Fatal(err)
		}
	}
	var qe *quota.ExceededError
	if err := limiter.Check(ctx, u); !errors.As(err, &qe) || qe.Limit != 3 || !strings.Contains(err.Error(), "去兑换卡密") {
		t.Fatalf("第 4 条应超限：%v", err)
	}
	if _, err := s.Grant(ctx, u.ID, pack.ID, "补偿"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := limiter.Check(ctx, u); err != nil {
			t.Fatalf("余额包第 %d 条：%v", i+1, err)
		}
		_ = limiter.Record(ctx, u.ID, 1, 10, 10)
	}
	if err := limiter.Check(ctx, u); err == nil {
		t.Fatal("余额包用完后应超限")
	}
	st, _ := limiter.Status(ctx, u)
	if !st.Member.Active || st.MessagesLimit != 3 || st.Member.PackMessages != 0 {
		t.Errorf("status = %+v", st)
	}

	// 管理员调整余额、停用账号。
	if err := s.Adjust(ctx, u.ID, store.UnitMessages, 5, "活动"); err != nil {
		t.Fatal(err)
	}
	if err := s.Adjust(ctx, u.ID, store.UnitMessages, -2, ""); err != nil {
		t.Fatal(err)
	}
	if ent, _ := s.Entitlement(ctx, u.ID); ent.PackMessages != 3 {
		t.Errorf("pack = %d", ent.PackMessages)
	}
	now := time.Now()
	u.DisabledAt = &now
	if err := limiter.Check(ctx, u); !errors.Is(err, quota.ErrDisabled) {
		t.Errorf("停用的账号 = %v", err)
	}
}

func TestCards(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	u := newUser(t, db, "bob")
	p, _ := s.CreatePlan(ctx, PlanInput{Name: "流量包", Kind: store.PlanPack, Unit: store.UnitTokens, Quota: 1000})
	if _, err := s.Generate(ctx, GenerateInput{PlanID: p.ID, Count: 2, Length: 8}); err == nil {
		t.Error("太短的卡密应拒绝")
	}
	past := time.Now().Add(-time.Hour)
	if _, err := s.Generate(ctx, GenerateInput{PlanID: p.ID, Count: 2, ExpiresAt: &past}); err == nil {
		t.Error("过去的截止时间应拒绝")
	}
	gen, _ := s.Generate(ctx, GenerateInput{PlanID: p.ID, Count: 5, Note: "闲鱼"})
	list, total, err := s.Cards(ctx, CardFilter{Batch: gen.Batch}, 50, 0)
	if err != nil || total != 5 || list[0].PlanName != "流量包" || !strings.Contains(list[0].Hint, "****") {
		t.Fatalf("%+v %d %v", list, total, err)
	}
	// 停用的卡兑换不了，恢复后可以。
	ids := []uint64{list[0].ID, list[1].ID}
	if n, _ := s.SetCardsDisabled(ctx, ids, true); n != 2 {
		t.Fatalf("disabled %d", n)
	}
	// list 是按 id 倒序的，对应生成顺序的最后两张。
	if _, err := s.Redeem(ctx, u.ID, gen.Codes[4]); !errors.Is(err, ErrCardInvalid) {
		t.Errorf("停用的卡 = %v", err)
	}
	s.SetCardsDisabled(ctx, ids, false)
	if _, err := s.Redeem(ctx, u.ID, gen.Codes[4]); err != nil {
		t.Fatal(err)
	}
	// 有没兑换的卡时不能删套餐；删除卡密时用过的保留。
	if err := s.DeletePlan(ctx, p.ID); err == nil {
		t.Error("还有卡时不应能删套餐")
	}
	all := []uint64{}
	for _, c := range list {
		all = append(all, c.ID)
	}
	if n, _ := s.DeleteCards(ctx, all); n != 4 {
		t.Errorf("deleted %d", n)
	}
	if _, total, _ := s.Cards(ctx, CardFilter{Status: store.CardUsed}, 50, 0); total != 1 {
		t.Errorf("used left %d", total)
	}
	if err := s.DeletePlan(ctx, p.ID); err != nil {
		t.Error(err)
	}
	// 套餐删了，已经兑换的余额还在。
	if ent, _ := s.Entitlement(ctx, u.ID); ent.PackTokens != 1000 {
		t.Errorf("tokens = %d", ent.PackTokens)
	}
}
