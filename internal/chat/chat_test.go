package chat

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/memory"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/persona"
	"github.com/HugehardJiang/buugoo/internal/prompt"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
	"github.com/HugehardJiang/buugoo/internal/vector/memstore"
)

type fakeStream struct {
	chunks []llm.Chunk
	err    error
	i      int
}

func (f *fakeStream) Recv() (llm.Chunk, error) {
	if f.i < len(f.chunks) {
		c := f.chunks[f.i]
		f.i++
		return c, nil
	}
	if f.err != nil {
		return llm.Chunk{}, f.err
	}
	return llm.Chunk{}, io.EOF
}
func (f *fakeStream) Close() error { return nil }

type fakeChat struct {
	mu      sync.Mutex
	reqs    []llm.ChatRequest
	stream  func() (llm.ChatStream, error)
	extract string
}

func (f *fakeChat) Model() string { return "fake-chat" }
func (f *fakeChat) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: f.extract, Usage: llm.Usage{PromptTokens: 50, CompletionTokens: 10}}, nil
}
func (f *fakeChat) ChatStream(_ context.Context, req llm.ChatRequest) (llm.ChatStream, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	return f.stream()
}

type fakeEmb struct{}

func (fakeEmb) Model() string { return "fake-emb" }
func (fakeEmb) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := []float32{0.01, 0, 0}
		if strings.Contains(t, "花生") {
			v[1] = 1
		}
		if strings.Contains(t, "猫") {
			v[2] = 1
		}
		out[i] = v
	}
	return out, nil
}

// fakeModels 把所有对话都解析到同一个假模型；personal 控制是否视为用户自带的 Key。
type fakeModels struct {
	chat     llm.ChatProvider
	personal bool
}

func (f *fakeModels) Resolve(_ context.Context, _ models.User, id *uint64) (*models.Resolved, error) {
	rid := uint64(0)
	if id != nil {
		rid = *id
	}
	return &models.Resolved{ID: rid, Name: "测试模型", Model: "fake-chat", Chat: f.chat, Personal: f.personal}, nil
}

func (f *fakeModels) Visible(_ context.Context, _ models.User, id uint64) (bool, error) {
	return id == 7, nil
}

type recordSink struct {
	started   bool
	model     *models.Resolved
	mems      []memory.Scored
	text      strings.Builder
	reasoning strings.Builder
	failAt    int
	n         int
}

func (r *recordSink) Start(_ *store.Conversation, _, _ *store.Message, m *models.Resolved) error {
	r.started, r.model = true, m
	return nil
}
func (r *recordSink) Memories(m []memory.Scored) error { r.mems = m; return nil }
func (r *recordSink) Reasoning(t string) error         { r.reasoning.WriteString(t); return nil }
func (r *recordSink) Delta(t string) error {
	r.n++
	if r.failAt > 0 && r.n >= r.failAt {
		return errors.New("client gone")
	}
	r.text.WriteString(t)
	return nil
}

type env struct {
	svc    *Service
	db     *gormstore.DB
	chat   *fakeChat
	models *fakeModels
	writer *memory.Writer
	user   *store.User
}

func newEnv(t *testing.T, q config.QuotaConfig) *env {
	t.Helper()
	ctx := context.Background()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "c.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	user, err := db.Users().EnsureOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	user.IsAdmin = false // 测试限额时不豁免
	fc := &fakeChat{
		stream: func() (llm.ChatStream, error) {
			return &fakeStream{chunks: []llm.Chunk{{Delta: "嗯，"}, {Delta: "记住啦"}, {Usage: &llm.Usage{PromptTokens: 100, CompletionTokens: 5}}}}, nil
		},
		extract: `{"memories":[{"type":"semantic","content":"用户对花生过敏","importance":9,"emotion":0.2,"source":"stated"}]}`,
	}
	cfg := config.Default()
	vs := memstore.New(db.Memories().LoadEmbeddings, cfg.Memory.Cache, nil)
	retriever := memory.NewRetriever(db.Memories(), vs, fakeEmb{}, cfg.Memory.Retrieval, cfg.Memory.Write.AccessBoost)
	limiter := quota.New(q, db.Usage(), time.UTC)
	writer := memory.NewWriter(db.Memories(), vs, fakeEmb{}, memory.NewLLMExtractor(fc), cfg.Memory.Write, nil,
		func(ctx context.Context, job memory.WriteJob, u llm.Usage) {
			if !job.Personal {
				_ = limiter.Record(ctx, job.UserID, 0, u.PromptTokens, u.CompletionTokens)
			}
		})
	p, _ := persona.Load("")
	fm := &fakeModels{chat: fc}
	svc := New(Deps{
		Store: db, Models: fm, Retriever: retriever, Writer: writer, Builder: prompt.NewBuilder(p, 6000),
		Quota: limiter, Config: cfg.Chat, Location: time.UTC,
	})
	return &env{svc: svc, db: db, chat: fc, models: fm, writer: writer, user: user}
}

func TestSendFlowAndMemoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, config.QuotaConfig{})
	e.writer.Start(ctx)

	sink := &recordSink{}
	res, err := e.svc.Send(ctx, e.user, SendInput{Content: "  我对花生过敏  "}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if !sink.started || sink.text.String() != "嗯，记住啦" {
		t.Fatalf("sink=%+v", sink)
	}
	if res.Reply.Status != store.MessageComplete || res.Reply.Content != "嗯，记住啦" || res.Usage.PromptTokens != 100 {
		t.Fatalf("reply=%+v usage=%+v", res.Reply, res.Usage)
	}
	if res.UserMessage.Content != "我对花生过敏" {
		t.Fatal("应去除首尾空白")
	}
	// 等待异步记忆写入完成。
	closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := e.writer.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	mems, total, _ := e.db.Memories().List(ctx, e.user.ID, store.MemoryFilter{}, 10, 0)
	if total != 1 || mems[0].Content != "用户对花生过敏" || *mems[0].SourceMessageID != res.UserMessage.ID {
		t.Fatalf("应写入一条记忆: %+v", mems)
	}

	// 第二轮：应检索到上一轮写入的记忆，并带上对话历史。
	sink2 := &recordSink{}
	res2, err := e.svc.Send(ctx, e.user, SendInput{Content: "晚饭吃花生酱拌面可以吗"}, sink2)
	if err != nil {
		t.Fatal(err)
	}
	if len(sink2.mems) != 1 || sink2.mems[0].Memory.Content != "用户对花生过敏" {
		t.Fatalf("应使用相关记忆: %+v", sink2.mems)
	}
	req := e.chat.reqs[1]
	if !strings.Contains(req.Messages[0].Content, "用户对花生过敏") {
		t.Fatal("系统提示词应包含记忆")
	}
	if len(req.Messages) != 4 || req.Messages[1].Content != "我对花生过敏" {
		t.Fatalf("应包含上一轮对话历史: %+v", req.Messages)
	}
	m, _ := e.db.Memories().Get(ctx, e.user.ID, mems[0].ID)
	if m.AccessCount != 1 {
		t.Fatal("被使用的记忆应更新访问次数")
	}
	_, msgs, err := e.svc.History(ctx, e.user, 0, 0, 10)
	if err != nil || len(msgs) != 4 || msgs[3].ID != res2.Reply.ID {
		t.Fatalf("history=%v err=%v", msgs, err)
	}
	// 用量：2 条消息 + 记忆提取的 token。
	u, _ := e.db.Usage().Get(ctx, e.user.ID, time.Now().UTC().Format("2006-01-02"))
	if u.Messages != 2 || u.PromptTokens < 250 {
		t.Fatalf("usage=%+v", u)
	}
}

func TestSendInterruptedAndErrors(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, config.QuotaConfig{})

	// 客户端在第二个片段时断开：已生成部分以 interrupted 保存，不写入记忆。
	res, err := e.svc.Send(ctx, e.user, SendInput{Content: "你好"}, &recordSink{failAt: 2})
	if err == nil {
		t.Fatal("应返回错误")
	}
	if res.Reply.Status != store.MessageInterrupted || res.Reply.Content != "嗯，" {
		t.Fatalf("reply=%+v", res.Reply)
	}

	// 模型直接报错：保存为 error 状态。
	e.chat.stream = func() (llm.ChatStream, error) { return nil, &llm.APIError{StatusCode: 500, Message: "boom"} }
	res, err = e.svc.Send(ctx, e.user, SendInput{Content: "在吗"}, &recordSink{})
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) || res.Reply.Status != store.MessageError {
		t.Fatalf("err=%v reply=%+v", err, res.Reply)
	}

	if _, err := e.svc.Send(ctx, e.user, SendInput{Content: "   "}, &recordSink{}); !errors.Is(err, ErrEmptyMessage) {
		t.Fatal(err)
	}
	if _, err := e.svc.Send(ctx, e.user, SendInput{Content: strings.Repeat("长", MaxMessageRunes+1)}, &recordSink{}); !errors.Is(err, ErrMessageTooLong) {
		t.Fatal(err)
	}
	// 失败的回复不应出现在下一轮的提示词历史中（内容为空的被跳过）。
	p, err := e.svc.Preview(ctx, e.user, 0, "测试")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range p.Messages[1:] {
		if m.Role == llm.RoleAssistant && m.Content == "" {
			t.Fatal("空回复不应进入历史")
		}
	}
}

