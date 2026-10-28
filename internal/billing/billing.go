// Package billing 是会员和计费系统：套餐（时长套餐、充值）、用户的订阅与余额、价格、消费、卡密、记录。
//
// 计费：管理员给每个模型（以及辅助模型、向量模型、联网搜索）设价格，用户用完免费额度或套餐额度后，
// 每次调用按实际的 token 数或调用次数从余额里扣（金额以微元为单位存整数，不会有浮点误差累积）。
//
// 安全上的考虑：
//   - 卡密只存 HMAC（密钥由主加密密钥派生），不存原文，数据库泄露也拿不到能用的卡密；
//   - 卡密有约 80 位随机性，兑换接口另外按 IP 和账号限速，暴力猜测不可行；
//   - 兑换用带条件的 UPDATE（只有未使用的才会被标记），同一张卡并发兑换也只会成功一次；
//   - 余额的加减都是数据库里的原子更新。
package billing

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/secret"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// 错误。
var (
	ErrNotFound    = errors.New("没有这个套餐")
	ErrCardInvalid = errors.New("卡密无效、已被使用或已过期")
)

// UserError 是填写的内容不对。
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func userErr(format string, a ...any) error { return &UserError{fmt.Sprintf(format, a...)} }

// Yuan 是 1 元对应的微元数。
const Yuan = 1_000_000

// Service 是会员和计费系统。
type Service struct {
	db  *gorm.DB
	box *secret.Box
	loc *time.Location
	now func() time.Time

	mu      sync.RWMutex
	pricing *Pricing // 缓存，修改时更新
}

// New 创建会员和计费系统。box 用来计算卡密的 HMAC，loc 是按天统计消费用的时区。
func New(db *gorm.DB, box *secret.Box, loc *time.Location) *Service {
	if loc == nil {
		loc = time.Local
	}
	return &Service{db: db, box: box, loc: loc, now: time.Now}
}

var _ quota.Billing = (*Service)(nil)

// ---------- 价格与扣费方式 ----------

// Price 是一个计价项的价格：模型按 token（元 / 百万 token，输入和输出分开），搜索和读网页按次（元 / 千次）。
type Price struct {
	Input  float64 `json:"input,omitempty"`
	Output float64 `json:"output,omitempty"`
	PerK   float64 `json:"per_k,omitempty"`
}

// Pricing 是全部计费设置。
type Pricing struct {
	// Mode 是扣费方式：quota_first（先用免费额度和套餐额度，用完扣余额）或 pay（没有免费额度，套餐之外全部扣余额）。
	Mode string `json:"mode"`
	// Prices 的键是计价项（见 quota.ModelKey、quota.KeyUtility 等）。
	Prices map[string]Price `json:"prices"`
}

const pricingKey = "pricing"

// Pricing 返回当前的计费设置。
func (s *Service) Pricing(ctx context.Context) Pricing {
	s.mu.RLock()
	p := s.pricing
	s.mu.RUnlock()
	if p != nil {
		return *p
	}
	out := Pricing{Mode: quota.ModeQuotaFirst, Prices: map[string]Price{}}
	var row store.BillingSetting
	if err := s.db.WithContext(ctx).Where("`key` = ?", pricingKey).First(&row).Error; err == nil {
		_ = json.Unmarshal([]byte(row.Value), &out)
	}
	if out.Prices == nil {
		out.Prices = map[string]Price{}
	}
	if out.Mode != quota.ModePay {
		out.Mode = quota.ModeQuotaFirst
	}
	s.mu.Lock()
	s.pricing = &out
	s.mu.Unlock()
	return out
}

