package chat

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/HugehardJiang/buugoo/internal/store"
)

// ConversationPatch 是新建或修改对话的参数，字段为空表示不修改（新建时使用默认值）。
type ConversationPatch struct {
	Title *string
	// SetModel 为 true 时修改模型；ModelID 为空表示改回默认模型。
	SetModel bool
	ModelID  *uint64
	Thinking *string
}

func (s *Service) patchFields(ctx context.Context, u *store.User, p ConversationPatch) (map[string]any, error) {
	fields := map[string]any{}
	if p.Title != nil {
		t := strings.TrimSpace(*p.Title)
		if utf8.RuneCountInString(t) > 60 {
			return nil, ErrTitleTooLong
		}
		fields["title"] = t
	}
	if p.Thinking != nil {
		if !store.ValidThinking(*p.Thinking) {
			return nil, ErrInvalidThinking
		}
		fields["thinking"] = *p.Thinking
	}
	if p.SetModel {
		if p.ModelID != nil {
			ok, err := s.models.Visible(ctx, modelUser(u), *p.ModelID)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, ErrModelNotFound
			}
			fields["model_id"] = *p.ModelID
		} else {
			fields["model_id"] = nil
		}
	}
	return fields, nil
}

// ListConversations 按最近活动时间倒序列出用户的对话。
func (s *Service) ListConversations(ctx context.Context, u *store.User) ([]store.Conversation, error) {
	return s.store.Conversations().List(ctx, u.ID)
}

// CreateConversation 新建对话。未指定模型与思考等级时沿用最近一段对话的设置。
func (s *Service) CreateConversation(ctx context.Context, u *store.User, p ConversationPatch) (*store.Conversation, error) {
	fields, err := s.patchFields(ctx, u, p)
	if err != nil {
		return nil, err
	}
	c := &store.Conversation{UserID: u.ID, Thinking: store.ThinkingOff}
	if recent, err := s.store.Conversations().List(ctx, u.ID); err == nil && len(recent) > 0 {
		c.ModelID, c.Thinking = recent[0].ModelID, recent[0].Thinking
	}
	if t, ok := fields["title"].(string); ok {
		c.Title = t
	}
	if t, ok := fields["thinking"].(string); ok {
		c.Thinking = t
	}
	if v, ok := fields["model_id"]; ok {
		c.ModelID = nil
		if id, ok := v.(uint64); ok {
			c.ModelID = &id
		}
	}
	if err := s.store.Conversations().Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// UpdateConversation 修改对话的标题、模型或思考等级。
func (s *Service) UpdateConversation(ctx context.Context, u *store.User, id uint64, p ConversationPatch) (*store.Conversation, error) {
	fields, err := s.patchFields(ctx, u, p)
	if err != nil {
		return nil, err
	}
	if len(fields) > 0 {
		if err := s.store.Conversations().Update(ctx, u.ID, id, fields); err != nil {
			return nil, err
		}
	}
	return s.store.Conversations().Get(ctx, u.ID, id)
}

// DeleteConversation 删除对话及其消息。记忆不受影响（记忆属于用户，在所有对话间共享）。
func (s *Service) DeleteConversation(ctx context.Context, u *store.User, id uint64) error {
	return s.store.Conversations().Delete(ctx, u.ID, id)
}
