// Package chat 编排一轮对话：
// 确定对话与模型 → 限额检查 → 处理附件 → 保存用户消息 → 检索记忆 → 组装提示词 →
// 调用模型（流式，可多轮调用工具）→ 保存回复 → 记录用量与记忆使用 → 异步写入新记忆。
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/HugehardJiang/buugoo/internal/logx"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/memory"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/persona"
	"github.com/HugehardJiang/buugoo/internal/prompt"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/tools"
	"github.com/HugehardJiang/buugoo/internal/uploads"
)

// MaxMessageRunes 是单条用户消息的最大长度。
const MaxMessageRunes = 4000

// maxToolRounds 是一轮回复中最多调用工具的轮数，到达后要求模型直接回答。
const maxToolRounds = 8

// 输入错误。
var (
	ErrEmptyMessage       = errors.New("消息不能为空")
	ErrMessageTooLong     = fmt.Errorf("消息不能超过 %d 个字", MaxMessageRunes)
	ErrInvalidThinking    = errors.New("思考等级无效（可选 off、low、medium、high）")
	ErrModelNotFound      = errors.New("模型不存在或无权使用")
	ErrTitleTooLong       = errors.New("对话标题最多 60 个字")
	ErrAttachmentNotFound = errors.New("附件不存在或已经发送过，请重新上传")
	ErrTooManyFiles       = errors.New("附带的文件太多")
	ErrUploadsDisabled    = errors.New("当前不支持上传文件")
	ErrImagesUnsupported  = errors.New("当前模型不能看图，也没有可以帮忙看图的模型。请在输入框下方换一个支持图片的模型，或在「设置 → 模型」中添加一个")
)

// ToolEvent 是一次工具调用的进展，推送给前端，并作为工具记录保存在回复中。
type ToolEvent struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Label   string `json:"label"`
	Status  string `json:"status"` // running | done | error
	Input   string `json:"input,omitempty"`
	Summary string `json:"summary,omitempty"`
	Detail  string `json:"detail,omitempty"`
	// Images 是代码生成的图片（已保存为回复的附件），显示在回复中。
	Images []ToolImage `json:"images,omitempty"`
	// Files 是工具发给用户的文件（已保存为回复的附件），显示为可以下载的文件。
	Files []ToolImage `json:"files,omitempty"`
}

// ToolImage 是工具生成并保存好的一张图片，字段与附件一致，前端按附件展示。
type ToolImage struct {
	ID       uint64 `json:"id"`
	Kind     string `json:"kind"`
	Filename string `json:"filename"`
	MIME     string `json:"mime"`
	Size     int64  `json:"size"`
	// Deleted 表示用户已经在文件管理中删除了这张图片（展示历史消息时填写）。
	Deleted bool `json:"deleted,omitempty"`
}

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
	// Tool 报告工具调用的开始与结果。
	Tool(ev ToolEvent) error
}

// ModelResolver 决定一轮对话使用的模型。
type ModelResolver interface {
	Resolve(ctx context.Context, u models.User, modelID *uint64) (*models.Resolved, error)
	Visible(ctx context.Context, u models.User, modelID uint64) (bool, error)
	// VisionHelper 返回能看图的模型，用于在当前模型不能看图时把图片转述成文字。
	VisionHelper(ctx context.Context, u models.User) (*models.Resolved, error)
}

// ToolProvider 为一轮对话提供工具。返回 nil 表示没有工具。
type ToolProvider interface {
	// pending 是本轮随消息发送、还没有关联到对话的附件（运行代码时也要能读到）。
	For(ctx context.Context, u *store.User, conv *store.Conversation, model *models.Resolved, pending []store.Attachment) *tools.Set
}

// EmotionProvider 提供布谷当前的心情（情绪系统）。
type EmotionProvider interface {
	// Before 在生成回复前更新情绪（时间消退、久别重逢、催促等）并返回心情说明。
	Before(ctx context.Context, userID uint64, content string) (string, error)
	// Peek 返回当前的心情说明，不修改状态。
	Peek(ctx context.Context, userID uint64) (string, error)
}

// SendInput 是发送消息的参数。
type SendInput struct {
	// ConversationID 为 0 时使用最近的对话（没有则新建）。
	ConversationID uint64
	Content        string
	// AttachmentIDs 是随消息发送的、已上传的附件。
	AttachmentIDs []uint64
}

// Result 是一轮对话的结果。
type Result struct {
	Conversation *store.Conversation
	UserMessage  *store.Message
	Reply        *store.Message
	Attachments  []store.Attachment
	Memories     []memory.Scored
	Tools        []ToolEvent
	Usage        llm.Usage
	Model        *models.Resolved
}

