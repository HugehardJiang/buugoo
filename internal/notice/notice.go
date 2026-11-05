// Package notice 是管理员发布的强制弹窗通知：用户打开网页时弹出，至少看够 Seconds 秒才能关，
// 关了就不再主动弹（设置页里还能翻看）。发布之后才注册的用户也会弹。
package notice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/HugehardJiang/buugoo/internal/store"
)

// 限制。
const (
	MaxTitleRunes   = 60
	MaxContentRunes = 5000
	MaxSeconds      = 60
	DefaultSeconds  = 10
	// pendingLimit 是一次最多弹几条（太多时先弹最早的，关完再取）。
	pendingLimit = 5
	// slack 是检查「看够了没有」时允许的误差（网络延迟、时钟误差）。
	slack = 1500 * time.Millisecond
)

// ErrNotFound 表示没有这条通知。
var ErrNotFound = errors.New("没有这条通知")

// UserError 是填写的内容或操作不对。
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

// Item 是给用户看的一条通知。
type Item struct {
	ID        uint64     `json:"id"`
	Title     string     `json:"title"`
	Content   string     `json:"content"`
	Seconds   int        `json:"seconds"`
	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
}

// AdminItem 是管理后台里的一条通知（带已读人数）。
type AdminItem struct {
	ID        uint64    `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Seconds   int       `json:"seconds"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Reads     int64     `json:"reads"`
}

// Input 是发布或修改的内容。
type Input struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Seconds int    `json:"seconds"`
}

// Service 管理通知。
type Service struct {
	db  *gorm.DB
	now func() time.Time
}

// New 创建 Service。
func New(db *gorm.DB) *Service { return &Service{db: db, now: time.Now} }

func clean(in Input) (Input, error) {
	in.Title = strings.Join(strings.Fields(in.Title), " ")
	in.Content = strings.TrimSpace(strings.ReplaceAll(in.Content, "\r\n", "\n"))
	switch n := utf8.RuneCountInString(in.Title); {
	case n == 0:
		return in, &UserError{"标题不能为空"}
	case n > MaxTitleRunes:
		return in, &UserError{fmt.Sprintf("标题最多 %d 个字", MaxTitleRunes)}
	}
	if utf8.RuneCountInString(in.Content) > MaxContentRunes {
		return in, &UserError{fmt.Sprintf("正文最多 %d 字", MaxContentRunes)}
	}
	if in.Seconds < 0 || in.Seconds > MaxSeconds {
		return in, &UserError{fmt.Sprintf("强制阅读时间要在 0 到 %d 秒之间", MaxSeconds)}
	}
	return in, nil
}

// Create 发布一条通知，所有用户（包括之后注册的）下次打开网页时都会弹出。
func (s *Service) Create(ctx context.Context, in Input) (*store.Notice, error) {
	in, err := clean(in)
	if err != nil {
		return nil, err
	}
	n := &store.Notice{Title: in.Title, Content: in.Content, Seconds: in.Seconds}
	return n, s.db.WithContext(ctx).Create(n).Error
}

// Update 修改一条通知。已经关掉的用户不会再弹；resend 为 true 时清掉已读记录，所有人重新弹一次。
func (s *Service) Update(ctx context.Context, id uint64, in Input, resend bool) (*store.Notice, error) {
	in, err := clean(in)
	if err != nil {
		return nil, err
	}
	var n store.Notice
	if err := s.db.WithContext(ctx).First(&n, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	n.Title, n.Content, n.Seconds = in.Title, in.Content, in.Seconds
	return &n, s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&n).Error; err != nil {
			return err
		}
		if resend {
			return tx.Where("notice_id = ?", id).Delete(&store.NoticeRead{}).Error
		}
		return nil
	})
}

// Delete 撤回一条通知（还没看的人不会再弹，历史里也看不到）。
func (s *Service) Delete(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Delete(&store.Notice{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return tx.Where("notice_id = ?", id).Delete(&store.NoticeRead{}).Error
	})
}

