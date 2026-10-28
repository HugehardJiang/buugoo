// Package accounts 是管理后台的用户管理：查看、搜索、停用、删除用户。
// 会员相关的操作（开通套餐、调整余额）在 billing 包里，改密码在 auth 包里。
package accounts

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/HugehardJiang/buugoo/internal/billing"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// ErrNotFound 表示没有这个用户。
var ErrNotFound = errors.New("没有这个用户")

// UserError 是操作不被允许。
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

// userTables 是按 user_id 存数据的表，删除用户时一起删。
var userTables = []string{
	"messages", "attachments", "conversations", "memory_usages", "memories", "emotion_events", "emotion_states",
	"reminders", "channel_bindings", "agent_actions", "agent_messages", "memo_pages", "todos", "usage_daily",
}

// Row 是用户列表里的一行。
type Row struct {
	ID          uint64     `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	IsAdmin     bool       `json:"is_admin"`
	DisabledAt  *time.Time `json:"disabled_at,omitempty"`
	Disabled    string     `json:"disabled_reason,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	PlanName    string     `json:"plan_name,omitempty"`
	PlanExpires *time.Time `json:"plan_expires,omitempty"`
	// Balance 是余额，TodayCost 是今天扣的钱（都是微元）。
	Balance     int64  `json:"balance"`
	TodayCost   int64  `json:"today_cost"`
	TodayMsgs   int    `json:"today_messages"`
	TodayTokens int    `json:"today_tokens"`
	TotalMsgs   int    `json:"total_messages"`
	TotalTokens int64  `json:"total_tokens"`
	LastActive  string `json:"last_active,omitempty"` // 最后有用量的日期
}

// Service 管理用户。
type Service struct {
	db  *gorm.DB
	loc *time.Location
	// OnDelete 在用户的数据库记录删除后调用（删文件、断开微信、清缓存）。
	OnDelete func(userID uint64)
}

// New 创建 Service。
func New(db *gorm.DB, loc *time.Location) *Service { return &Service{db: db, loc: loc} }

// Filter 是用户列表的条件。
type Filter struct {
	Query  string // 用户名或昵称
	Status string // ""、active、disabled、member（有有效的时长套餐）、admin
}