// Service 是对话服务。
type Service struct {
	store     store.Store
	models    ModelResolver
	tools     ToolProvider
	emotion   EmotionProvider
	uploads   *uploads.Service
	retriever *memory.Retriever
	writer    *memory.Writer
	builder   *prompt.Builder
	quota     *quota.Limiter
	cfg       config.ChatConfig
	disabled  []string

	summarizer  Summarizer
	sumMu       sync.Mutex
	summarizing map[uint64]bool
	bg          sync.WaitGroup
	loc         *time.Location
	log         *slog.Logger
	now         func() time.Time
}

// Deps 是对话服务的依赖。Tools 与 Uploads 可以为空（不提供工具 / 不支持附件）。
type Deps struct {
	Store     store.Store
	Models    ModelResolver
	Tools     ToolProvider
	Emotion   EmotionProvider
	Uploads   *uploads.Service
	Retriever *memory.Retriever
	Writer    *memory.Writer
	Builder   *prompt.Builder
	Quota     *quota.Limiter
	Config    config.ChatConfig
	Location  *time.Location
	Logger    *slog.Logger
	// DisabledPresets 是管理员关掉的预设性格。
	DisabledPresets []string
	// Summarizer 整理长对话的摘要，为空时不整理（只保留最近的消息）。
	Summarizer Summarizer
}

// New 创建对话服务。
func New(d Deps) *Service {
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		store: d.Store, disabled: d.DisabledPresets, summarizer: d.Summarizer, summarizing: map[uint64]bool{}, models: d.Models, tools: d.Tools, emotion: d.Emotion, uploads: d.Uploads, retriever: d.Retriever, writer: d.Writer,
		builder: d.Builder, quota: d.Quota, cfg: d.Config, loc: d.Location, log: log.With("component", "chat"), now: time.Now,
	}
}

// sessionID 是发给模型服务的会话编号：同一段对话一直不变（OpenCode Go 用它做路由和提示词缓存）。
func sessionID(conv *store.Conversation) string {
	return fmt.Sprintf("buugoo-%d-%d", conv.UserID, conv.ID)
}

func modelUser(u *store.User) models.User {
	return models.User{ID: u.ID, IsAdmin: u.IsAdmin, Restricted: u.RestrictedModels}
}

func validate(content string, hasFiles bool) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" && !hasFiles {
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
	// userText 是放进提示词的本轮用户消息（含文档内容与图片说明）。
	userText string
	images   []llm.Image
}

// turnInput 是组装提示词所需的本轮信息。
type turnInput struct {
	content     string
	attachments []store.Attachment
	model       *models.Resolved
	toolHints   string
	emotion     string
	// skip 是不放进历史的消息（重新生成时的原消息）。
	skip []uint64
}

func (s *Service) prepare(ctx context.Context, u *store.User, conv *store.Conversation, in turnInput) (*prepared, error) {
	// 已经整理进摘要的消息不再原样放进提示词；之后的消息全部放进去，按模型的上下文长度预算（超出时舍弃最早的）。
	history, err := s.unsummarized(ctx, u, conv)
	if err != nil {
		return nil, fmt.Errorf("读取对话历史: %w", err)
	}
	if len(in.skip) > 0 {
		history = slices.DeleteFunc(history, func(m store.Message) bool { return slices.Contains(in.skip, m.ID) })
	}
	vision := in.model != nil && in.model.Vision
	history, histImages := s.renderHistory(ctx, u, withToolNotes(history), vision)
	_, budget := contextSize(in.model)
	userText, images, err := s.renderAttachments(in.content, in.attachments, vision, s.docChars())
	if err != nil {
		return nil, err
	}
	preset := persona.Find(u.PersonaPreset, s.disabled)
	extra := 0
	if preset != nil {
		extra = preset.MemoryExtra
	}
	var mems []memory.Scored
	if !isolated(conv) {
		mems = s.retrieve(ctx, u.ID, userText, history, extra)
	}
	p := s.builder.Build(prompt.Input{
		UserName:      u.DisplayName,
		Now:           s.now().In(s.loc),
		Memories:      mems,
		History:       history,
		UserMessage:   userText,
		UserImages:    images,
		HistoryImages: histImages,
		KeepReasoning: in.toolHints != "" && in.model != nil && in.model.ThinkingStyle == store.ThinkingStyleDeepSeek,
		ToolHints:     in.toolHints,
		Emotion:       in.emotion,
		Preset:        preset,
		Summary:       conv.Summary,
		MaxTokens:     budget,
		Channel:       conv.Channel,
		Temporary:     conv.Temporary,
		Isolated:      isolated(conv),
	})
	return &prepared{conv: conv, history: history, prompt: p, userText: userText, images: images}, nil
}