// SetPricing 保存计费设置，立即生效。
func (s *Service) SetPricing(ctx context.Context, p Pricing) error {
	if p.Mode != quota.ModeQuotaFirst && p.Mode != quota.ModePay {
		return userErr("扣费方式不对")
	}
	clean := map[string]Price{}
	for k, v := range p.Prices {
		if k == "" || len(k) > 48 {
			return userErr("计价项不对：%s", k)
		}
		for _, x := range []float64{v.Input, v.Output, v.PerK} {
			if x < 0 || x > 100000 || math.IsNaN(x) || math.IsInf(x, 0) {
				return userErr("价格要在 0 到 100000 元之间")
			}
		}
		if v != (Price{}) {
			clean[k] = v
		}
	}
	p.Prices = clean
	b, _ := json.Marshal(p)
	row := store.BillingSetting{Key: pricingKey, Value: string(b), UpdatedAt: s.now()}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
		return err
	}
	s.mu.Lock()
	s.pricing = &p
	s.mu.Unlock()
	return nil
}

// Mode 实现 quota.Billing。
func (s *Service) Mode() string { return s.Pricing(context.Background()).Mode }

// Cost 实现 quota.Billing：按价格算出这次调用的金额（微元）。
// 价格是元 / 百万 token，所以 token 数乘以价格正好是微元；按次的价格是元 / 千次，每次是 1000 倍。
func (s *Service) Cost(u quota.Use) int64 {
	p, ok := s.Pricing(context.Background()).Prices[u.Key]
	if !ok {
		return 0
	}
	v := float64(u.PromptTokens)*p.Input + float64(u.CompletionTokens)*p.Output + float64(u.Calls)*p.PerK*1000
	return int64(math.Ceil(v - 1e-9))
}

// Charge 实现 quota.Billing：从余额里扣钱，记进当天的消费。
func (s *Service) Charge(ctx context.Context, userID uint64, u quota.Use, amount int64) error {
	day := s.now().In(s.loc).Format("2006-01-02")
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := addBalance(tx, userID, -amount); err != nil {
			return err
		}
		row := store.CostDaily{UserID: userID, Day: day, Key: u.Key, Amount: amount, Calls: max(u.Calls, 1),
			Tokens: int64(u.PromptTokens + u.CompletionTokens), UpdatedAt: s.now()}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "day"}, {Name: "key"}},
			DoUpdates: clause.Assignments(map[string]any{
				"amount": gorm.Expr("amount + ?", row.Amount), "calls": gorm.Expr("calls + ?", row.Calls),
				"tokens": gorm.Expr("tokens + ?", row.Tokens), "updated_at": row.UpdatedAt,
			}),
		}).Create(&row).Error
	})
}

// addBalance 原子地改余额（没有钱包时新建）。
func addBalance(tx *gorm.DB, userID uint64, delta int64) error {
	w := store.Wallet{UserID: userID, Balance: delta, UpdatedAt: time.Now()}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{"balance": gorm.Expr("balance + ?", delta), "updated_at": w.UpdatedAt}),
	}).Create(&w).Error
}

// Balance 返回用户的余额（微元）。
func (s *Service) Balance(ctx context.Context, userID uint64) (int64, error) {
	var w store.Wallet
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	return w.Balance, err
}

// ---------- 套餐 ----------

// PlanInput 是新建或修改套餐时填写的内容。
type PlanInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       string `json:"price"`
	Kind        string `json:"kind"`
	Days        int    `json:"days"`
	Cycle       string `json:"cycle"`
	// 时长套餐每周期的额度，0 表示这一项不限。
	QuotaMessages int64 `json:"quota_messages"`
	QuotaTokens   int64 `json:"quota_tokens"`
	QuotaSearches int64 `json:"quota_searches"`
	// Amount 是充值到账的金额（微元）。
	Amount  int64  `json:"amount"`
	BuyURL  string `json:"buy_url"`
	Visible bool   `json:"visible"`
	Sort    int    `json:"sort"`
}

