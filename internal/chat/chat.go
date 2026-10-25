// Package chat 编排一轮对话：
// 确定对话与模型 → 限额检查 → 保存用户消息 → 检索记忆 → 组装提示词 → 调用模型（流式）→
// 保存回复 → 记录用量与记忆使用 → 异步写入新记忆。
package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/memory"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/prompt"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// MaxMessageRunes 是单条用户消息的最大长度。
const MaxMessageRunes = 4000

// 输入错误。
var (
	ErrEmptyMessage    = errors.New("消息不能为空")
	ErrMessageTooLong  = fmt.Errorf("消息不能超过 %d 个字", MaxMessageRunes)
	ErrInvalidThinking = errors.New("思考等级无效（可选 off、low、medium、high）")
	ErrModelNotFound   = errors.New("模型不存在或无权使用")
	ErrTitleTooLong    = errors.New("对话标题最多 60 个字")
)

// Sink 接收一轮对话过程中的事件。任一方法返回错误（例如客户端断开）都会中止生成，
// 已生成的部分仍会保存。
type Sink interface {
	// Start 在用户消息与助手消息占位创建后调用。
	Start(conv *store.Conversation, userMsg, assistantMsg *store.Message, model *models.Resolved) error
	// Memories 报告本轮使用的记忆。
	Memories(used []memory.Scored) error
	// Reasoning 推送一段思考过程（开启思考且模型返回思考内容时）。
	Reasoning(text string) error
	// Delta 推送一段增量文本。
	Delta(text string) error
}

// ModelResolver 决定一轮对话使用的模型。
type ModelResolver interface {
	Resolve(ctx context.Context, u models.User, modelID *uint64) (*models.Resolved, error)
	Visible(ctx context.Context, u models.User, modelID uint64) (bool, error)
}

// SendInput 是发送消息的参数。
type SendInput struct {
	// ConversationID 为 0 时使用最近的对话（没有则新建）。
	ConversationID uint64
	Content        string
}

// Result 是一轮对话的结果。
type Result struct {
	Conversation *store.Conversation
	UserMessage  *store.Message
	Reply        *store.Message
	Memories     []memory.Scored
	Usage        llm.Usage
	Model        *models.Resolved
}

// Service 是对话服务。
type Service struct {
	store     store.Store
	models    ModelResolver
	retriever *memory.Retriever
	writer    *memory.Writer
	builder   *prompt.Builder
	quota     *quota.Limiter
	cfg       config.ChatConfig
	loc       *time.Location
	log       *slog.Logger
	now       func() time.Time
}

// Deps 是对话服务的依赖。
type Deps struct {
	Store     store.Store
	Models    ModelResolver
	Retriever *memory.Retriever
	Writer    *memory.Writer
	Builder   *prompt.Builder
	Quota     *quota.Limiter
	Config    config.ChatConfig
	Location  *time.Location
	Logger    *slog.Logger
}

// New 创建对话服务。
func New(d Deps) *Service {
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		store: d.Store, models: d.Models, retriever: d.Retriever, writer: d.Writer, builder: d.Builder,
		quota: d.Quota, cfg: d.Config, loc: d.Location, log: log.With("component", "chat"), now: time.Now,
	}
}

func modelUser(u *store.User) models.User { return models.User{ID: u.ID, IsAdmin: u.IsAdmin} }

func validate(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", ErrEmptyMessage
	}
	if utf8.RuneCountInString(content) > MaxMessageRunes {
		return "", ErrMessageTooLong
	}
	return content, nil
}

// conversation 返回指定的对话；id 为 0 时返回最近的对话。
func (s *Service) conversation(ctx context.Context, u *store.User, id uint64) (*store.Conversation, error) {
	if id == 0 {
		return s.store.Conversations().Primary(ctx, u.ID)
	}
	return s.store.Conversations().Get(ctx, u.ID, id)
}

// prepared 是生成回复前准备好的上下文。
type prepared struct {
	conv    *store.Conversation
	history []store.Message
	prompt  prompt.Prompt
}

func (s *Service) prepare(ctx context.Context, u *store.User, conv *store.Conversation, content string) (*prepared, error) {
	history, err := s.store.Messages().Recent(ctx, u.ID, conv.ID, s.cfg.HistoryMessages)
	if err != nil {
		return nil, fmt.Errorf("读取对话历史: %w", err)
	}
	mems := s.retrieve(ctx, u.ID, content, history)
	p := s.builder.Build(prompt.Input{
		UserName:    u.DisplayName,
		Now:         s.now().In(s.loc),
		Memories:    mems,
		History:     history,
		UserMessage: content,
	})
	return &prepared{conv: conv, history: history, prompt: p}, nil
}