// isolated 报告这段对话是不是不参考记忆和心情的临时对话。
func isolated(conv *store.Conversation) bool { return conv.Temporary && !conv.UseContext }

// retrieve 检索相关记忆。检索失败（例如向量服务不可用）不影响聊天，只记录告警。
func (s *Service) retrieve(ctx context.Context, userID uint64, content string, history []store.Message, extra int) []memory.Scored {
	query := content
	// 很短的消息（"然后呢""是的"）单独检索意义不大，带上上一句作为语境。
	if utf8.RuneCountInString(content) < 8 && len(history) > 0 {
		query = history[len(history)-1].Content + "\n" + content
	}
	if r := []rune(query); len(r) > 2000 {
		query = string(r[:2000])
	}
	mems, err := s.retriever.RetrieveMore(ctx, userID, query, extra)
	if err != nil {
		s.log.Warn("检索记忆失败，本轮不使用记忆", "user_id", userID, "err", err)
		return nil
	}
	return mems
}

// Preview 组装本轮会发送给模型的提示词，但不调用模型、不保存任何数据（调试用）。
func (s *Service) Preview(ctx context.Context, u *store.User, convID uint64, content string) (prompt.Prompt, error) {
	content, err := validate(content, false)
	if err != nil {
		return prompt.Prompt{}, err
	}
	conv, err := s.conversation(ctx, u, convID)
	if err != nil {
		return prompt.Prompt{}, err
	}
	model, _ := s.models.Resolve(ctx, modelUser(u), conv.ModelID)
	in := turnInput{content: content, model: model}
	if model != nil {
		in.toolHints = toolHints(s.toolSet(ctx, u, conv, model, nil))
	}
	if s.emotion != nil && !isolated(conv) {
		in.emotion, _ = s.emotion.Peek(ctx, u.ID)
	}
	p, err := s.prepare(ctx, u, conv, in)
	if err != nil {
		return prompt.Prompt{}, err
	}
	return p.prompt, nil
}

func (s *Service) toolSet(ctx context.Context, u *store.User, conv *store.Conversation, model *models.Resolved, pending []store.Attachment) *tools.Set {
	if s.tools == nil || model == nil || !model.Tools {
		return nil
	}
	return s.tools.For(ctx, u, conv, model, pending)
}

// Send 处理一轮对话。
func (s *Service) Send(ctx context.Context, u *store.User, in SendInput, sink Sink) (*Result, error) {
	content, err := validate(in.Content, len(in.AttachmentIDs) > 0)
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
	atts, err := s.pendingAttachments(ctx, u, in.AttachmentIDs)
	if err != nil {
		return nil, err
	}
	return s.respond(ctx, u, conv, model, content, atts, nil, sink)
}

// regen 是重新生成时的原消息：用户那条保留，原来的回复删掉换成新的。
type regen struct {
	user     *store.Message
	oldReply uint64
}

// ErrCannotRegenerate 表示最后一条不是可以重新生成的回复。
var ErrCannotRegenerate = errors.New("只能重新生成最后一条回复")

// Regenerate 重新生成对话里最后一条回复：用同一条用户消息再回答一次，原来的回复删掉。
// 用户消息之前已经提取过记忆、更新过情绪，这次不再重复。
func (s *Service) Regenerate(ctx context.Context, u *store.User, convID uint64, sink Sink) (*Result, error) {
	conv, err := s.conversation(ctx, u, convID)
	if err != nil {
		return nil, err
	}
	last, err := s.store.Messages().Recent(ctx, u.ID, conv.ID, 2)
	if err != nil {
		return nil, err
	}
	if len(last) != 2 || last[0].Role != store.RoleUser || last[1].Role != store.RoleAssistant || last[1].ID <= conv.SummaryUntil {
		return nil, ErrCannotRegenerate
	}
	userMsg := last[0]
	model, err := s.models.Resolve(ctx, modelUser(u), conv.ModelID)
	if err != nil {
		return nil, err
	}
	if !model.Personal {
		if err := s.quota.Check(ctx, u); err != nil {
			return nil, err
		}
	}
	atts, err := s.store.Attachments().ListByMessages(ctx, u.ID, []uint64{userMsg.ID})
	if err != nil {
		return nil, err
	}
	return s.respond(ctx, u, conv, model, userMsg.Content, atts, &regen{user: &userMsg, oldReply: last[1].ID}, sink)
}

