// Package memo 是用户的备忘录：可以有很多页，每页一个标题和 Markdown 正文，不主动删除就一直保留。
// 网页上可以直接编辑；聊天时布谷只在对方明确要读写备忘录时才用工具读写，平时不进入对话上下文。
package memo

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
	MaxPages        = 200
	MaxTitleRunes   = 40
	MaxContentBytes = 100 << 10
	// confirmTTL 是删除确认码的有效期。
	confirmTTL = 30 * time.Minute
)

// ErrNotFound 表示没有这一页。
var ErrNotFound = errors.New("备忘录里没有这一页")

// UserError 是用户（或模型）填写的内容不对。
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

// Page 是列表里的一页（不含正文）。
type Page struct {
	ID        uint64    `json:"id"`
	Title     string    `json:"title"`
	Size      int       `json:"size"` // 正文字数
	Preview   string    `json:"preview"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Service 管理备忘录。
type Service struct {
	db *gorm.DB

	mu      sync.Mutex
	pending map[string]pendingDelete
}

// pendingDelete 是等对方确认的删除：Text 为空表示删除整页。Turn 是提出删除的那一轮，确认必须在之后的回复里。
type pendingDelete struct {
	UserID  uint64
	PageID  uint64
	Text    string
	Turn    string
	Expires time.Time
}

// New 创建 Service。
func New(db *gorm.DB) *Service {
	return &Service{db: db, pending: map[string]pendingDelete{}}
}

func cleanTitle(t string) (string, error) {
	t = strings.Join(strings.Fields(t), " ")
	switch n := utf8.RuneCountInString(t); {
	case n == 0:
		return "", &UserError{"页面标题不能为空"}
	case n > MaxTitleRunes:
		return "", &UserError{fmt.Sprintf("页面标题最多 %d 个字", MaxTitleRunes)}
	}
	return t, nil
}

func checkContent(c string) error {
	if len(c) > MaxContentBytes {
		return &UserError{fmt.Sprintf("这一页太长了（最多 %d KB），可以分到新的一页", MaxContentBytes>>10)}
	}
	return nil
}

func preview(c string) string {
	c = strings.Join(strings.Fields(c), " ")
	if r := []rune(c); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return c
}

// List 按最近修改的顺序列出所有页面。
func (s *Service) List(ctx context.Context, userID uint64) ([]Page, error) {
	var rows []store.MemoPage
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Page, len(rows))
	for i, r := range rows {
		out[i] = Page{ID: r.ID, Title: r.Title, Size: utf8.RuneCountInString(r.Content), Preview: preview(r.Content), UpdatedAt: r.UpdatedAt}
	}
	return out, nil
}

// Get 返回一页。
func (s *Service) Get(ctx context.Context, userID, id uint64) (*store.MemoPage, error) {
	var p store.MemoPage
	if err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

// Find 按标题找一页（忽略大小写和首尾空格）；找不到时返回 ErrNotFound。
func (s *Service) Find(ctx context.Context, userID uint64, title string) (*store.MemoPage, error) {
	t := strings.ToLower(strings.Join(strings.Fields(title), " "))
	var rows []store.MemoPage
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		if strings.ToLower(rows[i].Title) == t {
			return &rows[i], nil
		}
	}
	return nil, ErrNotFound
}

// Create 新建一页。
func (s *Service) Create(ctx context.Context, userID uint64, title, content string) (*store.MemoPage, error) {
	title, err := cleanTitle(title)
	if err != nil {
		return nil, err
	}
	if err := checkContent(content); err != nil {
		return nil, err
	}
	if _, err := s.Find(ctx, userID, title); err == nil {
		return nil, &UserError{"已经有一页叫「" + title + "」了"}
	}
	var n int64
	s.db.WithContext(ctx).Model(&store.MemoPage{}).Where("user_id = ?", userID).Count(&n)
	if n >= MaxPages {
		return nil, &UserError{fmt.Sprintf("备忘录最多 %d 页，删掉一些不用的再建", MaxPages)}
	}
	p := &store.MemoPage{UserID: userID, Title: title, Content: content}
	if err := s.db.WithContext(ctx).Create(p).Error; err != nil {
		return nil, err
	}
	return p, nil
}

// Update 修改一页的标题或正文（为 nil 的不改）。
func (s *Service) Update(ctx context.Context, userID, id uint64, title, content *string) (*store.MemoPage, error) {
	p, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{}
	if title != nil {
		t, err := cleanTitle(*title)
		if err != nil {
			return nil, err
		}
		if other, err := s.Find(ctx, userID, t); err == nil && other.ID != id {
			return nil, &UserError{"已经有一页叫「" + t + "」了"}
		}
		fields["title"], p.Title = t, t
	}
	if content != nil {
		if err := checkContent(*content); err != nil {
			return nil, err
		}
		fields["content"], p.Content = *content, *content
	}
	if len(fields) == 0 {
		return p, nil
	}
	fields["updated_at"] = time.Now()
	if err := s.db.WithContext(ctx).Model(&store.MemoPage{}).Where("id = ? AND user_id = ?", id, userID).Updates(fields).Error; err != nil {
		return nil, err
	}
	return s.Get(ctx, userID, id)
}

// Delete 删除一页。
func (s *Service) Delete(ctx context.Context, userID, id uint64) error {
	res := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&store.MemoPage{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
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
		return d, &UserError{"确认码无效或已过期，重新调用一次 memo_delete（不带确认码）"}
	case d.Turn == turn:
		return d, &UserError{"要先在回复里问对方，对方明确同意后，在下一次回复时再带上确认码"}
	}
	delete(s.pending, code)
	return d, nil
}