func (in *PlanInput) validate() error {
	in.Name, in.Price, in.BuyURL = strings.TrimSpace(in.Name), strings.TrimSpace(in.Price), strings.TrimSpace(in.BuyURL)
	in.Description = strings.TrimSpace(in.Description)
	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > 30:
		return userErr("套餐名称要 1 到 30 个字")
	case utf8.RuneCountInString(in.Price) > 30:
		return userErr("价格最多 30 个字")
	case utf8.RuneCountInString(in.Description) > 2000:
		return userErr("介绍最多 2000 字")
	}
	switch in.Kind {
	case store.PlanPeriod:
		if in.Days <= 0 || in.Days > 3660 {
			return userErr("时长要 1 到 3660 天")
		}
		if in.Cycle != store.CycleDay && in.Cycle != store.CycleMonth {
			return userErr("额度周期只能是每天或每月")
		}
		for _, q := range []int64{in.QuotaMessages, in.QuotaTokens, in.QuotaSearches} {
			if q < 0 || q > 1e13 {
				return userErr("额度不对")
			}
		}
		in.Amount = 0
	case store.PlanRecharge:
		if in.Amount <= 0 || in.Amount > 1e6*Yuan {
			return userErr("充值金额要大于 0，最多 100 万元")
		}
		in.Days, in.Cycle, in.QuotaMessages, in.QuotaTokens, in.QuotaSearches = 0, "", 0, 0, 0
	case store.PlanPack:
		return userErr("余额包已经停用，请改用「充值」套餐")
	default:
		return userErr("套餐种类只能是时长套餐或充值")
	}
	if in.BuyURL != "" {
		u, err := url.Parse(in.BuyURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return userErr("购买链接要以 http:// 或 https:// 开头")
		}
	}
	return nil
}

func (in *PlanInput) apply(p *store.Plan) {
	p.Name, p.Description, p.Price, p.Kind = in.Name, in.Description, in.Price, in.Kind
	p.Days, p.Cycle, p.QuotaMessages, p.QuotaTokens, p.QuotaSearches = in.Days, in.Cycle, in.QuotaMessages, in.QuotaTokens, in.QuotaSearches
	p.Amount, p.BuyURL, p.Visible, p.Sort = in.Amount, in.BuyURL, in.Visible, in.Sort
	p.Unit, p.Quota, p.ValidDays = "", 0, 0
}

// Plans 返回套餐。onlyVisible 为 true 时只返回显示给用户的（停用的余额包不返回）。
func (s *Service) Plans(ctx context.Context, onlyVisible bool) ([]store.Plan, error) {
	q := s.db.WithContext(ctx).Order("sort, id")
	if onlyVisible {
		q = q.Where("visible = ? AND kind <> ?", true, store.PlanPack)
	}
	var out []store.Plan
	return out, q.Find(&out).Error
}

// Plan 返回一个套餐。
func (s *Service) Plan(ctx context.Context, id uint64) (*store.Plan, error) {
	var p store.Plan
	if err := s.db.WithContext(ctx).First(&p, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

// CreatePlan 新建套餐。
func (s *Service) CreatePlan(ctx context.Context, in PlanInput) (*store.Plan, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	p := &store.Plan{}
	in.apply(p)
	// visible 列有默认值 true，GORM 建记录时会跳过 false（还会把 true 填回结构体），得单独写一次。
	visible := p.Visible
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(p).Error; err != nil {
			return err
		}
		if !visible {
			p.Visible = false
			return tx.Model(p).Update("visible", false).Error
		}
		return nil
	})
	return p, err
}

// UpdatePlan 修改套餐（已经开通的不受影响，只影响之后的兑换和开通）。
func (s *Service) UpdatePlan(ctx context.Context, id uint64, in PlanInput) (*store.Plan, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	p, err := s.Plan(ctx, id)
	if err != nil {
		return nil, err
	}
	in.apply(p)
	return p, s.db.WithContext(ctx).Save(p).Error
}

// SetVisible 显示或隐藏套餐。
func (s *Service) SetVisible(ctx context.Context, id uint64, on bool) error {
	p, err := s.Plan(ctx, id)
	if err != nil {
		return err
	}
	if on && p.Kind == store.PlanPack {
		return userErr("余额包已经停用，不能再显示")
	}
	return s.db.WithContext(ctx).Model(&store.Plan{}).Where("id = ?", id).Update("visible", on).Error
}

