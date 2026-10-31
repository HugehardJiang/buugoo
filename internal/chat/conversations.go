package chat

import (
	"context"
	"strings"
	"time"
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
	// Channel 是对话来自哪个聊天软件，只在创建时使用。
	Channel string
	// Temporary 新建临时对话（只在创建时使用）；UseContext 是临时对话里是否参考记忆和心情。
	Temporary  bool
	UseContext *bool
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
	if p.UseContext != nil {
		fields["use_context"] = *p.UseContext
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
	c := &store.Conversation{UserID: u.ID, Thinking: store.ThinkingOff, Channel: p.Channel, Temporary: p.Temporary}
	if p.Temporary && p.UseContext != nil {
		c.UseContext = *p.UseContext
	}
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

// DeleteConversation 删除对话及其消息与附件。记忆不受影响（记忆属于用户，在所有对话间共享）。
func (s *Service) DeleteConversation(ctx context.Context, u *store.User, id uint64) error {
	atts, err := s.store.Attachments().ListByConversation(ctx, u.ID, id)
	if err != nil {
		return err
	}
	if err := s.store.Conversations().Delete(ctx, u.ID, id); err != nil {
		return err
	}
	// 对话中的附件与运行代码的工作目录随对话一起删除。
	if s.uploads != nil {
		if err := s.uploads.Remove(ctx, atts); err != nil {
			s.log.Error("删除对话附件失败", "user_id", u.ID, "conversation_id", id, "err", err)
		}
		s.uploads.RemoveWorkspace(u.ID, id)
	}
	return nil
}

// TemporaryIdle 是临时对话闲置多久后自动删除（用户关掉网页没点「结束」时）。
const TemporaryIdle = 6 * time.Hour

// CleanupTemporary 删除闲置太久的临时对话，返回删了几个。
func (s *Service) CleanupTemporary(ctx context.Context) int {
	stale, err := s.store.Conversations().StaleTemporary(ctx, s.now().Add(-TemporaryIdle))
	if err != nil {
		s.log.Error("查找过期的临时对话失败", "err", err)
		return 0
	}
	n := 0
	for _, c := range stale {
		if err := s.DeleteConversation(ctx, &store.User{ID: c.UserID}, c.ID); err != nil {
			s.log.Error("删除过期的临时对话失败", "conversation_id", c.ID, "err", err)
			continue
		}
		n++
	}
	return n
}

// EmptyIdle 是一条消息都没有的对话放多久后自动删除（以前点「新对话」就会建一个，没聊也一直留着）。
const EmptyIdle = 24 * time.Hour

// CleanupEmpty 删除放了很久、一条消息都没有的网页对话，返回删了几个。
func (s *Service) CleanupEmpty(ctx context.Context) int {
	stale, err := s.store.Conversations().StaleEmpty(ctx, s.now().Add(-EmptyIdle))
	if err != nil {
		s.log.Error("查找空对话失败", "err", err)
		return 0
	}
	n := 0
	for _, c := range stale {
		if err := s.DeleteConversation(ctx, &store.User{ID: c.UserID}, c.ID); err != nil {
			s.log.Error("删除空对话失败", "conversation_id", c.ID, "err", err)
			continue
		}
		n++
	}
	return n
}

// RunCleanup 每 30 分钟清理一次闲置的临时对话和放了很久的空对话，直到 ctx 取消。
func (s *Service) RunCleanup(ctx context.Context) {
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for {
		s.CleanupTemporary(ctx)
		s.CleanupEmpty(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
