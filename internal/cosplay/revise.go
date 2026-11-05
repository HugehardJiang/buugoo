package cosplay

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/HugehardJiang/buugoo/internal/store"
)

// 自我进化：Cosplay 对话里，对方明确要求改进角色（说话方式、设定、规则）时，AI 用工具改自己的角色提示词。
// 改之前的样子存进修改记录，在「设置 → 角色卡」里可以恢复。公共卡不能被某个用户改掉，改的时候会复制一张成为对方自己的卡。

// 限制。
const (
	// MaxRevisions 是每张卡保留的修改记录条数。
	MaxRevisions = 20
	// MaxEditRunes 是一次改动最多写多少字。
	MaxEditRunes = 4000
	// EvolveHeading 是追加的改进要求放在哪一节下面。
	EvolveHeading = "## 对方要求的调整（自我进化记下的，和上面冲突时以这里为准）"
)

// 改动方式。
const (
	EditAppend  = "append"  // 在卡的最后追加一条要求
	EditReplace = "replace" // 把原文里的一段换成新的（新内容为空就是删掉）
)

// Edit 是一次改动。
type Edit struct {
	Mode string
	Old  string
	Text string
	// Note 是改了什么（一句话，记进修改记录）。
	Note string
}

// apply 返回改动后的提示词。
func apply(prompt string, e Edit) (string, error) {
	e.Text = strings.TrimSpace(e.Text)
	if utf8.RuneCountInString(e.Text) > MaxEditRunes {
		return "", &UserError{fmt.Sprintf("一次最多改 %d 字", MaxEditRunes)}
	}
	switch e.Mode {
	case EditAppend:
		if e.Text == "" {
			return "", &UserError{"要追加的内容是空的"}
		}
		line := "- " + strings.ReplaceAll(e.Text, "\n", "\n  ")
		if strings.Contains(prompt, EvolveHeading) {
			return strings.TrimRight(prompt, "\n") + "\n" + line, nil
		}
		return strings.TrimRight(prompt, "\n") + "\n\n" + EvolveHeading + "\n" + line, nil
	case EditReplace:
		if strings.TrimSpace(e.Old) == "" {
			return "", &UserError{"要替换的原文是空的"}
		}
		switch n := strings.Count(prompt, e.Old); {
		case n == 0:
			return "", &UserError{"角色卡里没找到这段原文（要一字不差地照抄），可以改用追加"}
		case n > 1:
			return "", &UserError{"这段原文在角色卡里出现了好几次，多抄一点上下文让它只出现一次"}
		}
		return strings.Replace(prompt, e.Old, e.Text, 1), nil
	}
	return "", &UserError{"改动方式只能是 append 或 replace"}
}

// Revise 按改动修改这段对话用的角色卡。自己的卡直接改（改之前的样子存进修改记录）；
// 公共卡或已经删掉的卡复制一张成为对方自己的卡再改，返回的 copied 为 true，对话要换成用新卡。
func (s *Service) Revise(ctx context.Context, u User, id uint64, e Edit) (card *store.Character, copied bool, err error) {
	c, err := s.Load(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if c.UserID != u.ID && !c.Public {
		return nil, false, ErrNotFound
	}
	prompt, err := apply(c.Prompt, e)
	if err != nil {
		return nil, false, err
	}
	if n := utf8.RuneCountInString(prompt); n > s.maxRunes {
		return nil, false, &UserError{fmt.Sprintf("改完有 %d 字，超过了上限 %d 字；可以先删掉一些用不上的规则", n, s.maxRunes)}
	}
	note := strings.TrimSpace(e.Note)
	if r := []rune(note); len(r) > 100 {
		note = string(r[:100])
	}
	if c.Public || c.UserID != u.ID || c.DeletedAt.Valid {
		// 公共卡（就算是管理员自己的，聊天里也不改掉大家用的那张）或者已经删掉的卡：另存一张自己的副本再改。
		nc, err := s.fork(ctx, u, c, Input{Name: c.Name, Description: c.Description, Prompt: prompt, Source: c.Source}, "复制前的原版"+suffix(note))
		if err != nil {
			return nil, false, err
		}
		return nc, true, nil
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.saveRevision(tx, c.ID, u.ID, c.Prompt, note); err != nil {
			return err
		}
		c.Prompt = prompt
		return tx.Model(c).Select("prompt", "updated_at").Updates(c).Error
	})
	return c, false, err
}