// DeletePlan 删除套餐。还有没兑换的卡密时不能删（否则那些卡兑换不了），可以先隐藏。
func (s *Service) DeletePlan(ctx context.Context, id uint64) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&store.CardKey{}).Where("plan_id = ? AND status = ?", id, store.CardUnused).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return userErr("这个套餐还有 %d 张没兑换的卡密，先停用或删除这些卡密，或者把套餐隐藏起来", n)
	}
	res := s.db.WithContext(ctx).Delete(&store.Plan{}, id)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

// ---------- 权益 ----------

// Entitlement 实现 quota.Billing：当前有效的时长套餐和余额。
func (s *Service) Entitlement(ctx context.Context, userID uint64) (quota.Entitlement, error) {
	var ent quota.Entitlement
	var sub store.Subscription
	err := s.db.WithContext(ctx).Where("user_id = ? AND expires_at > ?", userID, s.now()).Order("expires_at DESC").First(&sub).Error
	switch {
	case err == nil:
		ent.Active, ent.PlanName, ent.Cycle, ent.ExpiresAt = true, sub.PlanName, sub.Cycle, sub.ExpiresAt
		ent.QuotaMessages, ent.QuotaTokens, ent.QuotaSearches = sub.QuotaMessages, sub.QuotaTokens, sub.QuotaSearches
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return ent, err
	}
	ent.Balance, err = s.Balance(ctx, userID)
	return ent, err
}

// grant 在事务里给用户开通套餐：时长套餐在当前有效期之后续上（换了套餐就按新套餐的额度），充值加到余额。
func (s *Service) grant(tx *gorm.DB, userID uint64, p *store.Plan) (string, error) {
	now := s.now()
	switch p.Kind {
	case store.PlanRecharge:
		if err := addBalance(tx, userID, p.Amount); err != nil {
			return "", err
		}
		return fmt.Sprintf("充值「%s」¥%s", p.Name, quota.Yuan(p.Amount)), nil
	case store.PlanPeriod:
	default:
		return "", userErr("这个套餐（旧的余额包）已经停用了，请联系管理员")
	}
	var sub store.Subscription
	err := tx.Where("user_id = ? AND expires_at > ?", userID, now).Order("expires_at DESC").First(&sub).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		sub = store.Subscription{UserID: userID, ExpiresAt: now}
	case err != nil:
		return "", err
	}
	sub.PlanID, sub.PlanName, sub.Cycle = p.ID, p.Name, p.Cycle
	sub.QuotaMessages, sub.QuotaTokens, sub.QuotaSearches = p.QuotaMessages, p.QuotaTokens, p.QuotaSearches
	sub.Unit, sub.Quota = "", 0
	sub.ExpiresAt = sub.ExpiresAt.AddDate(0, 0, p.Days)
	if err := tx.Save(&sub).Error; err != nil {
		return "", err
	}
	return fmt.Sprintf("开通「%s」%d 天，到期 %s", p.Name, p.Days, sub.ExpiresAt.Format("2006-01-02 15:04")), nil
}

func (s *Service) log(tx *gorm.DB, userID uint64, kind, detail string) error {
	if r := []rune(detail); len(r) > 480 {
		detail = string(r[:480]) + "…"
	}
	return tx.Create(&store.BillingLog{UserID: userID, Kind: kind, Detail: detail}).Error
}

// Grant 由管理员给用户开通套餐（充值套餐就是加余额）。
func (s *Service) Grant(ctx context.Context, userID, planID uint64, note string) (string, error) {
	p, err := s.Plan(ctx, planID)
	if err != nil {
		return "", err
	}
	var msg string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if msg, err = s.grant(tx, userID, p); err != nil {
			return err
		}
		return s.log(tx, userID, "grant", "管理员"+msg+noteSuffix(note))
	})
	return msg, err
}

