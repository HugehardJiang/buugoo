// Package cosplay 管理 Cosplay 用的角色卡：用户可以粘贴、上传或从网址导入角色提示词
// （例如 aigengtu.com/persona 上的大肥鱼人设卡、SillyTavern 角色卡），新建对话时选一张，
// 这段对话里 AI 就扮演这个角色。每个用户导入的卡只有自己能用；管理员可以把卡设为公共卡给所有人用。
package cosplay

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/HugehardJiang/buugoo/internal/prompt"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// 限制。
const (
	MaxCards       = 100 // 每个用户最多导入多少张
	MaxNameRunes   = 40
	MaxDescRunes   = 200
	MaxSourceBytes = 500
)

// ErrNotFound 表示没有这张卡（或者看不到）。
var ErrNotFound = errors.New("没有这张角色卡")

// ErrForbidden 表示没有权限改这张卡。
var ErrForbidden = errors.New("这张角色卡不是你导入的，不能修改")

// UserError 是填写的内容不对。
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

// User 是操作的人。
type User struct {
	ID      uint64
	IsAdmin bool
}

// Card 是列表里的一张卡（不含全文）。
type Card struct {
	ID          uint64 `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Public      bool   `json:"public"`
	// Mine 表示是自己导入的（可以修改、删除）。
	Mine   bool   `json:"mine"`
	Source string `json:"source,omitempty"`
	// Runes 是全文字数，Tokens 是大约多少 token（每轮都会发给模型）。
	Runes     int       `json:"runes"`
	Tokens    int       `json:"tokens"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Input 是新建或修改一张卡的内容。
type Input struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Source      string `json:"source"`
	Public      bool   `json:"public"`
}

// Service 管理角色卡。
type Service struct {
	db *gorm.DB
	// maxRunes 是一张卡最多多少字。
	maxRunes int
	fetcher  *fetcher
}

// New 创建 Service。maxRunes 是一张卡最多多少字。
func New(db *gorm.DB, maxRunes int) *Service {
	return &Service{db: db, maxRunes: maxRunes, fetcher: newFetcher()}
}

// MaxRunes 返回一张卡最多多少字。
func (s *Service) MaxRunes() int { return s.maxRunes }

func toCard(c *store.Character, u User) Card {
	return Card{
		ID: c.ID, Name: c.Name, Description: c.Description, Public: c.Public, Mine: c.UserID == u.ID, Source: c.Source,
		Runes: utf8.RuneCountInString(c.Prompt), Tokens: prompt.EstimateTokens(c.Prompt), UpdatedAt: c.UpdatedAt,
	}
}

// visible 限定为自己导入的和公共的卡。
func visible(db *gorm.DB, userID uint64) *gorm.DB {
	return db.Where("user_id = ? OR public = ?", userID, true)
}

// List 列出能用的卡：自己导入的在前，然后是公共卡，各自按最近修改排序。
func (s *Service) List(ctx context.Context, u User) ([]Card, error) {
	var rows []store.Character
	// 列表不需要全文，但要算字数；卡最多一百来张，直接取出来算。
	if err := visible(s.db.WithContext(ctx), u.ID).Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Card, 0, len(rows))
	for i := range rows {
		if rows[i].UserID == u.ID {
			out = append(out, toCard(&rows[i], u))
		}
	}
	for i := range rows {
		if rows[i].UserID != u.ID {
			out = append(out, toCard(&rows[i], u))
		}
	}
	return out, nil
}

