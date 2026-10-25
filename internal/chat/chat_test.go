package chat

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"os"
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
	"github.com/HugehardJiang/buugoo/internal/tools"
	"github.com/HugehardJiang/buugoo/internal/uploads"
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
	mu       sync.Mutex
	reqs     []llm.ChatRequest
	chatReqs []llm.ChatRequest
	stream   func() (llm.ChatStream, error)
	// script 中的流按顺序优先使用（用于模拟多轮工具调用）。
	script   []*fakeStream
	extract  string
	describe string
}

func (f *fakeChat) Model() string { return "fake-chat" }
func (f *fakeChat) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.mu.Lock()
	f.chatReqs = append(f.chatReqs, req)
	f.mu.Unlock()
	if !req.JSON {
		return &llm.ChatResponse{Content: f.describe, Usage: llm.Usage{PromptTokens: 30, CompletionTokens: 20}}, nil
	}
	return &llm.ChatResponse{Content: f.extract, Usage: llm.Usage{PromptTokens: 50, CompletionTokens: 10}}, nil
}
func (f *fakeChat) ChatStream(_ context.Context, req llm.ChatRequest) (llm.ChatStream, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	var next *fakeStream
	if len(f.script) > 0 {
		next, f.script = f.script[0], f.script[1:]
	}
	f.mu.Unlock()
	if next != nil {
		return next, nil
	}
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
	vision   bool
	tools    bool
	style    string
	helper   *models.Resolved
}

func (f *fakeModels) Resolve(_ context.Context, _ models.User, id *uint64) (*models.Resolved, error) {
	rid := uint64(0)
	if id != nil {
		rid = *id
	}
	return &models.Resolved{ID: rid, Name: "测试模型", Model: "fake-chat", Chat: f.chat, Personal: f.personal,
		Vision: f.vision, Tools: f.tools, ThinkingStyle: f.style}, nil
}

func (f *fakeModels) VisionHelper(context.Context, models.User) (*models.Resolved, error) {
	if f.helper == nil {
		return nil, models.ErrNoModel
	}
	return f.helper, nil
}

// fakeTools 提供运行 Python 工具，运行结果固定。
type fakeTools struct {
	out    string
	images []tools.Image
}

func (f fakeTools) For(context.Context, *store.User, *store.Conversation, *models.Resolved, []store.Attachment) *tools.Set {
	return tools.New(tools.Options{Python: fakeRunner{f.out, f.images}})
}

type fakeRunner struct {
	out    string
	images []tools.Image
}

func (r fakeRunner) Run(context.Context, string, tools.RunEnv) (tools.ExecResult, error) {
	return tools.ExecResult{Stdout: r.out, Images: r.images}, nil
}
func (fakeRunner) Sandboxed() bool { return true }

func (f *fakeModels) Visible(_ context.Context, _ models.User, id uint64) (bool, error) {
	return id == 7, nil
}