// Adjust 由管理员调整用户的余额（delta 为微元，正数加、负数扣，可以扣成负数）。
func (s *Service) Adjust(ctx context.Context, userID uint64, delta int64, note string) error {
	if delta == 0 || delta > 1e6*Yuan || delta < -1e6*Yuan {
		return userErr("调整的金额不对")
	}
	verb := "增加"
	if delta < 0 {
		verb = "扣除"
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := addBalance(tx, userID, delta); err != nil {
			return err
		}
		return s.log(tx, userID, "adjust", fmt.Sprintf("管理员%s余额 ¥%s%s", verb, quota.Yuan(abs(delta)), noteSuffix(note)))
	})
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func noteSuffix(note string) string {
	if note = strings.TrimSpace(note); note != "" {
		return "（" + note + "）"
	}
	return ""
}

// CostItem 是一段时间里在一个计价项上花的钱。
type CostItem struct {
	Key    string `json:"key"`
	Amount int64  `json:"amount"`
	Calls  int    `json:"calls"`
	Tokens int64  `json:"tokens"`
}

// Costs 返回用户在 [fromDay, toDay] 之间按计价项合计的消费（金额从大到小）。
func (s *Service) Costs(ctx context.Context, userID uint64, fromDay, toDay string) ([]CostItem, error) {
	out := []CostItem{}
	err := s.db.WithContext(ctx).Model(&store.CostDaily{}).
		Select("`key`, SUM(amount) AS amount, SUM(calls) AS calls, SUM(tokens) AS tokens").
		Where("user_id = ? AND day >= ? AND day <= ?", userID, fromDay, toDay).Group("`key`").Order("amount DESC").Scan(&out).Error
	return out, err
}

// Account 是一个用户的会员情况。
type Account struct {
	Subscription *store.Subscription `json:"subscription,omitempty"`
	Balance      int64               `json:"balance"`
	// Today、Month 是今天、本月按计价项合计的消费。
	Today []CostItem         `json:"today"`
	Month []CostItem         `json:"month"`
	Logs  []store.BillingLog `json:"logs"`
}

// Account 返回用户的会员情况：有效的时长套餐、余额、今天和本月的消费、最近的记录。
func (s *Service) Account(ctx context.Context, userID uint64, logs int) (*Account, error) {
	now := s.now()
	a := &Account{Logs: []store.BillingLog{}}
	var sub store.Subscription
	err := s.db.WithContext(ctx).Where("user_id = ? AND expires_at > ?", userID, now).Order("expires_at DESC").First(&sub).Error
	switch {
	case err == nil:
		a.Subscription = &sub
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, err
	}
	if a.Balance, err = s.Balance(ctx, userID); err != nil {
		return nil, err
	}
	local := now.In(s.loc)
	day := local.Format("2006-01-02")
	if a.Today, err = s.Costs(ctx, userID, day, day); err != nil {
		return nil, err
	}
	if a.Month, err = s.Costs(ctx, userID, local.Format("2006-01")+"-01", day); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("id DESC").Limit(logs).Find(&a.Logs).Error; err != nil {
		return nil, err
	}
	return a, nil
}

// DeleteUserData 删除用户的会员数据（删除用户时调用，tx 为外层事务）。
func DeleteUserData(tx *gorm.DB, userID uint64) error {
	for _, m := range []any{&store.Subscription{}, &store.CreditPack{}, &store.BillingLog{}, &store.Wallet{}, &store.CostDaily{}} {
		if err := tx.Where("user_id = ?", userID).Delete(m).Error; err != nil {
			return err
		}
	}
	// 用过的卡密保留（对账用），只去掉和这个用户的关联。
	return tx.Model(&store.CardKey{}).Where("used_by = ?", userID).Update("used_by", nil).Error
}

// ---------- 卡密 ----------

// 卡密字符：去掉容易看错的 0 O 1 I L。
const cardAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// GenerateInput 是批量生成卡密的参数。
type GenerateInput struct {
	PlanID    uint64     `json:"plan_id"`
	Count     int        `json:"count"`
	Prefix    string     `json:"prefix"`
	Length    int        `json:"length"`
	ExpiresAt *time.Time `json:"expires_at"`
	Note      string     `json:"note"`
}

// Generated 是一批新生成的卡密：原文只在这里出现一次。
type Generated struct {
	Batch string   `json:"batch"`
	Plan  string   `json:"plan"`
	Codes []string `json:"codes"`
}