// DeleteMessage 删除对话里的一条消息（用户的或布谷的）。已经提取的记忆和发过的文件保留。
func (s *Service) DeleteMessage(ctx context.Context, u *store.User, convID, msgID uint64) error {
	conv, err := s.conversation(ctx, u, convID)
	if err != nil {
		return err
	}
	got, err := s.store.Messages().Before(ctx, u.ID, conv.ID, msgID+1, 1)
	if err != nil {
		return err
	}
	if len(got) == 0 || got[0].ID != msgID {
		return store.ErrNotFound
	}
	return s.store.Messages().Delete(ctx, u.ID, msgID)
}

// respond 生成一轮回复。again 不为空时是重新生成：不再保存用户消息，原来的回复删掉。
func (s *Service) respond(ctx context.Context, u *store.User, conv *store.Conversation, model *models.Resolved,
	content string, atts []store.Attachment, again *regen, sink Sink) (*Result, error) {
	var err error
	ctx = llm.WithSession(ctx, sessionID(conv))
	// 上一轮用的上下文超过了阈值：先压缩，再处理这条消息。压缩失败不影响聊天（超出预算时会舍弃最早的消息）。
	if conv.CompactPending && s.summarizer != nil {
		if err := s.compact(ctx, u, conv, model); err != nil && !errors.Is(err, ErrNothingToCompact) && !errors.Is(err, ErrCompacting) {
			s.log.Warn("自动压缩上下文失败", "user_id", u.ID, "conversation_id", conv.ID, "err", err)
		}
	}
	if err := s.describeImages(ctx, u, model, atts); err != nil {
		return nil, err
	}
	toolset := s.toolSet(ctx, u, conv, model, atts)
	var mood string
	if s.emotion != nil && !isolated(conv) {
		// 情绪更新失败不影响聊天。重新生成时这条消息已经影响过情绪，只读取现在的；
		// 临时对话只参考现在的心情，不改变它。
		if again != nil || conv.Temporary {
			mood, err = s.emotion.Peek(ctx, u.ID)
		} else {
			mood, err = s.emotion.Before(ctx, u.ID, titleSource(content, atts))
		}
		if err != nil {
			s.log.Warn("更新情绪失败", "user_id", u.ID, "err", err)
		}
	}
	in := turnInput{content: content, attachments: atts, model: model, toolHints: toolHints(toolset), emotion: mood}
	if again != nil {
		in.skip = []uint64{again.user.ID, again.oldReply}
	}
	p, err := s.prepare(ctx, u, conv, in)
	if err != nil {
		return nil, err
	}

	userMsg := &store.Message{ConversationID: conv.ID, UserID: u.ID, Role: store.RoleUser, Content: content}
	if again != nil {
		userMsg = again.user
		if err := s.store.Messages().Delete(ctx, u.ID, again.oldReply); err != nil {
			return nil, fmt.Errorf("删除原来的回复: %w", err)
		}
	} else {
		if err := s.store.Messages().Create(ctx, userMsg); err != nil {
			return nil, fmt.Errorf("保存消息: %w", err)
		}
		if len(atts) > 0 {
			if err := s.store.Attachments().Attach(ctx, u.ID, attachmentIDs(atts), conv.ID, userMsg.ID); err != nil {
				return nil, fmt.Errorf("关联附件: %w", err)
			}
		}
		if !conv.Temporary {
			s.autoTitle(ctx, u, conv, titleSource(content, atts))
		}
	}
	modelID := model.ID
	// 助手消息先以"中断"状态占位，正常结束后再更新为完成。
	reply := &store.Message{ConversationID: conv.ID, UserID: u.ID, Role: store.RoleAssistant,
		Model: model.ModelFor(conv.Thinking), ModelID: &modelID, Status: store.MessageInterrupted}
	if err := s.store.Messages().Create(ctx, reply); err != nil {
		return nil, fmt.Errorf("保存消息: %w", err)
	}
	res := &Result{Conversation: conv, UserMessage: userMsg, Reply: reply, Attachments: atts, Memories: p.prompt.UsedMemories, Model: model}

	t := &turn{s: s, ctx: ctx, model: model, thinking: conv.Thinking, tools: toolset, sink: sink, msgs: p.prompt.Messages,
		userID: u.ID, convID: conv.ID, replyID: reply.ID, userText: content}
	genErr := t.run(func() error {
		if err := sink.Start(conv, userMsg, reply, model); err != nil {
			return err
		}
		return sink.Memories(p.prompt.UsedMemories)
	})

	// 无论成功与否都要落库，使用不随请求取消的 context。
	bg := context.WithoutCancel(ctx)
	reply.Content, reply.Reasoning = stripToolNotes(t.text.String()), t.reasoning.String()
	res.Tools = t.records
	if len(t.records) > 0 {
		b, _ := json.Marshal(t.records)
		reply.ToolLog = string(b)
	}
	switch {
	case genErr == nil:
		reply.Status = store.MessageComplete
	case t.text.Len() > 0 || isCanceled(ctx, genErr):
		reply.Status = store.MessageInterrupted
	default:
		reply.Status = store.MessageError
	}
	if genErr != nil && !isCanceled(ctx, genErr) {
		// 记下失败原因，刷新页面后也能看到（密钥打码）。
		reply.Error = clip(logx.Redact(genErr.Error()), 1000)
	}
	if t.usageReported {
		res.Usage = t.usage
	} else {
		// 部分服务的流式输出不返回用量，按估算值计入限额。
		res.Usage = llm.Usage{PromptTokens: p.prompt.EstimatedTokens*max(1, t.rounds) + t.toolTokens,
			CompletionTokens: prompt.EstimateTokens(reply.Content) + prompt.EstimateTokens(reply.Reasoning)}
	}
	reply.PromptTokens, reply.CompletionTokens, reply.CachedTokens = res.Usage.PromptTokens, res.Usage.CompletionTokens, cachedOf(res.Usage)
	if err := s.store.Messages().Finish(bg, reply); err != nil {
		return res, errors.Join(genErr, fmt.Errorf("保存回复: %w", err))
	}
	if reply.Content != "" {
		// 这一轮大约用了多少上下文：提示词 + 工具结果 + 回复。超过阈值时下一条消息前先压缩。
		s.recordContext(bg, u, conv, model, p.prompt.EstimatedTokens+t.toolTokens+prompt.EstimateTokens(reply.Content), p.prompt.DroppedHistory > 0)
	}
	s.afterReply(bg, u, p, model, userMsg, reply, atts, res.Usage, again == nil && !conv.Temporary)
	if genErr != nil {
		return res, genErr
	}
	return res, nil
}