// Get 返回一张能用的卡（含全文）。
func (s *Service) Get(ctx context.Context, u User, id uint64) (*store.Character, error) {
	var c store.Character
	if err := visible(s.db.WithContext(ctx), u.ID).Where("id = ?", id).First(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

// Load 返回一张卡，包括已经删除的（聊天时用：对话建好后卡被删了也照样能继续聊）。
func (s *Service) Load(ctx context.Context, id uint64) (*store.Character, error) {
	var c store.Character
	if err := s.db.WithContext(ctx).Unscoped().Where("id = ?", id).First(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

// clean 校验并整理填写的内容。
func (s *Service) clean(in Input) (Input, error) {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Description = strings.Join(strings.Fields(in.Description), " ")
	in.Prompt = normalizeText(in.Prompt)
	in.Source = strings.TrimSpace(in.Source)
	switch n := utf8.RuneCountInString(in.Name); {
	case n == 0:
		return in, &UserError{"给角色起个名字"}
	case n > MaxNameRunes:
		return in, &UserError{fmt.Sprintf("角色名最多 %d 个字", MaxNameRunes)}
	}
	if r := []rune(in.Description); len(r) > MaxDescRunes {
		in.Description = string(r[:MaxDescRunes-1]) + "…"
	}
	switch n := utf8.RuneCountInString(in.Prompt); {
	case n == 0:
		return in, &UserError{"角色提示词不能为空"}
	case n > s.maxRunes:
		return in, &UserError{fmt.Sprintf("角色提示词有 %d 字，超过了上限 %d 字（每轮都会整张发给模型，太长很费钱）。可以删掉用不上的部分再导入", n, s.maxRunes)}
	}
	if len(in.Source) > MaxSourceBytes || (in.Source != "" && !strings.HasPrefix(in.Source, "http://") && !strings.HasPrefix(in.Source, "https://")) {
		in.Source = ""
	}
	return in, nil
}

// Create 导入一张卡。只有管理员能设为公共卡。
func (s *Service) Create(ctx context.Context, u User, in Input) (*Card, error) {
	in, err := s.clean(in)
	if err != nil {
		return nil, err
	}
	if in.Public && !u.IsAdmin {
		return nil, &UserError{"只有管理员能发布公共角色卡"}
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&store.Character{}).Where("user_id = ?", u.ID).Count(&n).Error; err != nil {
		return nil, err
	}
	if n >= MaxCards {
		return nil, &UserError{fmt.Sprintf("最多导入 %d 张角色卡，先删掉一些不用的", MaxCards)}
	}
	c := &store.Character{UserID: u.ID, Public: in.Public, Name: in.Name, Description: in.Description, Prompt: in.Prompt, Source: in.Source}
	if err := s.db.WithContext(ctx).Create(c).Error; err != nil {
		return nil, err
	}
	card := toCard(c, u)
	return &card, nil
}

// editable 返回能修改的卡：自己导入的，或者管理员改公共卡。
func (s *Service) editable(ctx context.Context, u User, id uint64) (*store.Character, error) {
	c, err := s.Get(ctx, u, id)
	if err != nil {
		return nil, err
	}
	if c.UserID != u.ID && !(u.IsAdmin && c.Public) {
		return nil, ErrForbidden
	}
	return c, nil
}

// Update 修改一张卡。已经在用这张卡的对话下一条消息起按新内容扮演。
func (s *Service) Update(ctx context.Context, u User, id uint64, in Input) (*Card, error) {
	c, err := s.editable(ctx, u, id)
	if err != nil {
		return nil, err
	}
	if in, err = s.clean(in); err != nil {
		return nil, err
	}
	if in.Public != c.Public && !u.IsAdmin {
		return nil, &UserError{"只有管理员能发布公共角色卡"}
	}
	c.Name, c.Description, c.Prompt, c.Public = in.Name, in.Description, in.Prompt, in.Public
	if in.Source != "" {
		c.Source = in.Source
	}
	if err := s.db.WithContext(ctx).Model(c).Select("name", "description", "prompt", "public", "source", "updated_at").Updates(c).Error; err != nil {
		return nil, err
	}
	card := toCard(c, u)
	return &card, nil
}

// Delete 删除一张卡。已经用这张卡建的对话不受影响，照样能继续聊。
func (s *Service) Delete(ctx context.Context, u User, id uint64) error {
	c, err := s.editable(ctx, u, id)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Delete(c).Error
}

// normalizeText 统一换行、去掉首尾空白和多余的空行。
func normalizeText(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	s = strings.ReplaceAll(s, " ", " ")
	lines := strings.Split(s, "\n")
	out := lines[:0]
	blank := 0
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		if l == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// Usable 返回这个用户能用的卡（自己导入的或公共的），实现 chat.CharacterSource。
func (s *Service) Usable(ctx context.Context, userID, id uint64) (*store.Character, error) {
	return s.Get(ctx, User{ID: userID}, id)
}