// retrieve 检索相关记忆。检索失败（例如向量服务不可用）不影响聊天，只记录告警。
func (s *Service) retrieve(ctx context.Context, userID uint64, content string, history []store.Message) []memory.Scored {
	query := content
	// 很短的消息（"然后呢""是的"）单独检索意义不大，带上上一句作为语境。
	if utf8.RuneCountInString(content) < 8 && len(history) > 0 {
		query = history[len(history)-1].Content + "\n" + content
	}
	mems, err := s.retriever.Retrieve(ctx, userID, query)
	if err != nil {
		s.log.Warn("检索记忆失败，本轮不使用记忆", "user_id", userID, "err", err)
		return nil
	}
	return mems
}

// Preview 组装本轮会发送给模型的提示词，但不调用模型、不保存任何数据（调试用）。
func (s *Service) Preview(ctx context.Context, u *store.User, convID uint64, content string) (prompt.Prompt, error) {
	content, err := validate(content)
	if err != nil {
		return prompt.Prompt{}, err
	}
	conv, err := s.conversation(ctx, u, convID)
	if err != nil {
		return prompt.Prompt{}, err
	}
	p, err := s.prepare(ctx, u, conv, content)
	if err != nil {
		return prompt.Prompt{}, err
	}
	return p.prompt, nil
}

// Send 处理一轮对话。
func (s *Service) Send(ctx context.Context, u *store.User, in SendInput, sink Sink) (*Result, error) {
	content, err := validate(in.Content)
	if err != nil {
		return nil, err
	}
	conv, err := s.conversation(ctx, u, in.ConversationID)
	if err != nil {
		return nil, err
	}
	model, err := s.models.Resolve(ctx, modelUser(u), conv.ModelID)
	if err != nil {
		return nil, err
	}
	// 用户自己的 API Key 不计入每日限额。
	if !model.Personal {
		if err := s.quota.Check(ctx, u); err != nil {
			return nil, err
		}
	}
	p, err := s.prepare(ctx, u, conv, content)
	if err != nil {
		return nil, err
	}

	userMsg := &store.Message{ConversationID: conv.ID, UserID: u.ID, Role: store.RoleUser, Content: content}
	if err := s.store.Messages().Create(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("保存消息: %w", err)
	}
	s.autoTitle(ctx, u, conv, content)
	modelID := model.ID
	// 助手消息先以"中断"状态占位，正常结束后再更新为完成。
	reply := &store.Message{ConversationID: conv.ID, UserID: u.ID, Role: store.RoleAssistant,
		Model: model.ModelFor(conv.Thinking), ModelID: &modelID, Status: store.MessageInterrupted}
	if err := s.store.Messages().Create(ctx, reply); err != nil {
		return nil, fmt.Errorf("保存消息: %w", err)
	}
	res := &Result{Conversation: conv, UserMessage: userMsg, Reply: reply, Memories: p.prompt.UsedMemories, Model: model}

	// 先打开模型的流式连接再开始推送：模型不可用时，调用方仍可返回普通错误响应。
	var text, reasoning strings.Builder
	var usage *llm.Usage
	stream, genErr := model.Chat.ChatStream(ctx, llm.ChatRequest{Messages: p.prompt.Messages, Thinking: conv.Thinking})
	if genErr == nil {
		defer stream.Close()
		genErr = sink.Start(conv, userMsg, reply, model)
		if genErr == nil {
			genErr = sink.Memories(p.prompt.UsedMemories)
		}
		if genErr == nil {
			usage, genErr = s.consume(stream, &text, &reasoning, sink)
		}
	}

	// 无论成功与否都要落库，使用不随请求取消的 context。
	bg := context.WithoutCancel(ctx)
	reply.Content, reply.Reasoning = text.String(), reasoning.String()
	switch {
	case genErr == nil:
		reply.Status = store.MessageComplete
	case text.Len() > 0 || isCanceled(ctx, genErr):
		reply.Status = store.MessageInterrupted
	default:
		reply.Status = store.MessageError
	}
	if usage != nil {
		res.Usage = *usage
	} else {
		// 部分服务的流式输出不返回用量，按估算值计入限额。
		res.Usage = llm.Usage{PromptTokens: p.prompt.EstimatedTokens,
			CompletionTokens: prompt.EstimateTokens(reply.Content) + prompt.EstimateTokens(reply.Reasoning)}
	}
	reply.PromptTokens, reply.CompletionTokens = res.Usage.PromptTokens, res.Usage.CompletionTokens
	if err := s.store.Messages().Finish(bg, reply); err != nil {
		return res, errors.Join(genErr, fmt.Errorf("保存回复: %w", err))
	}
	s.afterReply(bg, u, p, model, userMsg, reply, res.Usage)
	if genErr != nil {
		return res, genErr
	}
	return res, nil
}