// turn 是一轮回复的生成过程：调用模型，模型要求调用工具时执行工具并把结果交回模型，直到得到回答。
type turn struct {
	s        *Service
	ctx      context.Context
	model    *models.Resolved
	thinking string
	tools    *tools.Set
	sink     Sink
	msgs     []llm.Message
	// 工具生成的图片保存为这条回复的附件。
	userID, convID, replyID uint64

	started       bool
	rounds        int
	text          strings.Builder
	reasoning     strings.Builder
	records       []ToolEvent
	usage         llm.Usage
	usageReported bool
	toolTokens    int
	// calledWith 记下这一轮已经调用过的工具和参数，重复的不再执行。
	calledWith map[string]bool
	// reminderNudged 表示已经提醒过模型补设提醒（每轮回复最多一次）。
	reminderNudged bool
	// userText 是对方这一条消息的原文（不含附上的〔此刻〕信息）。
	userText string
}

// reminderNudge 是模型答应了提醒却没调用工具时补发给它的话。
const reminderNudge = "〔系统提示，对方看不到〕你刚才答应了到时间提醒对方，但没有调用 set_reminder（或 schedule_task），提醒并没有设上。" +
	"现在按你刚才说的时间调用工具把它设上。设好后不要再输出任何文字（刚才的回复对方已经看到了）；如果其实不需要设提醒，也什么都不输出。"

var (
	// askRemindRe 匹配对方让你到点提醒的说法。
	askRemindRe = regexp.MustCompile(`提醒我|叫我|喊我|催我|叫一下我|闹钟|别让我忘|记得告诉我|到时候告诉我`)
	// promiseRemindRe 匹配回复里明确答应提醒的说法；looseRemindRe 宽一些，只在对方明确要求提醒时才算。
	promiseRemindRe = regexp.MustCompile(`(提醒|叫|喊|催)你|叫醒你`)
	looseRemindRe   = regexp.MustCompile(`告诉你|到时候|到点|准时`)
	// timeRe 匹配说出来的时间。
	timeRe = regexp.MustCompile(`\d{1,2}\s*[:：点]|\d+\s*(分钟|小时|个小时)|[一二两三四五六七八九十半]+(分钟|小时|个小时|点)|明天|后天|今晚|今天|早上|上午|中午|下午|晚上|周[一二三四五六日天]|星期|号`)
)