// List 分页列出用户，同时返回总数。
func (s *Service) List(ctx context.Context, f Filter, limit, offset int) ([]Row, int64, error) {
	now := time.Now()
	q := s.db.WithContext(ctx).Model(&store.User{})
	if v := strings.TrimSpace(f.Query); v != "" {
		like := "%" + strings.NewReplacer("%", "", "_", "", `\`, "").Replace(v) + "%"
		q = q.Where("username LIKE ? OR display_name LIKE ?", like, like)
	}
	switch f.Status {
	case "active":
		q = q.Where("disabled_at IS NULL")
	case "disabled":
		q = q.Where("disabled_at IS NOT NULL")
	case "admin":
		q = q.Where("is_admin = ?", true)
	case "member":
		q = q.Where("id IN (?)", s.db.Model(&store.Subscription{}).Select("user_id").Where("expires_at > ?", now))
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var users []store.User
	if err := q.Order("id DESC").Limit(limit).Offset(offset).Find(&users).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]Row, len(users))
	idx := map[uint64]int{}
	ids := make([]uint64, len(users))
	for i, u := range users {
		ids[i], idx[u.ID] = u.ID, i
		rows[i] = Row{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, IsAdmin: u.IsAdmin,
			DisabledAt: u.DisabledAt, Disabled: u.DisabledReason, CreatedAt: u.CreatedAt}
	}
	if len(ids) == 0 {
		return rows, total, nil
	}
	var subs []store.Subscription
	s.db.WithContext(ctx).Where("user_id IN ? AND expires_at > ?", ids, now).Order("expires_at").Find(&subs)
	for _, sub := range subs {
		r := &rows[idx[sub.UserID]]
		t := sub.ExpiresAt
		r.PlanName, r.PlanExpires = sub.PlanName, &t
	}
	var wallets []store.Wallet
	s.db.WithContext(ctx).Where("user_id IN ?", ids).Find(&wallets)
	for _, w := range wallets {
		rows[idx[w.UserID]].Balance = w.Balance
	}
	var costs []struct {
		UserID uint64
		Total  int64
	}
	s.db.WithContext(ctx).Model(&store.CostDaily{}).Select("user_id, COALESCE(SUM(amount), 0) AS total").
		Where("user_id IN ? AND day = ?", ids, now.In(s.loc).Format("2006-01-02")).Group("user_id").Scan(&costs)
	for _, c := range costs {
		rows[idx[c.UserID]].TodayCost = c.Total
	}
	var usage []struct {
		UserID   uint64
		Messages int
		Tokens   int64
		Last     string
	}
	s.db.WithContext(ctx).Model(&store.UsageDaily{}).
		Select("user_id, COALESCE(SUM(messages), 0) AS messages, COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS tokens, MAX(day) AS last").
		Where("user_id IN ?", ids).Group("user_id").Scan(&usage)
	for _, u := range usage {
		r := &rows[idx[u.UserID]]
		r.TotalMsgs, r.TotalTokens, r.LastActive = u.Messages, u.Tokens, u.Last
	}
	today := now.In(s.loc).Format("2006-01-02")
	var td []store.UsageDaily
	s.db.WithContext(ctx).Where("user_id IN ? AND day = ?", ids, today).Find(&td)
	for _, u := range td {
		r := &rows[idx[u.UserID]]
		r.TodayMsgs, r.TodayTokens = u.Messages, u.TotalTokens()
	}
	return rows, total, nil
}

// Get 返回用户。
func (s *Service) Get(ctx context.Context, id uint64) (*store.User, error) {
	var u store.User
	if err := s.db.WithContext(ctx).First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

// SetDisabled 停用或恢复用户。停用时让他的登录全部失效。管理员账号不能停用（先取消管理员）。
func (s *Service) SetDisabled(ctx context.Context, id uint64, disabled bool, reason string) error {
	u, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	fields := map[string]any{"disabled_at": nil, "disabled_reason": ""}
	if disabled {
		if u.IsAdmin {
			return &UserError{"管理员账号不能停用，先取消他的管理员"}
		}
		reason = strings.TrimSpace(reason)
		if r := []rune(reason); len(r) > 100 {
			reason = string(r[:100])
		}
		fields = map[string]any{"disabled_at": time.Now(), "disabled_reason": reason, "token_version": gorm.Expr("token_version + 1")}
	}
	return s.db.WithContext(ctx).Model(&store.User{}).Where("id = ?", id).Updates(fields).Error
}

// SetAdmin 设为或取消管理员。不能取消最后一个管理员。
func (s *Service) SetAdmin(ctx context.Context, id uint64, admin bool) error {
	u, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if admin && u.DisabledAt != nil {
		return &UserError{"停用的账号不能设为管理员"}
	}
	if !admin && u.IsAdmin {
		var n int64
		s.db.WithContext(ctx).Model(&store.User{}).Where("is_admin = ?", true).Count(&n)
		if n <= 1 {
			return &UserError{"这是最后一个管理员，不能取消"}
		}
	}
	// 权限变了，让他重新登录。
	return s.db.WithContext(ctx).Model(&store.User{}).Where("id = ?", id).
		Updates(map[string]any{"is_admin": admin, "token_version": gorm.Expr("token_version + 1")}).Error
}

// Delete 删除用户和他的全部数据（对话、记忆、文件、会员记录……），不可恢复。
// confirm 必须等于用户名，防止点错。管理员账号不能删（先取消管理员）。
func (s *Service) Delete(ctx context.Context, id uint64, confirm string) error {
	u, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(confirm) != u.Username {
		return &UserError{"输入的用户名不对，没有删除"}
	}
	if u.IsAdmin {
		return &UserError{"管理员账号不能删除，先取消他的管理员"}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, t := range userTables {
			if err := tx.Exec("DELETE FROM "+t+" WHERE user_id = ?", id).Error; err != nil {
				return err
			}
		}
		// 他自己添加的模型（不是公共模型）一起删。
		if err := tx.Where("owner_id = ? AND owner_id <> ?", id, store.PublicOwner).Delete(&store.ModelConfig{}).Error; err != nil {
			return err
		}
		if err := billing.DeleteUserData(tx, id); err != nil {
			return err
		}
		return tx.Delete(&store.User{}, id).Error
	})
	if err != nil {
		return err
	}
	if s.OnDelete != nil {
		s.OnDelete(id)
	}
	return nil
}