func TestSendQuota(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, config.QuotaConfig{DailyMessages: 1})
	if _, err := e.svc.Send(ctx, e.user, SendInput{Content: "第一条"}, &recordSink{}); err != nil {
		t.Fatal(err)
	}
	var ex *quota.ExceededError
	if _, err := e.svc.Send(ctx, e.user, SendInput{Content: "第二条"}, &recordSink{}); !errors.As(err, &ex) {
		t.Fatalf("应超出限额: %v", err)
	}
	if len(e.chat.reqs) != 1 {
		t.Fatal("超出限额时不应调用模型")
	}
}

func TestPersonalModelSkipsQuota(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, config.QuotaConfig{DailyMessages: 1})
	e.models.personal = true
	e.writer.Start(ctx)
	for _, msg := range []string{"第一条", "第二条", "第三条"} {
		if _, err := e.svc.Send(ctx, e.user, SendInput{Content: msg}, &recordSink{}); err != nil {
			t.Fatalf("自带 Key 不应受限额约束: %v", err)
		}
	}
	closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = e.writer.Close(closeCtx)
	u, _ := e.db.Usage().Get(ctx, e.user.ID, time.Now().UTC().Format("2006-01-02"))
	if u.Messages != 0 || u.PromptTokens != 0 {
		t.Fatalf("自带 Key 不应计入用量: %+v", u)
	}
}

func TestConversationsAndThinking(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, config.QuotaConfig{})
	e.chat.stream = func() (llm.ChatStream, error) {
		return &fakeStream{chunks: []llm.Chunk{{Reasoning: "先想想"}, {Reasoning: "……"}, {Delta: "好的"}}}, nil
	}

	long := strings.Repeat("长", 30)
	first, err := e.svc.Send(ctx, e.user, SendInput{Content: long}, &recordSink{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Conversation.Title != strings.Repeat("长", 24)+"…" {
		t.Fatalf("自动标题=%q", first.Conversation.Title)
	}

	high := store.ThinkingHigh
	mid := uint64(7)
	conv, err := e.svc.CreateConversation(ctx, e.user, ConversationPatch{Thinking: &high, SetModel: true, ModelID: &mid})
	if err != nil {
		t.Fatal(err)
	}
	if conv.Thinking != store.ThinkingHigh || conv.ModelID == nil || *conv.ModelID != 7 {
		t.Fatalf("conv=%+v", conv)
	}
	bad := uint64(8)
	if _, err := e.svc.UpdateConversation(ctx, e.user, conv.ID, ConversationPatch{SetModel: true, ModelID: &bad}); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("不可见的模型应被拒绝: %v", err)
	}
	wrong := "extreme"
	if _, err := e.svc.UpdateConversation(ctx, e.user, conv.ID, ConversationPatch{Thinking: &wrong}); !errors.Is(err, ErrInvalidThinking) {
		t.Fatal(err)
	}

	sink := &recordSink{}
	res, err := e.svc.Send(ctx, e.user, SendInput{ConversationID: conv.ID, Content: "新对话"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if sink.reasoning.String() != "先想想……" || res.Reply.Reasoning != "先想想……" || res.Reply.Content != "好的" {
		t.Fatalf("思考过程: sink=%q reply=%+v", sink.reasoning.String(), res.Reply)
	}
	if sink.model == nil || sink.model.ID != 7 || *res.Reply.ModelID != 7 {
		t.Fatalf("应使用对话选择的模型: %+v", sink.model)
	}
	last := e.chat.reqs[len(e.chat.reqs)-1]
	if last.Thinking != store.ThinkingHigh {
		t.Fatalf("应传递思考等级: %q", last.Thinking)
	}
	// 新对话不应带上其他对话的历史。
	if len(last.Messages) != 2 {
		t.Fatalf("新对话的提示词不应包含其他对话: %+v", last.Messages)
	}

	list, err := e.svc.ListConversations(ctx, e.user)
	if err != nil || len(list) != 2 || list[0].ID != conv.ID {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	_, msgs, err := e.svc.History(ctx, e.user, conv.ID, 0, 10)
	if err != nil || len(msgs) != 2 || msgs[1].Reasoning != "先想想……" {
		t.Fatalf("history=%+v err=%v", msgs, err)
	}

	if err := e.svc.DeleteConversation(ctx, e.user, conv.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.History(ctx, e.user, conv.ID, 0, 10); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("删除后应找不到: %v", err)
	}
	list, _ = e.svc.ListConversations(ctx, e.user)
	if len(list) != 1 || list[0].ID != first.Conversation.ID {
		t.Fatalf("list=%+v", list)
	}
}