// autoTitle 用第一条消息为未命名的对话生成标题。
func (s *Service) autoTitle(ctx context.Context, u *store.User, conv *store.Conversation, content string) {
	if conv.Title != "" {
		return
	}
	title := strings.Join(strings.Fields(content), " ")
	if r := []rune(title); len(r) > 24 {
		title = string(r[:24]) + "…"
	}
	if err := s.store.Conversations().Update(ctx, u.ID, conv.ID, map[string]any{"title": title}); err != nil {
		s.log.Warn("生成对话标题失败", "err", err)
		return
	}
	conv.Title = title
}

func isCanceled(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled)
}

func (s *Service) consume(stream llm.ChatStream, text, reasoning *strings.Builder, sink Sink) (*llm.Usage, error) {
	var usage *llm.Usage
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if text.Len() == 0 {
				return usage, errors.New("模型返回了空回复")
			}
			return usage, nil
		}
		if err != nil {
			return usage, err
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if chunk.Reasoning != "" {
			if err := sink.Reasoning(chunk.Reasoning); err != nil {
				return usage, err
			}
			reasoning.WriteString(chunk.Reasoning)
		}
		if chunk.Delta == "" {
			continue
		}
		// 只保存已成功推送给用户的内容。
		if err := sink.Delta(chunk.Delta); err != nil {
			return usage, err
		}
		text.WriteString(chunk.Delta)
	}
}

// afterReply 记录用量、记忆使用，并提交记忆写入任务。这些步骤失败只记录日志。
func (s *Service) afterReply(ctx context.Context, u *store.User, p *prepared, model *models.Resolved, userMsg, reply *store.Message, usage llm.Usage) {
	if !model.Personal {
		if err := s.quota.Record(ctx, u.ID, 1, usage.PromptTokens, usage.CompletionTokens); err != nil {
			s.log.Error("记录用量失败", "user_id", u.ID, "err", err)
		}
	}
	if err := s.store.Conversations().Touch(ctx, u.ID, p.conv.ID, reply.CreatedAt); err != nil {
		s.log.Error("更新对话时间失败", "user_id", u.ID, "err", err)
	}
	if reply.Content == "" {
		return
	}
	if err := s.retriever.MarkUsed(ctx, u.ID, reply.ID, p.prompt.UsedMemories); err != nil {
		s.log.Error("记录记忆使用失败", "user_id", u.ID, "err", err)
	}
	if reply.Status != store.MessageComplete {
		return
	}
	known := make([]string, 0, len(p.prompt.UsedMemories))
	for _, m := range p.prompt.UsedMemories {
		known = append(known, m.Memory.Content)
	}
	var recent []llm.Message
	if n := len(p.history); n > 0 {
		for _, m := range p.history[max(0, n-4):] {
			recent = append(recent, llm.Message{Role: m.Role, Content: m.Content})
		}
	}
	// 思考过程不参与记忆提取，只用正式回复。
	s.writer.Enqueue(memory.WriteJob{
		UserID: u.ID, ConversationID: p.conv.ID, MessageID: userMsg.ID,
		Chat: model.Chat, Personal: model.Personal,
		Input: memory.ExtractInput{
			UserName: u.DisplayName, UserMessage: userMsg.Content, AssistantReply: reply.Content,
			Context: recent, Known: known, Now: s.now().In(s.loc),
		},
	})
}

// History 分页读取对话的消息（按时间正序）。convID 为 0 时读取最近的对话。
func (s *Service) History(ctx context.Context, u *store.User, convID, beforeID uint64, limit int) (*store.Conversation, []store.Message, error) {
	conv, err := s.conversation(ctx, u, convID)
	if err != nil {
		return nil, nil, err
	}
	msgs, err := s.store.Messages().Before(ctx, u.ID, conv.ID, beforeID, limit)
	if err != nil {
		return nil, nil, err
	}
	return conv, msgs, nil
}