func normalize(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func randomString(n int) (string, error) {
	max := big.NewInt(int64(len(cardAlphabet)))
	b := make([]byte, n)
	for i := range b {
		k, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = cardAlphabet[k.Int64()]
	}
	return string(b), nil
}

// format 把卡密按 4 个一组用短横线分开，前缀单独一段。
func format(prefix, body string) string {
	var parts []string
	if prefix != "" {
		parts = append(parts, prefix)
	}
	for i := 0; i < len(body); i += 4 {
		parts = append(parts, body[i:min(i+4, len(body))])
	}
	return strings.Join(parts, "-")
}

func hint(prefix, body string) string {
	masked := strings.Repeat("*", len(body)-4) + body[len(body)-4:]
	return format(prefix, masked)
}

// Generate 批量生成卡密。返回的原文只有这一次能看到，数据库里只存 HMAC。
func (s *Service) Generate(ctx context.Context, in GenerateInput) (*Generated, error) {
	p, err := s.Plan(ctx, in.PlanID)
	if err != nil {
		return nil, err
	}
	if p.Kind == store.PlanPack {
		return nil, userErr("余额包已经停用，请改用「充值」套餐")
	}
	in.Prefix = normalize(in.Prefix)
	in.Note = strings.TrimSpace(in.Note)
	if in.Length == 0 {
		in.Length = 16
	}
	switch {
	case in.Count < 1 || in.Count > 1000:
		return nil, userErr("一次生成 1 到 1000 张")
	case len(in.Prefix) > 8:
		return nil, userErr("前缀最多 8 个字母或数字")
	case in.Length < 12 || in.Length > 32:
		return nil, userErr("随机部分的长度要在 12 到 32 之间（太短容易被猜到）")
	case utf8.RuneCountInString(in.Note) > 100:
		return nil, userErr("备注最多 100 字")
	case in.ExpiresAt != nil && !in.ExpiresAt.After(s.now()):
		return nil, userErr("兑换截止时间要在以后")
	}
	batch, err := randomString(8)
	if err != nil {
		return nil, err
	}
	out := &Generated{Batch: batch, Plan: p.Name}
	rows := make([]store.CardKey, 0, in.Count)
	seen := map[string]bool{}
	for len(rows) < in.Count {
		body, err := randomString(in.Length)
		if err != nil {
			return nil, err
		}
		h := s.box.HMAC(in.Prefix + body)
		if seen[h] {
			continue
		}
		seen[h] = true
		rows = append(rows, store.CardKey{CodeHash: h, Hint: hint(in.Prefix, body), PlanID: p.ID, Batch: batch, Note: in.Note,
			Status: store.CardUnused, ExpiresAt: in.ExpiresAt})
		out.Codes = append(out.Codes, format(in.Prefix, body))
	}
	if err := s.db.WithContext(ctx).CreateInBatches(rows, 200).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// Redeem 用卡密开通套餐，返回开通的说明。卡密无效、已用、已停用或过期都返回 ErrCardInvalid（不区分，免得被用来探测）。
func (s *Service) Redeem(ctx context.Context, userID uint64, code string) (string, error) {
	norm := normalize(code)
	if len(norm) < 12 || len(norm) > 40 {
		return "", ErrCardInvalid
	}
	h := s.box.HMAC(norm)
	now := s.now()
	var msg string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先占住卡：只有还没用过、没停用、没过期的才会被改成已使用，并发兑换只会有一个成功。
		res := tx.Model(&store.CardKey{}).
			Where("code_hash = ? AND status = ? AND (expires_at IS NULL OR expires_at > ?)", h, store.CardUnused, now).
			Updates(map[string]any{"status": store.CardUsed, "used_by": userID, "used_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrCardInvalid
		}
		var card store.CardKey
		if err := tx.Where("code_hash = ?", h).First(&card).Error; err != nil {
			return err
		}
		var p store.Plan
		if err := tx.First(&p, card.PlanID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return userErr("这张卡密对应的套餐已经不存在了，请联系管理员")
			}
			return err
		}
		var err error
		if msg, err = s.grant(tx, userID, &p); err != nil {
			return err
		}
		return s.log(tx, userID, "redeem", "兑换卡密 "+card.Hint+"："+msg)
	})
	return msg, err
}