// Fork 把一张不能改的卡（公共卡）按填写的内容另存为自己的副本，并把自己正在用原卡的对话都换成副本。
func (s *Service) Fork(ctx context.Context, u User, id uint64, in Input) (*Card, error) {
	c, err := s.Get(ctx, u, id)
	if err != nil {
		return nil, err
	}
	if c.UserID == u.ID && !c.Public {
		return nil, &UserError{"这是你自己的角色卡，直接修改就行"}
	}
	nc, err := s.fork(ctx, u, c, in, "复制前的原版")
	if err != nil {
		return nil, err
	}
	card := toCard(nc, u)
	return &card, nil
}

// fork 另存副本：原版存进副本的修改记录，用户自己用原卡的对话都换成副本（公共卡本身不变）。
func (s *Service) fork(ctx context.Context, u User, c *store.Character, in Input, note string) (*store.Character, error) {
	in.Public = false
	if c.Public && c.UserID != u.ID && !strings.Contains(in.Description, "从公共角色卡") {
		in.Description = strings.TrimSpace("从公共角色卡「" + c.Name + "」另存的副本。" + in.Description)
	}
	in, err := s.clean(in)
	if err != nil {
		return nil, err
	}
	cp, err := s.Create(ctx, u, in)
	if err != nil {
		return nil, err
	}
	nc, err := s.Load(ctx, cp.ID)
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.saveRevision(tx, nc.ID, u.ID, c.Prompt, note); err != nil {
			return err
		}
		return tx.Model(&store.Conversation{}).Where("user_id = ? AND character_id = ?", u.ID, c.ID).
			Updates(map[string]any{"character_id": nc.ID, "character_name": nc.Name}).Error
	})
	return nc, err
}

func suffix(note string) string {
	if note == "" {
		return ""
	}
	return "（之后：" + note + "）"
}

// saveRevision 存一条修改记录，只留最近 MaxRevisions 条。
func (s *Service) saveRevision(tx *gorm.DB, cardID, userID uint64, prompt, note string) error {
	if note == "" {
		note = "修改前"
	}
	if r := []rune(note); len(r) > 180 {
		note = string(r[:180])
	}
	if err := tx.Create(&store.CharacterRevision{CharacterID: cardID, UserID: userID, Prompt: prompt, Note: note}).Error; err != nil {
		return err
	}
	// 取出这张卡所有记录的编号（最多二十来条），在程序里挑出多余的删掉。
	// 不用 OFFSET：MySQL 不支持没有 LIMIT 的 OFFSET。
	var ids []uint64
	if err := tx.Model(&store.CharacterRevision{}).Where("character_id = ?", cardID).Order("id DESC").Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) > MaxRevisions {
		return tx.Where("id IN ?", ids[MaxRevisions:]).Delete(&store.CharacterRevision{}).Error
	}
	return nil
}

// Revision 是修改记录里的一条（列表里不含全文）。
type Revision struct {
	store.CharacterRevision
	Runes int `json:"runes"`
}

// Revisions 列出自己的卡的修改记录（最新的在前）。
func (s *Service) Revisions(ctx context.Context, u User, id uint64) ([]Revision, error) {
	c, err := s.editable(ctx, u, id)
	if err != nil {
		return nil, err
	}
	var rows []store.CharacterRevision
	if err := s.db.WithContext(ctx).Where("character_id = ?", c.ID).Order("id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Revision, len(rows))
	for i, r := range rows {
		out[i] = Revision{CharacterRevision: r, Runes: utf8.RuneCountInString(r.Prompt)}
	}
	return out, nil
}

// Restore 把卡恢复成某条修改记录里的样子（恢复前的样子也存一条，可以再恢复回来）。
func (s *Service) Restore(ctx context.Context, u User, id, revID uint64) (*Card, error) {
	c, err := s.editable(ctx, u, id)
	if err != nil {
		return nil, err
	}
	var r store.CharacterRevision
	if err := s.db.WithContext(ctx).Where("id = ? AND character_id = ?", revID, c.ID).First(&r).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.saveRevision(tx, c.ID, u.ID, c.Prompt, "恢复之前的样子"); err != nil {
			return err
		}
		c.Prompt = r.Prompt
		return tx.Model(c).Select("prompt", "updated_at").Updates(c).Error
	})
	if err != nil {
		return nil, err
	}
	card := toCard(c, u)
	return &card, nil
}