// forgotReminder 报告这一轮是不是答应了提醒却没设：回复里答应了到某个时间提醒，但这一轮没调用提醒工具。
func (t *turn) forgotReminder() bool {
	if t.userText == "" || !t.tools.Has("set_reminder") {
		return false
	}
	for _, r := range t.records {
		if r.Name == "set_reminder" || r.Name == "schedule_task" {
			return false
		}
	}
	text := t.text.String()
	if !timeRe.MatchString(text) {
		return false
	}
	return promiseRemindRe.MatchString(text) || (askRemindRe.MatchString(t.userText) && looseRemindRe.MatchString(text))
}

// run 执行生成。start 在第一次成功连上模型后调用（此时才开始向客户端推送）。
func (t *turn) run(start func() error) error {
	for {
		defs := t.tools.Defs()
		if t.rounds >= maxToolRounds {
			defs = nil // 工具调用轮数已到上限，要求模型直接回答
		}
		// 先打开模型的流式连接再开始推送：模型不可用时，调用方仍可返回普通错误响应。
		stream, err := t.model.Chat.ChatStream(t.ctx, llm.ChatRequest{Messages: t.msgs, Thinking: t.thinking, Tools: defs})
		if err != nil {
			return err
		}
		t.rounds++
		if !t.started {
			t.started = true
			if err := start(); err != nil {
				stream.Close()
				return err
			}
		}
		roundText, roundReasoning, calls, err := t.consume(stream)
		stream.Close()
		if err != nil {
			return err
		}
		// DeepSeek 偶尔把工具调用写成 DSML 标记混在正文里（没有推送给用户）：去掉，并当成真正的工具调用来执行。
		if clean, dsml, found := llm.ExtractDSML(roundText); found {
			roundText = clean
			// 标记前面的空行已经推送了，保存时去掉。
			kept := strings.TrimRight(t.text.String(), " \t\n")
			t.text.Reset()
			t.text.WriteString(kept)
			if len(calls) == 0 && t.rounds < maxToolRounds {
				for i := range dsml {
					dsml[i].ID = fmt.Sprintf("dsml_%d_%d", t.rounds, i)
				}
				calls = dsml
			}
		}
		if len(calls) == 0 {
			if t.text.Len() == 0 {
				return errors.New("模型返回了空回复")
			}
			// 嘴上答应了到点提醒、却没有调用工具（提醒根本没设上）：让模型马上补设，只补一次。
			if !t.reminderNudged && t.forgotReminder() {
				t.reminderNudged = true
				t.msgs = append(t.msgs, llm.Message{Role: llm.RoleAssistant, Content: roundText, Reasoning: roundReasoning},
					llm.Message{Role: llm.RoleUser, Content: reminderNudge})
				continue
			}
			return nil
		}
		t.msgs = append(t.msgs, llm.Message{Role: llm.RoleAssistant, Content: roundText, Reasoning: roundReasoning, ToolCalls: calls})
		for _, call := range calls {
			out, err := t.runTool(call)
			if err != nil {
				return err
			}
			t.msgs = append(t.msgs, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: out})
		}
	}
}

func (t *turn) runTool(call llm.ToolCall) (string, error) {
	if call.ID == "" {
		call.ID = fmt.Sprintf("call_%d", len(t.records)+1)
	}
	ev := ToolEvent{ID: call.ID, Name: call.Name, Label: t.tools.Label(call.Name), Status: "running"}
	if t.tools == nil {
		return "当前没有可用的工具", nil
	}
	// 同一轮里用一模一样的参数重复调用（模型偶尔会连着查好几遍提醒）：不再执行，也不显示步骤，让它直接用之前的结果。
	sig := toolSignature(call)
	if t.calledWith == nil {
		t.calledWith = map[string]bool{}
	}
	if t.calledWith[sig] {
		return "你刚才已经用同样的参数调用过 " + call.Name + "，结果和上次一样，不用再调用。直接根据之前的结果继续。", nil
	}
	t.calledWith[sig] = true
	// 先解析出输入用于展示（搜索词、网址、代码）。
	ev.Input = toolInput(call)
	if err := t.sink.Tool(ev); err != nil {
		return "", err
	}
	res := t.tools.Run(t.ctx, call)
	if t.ctx.Err() != nil {
		return "", t.ctx.Err()
	}
	ev.Status, ev.Summary, ev.Detail = "done", res.Summary, res.Detail
	if !res.OK {
		ev.Status = "error"
	}
	if res.Input != "" {
		ev.Input = res.Input
	}
	if len(res.Images) > 0 {
		var note string
		ev.Images, note = t.saveImages(res.Images)
		if note != "" {
			res.Output += "\n" + note
		}
	}
	if len(res.Files) > 0 {
		var note string
		ev.Files, note = t.saveFiles(res.Files)
		if note != "" {
			res.Output += "\n" + note
		}
	}
	t.records = append(t.records, ev)
	t.toolTokens += prompt.EstimateTokens(res.Output)
	if err := t.sink.Tool(ev); err != nil {
		return "", err
	}
	return res.Output, nil
}