// CardFilter 是查卡密的条件。
type CardFilter struct {
	PlanID uint64
	Status string
	Batch  string
	Query  string // 备注或打码后的卡密
}

// CardView 是列表里的一张卡密（不含原文）。
type CardView struct {
	ID        uint64     `json:"id"`
	Hint      string     `json:"hint"`
	PlanID    uint64     `json:"plan_id"`
	PlanName  string     `json:"plan_name"`
	Batch     string     `json:"batch"`
	Note      string     `json:"note"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at"`
	UsedBy    *uint64    `json:"used_by"`
	UsedName  string     `json:"used_name,omitempty"`
	UsedAt    *time.Time `json:"used_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// Cards 分页查卡密，同时返回总数。
func (s *Service) Cards(ctx context.Context, f CardFilter, limit, offset int) ([]CardView, int64, error) {
	q := s.db.WithContext(ctx).Model(&store.CardKey{})
	if f.PlanID != 0 {
		q = q.Where("plan_id = ?", f.PlanID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Batch != "" {
		q = q.Where("batch = ?", f.Batch)
	}
	if v := strings.TrimSpace(f.Query); v != "" {
		like := "%" + strings.NewReplacer("%", "", "_", "", `\`, "").Replace(v) + "%"
		q = q.Where("note LIKE ? OR hint LIKE ?", like, strings.ToUpper(like))
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var cards []store.CardKey
	if err := q.Order("id DESC").Limit(limit).Offset(offset).Find(&cards).Error; err != nil {
		return nil, 0, err
	}
	plans := map[uint64]string{}
	users := map[uint64]string{}
	var planIDs, userIDs []uint64
	for _, c := range cards {
		planIDs = append(planIDs, c.PlanID)
		if c.UsedBy != nil {
			userIDs = append(userIDs, *c.UsedBy)
		}
	}
	if len(planIDs) > 0 {
		var ps []store.Plan
		s.db.WithContext(ctx).Where("id IN ?", planIDs).Find(&ps)
		for _, p := range ps {
			plans[p.ID] = p.Name
		}
	}
	if len(userIDs) > 0 {
		var us []store.User
		s.db.WithContext(ctx).Select("id", "username").Where("id IN ?", userIDs).Find(&us)
		for _, u := range us {
			users[u.ID] = u.Username
		}
	}
	out := make([]CardView, len(cards))
	for i, c := range cards {
		out[i] = CardView{ID: c.ID, Hint: c.Hint, PlanID: c.PlanID, PlanName: plans[c.PlanID], Batch: c.Batch, Note: c.Note,
			Status: c.Status, ExpiresAt: c.ExpiresAt, UsedBy: c.UsedBy, UsedAt: c.UsedAt, CreatedAt: c.CreatedAt}
		if c.UsedBy != nil {
			out[i].UsedName = users[*c.UsedBy]
		}
	}
	return out, total, nil
}

// SetCardsDisabled 停用或恢复卡密（已经兑换的不受影响）。
func (s *Service) SetCardsDisabled(ctx context.Context, ids []uint64, disabled bool) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	from, to := store.CardUnused, store.CardDisabled
	if !disabled {
		from, to = to, from
	}
	res := s.db.WithContext(ctx).Model(&store.CardKey{}).Where("id IN ? AND status = ?", ids, from).Update("status", to)
	return res.RowsAffected, res.Error
}

// DeleteCards 删除没兑换的卡密（已经兑换的保留，用来对账）。
func (s *Service) DeleteCards(ctx context.Context, ids []uint64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := s.db.WithContext(ctx).Where("id IN ? AND status <> ?", ids, store.CardUsed).Delete(&store.CardKey{})
	return res.RowsAffected, res.Error
}
