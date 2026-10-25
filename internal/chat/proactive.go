package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// ProactiveResult 是一条主动消息。
type ProactiveResult struct {
	Conversation *store.Conversation
	Message      *store.Message
}

// ErrNothingToSay 表示模型觉得这次不必发消息。
var ErrNothingToSay = errors.New("这次不用发消息")

// skipToken 是模型决定不发消息时输出的内容。
const skipToken = "[SKIP]"

// Proactive 让布谷主动给用户发一条消息（到点提醒、主动关心），写进用户最近聊天的那段对话。
// reason 说明为什么要发；allowSkip 为 true 时模型可以输出 [SKIP] 表示这次不发。
func (s *Service) Proactive(ctx context.Context, u *store.User, reason string, allowSkip bool) (*ProactiveResult, error) {
	conv, err := s.latestConversation(ctx, u)
	if err != nil {
		return nil, err
	}
	model, err := s.models.Resolve(ctx, modelUser(u), conv.ModelID)
	if err != nil {
		return nil, err
	}
	if !model.Personal {
		if err := s.quota.Check(ctx, u); err != nil {
			return nil, err
		}
	}
	var mood string
	if s.emotion != nil {
		mood, _ = s.emotion.Peek(ctx, u.ID)
	}
	instruction := "（这不是对方发来的消息，对方现在没有说话。" + reason +
		" 现在由你主动给对方发消息：就像朋友在微信上突然想起对方那样，自然、简短，一两句话；" +
		"不要提「提醒功能」「系统」「定时」这类词，也不要假装对方刚说过什么。"
	if allowSkip {
		instruction += "如果觉得现在不适合打扰对方，只输出 " + skipToken + "。"
	}
	instruction += "）"
	p, err := s.prepare(ctx, u, conv, turnInput{content: instruction, model: model, emotion: mood})
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	resp, err := model.Chat.Chat(cctx, llm.ChatRequest{Messages: p.prompt.Messages, Thinking: "off"})
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(resp.Content)
	if !model.Personal {
		if err := s.quota.Record(ctx, u.ID, 0, resp.Usage.PromptTokens, resp.Usage.CompletionTokens); err != nil {
			s.log.Error("记录用量失败", "user_id", u.ID, "err", err)
		}
	}
	if text == "" || (allowSkip && strings.Contains(text, skipToken)) {
		return nil, ErrNothingToSay
	}
	modelID := model.ID
	msg := &store.Message{ConversationID: conv.ID, UserID: u.ID, Role: store.RoleAssistant, Content: text,
		Model: model.ModelFor("off"), ModelID: &modelID, Status: store.MessageComplete,
		PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens}
	if err := s.store.Messages().Create(ctx, msg); err != nil {
		return nil, fmt.Errorf("保存消息: %w", err)
	}
	if err := s.store.Conversations().Touch(ctx, u.ID, conv.ID, msg.CreatedAt); err != nil {
		s.log.Warn("更新对话时间失败", "err", err)
	}
	if err := s.store.Users().MarkProactive(ctx, u.ID, msg.CreatedAt); err != nil {
		s.log.Warn("记录主动消息时间失败", "err", err)
	}
	return &ProactiveResult{Conversation: conv, Message: msg}, nil
}

// latestConversation 返回用户最近说过话的对话，没有时使用最近的对话（没有则新建）。
func (s *Service) latestConversation(ctx context.Context, u *store.User) (*store.Conversation, error) {
	if m, err := s.store.Messages().LastUserMessage(ctx, u.ID); err == nil {
		if conv, err := s.store.Conversations().Get(ctx, u.ID, m.ConversationID); err == nil {
			return conv, nil
		}
	}
	return s.store.Conversations().Primary(ctx, u.ID)
}