// AdminList 列出所有通知（最新的在前），带已读人数。
func (s *Service) AdminList(ctx context.Context) ([]AdminItem, error) {
	var rows []store.Notice
	if err := s.db.WithContext(ctx).Order("id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	type count struct {
		NoticeID uint64
		N        int64
	}
	var counts []count
	if err := s.db.WithContext(ctx).Model(&store.NoticeRead{}).Select("notice_id, COUNT(*) AS n").
		Where("read_at IS NOT NULL").Group("notice_id").Scan(&counts).Error; err != nil {
		return nil, err
	}
	reads := map[uint64]int64{}
	for _, c := range counts {
		reads[c.NoticeID] = c.N
	}
	out := make([]AdminItem, len(rows))
	for i, n := range rows {
		out[i] = AdminItem{ID: n.ID, Title: n.Title, Content: n.Content, Seconds: n.Seconds, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt, Reads: reads[n.ID]}
	}
	return out, nil
}

// Pending 返回用户还没关掉的通知（最早的在前），并记下第一次弹出的时间。
func (s *Service) Pending(ctx context.Context, userID uint64) ([]Item, error) {
	var rows []store.Notice
	err := s.db.WithContext(ctx).
		Where("NOT EXISTS (SELECT 1 FROM notice_reads r WHERE r.notice_id = notices.id AND r.user_id = ? AND r.read_at IS NOT NULL)", userID).
		Order("id").Limit(pendingLimit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]Item, len(rows))
	now := s.now()
	for i, n := range rows {
		out[i] = Item{ID: n.ID, Title: n.Title, Content: n.Content, Seconds: n.Seconds, CreatedAt: n.CreatedAt}
		// 第一次弹出时记下时间；已经记过的保持不变（刷新页面不会重新计时，但也不会因此提前放行）。
		row := store.NoticeRead{UserID: userID, NoticeID: n.ID, ShownAt: now}
		if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return nil, err
		}
	}
	return out, nil
}

// MarkRead 关掉一条通知。从第一次弹出算起，没看够 Seconds 秒时拒绝（防止绕过网页上的倒计时）。
func (s *Service) MarkRead(ctx context.Context, userID, noticeID uint64) error {
	var n store.Notice
	if err := s.db.WithContext(ctx).First(&n, noticeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	var r store.NoticeRead
	if err := s.db.WithContext(ctx).Where("user_id = ? AND notice_id = ?", userID, noticeID).First(&r).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &UserError{"这条通知还没弹出来过"}
		}
		return err
	}
	if r.ReadAt != nil {
		return nil
	}
	now := s.now()
	if wait := time.Duration(n.Seconds)*time.Second - now.Sub(r.ShownAt); wait > slack {
		return &UserError{fmt.Sprintf("请再看 %d 秒", int(wait.Round(time.Second)/time.Second))}
	}
	return s.db.WithContext(ctx).Model(&store.NoticeRead{}).Where("user_id = ? AND notice_id = ?", userID, noticeID).Update("read_at", now).Error
}

// History 返回所有通知（最新的在前），标上用户有没有看过。
func (s *Service) History(ctx context.Context, userID uint64) ([]Item, error) {
	var rows []store.Notice
	if err := s.db.WithContext(ctx).Order("id DESC").Limit(200).Find(&rows).Error; err != nil {
		return nil, err
	}
	var reads []store.NoticeRead
	if err := s.db.WithContext(ctx).Where("user_id = ? AND read_at IS NOT NULL", userID).Find(&reads).Error; err != nil {
		return nil, err
	}
	readAt := map[uint64]*time.Time{}
	for _, r := range reads {
		readAt[r.NoticeID] = r.ReadAt
	}
	out := make([]Item, len(rows))
	for i, n := range rows {
		out[i] = Item{ID: n.ID, Title: n.Title, Content: n.Content, Seconds: n.Seconds, CreatedAt: n.CreatedAt, ReadAt: readAt[n.ID]}
	}
	return out, nil
}
