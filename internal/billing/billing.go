// Package billing 是会员系统：套餐（时长套餐、余额包）、用户的订阅与余额、卡密、记录。
//
// 安全上的考虑：
//   - 卡密只存 HMAC（密钥由主加密密钥派生），不存原文，数据库泄露也拿不到能用的卡密；
//   - 卡密有约 80 位随机性，兑换接口另外按 IP 和账号限速，暴力猜测不可行；
//   - 兑换用带条件的 UPDATE（只有未使用的才会被标记），同一张卡并发兑换也只会成功一次；
//   - 余额扣减也用带条件的 UPDATE，不会扣成负数。
package billing

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
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

// Service 是会员系统。
type Service struct {
	db  *gorm.DB
	box *secret.Box
	now func() time.Time
}

// New 创建会员系统。box 用来计算卡密的 HMAC。
func New(db *gorm.DB, box *secret.Box) *Service {
	return &Service{db: db, box: box, now: time.Now}
}

var _ quota.Plans = (*Service)(nil)

// ---------- 套餐 ----------

// PlanInput 是新建或修改套餐时填写的内容。
type PlanInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       string `json:"price"`
	Kind        string `json:"kind"`
	Unit        string `json:"unit"`
	Days        int    `json:"days"`
	Cycle       string `json:"cycle"`
	Quota       int64  `json:"quota"`
	ValidDays   int    `json:"valid_days"`
	BuyURL      string `json:"buy_url"`
	Visible     bool   `json:"visible"`
	Sort        int    `json:"sort"`
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
	case in.Unit != store.UnitMessages && in.Unit != store.UnitTokens:
		return userErr("计费单位只能是消息条数或 token")
	case in.Quota <= 0 || in.Quota > 1e12:
		return userErr("额度要大于 0")
	}
	switch in.Kind {
	case store.PlanPeriod:
		if in.Days <= 0 || in.Days > 3660 {
			return userErr("时长要 1 到 3660 天")
		}
		if in.Cycle != store.CycleDay && in.Cycle != store.CycleMonth {
			return userErr("额度周期只能是每天或每月")
		}
		in.ValidDays = 0
	case store.PlanPack:
		if in.ValidDays < 0 || in.ValidDays > 3660 {
			return userErr("有效期要在 0（永久）到 3660 天之间")
		}
		in.Days, in.Cycle = 0, ""
	default:
		return userErr("套餐种类只能是时长套餐或余额包")
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
	p.Name, p.Description, p.Price, p.Kind, p.Unit = in.Name, in.Description, in.Price, in.Kind, in.Unit
	p.Days, p.Cycle, p.Quota, p.ValidDays, p.BuyURL, p.Visible, p.Sort = in.Days, in.Cycle, in.Quota, in.ValidDays, in.BuyURL, in.Visible, in.Sort
}

// Plans 返回套餐。onlyVisible 为 true 时只返回显示给用户的。
func (s *Service) Plans(ctx context.Context, onlyVisible bool) ([]store.Plan, error) {
	q := s.db.WithContext(ctx).Order("sort, id")
	if onlyVisible {
		q = q.Where("visible = ?", true)
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
	res := s.db.WithContext(ctx).Model(&store.Plan{}).Where("id = ?", id).Update("visible", on)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
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

// Entitlement 实现 quota.Plans：当前有效的时长套餐和余额合计。
func (s *Service) Entitlement(ctx context.Context, userID uint64) (quota.Entitlement, error) {
	now := s.now()
	var ent quota.Entitlement
	var sub store.Subscription
	err := s.db.WithContext(ctx).Where("user_id = ? AND expires_at > ?", userID, now).Order("expires_at DESC").First(&sub).Error
	switch {
	case err == nil:
		ent.Active, ent.PlanName, ent.Unit, ent.Cycle, ent.Quota, ent.ExpiresAt = true, sub.PlanName, sub.Unit, sub.Cycle, sub.Quota, sub.ExpiresAt
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return ent, err
	}
	var sums []struct {
		Unit  string
		Total int64
	}
	if err := s.db.WithContext(ctx).Model(&store.CreditPack{}).Select("unit, COALESCE(SUM(remaining), 0) AS total").
		Where("user_id = ? AND remaining > 0 AND (expires_at IS NULL OR expires_at > ?)", userID, now).Group("unit").Scan(&sums).Error; err != nil {
		return ent, err
	}
	for _, x := range sums {
		if x.Unit == store.UnitTokens {
			ent.PackTokens = x.Total
		} else {
			ent.PackMessages = x.Total
		}
	}
	return ent, nil
}

// Deduct 实现 quota.Plans：从最早过期的余额包开始扣。
func (s *Service) Deduct(ctx context.Context, userID uint64, unit string, n int64) error {
	now := s.now()
	for n > 0 {
		var p store.CreditPack
		err := s.db.WithContext(ctx).Where("user_id = ? AND unit = ? AND remaining > 0 AND (expires_at IS NULL OR expires_at > ?)", userID, unit, now).
			Order(clause.OrderBy{Expression: clause.Expr{SQL: "CASE WHEN expires_at IS NULL THEN 1 ELSE 0 END, expires_at, id"}}).First(&p).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // 扣完了
		}
		if err != nil {
			return err
		}
		take := min(n, p.Remaining)
		res := s.db.WithContext(ctx).Model(&store.CreditPack{}).Where("id = ? AND remaining >= ?", p.ID, take).
			Update("remaining", gorm.Expr("remaining - ?", take))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 1 {
			n -= take
		} // 被别的请求抢先扣了，重新查一次
	}
	return nil
}

// grant 在事务里给用户开通套餐：时长套餐在当前有效期之后续上（换了套餐就按新套餐的额度），余额包加一笔。
func (s *Service) grant(tx *gorm.DB, userID uint64, p *store.Plan) (string, error) {
	now := s.now()
	if p.Kind == store.PlanPack {
		pack := &store.CreditPack{UserID: userID, PlanID: p.ID, Name: p.Name, Unit: p.Unit, Total: p.Quota, Remaining: p.Quota}
		if p.ValidDays > 0 {
			t := now.AddDate(0, 0, p.ValidDays)
			pack.ExpiresAt = &t
		}
		if err := tx.Create(pack).Error; err != nil {
			return "", err
		}
		return fmt.Sprintf("获得余额包「%s」%s", p.Name, amount(p.Unit, p.Quota)), nil
	}
	var sub store.Subscription
	err := tx.Where("user_id = ? AND expires_at > ?", userID, now).Order("expires_at DESC").First(&sub).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		sub = store.Subscription{UserID: userID, ExpiresAt: now}
	case err != nil:
		return "", err
	}
	sub.PlanID, sub.PlanName, sub.Unit, sub.Cycle, sub.Quota = p.ID, p.Name, p.Unit, p.Cycle, p.Quota
	sub.ExpiresAt = sub.ExpiresAt.AddDate(0, 0, p.Days)
	if err := tx.Save(&sub).Error; err != nil {
		return "", err
	}
	return fmt.Sprintf("开通「%s」%d 天，到期 %s", p.Name, p.Days, sub.ExpiresAt.Format("2006-01-02 15:04")), nil
}

