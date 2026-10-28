package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/memory"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/prompt"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// ProactiveResult 是一条主动消息。
type ProactiveResult struct {
	Conversation *store.Conversation
	Message      *store.Message
	// Tools 是执行定时任务时调用的工具（其中生成的图片、文件要一起发出去）。
	Tools []ToolEvent
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
		if err := s.quota.Record(ctx, u.ID, quota.Tokens(quota.ModelKey(model.ID), resp.Usage)); err != nil {
			s.log.Error("记录用量失败", "user_id", u.ID, "err", err)
		}
	}
	if text == "" || (allowSkip && strings.Contains(text, skipToken)) {
		return nil, ErrNothingToSay
	}
	modelID := model.ID
	msg := &store.Message{ConversationID: conv.ID, UserID: u.ID, Role: store.RoleAssistant, Content: text,
		Model: model.ModelFor("off"), ModelID: &modelID, Status: store.MessageComplete,
		PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens, CachedTokens: cachedOf(resp.Usage)}
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

// RunTask 执行一个定时任务：模型按任务说明去做（需要时调用工具），把结果作为一条主动消息写进用户最近聊天的对话。
func (s *Service) RunTask(ctx context.Context, u *store.User, task string) (*ProactiveResult, error) {
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
	toolset := s.toolSet(ctx, u, conv, model, nil)
	var mood string
	if s.emotion != nil {
		mood, _ = s.emotion.Peek(ctx, u.ID)
	}
	now := time.Now().In(s.loc)
	instruction := "（这不是对方发来的消息，对方现在没有说话。对方之前让你定时做一件事，现在到时间了（" + now.Format("01-02 15:04 Mon") + "）：「" + task + "」。" +
		"现在就去做：需要最新信息时先用工具搜索、读网页，需要计算或处理数据时运行代码，不要凭印象编。" +
		"做完后主动把结果发给对方：像朋友在微信上那样说，简洁、挑重点；不要提「定时任务」「系统」这类词，也不要假装对方刚说过什么。）"
	p, err := s.prepare(ctx, u, conv, turnInput{content: instruction, model: model, toolHints: toolHints(toolset), emotion: mood})
	if err != nil {
		return nil, err
	}
	modelID := model.ID
	reply := &store.Message{ConversationID: conv.ID, UserID: u.ID, Role: store.RoleAssistant,
		Model: model.ModelFor(store.ThinkingOff), ModelID: &modelID, Status: store.MessageInterrupted}
	if err := s.store.Messages().Create(ctx, reply); err != nil {
		return nil, fmt.Errorf("保存消息: %w", err)
	}
	t := &turn{s: s, ctx: ctx, model: model, thinking: store.ThinkingOff, tools: toolset, sink: discardSink{}, msgs: p.prompt.Messages,
		userID: u.ID, convID: conv.ID, replyID: reply.ID}
	genErr := t.run(func() error { return nil })

	bg := context.WithoutCancel(ctx)
	reply.Content = stripToolNotes(strings.TrimSpace(t.text.String()))
	if len(t.records) > 0 {
		b, _ := json.Marshal(t.records)
		reply.ToolLog = string(b)
	}
	usage := t.usage
	if !t.usageReported {
		usage = llm.Usage{PromptTokens: p.prompt.EstimatedTokens*max(1, t.rounds) + t.toolTokens, CompletionTokens: prompt.EstimateTokens(reply.Content)}
	}
	reply.PromptTokens, reply.CompletionTokens, reply.CachedTokens = usage.PromptTokens, usage.CompletionTokens, cachedOf(usage)
	if !model.Personal {
		if err := s.quota.Record(bg, u.ID, quota.Tokens(quota.ModelKey(model.ID), usage)); err != nil {
			s.log.Error("记录用量失败", "user_id", u.ID, "err", err)
		}
	}
	if genErr != nil || reply.Content == "" {
		// 没做成：删掉占位消息和已经生成的图片、文件。
		var ids []uint64
		for _, ev := range t.records {
			for _, f := range append(append([]ToolImage{}, ev.Images...), ev.Files...) {
				ids = append(ids, f.ID)
			}
		}
		if len(ids) > 0 && s.uploads != nil {
			_, _ = s.uploads.Delete(bg, u.ID, ids)
		}
		_ = s.store.Messages().Delete(bg, u.ID, reply.ID)
		if genErr == nil {
			genErr = errors.New("模型返回了空回复")
		}
		return nil, genErr
	}
	reply.Status = store.MessageComplete
	if err := s.store.Messages().Finish(bg, reply); err != nil {
		return nil, fmt.Errorf("保存回复: %w", err)
	}
	if err := s.store.Conversations().Touch(bg, u.ID, conv.ID, reply.CreatedAt); err != nil {
		s.log.Warn("更新对话时间失败", "err", err)
	}
	if err := s.store.Users().MarkProactive(bg, u.ID, reply.CreatedAt); err != nil {
		s.log.Warn("记录主动消息时间失败", "err", err)
	}
	return &ProactiveResult{Conversation: conv, Message: reply, Tools: t.records}, nil
}

// discardSink 丢弃生成过程（定时任务在后台执行，没有人在看）。
type discardSink struct{}

func (discardSink) Start(*store.Conversation, *store.Message, *store.Message, *models.Resolved) error {
	return nil
}
func (discardSink) Memories([]memory.Scored) error { return nil }
func (discardSink) Reasoning(string) error         { return nil }
func (discardSink) Delta(string) error             { return nil }
func (discardSink) Tool(ToolEvent) error           { return nil }

// latestConversation 返回用户最近说过话的对话，没有时使用最近的对话（没有则新建）。
func (s *Service) latestConversation(ctx context.Context, u *store.User) (*store.Conversation, error) {
	if m, err := s.store.Messages().LastUserMessage(ctx, u.ID); err == nil {
		if conv, err := s.store.Conversations().Get(ctx, u.ID, m.ConversationID); err == nil {
			return conv, nil
		}
	}
	return s.store.Conversations().Primary(ctx, u.ID)
}