// saveImages 把工具生成的图片保存为回复的附件。保存失败的图片不显示，并返回告诉模型的说明。
func (t *turn) saveImages(images []tools.Image) ([]ToolImage, string) {
	if t.s.uploads == nil {
		return nil, "（当前不支持保存图片，图片没有显示给用户）"
	}
	bg := context.WithoutCancel(t.ctx)
	var out []ToolImage
	var failed []string
	for _, img := range images {
		a, err := t.s.uploads.SaveGenerated(bg, t.userID, t.convID, t.replyID, img.Name, img.Data)
		if err != nil {
			t.s.log.Warn("保存代码生成的图片失败", "user_id", t.userID, "name", img.Name, "err", err)
			var ue *uploads.UserError
			if errors.As(err, &ue) {
				failed = append(failed, img.Name+"："+ue.Msg)
			} else {
				failed = append(failed, img.Name+"：保存失败")
			}
			continue
		}
		out = append(out, ToolImage{ID: a.ID, Kind: a.Kind, Filename: a.Filename, MIME: a.MIME, Size: a.Size})
	}
	if len(failed) > 0 {
		return out, "以下图片没有显示给用户：" + strings.Join(failed, "；")
	}
	return out, ""
}

// saveFiles 把工具发给用户的文件保存为回复的附件。
func (t *turn) saveFiles(files []tools.Image) ([]ToolImage, string) {
	if t.s.uploads == nil {
		return nil, "（当前不支持发送文件，文件没有发给用户）"
	}
	bg := context.WithoutCancel(t.ctx)
	var out []ToolImage
	var failed []string
	for _, f := range files {
		a, err := t.s.uploads.SaveSent(bg, t.userID, t.convID, t.replyID, f.Name, f.Data)
		if err != nil {
			var ue *uploads.UserError
			if errors.As(err, &ue) {
				failed = append(failed, f.Name+"："+ue.Msg)
			} else {
				t.s.log.Warn("保存发送的文件失败", "user_id", t.userID, "name", f.Name, "err", err)
				failed = append(failed, f.Name+"：保存失败")
			}
			continue
		}
		out = append(out, ToolImage{ID: a.ID, Kind: a.Kind, Filename: a.Filename, MIME: a.MIME, Size: a.Size})
	}
	if len(failed) > 0 {
		return out, "以下文件没有发给用户：" + strings.Join(failed, "；")
	}
	return out, ""
}

func toolInput(call llm.ToolCall) string {
	var m map[string]any
	if json.Unmarshal([]byte(call.Arguments), &m) != nil {
		return ""
	}
	for _, k := range []string{"query", "url", "code"} {
		if v, ok := m[k].(string); ok {
			return v
		}
	}
	return ""
}

// consume 读取一次流式输出，返回本次的正文、思考过程与工具调用。
func (t *turn) consume(stream llm.ChatStream) (string, string, []llm.ToolCall, error) {
	var text, reasoning strings.Builder
	var calls llm.ToolCallBuilder
	// 多轮工具调用之间的正文用空行分开。
	sep := t.text.Len() > 0
	// 混在正文里的 DSML 工具调用标记不推送给用户。
	var filter llm.DSMLFilter
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if rest := filter.Flush(); rest != "" {
				if sep {
					rest = "\n\n" + rest
				}
				if err := t.sink.Delta(rest); err != nil {
					return text.String(), reasoning.String(), nil, err
				}
				t.text.WriteString(rest)
			}
			return text.String(), reasoning.String(), calls.Calls(), nil
		}
		if err != nil {
			return text.String(), reasoning.String(), nil, err
		}
		if chunk.Usage != nil {
			t.usage.Add(*chunk.Usage)
			t.usageReported = true
		}
		calls.Add(chunk.ToolCalls)
		if chunk.Reasoning != "" {
			if err := t.sink.Reasoning(chunk.Reasoning); err != nil {
				return text.String(), reasoning.String(), nil, err
			}
			if reasoning.Len() == 0 && t.reasoning.Len() > 0 {
				t.reasoning.WriteString("\n\n")
			}
			reasoning.WriteString(chunk.Reasoning)
			t.reasoning.WriteString(chunk.Reasoning)
		}
		if chunk.Delta == "" {
			continue
		}
		text.WriteString(chunk.Delta)
		delta := filter.Push(chunk.Delta)
		if delta == "" {
			continue
		}
		if sep {
			delta, sep = "\n\n"+delta, false
		}
		// 只保存已成功推送给用户的内容。
		if err := t.sink.Delta(delta); err != nil {
			return text.String(), reasoning.String(), nil, err
		}
		t.text.WriteString(delta)
	}
}

