// Package todo 是用户的待办清单：一件件要持续去做、做完打勾的事，可以有截止时间和备注。
// 网页上可以直接增删改、打勾；聊天时布谷用工具帮对方记、查、打勾；有截止时间的事快到期还没做完时，布谷会主动问一句。
package todo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/HugehardJiang/buugoo/internal/store"
)

// 限制。
const (
	MaxItems      = 500
	MaxTitleRunes = 100
	MaxNoteRunes  = 2000
	// maxDone 是列表里最多显示的已完成项。
	maxDone = 100
	// confirmTTL 是删除确认码的有效期。
	confirmTTL = 30 * time.Minute
)

// ErrNotFound 表示没有这一项。
var ErrNotFound = errors.New("待办清单里没有这一项")

// UserError 是用户（或模型）填写的内容不对。
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

// Item 是返回给网页的一项。
type Item struct {
	ID    uint64     `json:"id"`
	Title string     `json:"title"`
	Note  string     `json:"note"`
	DueAt *time.Time `json:"due_at,omitempty"`
	// Due 是截止时间按服务器时区写成的「YYYY-MM-DD」或「YYYY-MM-DD HH:MM」（和编辑框一致），没有时为空。
	Due       string     `json:"due"`
	AllDay    bool       `json:"all_day"`
	Done      bool       `json:"done"`
	DoneAt    *time.Time `json:"done_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func (s *Service) toItem(t *store.Todo) Item {
	it := Item{ID: t.ID, Title: t.Title, Note: t.Note, DueAt: t.DueAt, AllDay: t.AllDay, Done: t.Done, DoneAt: t.DoneAt, CreatedAt: t.CreatedAt}
	if t.DueAt != nil {
		layout := "2006-01-02 15:04"
		if t.AllDay {
			layout = "2006-01-02"
		}
		it.Due = t.DueAt.In(s.loc).Format(layout)
	}
	return it
}

// Service 管理待办清单。
type Service struct {
	db  *gorm.DB
	loc *time.Location

	mu      sync.Mutex
	pending map[string]pendingDelete
}

// pendingDelete 是等对方确认的删除。Turn 是提出删除的那一轮，确认必须在之后的回复里。
type pendingDelete struct {
	UserID  uint64
	ItemID  uint64
	Turn    string
	Expires time.Time
}

// New 创建 Service。loc 是解析和显示截止时间用的时区。
func New(db *gorm.DB, loc *time.Location) *Service {
	if loc == nil {
		loc = time.Local
	}
	return &Service{db: db, loc: loc, pending: map[string]pendingDelete{}}
}

// Location 返回显示截止时间用的时区。
func (s *Service) Location() *time.Location { return s.loc }

func cleanTitle(t string) (string, error) {
	t = strings.Join(strings.Fields(t), " ")
	switch n := utf8.RuneCountInString(t); {
	case n == 0:
		return "", &UserError{"待办的内容不能为空"}
	case n > MaxTitleRunes:
		return "", &UserError{fmt.Sprintf("待办的内容最多 %d 个字，细节可以写在备注里", MaxTitleRunes)}
	}
	return t, nil
}

func cleanNote(n string) (string, error) {
	n = strings.TrimSpace(n)
	if utf8.RuneCountInString(n) > MaxNoteRunes {
		return "", &UserError{fmt.Sprintf("备注最多 %d 个字", MaxNoteRunes)}
	}
	return n, nil
}

// ParseDue 解析截止时间：空表示没有截止时间；「YYYY-MM-DD」只定日期；「YYYY-MM-DD HH:MM」（或中间是 T）定到分钟。
func (s *Service) ParseDue(v string) (due *time.Time, allDay bool, err error) {
	v = strings.TrimSpace(strings.Replace(v, "T", " ", 1))
	if v == "" {
		return nil, false, nil
	}
	// 统一按 UTC 保存（SQLite 按字符串比较时间，时区要一致）。
	if t, err := time.ParseInLocation("2006-01-02 15:04", v, s.loc); err == nil {
		t = t.UTC()
		return &t, false, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", v, s.loc); err == nil {
		t = t.UTC()
		return &t, true, nil
	}
	return nil, false, &UserError{"截止时间的格式是 YYYY-MM-DD 或 YYYY-MM-DD HH:MM（本地时间）"}
}

// FormatDue 把截止时间写成「10-30 18:00」或「10-30」（不是今年时带上年份）。
func (s *Service) FormatDue(t *store.Todo, now time.Time) string {
	if t.DueAt == nil {
		return ""
	}
	d := t.DueAt.In(s.loc)
	layout := "01-02"
	if d.Year() != now.In(s.loc).Year() {
		layout = "2006-01-02"
	}
	if !t.AllDay {
		layout += " 15:04"
	}
	return d.Format(layout)
}

// List 列出待办：没做完的按截止时间排在前（没有截止时间的在后），做完的按完成时间倒序跟在后面（最多 100 项）。
func (s *Service) List(ctx context.Context, userID uint64) ([]store.Todo, error) {
	var open, done []store.Todo
	db := s.db.WithContext(ctx)
	if err := db.Where("user_id = ? AND done = ?", userID, false).
		Order("CASE WHEN due_at IS NULL THEN 1 ELSE 0 END, due_at, id").Find(&open).Error; err != nil {
		return nil, err
	}
	if err := db.Where("user_id = ? AND done = ?", userID, true).Order("done_at DESC, id DESC").Limit(maxDone).Find(&done).Error; err != nil {
		return nil, err
	}
	return append(open, done...), nil
}

// Items 把待办转成返回给网页的格式。
func (s *Service) Items(list ...store.Todo) []Item {
	out := make([]Item, len(list))
	for i := range list {
		out[i] = s.toItem(&list[i])
	}
	return out
}

// Get 返回一项。
func (s *Service) Get(ctx context.Context, userID, id uint64) (*store.Todo, error) {
	var t store.Todo
	if err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

// Create 加一项。due 的格式见 ParseDue。
func (s *Service) Create(ctx context.Context, userID uint64, title, note, due string) (*store.Todo, error) {
	title, err := cleanTitle(title)
	if err != nil {
		return nil, err
	}
	if note, err = cleanNote(note); err != nil {
		return nil, err
	}
	at, allDay, err := s.ParseDue(due)
	if err != nil {
		return nil, err
	}
	var n int64
	s.db.WithContext(ctx).Model(&store.Todo{}).Where("user_id = ?", userID).Count(&n)
	if n >= MaxItems {
		return nil, &UserError{fmt.Sprintf("待办清单最多 %d 项，先删掉一些做完的", MaxItems)}
	}
	t := &store.Todo{UserID: userID, Title: title, Note: note, DueAt: at, AllDay: allDay}
	if err := s.db.WithContext(ctx).Create(t).Error; err != nil {
		return nil, err
	}
	return t, nil
}

// Patch 是要修改的内容，为 nil 的不改；Due 为空字符串表示去掉截止时间。
type Patch struct {
	Title, Note, Due *string
}

// Update 修改一项。改了截止时间后，快到期时会重新主动问一次。
func (s *Service) Update(ctx context.Context, userID, id uint64, p Patch) (*store.Todo, error) {
	if _, err := s.Get(ctx, userID, id); err != nil {
		return nil, err
	}
	fields := map[string]any{}
	if p.Title != nil {
		t, err := cleanTitle(*p.Title)
		if err != nil {
			return nil, err
		}
		fields["title"] = t
	}
	if p.Note != nil {
		n, err := cleanNote(*p.Note)
		if err != nil {
			return nil, err
		}
		fields["note"] = n
	}
	if p.Due != nil {
		at, allDay, err := s.ParseDue(*p.Due)
		if err != nil {
			return nil, err
		}
		fields["due_at"], fields["all_day"], fields["nudged_at"] = at, allDay, nil
	}
	if len(fields) > 0 {
		fields["updated_at"] = time.Now()
		if err := s.db.WithContext(ctx).Model(&store.Todo{}).Where("id = ? AND user_id = ?", id, userID).Updates(fields).Error; err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, userID, id)
}

// SetDone 打勾（done 为 true）或取消打勾。
func (s *Service) SetDone(ctx context.Context, userID, id uint64, done bool) (*store.Todo, error) {
	if _, err := s.Get(ctx, userID, id); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var doneAt *time.Time
	if done {
		doneAt = &now
	}
	if err := s.db.WithContext(ctx).Model(&store.Todo{}).Where("id = ? AND user_id = ?", id, userID).
		Updates(map[string]any{"done": done, "done_at": doneAt, "updated_at": now}).Error; err != nil {
		return nil, err
	}
	return s.Get(ctx, userID, id)
}

// Delete 删除一项。
func (s *Service) Delete(ctx context.Context, userID, id uint64) error {
	res := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&store.Todo{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearDone 删掉所有做完的，返回删了几项。
func (s *Service) ClearDone(ctx context.Context, userID uint64) (int64, error) {
	res := s.db.WithContext(ctx).Where("user_id = ? AND done = ?", userID, true).Delete(&store.Todo{})
	return res.RowsAffected, res.Error
}

// 快到期的主动询问：有具体时间的提前 nudgeLead 问；只定了日期的在那天 allDayHour 点问。
const (
	nudgeLead  = 2 * time.Hour
	allDayHour = 10
)

// NudgeAt 返回这一项该主动问的时间（没有截止时间时返回零值）。
func (s *Service) NudgeAt(t *store.Todo) time.Time {
	if t.DueAt == nil {
		return time.Time{}
	}
	if t.AllDay {
		d := t.DueAt.In(s.loc)
		return time.Date(d.Year(), d.Month(), d.Day(), allDayHour, 0, 0, 0, s.loc)
	}
	return t.DueAt.Add(-nudgeLead)
}

// DueSoon 返回所有用户中到了该主动问的时间、还没做完也没问过的待办。
func (s *Service) DueSoon(ctx context.Context, now time.Time, limit int) ([]store.Todo, error) {
	var rows []store.Todo
	// 只定了日期的 DueAt 是那天 0 点，到 10 点才问，所以往后多查 nudgeLead 足够覆盖两种情况。
	err := s.db.WithContext(ctx).Where("done = ? AND nudged_at IS NULL AND due_at IS NOT NULL AND due_at <= ?", false, now.Add(nudgeLead).UTC()).
		Order("due_at").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := rows[:0]
	for _, r := range rows {
		if !now.Before(s.NudgeAt(&r)) {
			out = append(out, r)
		}
	}
	return out, nil
}

// MarkNudged 记下已经主动问过（或者决定不问了）。
func (s *Service) MarkNudged(ctx context.Context, id uint64, at time.Time) error {
	return s.db.WithContext(ctx).Model(&store.Todo{}).Where("id = ?", id).Update("nudged_at", at.UTC()).Error
}

// propose 记下一次等确认的删除，返回确认码。
func (s *Service) propose(d pendingDelete) string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	code := hex.EncodeToString(b[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.pending {
		if now.After(v.Expires) {
			delete(s.pending, k)
		}
	}
	d.Expires = now.Add(confirmTTL)
	s.pending[code] = d
	return code
}

// take 取出确认码对应的删除（必须是同一个用户、在提出之后的回复里）。
func (s *Service) take(code string, userID uint64, turn string) (pendingDelete, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.pending[strings.TrimSpace(code)]
	switch {
	case !ok || d.UserID != userID || time.Now().After(d.Expires):
		return d, &UserError{"确认码无效或已过期，重新调用一次 todo_delete（不带确认码）"}
	case d.Turn == turn:
		return d, &UserError{"要先在回复里问对方，对方明确同意后，在下一次回复时再带上确认码"}
	}
	delete(s.pending, code)
	return d, nil
}