type recordSink struct {
	tools     []ToolEvent
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
func (r *recordSink) Tool(ev ToolEvent) error          { r.tools = append(r.tools, ev); return nil }
func (r *recordSink) Delta(t string) error {
	r.n++
	if r.failAt > 0 && r.n >= r.failAt {
		return errors.New("client gone")
	}
	r.text.WriteString(t)
	return nil
}

type env struct {
	svc     *Service
	db      *gormstore.DB
	chat    *fakeChat
	models  *fakeModels
	writer  *memory.Writer
	user    *store.User
	uploads *uploads.Service
	dir     string
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
	dir := filepath.Join(t.TempDir(), "uploads")
	up := uploads.New(db.Attachments(), config.UploadsConfig{Dir: dir, MaxFileMB: 1, MaxFiles: 3, DocChars: 50}, nil)
	svc := New(Deps{
		Store: db, Models: fm, Tools: fakeTools{out: "1234567"}, Uploads: up, Retriever: retriever, Writer: writer,
		Builder: prompt.NewBuilder(p, 6000), Quota: limiter, Config: cfg.Chat, Location: time.UTC,
	})
	return &env{svc: svc, db: db, chat: fc, models: fm, writer: writer, user: user, uploads: up, dir: dir}
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

func TestToolLoop(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, config.QuotaConfig{})
	e.models.tools, e.models.style = true, store.ThinkingStyleDeepSeek
	e.chat.script = []*fakeStream{
		{chunks: []llm.Chunk{
			{Reasoning: "要算一下"},
			{Delta: "我算一下。"},
			{ToolCalls: []llm.ToolCallDelta{{Index: 0, ID: "c1", Name: "run_python", Arguments: `{"code":"print(1111*1111)"}`}}},
			{Usage: &llm.Usage{PromptTokens: 100, CompletionTokens: 10}},
		}},
		{chunks: []llm.Chunk{{Reasoning: "结果出来了"}, {Delta: "是 1234321"}, {Usage: &llm.Usage{PromptTokens: 150, CompletionTokens: 5}}}},
	}
	sink := &recordSink{}
	res, err := e.svc.Send(ctx, e.user, SendInput{Content: "1111 乘 1111 是多少"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply.Content != "我算一下。\n\n是 1234321" || sink.text.String() != res.Reply.Content {
		t.Fatalf("reply=%q sink=%q", res.Reply.Content, sink.text.String())
	}
	if res.Reply.Reasoning != "要算一下\n\n结果出来了" {
		t.Fatalf("reasoning=%q", res.Reply.Reasoning)
	}
	if len(sink.tools) != 2 || sink.tools[0].Status != "running" || sink.tools[1].Status != "done" ||
		sink.tools[1].Input != "print(1111*1111)" || sink.tools[1].Label != "运行代码" {
		t.Fatalf("tools=%+v", sink.tools)
	}
	if res.Usage.PromptTokens != 250 || res.Usage.CompletionTokens != 15 {
		t.Fatalf("用量应累加各轮: %+v", res.Usage)
	}
	// 第二次请求应带上助手的工具调用（含思考过程）与工具结果。
	req := e.chat.reqs[1]
	n := len(req.Messages)
	asst, tool := req.Messages[n-2], req.Messages[n-1]
	if asst.Role != llm.RoleAssistant || len(asst.ToolCalls) != 1 || asst.Reasoning != "要算一下" ||
		tool.Role != llm.RoleTool || tool.ToolCallID != "c1" || !strings.Contains(tool.Content, "1234567") {
		t.Fatalf("messages=%+v", req.Messages[n-2:])
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "run_python" || !strings.Contains(req.Messages[0].Content, "run_python") {
		t.Fatal("应提供工具并在系统提示词中说明")
	}
	// 工具记录随回复保存。
	_, msgs, _ := e.svc.History(ctx, e.user, res.Conversation.ID, 0, 10)
	if !strings.Contains(msgs[1].ToolLog, `"name":"run_python"`) {
		t.Fatalf("tool_log=%q", msgs[1].ToolLog)
	}
	// 下一轮的历史中，助手消息带上思考过程（DeepSeek 使用工具时要求回传）。
	e.chat.script = nil
	if _, err := e.svc.Send(ctx, e.user, SendInput{Content: "谢谢"}, &recordSink{}); err != nil {
		t.Fatal(err)
	}
	last := e.chat.reqs[len(e.chat.reqs)-1]
	if last.Messages[2].Role != llm.RoleAssistant || last.Messages[2].Reasoning == "" {
		t.Fatalf("历史助手消息应带思考过程: %+v", last.Messages[2])
	}
}

func TestToolImages(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, config.QuotaConfig{})
	e.models.tools = true
	png := pngData(t)
	e.svc.tools = fakeTools{images: []tools.Image{{Name: "chart.png", Data: png}, {Name: "fake.png", Data: []byte("not an image")}}}
	e.chat.script = []*fakeStream{
		{chunks: []llm.Chunk{{ToolCalls: []llm.ToolCallDelta{{Index: 0, ID: "c1", Name: "run_python", Arguments: `{"code":"plot()"}`}}}}},
		{chunks: []llm.Chunk{{Delta: "画好了"}}},
	}
	sink := &recordSink{}
	res, err := e.svc.Send(ctx, e.user, SendInput{Content: "画个图"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	done := sink.tools[len(sink.tools)-1]
	if len(done.Images) != 1 || done.Images[0].Filename != "chart.png" || done.Images[0].Kind != store.AttachmentImage {
		t.Fatalf("images=%+v", done.Images)
	}
	// 图片保存为回复的附件，不会被当作未发送的上传清理掉，并随对话一起删除。
	a, err := e.db.Attachments().Get(ctx, e.user.ID, done.Images[0].ID)
	if err != nil || a.MessageID == nil || *a.MessageID != res.Reply.ID || a.ConversationID == nil || *a.ConversationID != res.Conversation.ID {
		t.Fatalf("a=%+v err=%v", a, err)
	}
	if !strings.Contains(res.Reply.ToolLog, `"images":[{"id":`) {
		t.Fatalf("tool_log=%q", res.Reply.ToolLog)
	}
	tool := e.chat.reqs[1].Messages[len(e.chat.reqs[1].Messages)-1]
	if !strings.Contains(tool.Content, "已经显示在你的回复里") || !strings.Contains(tool.Content, "fake.png") {
		t.Fatalf("tool=%q", tool.Content)
	}
	if err := e.svc.DeleteConversation(ctx, e.user, res.Conversation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Attachments().Get(ctx, e.user.ID, a.ID); err == nil {
		t.Fatal("图片应随对话删除")
	}
	if _, err := os.Stat(filepath.Join(e.dir, filepath.FromSlash(a.Path))); !os.IsNotExist(err) {
		t.Fatalf("图片文件应删除: %v", err)
	}
}

func TestToolRoundsLimited(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, config.QuotaConfig{})
	e.models.tools = true
	call := func() *fakeStream {
		return &fakeStream{chunks: []llm.Chunk{{ToolCalls: []llm.ToolCallDelta{{Index: 0, ID: "c", Name: "run_python", Arguments: `{"code":"1"}`}}}}}
	}
	for i := 0; i < maxToolRounds; i++ {
		e.chat.script = append(e.chat.script, call())
	}
	if _, err := e.svc.Send(ctx, e.user, SendInput{Content: "算"}, &recordSink{}); err != nil {
		t.Fatal(err)
	}
	if n := len(e.chat.reqs); n != maxToolRounds+1 || e.chat.reqs[n-1].Tools != nil {
		t.Fatalf("到达上限后应不再提供工具: rounds=%d", n)
	}
}

func pngData(t *testing.T) []byte {
	t.Helper()
	// 1x1 透明 PNG。
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAttachments(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, config.QuotaConfig{})
	e.writer.Start(ctx)
	doc, err := e.uploads.Save(ctx, e.user.ID, "日记.txt", strings.NewReader(strings.Repeat("今天去公园散步了。", 20)))
	if err != nil {
		t.Fatal(err)
	}
	img, err := e.uploads.Save(ctx, e.user.ID, "猫.png", bytes.NewReader(pngData(t)))
	if err != nil {
		t.Fatal(err)
	}

	// 当前模型不能看图，也没有看图模型：拒绝，并且不保存消息。
	if _, err := e.svc.Send(ctx, e.user, SendInput{AttachmentIDs: []uint64{img.ID}}, &recordSink{}); !errors.Is(err, ErrImagesUnsupported) {
		t.Fatalf("err=%v", err)
	}
	// 借看图模型转述。
	helperChat := &fakeChat{describe: "一只橘猫趴在窗台上"}
	e.models.helper = &models.Resolved{ID: 9, Name: "看图模型", Chat: helperChat, Vision: true}
	res, err := e.svc.Send(ctx, e.user, SendInput{AttachmentIDs: []uint64{doc.ID, img.ID}}, &recordSink{})
	if err != nil {
		t.Fatal(err)
	}
	if len(helperChat.chatReqs) != 1 || len(helperChat.chatReqs[0].Messages[0].Images) != 1 {
		t.Fatal("应请看图模型描述图片")
	}
	user := e.chat.reqs[len(e.chat.reqs)-1].Messages
	text := user[len(user)-1].Content
	if !strings.Contains(text, "【文件：日记.txt】") || !strings.Contains(text, "已省略") || !strings.Contains(text, "一只橘猫趴在窗台上") {
		t.Fatalf("用户消息应包含文档与图片描述: %s", text)
	}
	if res.Conversation.Title != "日记.txt" || res.UserMessage.Content != "" {
		t.Fatalf("title=%q content=%q", res.Conversation.Title, res.UserMessage.Content)
	}
	// 已发送的附件不能再次使用。
	if _, err := e.svc.Send(ctx, e.user, SendInput{AttachmentIDs: []uint64{doc.ID}}, &recordSink{}); !errors.Is(err, ErrAttachmentNotFound) {
		t.Fatalf("err=%v", err)
	}
	if _, err := e.svc.Send(ctx, e.user, SendInput{AttachmentIDs: []uint64{1, 2, 3, 4}}, &recordSink{}); !errors.Is(err, ErrTooManyFiles) {
		t.Fatalf("err=%v", err)
	}

	// 能看图的模型直接收到图片；之后的轮次历史中也带上图片。
	e.models.vision = true
	img2, _ := e.uploads.Save(ctx, e.user.ID, "狗.png", bytes.NewReader(pngData(t)))
	if _, err := e.svc.Send(ctx, e.user, SendInput{ConversationID: res.Conversation.ID, Content: "看看这只", AttachmentIDs: []uint64{img2.ID}}, &recordSink{}); err != nil {
		t.Fatal(err)
	}
	msgs := e.chat.reqs[len(e.chat.reqs)-1].Messages
	lastUser := msgs[len(msgs)-1]
	if len(lastUser.Images) != 1 || !strings.HasPrefix(lastUser.Images[0].URL, "data:image/png;base64,") {
		t.Fatalf("应直接发送图片: %+v", lastUser)
	}
	if _, err := e.svc.Send(ctx, e.user, SendInput{ConversationID: res.Conversation.ID, Content: "继续"}, &recordSink{}); err != nil {
		t.Fatal(err)
	}
	msgs = e.chat.reqs[len(e.chat.reqs)-1].Messages
	imgs := 0
	for _, m := range msgs {
		imgs += len(m.Images)
	}
	if imgs != 2 {
		t.Fatalf("历史中应带上最近的图片: %d", imgs)
	}

	_, hist, _ := e.svc.History(ctx, e.user, res.Conversation.ID, 0, 20)
	byMsg := e.svc.MessageAttachments(ctx, e.user, hist)
	if len(byMsg[hist[0].ID]) != 2 {
		t.Fatalf("附件应关联到用户消息: %v", byMsg)
	}

	// 删除对话时一并删除附件文件。
	closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = e.writer.Close(closeCtx)
	if err := e.svc.DeleteConversation(ctx, e.user, res.Conversation.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.db.Attachments().TotalSize(ctx, e.user.ID); n != 0 {
		t.Fatalf("附件应被删除，剩余 %d 字节", n)
	}
	files := 0
	_ = filepath.WalkDir(e.dir, func(_ string, d fs.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			files++
		}
		return nil
	})
	if files != 0 {
		t.Fatalf("文件应被删除，剩余 %d 个", files)
	}
}

func TestPersonaPreset(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, config.QuotaConfig{})
	e.user.PersonaPreset = "snarky"
	if _, err := e.svc.Send(ctx, e.user, SendInput{Content: "在吗"}, &recordSink{}); err != nil {
		t.Fatal(err)
	}
	sys := e.chat.reqs[len(e.chat.reqs)-1].Messages[0].Content
	if !strings.Contains(sys, "损友") || strings.Contains(sys, "风趣幽默的大肥鸟") || !strings.Contains(sys, "开玩笑有分寸") {
		t.Fatalf("应套用预设、保留原则: %s", sys)
	}
	// 管理员关掉预设后改用默认人格。
	e.svc.disabled = []string{"snarky"}
	if _, err := e.svc.Send(ctx, e.user, SendInput{Content: "在吗"}, &recordSink{}); err != nil {
		t.Fatal(err)
	}
	sys = e.chat.reqs[len(e.chat.reqs)-1].Messages[0].Content
	if strings.Contains(sys, "损友") || !strings.Contains(sys, "风趣幽默的大肥鸟") {
		t.Fatal("关掉的预设不应生效")
	}
}