// toolSignature 是一次工具调用的「名字 + 参数」（参数去掉空白后比较）。
func toolSignature(call llm.ToolCall) string {
	var b bytes.Buffer
	args := strings.TrimSpace(call.Arguments)
	if args == "" {
		args = "{}"
	}
	if json.Compact(&b, []byte(args)) != nil {
		b.Reset()
		b.WriteString(args)
	}
	return call.Name + "|" + b.String()
}

// toolHints 返回可用工具的使用说明，放进系统提示词。
func toolHints(set *tools.Set) string {
	defs := set.Defs()
	if len(defs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 工具\n你可以在需要时调用工具，由你自己判断是否需要：")
	for _, d := range defs {
		switch d.Name {
		case tools.NameSearch:
			b.WriteString("\n- web_search：问到新闻、天气、价格、近期发生的事或你不确定的事实时先搜索，不要凭印象回答")
		case tools.NameFetch:
			b.WriteString("\n- web_fetch：需要网页的详细内容时读取（包括对方发来的链接）")
		case tools.NamePython:
			b.WriteString("\n- run_python：需要精确计算、日期推算、换算或数据处理时写代码运行，用 print 输出结果，不要心算")
		case tools.NameInstall:
			b.WriteString("\n- install_package：运行代码缺少第三方库（ModuleNotFoundError）时安装后再运行")
		}
	}
	for _, h := range set.Hints() {
		b.WriteString("\n- " + h)
	}
	b.WriteString("\n- 用了搜索结果时，在回答里自然地提到来源（网站名即可，必要时附链接）；日常聊天不需要用工具")
	return b.String()
}

// autoTitle 用第一条消息为未命名的对话生成标题。
func (s *Service) autoTitle(ctx context.Context, u *store.User, conv *store.Conversation, content string) {
	if conv.Title != "" || content == "" {
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

// cachedOf 是要记进消息的缓存命中 token 数；服务商没返回缓存信息时为空。
func cachedOf(u llm.Usage) *int {
	if !u.CacheReported {
		return nil
	}
	n := u.CachedTokens
	return &n
}

func isCanceled(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled)
}

// afterReply 记录用量、记忆使用，并提交记忆写入任务。这些步骤失败只记录日志。
func (s *Service) afterReply(ctx context.Context, u *store.User, p *prepared, model *models.Resolved,
	userMsg, reply *store.Message, atts []store.Attachment, usage llm.Usage, remember bool) {
	if !model.Personal {
		if err := s.quota.Record(ctx, u.ID, quota.Use{Key: quota.ModelKey(model.ID), Messages: 1, PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens, CachedTokens: usage.CachedTokens}); err != nil {
			s.log.Error("记录用量失败", "user_id", u.ID, "err", err)
		}
	}
	if err := s.store.Conversations().Touch(ctx, u.ID, p.conv.ID, reply.CreatedAt); err != nil {
		s.log.Error("更新对话时间失败", "user_id", u.ID, "err", err)
	}
	// 临时对话不留下任何记忆痕迹（包括「想起过哪些记忆」）。
	if reply.Content == "" || p.conv.Temporary {
		return
	}
	if err := s.retriever.MarkUsed(ctx, u.ID, reply.ID, p.prompt.UsedMemories); err != nil {
		s.log.Error("记录记忆使用失败", "user_id", u.ID, "err", err)
	}
	if reply.Status != store.MessageComplete || !remember {
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
	// 记忆提取使用较短的文档摘录；思考过程不参与记忆提取，只用正式回复。
	userText, _, _ := s.renderAttachments(userMsg.Content, atts, false, 1500)
	var images []llm.Image
	if model.Vision {
		images = p.images
	}
	s.writer.Enqueue(memory.WriteJob{
		UserID: u.ID, ConversationID: p.conv.ID, MessageID: userMsg.ID,
		Chat: model.Chat, Personal: model.Personal,
		Input: memory.ExtractInput{
			UserName: u.DisplayName, UserMessage: userText, AssistantReply: reply.Content,
			Context: recent, Known: known, Now: s.now().In(s.loc), Images: images,
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
