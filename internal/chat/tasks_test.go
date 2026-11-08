package chat

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
)

type fakeProgress struct {
	mu     sync.Mutex
	steps  []ToolEvent
	rounds []int
	used   int64
}

func (p *fakeProgress) Step(ev ToolEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.steps = append(p.steps, ev)
}
func (p *fakeProgress) Round(used int64, rounds int, _ []llm.Message) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.used, p.rounds = used, append(p.rounds, rounds)
}

func TestExecuteTask(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, config.QuotaConfig{})
	e.models.tools = true
	conv, err := e.svc.CreateConversation(ctx, e.user, ConversationPatch{})
	if err != nil {
		t.Fatal(err)
	}
	e.chat.script = []*fakeStream{
		{chunks: []llm.Chunk{
			{Delta: "先算一下。"},
			{ToolCalls: []llm.ToolCallDelta{{Index: 0, ID: "c1", Name: "run_python", Arguments: `{"code":"print(1)"}`}}},
			{Usage: &llm.Usage{PromptTokens: 1000, CompletionTokens: 50}},
		}},
		{chunks: []llm.Chunk{{Delta: "做完啦！\n\n## 结果\n是 1234567"}, {Usage: &llm.Usage{PromptTokens: 1200, CompletionTokens: 40}}}},
	}
	p := &fakeProgress{}
	task := &store.BgTask{ID: 9, UserID: e.user.ID, ConversationID: conv.ID, Title: "算个数", Goal: "算出结果写成报告", BudgetTokens: 100000}
	res, report, err := e.svc.ExecuteTask(ctx, e.user, task, TaskRun{MaxRounds: 10, Progress: p})
	if err != nil {
		t.Fatal(err)
	}
	// 结果只取最后一轮的正文，中间的「先算一下」不算。
	if report != "做完啦！\n\n## 结果\n是 1234567" || res.Message.Content != report || res.Message.Status != store.MessageComplete || !strings.Contains(res.Message.ToolLog, "run_python") {
		t.Fatalf("report=%q msg=%+v", report, res.Message)
	}
	if len(p.steps) != 2 || p.used != 2290 || len(p.rounds) < 2 {
		t.Fatalf("进展：%+v", p)
	}
	// 发给模型的是后台助手的说明（不是布谷的人格），带上任务。
	req := e.chat.reqs[0]
	if !strings.Contains(req.Messages[0].Content, "后台工作助手") || !strings.Contains(req.Messages[1].Content, "算出结果写成报告") {
		t.Fatalf("提示词：%+v", req.Messages[:2])
	}
	// 用量计入额度。
	st, _ := e.svc.quota.Status(ctx, e.user)
	if st.Tokens != 2290 || st.Messages != 1 {
		t.Fatalf("额度：%+v", st)
	}
	_, msgs, _ := e.svc.History(ctx, e.user, conv.ID, 0, 10)
	if len(msgs) != 1 || msgs[0].Content != report {
		t.Fatalf("对话里应该有结果：%+v", msgs)
	}
}

func TestExecuteTaskBudget(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, config.QuotaConfig{})
	e.models.tools = true
	conv, _ := e.svc.CreateConversation(ctx, e.user, ConversationPatch{})
	call := func(id string) *fakeStream {
		return &fakeStream{chunks: []llm.Chunk{
			{ToolCalls: []llm.ToolCallDelta{{Index: 0, ID: id, Name: "run_python", Arguments: `{"code":"print(` + id + `)"}`}}},
			{Usage: &llm.Usage{PromptTokens: 3000, CompletionTokens: 100}},
		}}
	}
	// 第三次请求没给工具，模型还是要调用（不应执行），同时交出了结果。
	third := call("3")
	third.chunks = append([]llm.Chunk{{Delta: "预算到了，先交这些"}}, third.chunks...)
	e.chat.script = []*fakeStream{call("1"), call("2"), third}
	task := &store.BgTask{ID: 1, UserID: e.user.ID, ConversationID: conv.ID, Title: "大任务", Goal: "做很多事", BudgetTokens: 5000}
	_, report, err := e.svc.ExecuteTask(ctx, e.user, task, TaskRun{MaxRounds: 40})
	if err != nil {
		t.Fatal(err)
	}
	// 预算 5000：两轮后（6200）就不再给工具，让模型交出结果。
	if report != "预算到了，先交这些" || len(e.chat.reqs) != 3 {
		t.Fatalf("report=%q reqs=%d", report, len(e.chat.reqs))
	}
	last := e.chat.reqs[2]
	if last.Tools != nil || !strings.Contains(last.Messages[len(last.Messages)-1].Content, "不要再调用工具") {
		t.Fatalf("最后一次请求应不带工具并要求交结果：%+v", last.Messages[len(last.Messages)-1])
	}
}