func amount(unit string, n int64) string {
	if unit == store.UnitTokens {
		return fmt.Sprintf(" %d token", n)
	}
	return fmt.Sprintf(" %d 条消息", n)
}

func (s *Service) log(tx *gorm.DB, userID uint64, kind, detail string) error {
	if r := []rune(detail); len(r) > 480 {
		detail = string(r[:480]) + "…"
	}
	return tx.Create(&store.BillingLog{UserID: userID, Kind: kind, Detail: detail}).Error
}

// Grant 由管理员给用户开通套餐。
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

// Adjust 由管理员调整用户的余额：delta 为正时加一笔永久余额，为负时从余额包里扣。
func (s *Service) Adjust(ctx context.Context, userID uint64, unit string, delta int64, note string) error {
	if unit != store.UnitMessages && unit != store.UnitTokens {
		return userErr("单位只能是消息条数或 token")
	}
	if delta == 0 || delta > 1e12 || delta < -1e12 {
		return userErr("调整的数量不对")
	}
	if delta < 0 {
		if err := s.Deduct(ctx, userID, unit, -delta); err != nil {
			return err
		}
		return s.log(s.db.WithContext(ctx), userID, "adjust", "管理员扣除"+amount(unit, -delta)+noteSuffix(note))
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&store.CreditPack{UserID: userID, Name: "管理员调整", Unit: unit, Total: delta, Remaining: delta}).Error; err != nil {
			return err
		}
		return s.log(tx, userID, "adjust", "管理员增加"+amount(unit, delta)+noteSuffix(note))
	})
}

func noteSuffix(note string) string {
	if note = strings.TrimSpace(note); note != "" {
		return "（" + note + "）"
	}
	return ""
}

// Account 是一个用户的会员情况。
type Account struct {
	Subscription *store.Subscription `json:"subscription,omitempty"`
	Packs        []store.CreditPack  `json:"packs"`
	Logs         []store.BillingLog  `json:"logs"`
}

// Account 返回用户的会员情况：有效的时长套餐、还有余额的余额包、最近的记录。
func (s *Service) Account(ctx context.Context, userID uint64, logs int) (*Account, error) {
	now := s.now()
	a := &Account{Packs: []store.CreditPack{}, Logs: []store.BillingLog{}}
	var sub store.Subscription
	err := s.db.WithContext(ctx).Where("user_id = ? AND expires_at > ?", userID, now).Order("expires_at DESC").First(&sub).Error
	switch {
	case err == nil:
		a.Subscription = &sub
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, err
	}
	if err := s.db.WithContext(ctx).Where("user_id = ? AND remaining > 0 AND (expires_at IS NULL OR expires_at > ?)", userID, now).
		Order("id").Find(&a.Packs).Error; err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("id DESC").Limit(logs).Find(&a.Logs).Error; err != nil {
		return nil, err
	}
	return a, nil
}

// DeleteUserData 删除用户的会员数据（删除用户时调用，tx 为外层事务）。
func DeleteUserData(tx *gorm.DB, userID uint64) error {
	for _, m := range []any{&store.Subscription{}, &store.CreditPack{}, &store.BillingLog{}} {
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
