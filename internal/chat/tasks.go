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
	"github.com/HugehardJiang/buugoo/internal/tools"
)

// 后台长任务的「工人」：拿到聊天里提出、对方确认过的任务，用更多轮工具慢慢做，做完写出结果发回对话。
// 调度、排队、预算记账在 bgtask 包里；这里只管怎么做。

// TaskProgress 接收后台任务的进展（bgtask 写进数据库，网页上轮询看）。
type TaskProgress interface {
	// Step 是一个工具步骤开始或结束。
	Step(ev ToolEvent)
	// Round 在每轮工具结果交回后调用：用了多少 token、第几轮、现在的工作过程（打断后接着做用）。
	Round(used int64, rounds int, transcript []llm.Message)
}

// TaskRun 是一次执行的参数。
type TaskRun struct {
	MaxRounds int
	// Deadline 之后不再调用工具，让模型交出结果。
	Deadline time.Time
	// Network 为 true 时运行代码的沙箱可以联网（套餐允许）。
	Network  bool
	Progress TaskProgress
}

// TaskToolProvider 给后台任务提供工具：运行代码用宽松的资源档，按套餐决定沙箱能不能联网。
type TaskToolProvider interface {
	ForTask(ctx context.Context, u *store.User, conv *store.Conversation, model *models.Resolved, network bool) *tools.Set
}

// taskWrapUp 是预算、轮数或时间快用完时追加给模型的话。
const taskWrapUp = "〔系统提示〕时间或预算快用完了：不要再调用工具，根据目前已经做完的部分，按要求写出最终结果；没做完的地方简单说明一下。"

// taskSink 把工具步骤交给进展记录，正文不用推送（没有人在实时看）。
type taskSink struct{ p TaskProgress }

func (taskSink) Start(*store.Conversation, *store.Message, *store.Message, *models.Resolved) error {
	return nil
}
func (taskSink) Memories([]memory.Scored) error { return nil }
func (taskSink) Reasoning(string) error         { return nil }
func (taskSink) Delta(string) error             { return nil }
func (s taskSink) Tool(ev ToolEvent) error {
	if s.p != nil {
		s.p.Step(ev)
	}
	return nil
}

func (s *Service) taskSystemPrompt(u *store.User, title string, toolset *tools.Set) string {
	name := s.builder.Persona().Name
	who := u.DisplayName
	if who == "" {
		who = "对方"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "你是%s的后台工作助手，正在替%s完成一个比较大的任务「%s」。%s现在不在线等你，做完后结果会发给%s。\n\n", name, who, title, who, who)
	b.WriteString("## 怎么做\n")
	b.WriteString("- 先想清楚要分几步，再一步步用工具完成；需要最新信息就搜索、读网页（多看几个来源再下结论），需要计算或处理文件就运行代码、执行命令。不要凭印象编\n")
	b.WriteString("- 对方在这段对话里发过的文件在运行代码的 input/ 目录；做出来的文件保存在当前目录，最后用 send_file 发给对方\n")
	b.WriteString("- 不要向对方提问（对方不在线），信息不够时做合理的假设，在结果里说明\n")
	b.WriteString("- 时间和预算有限：抓住重点，不做和任务无关的事；同一件事试了几次都不行就换个办法或者跳过，在结果里说明\n\n")
	b.WriteString("## 最后交出的结果\n")
	fmt.Fprintf(&b, "- 开头一两句话用%s的口吻（像朋友在微信上那样，口语、简短）告诉对方做完了\n", name)
	b.WriteString("- 然后是结果正文：可以用 Markdown 的小标题、列表、表格，写清楚结论和依据；用了搜索结果就在最后列出参考来源（标题和链接）\n")
	b.WriteString("- 发了文件的话说一下每个文件是什么\n")
	if hints := toolHints(toolset); hints != "" {
		b.WriteString("\n" + hints)
	}
	return b.String()
}

// ExecuteTask 实现 bgtask.Executor：在后台做一个任务，做完把结果作为一条消息发到原来的对话里。
// 返回保存的消息和结果正文；出错时已经花掉的用量照样计费。
func (s *Service) ExecuteTask(ctx context.Context, u *store.User, task *store.BgTask, run TaskRun) (*ProactiveResult, string, error) {
	conv, err := s.store.Conversations().Get(ctx, u.ID, task.ConversationID)
	if err != nil {
		// 原来的对话删掉了：结果发到最近的对话里。
		if conv, err = s.latestConversation(ctx, u); err != nil {
			return nil, "", err
		}
	}
	ctx = llm.WithSession(ctx, fmt.Sprintf("buugoo-task-%d", task.ID))
	modelID := conv.ModelID
	if task.ModelID != nil {
		modelID = task.ModelID
	}
	model, err := s.models.Resolve(ctx, modelUser(u), modelID)
	if err != nil {
		return nil, "", err
	}
	if !model.Personal {
		if err := s.quota.Check(ctx, u); err != nil {
			return nil, "", err
		}
	}
	var toolset *tools.Set
	if tp, ok := s.tools.(TaskToolProvider); ok && model.Tools {
		toolset = tp.ForTask(ctx, u, conv, model, run.Network)
	} else {
		toolset = s.toolSet(ctx, u, conv, model, nil)
	}

	// 接着做被打断的任务：用之前的工作过程，告诉模型从哪接着来。
	var msgs []llm.Message
	if task.Transcript != "" && json.Unmarshal([]byte(task.Transcript), &msgs) == nil && len(msgs) > 0 {
		msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: "〔系统提示〕服务器刚才重启，任务被打断了。根据上面已经做完的部分接着做，不要从头再来。"})
	} else {
		now := s.now().In(s.loc)
		user := fmt.Sprintf("现在是 %s。\n\n任务：%s\n\n%s", now.Format("2006-01-02 15:04 Mon"), task.Title, task.Goal)
		if ctxLines := toolset.Context(); len(ctxLines) > 0 {
			user += "\n\n（" + strings.Join(ctxLines, "；") + "）"
		}
		msgs = []llm.Message{{Role: llm.RoleSystem, Content: s.taskSystemPrompt(u, task.Title, toolset)}, {Role: llm.RoleUser, Content: user}}
	}

	// 结果消息在需要时才建：工具生成了图片或文件要挂上去，或者做完了。
	var reply *store.Message
	replyFor := func() (uint64, error) {
		if reply != nil {
			return reply.ID, nil
		}
		mid := model.ID
		reply = &store.Message{ConversationID: conv.ID, UserID: u.ID, Role: store.RoleAssistant, Model: model.Model, ModelID: &mid,
			Content: "（后台任务「" + task.Title + "」正在进行中……）", Status: store.MessageInterrupted}
		if err := s.store.Messages().Create(context.WithoutCancel(ctx), reply); err != nil {
			reply = nil
			return 0, err
		}
		return reply.ID, nil
	}

	budget := int(task.BudgetTokens - task.UsedTokens)
	if budget < 1000 {
		budget = 1000
	}
	used := task.UsedTokens
	var recorded llm.Usage
	first := task.UsedTokens == 0
	// charge 把这轮新增的用量计入额度和余额（每轮都记，服务器崩了也不会白用）。
	charge := func(t *turn) {
		delta := llm.Usage{PromptTokens: t.usage.PromptTokens - recorded.PromptTokens, CompletionTokens: t.usage.CompletionTokens - recorded.CompletionTokens,
			CachedTokens: t.usage.CachedTokens - recorded.CachedTokens}
		recorded = t.usage
		if delta.PromptTokens <= 0 && delta.CompletionTokens <= 0 {
			return
		}
		used += int64(delta.PromptTokens + delta.CompletionTokens)
		if model.Personal {
			return
		}
		use := quota.Tokens(quota.ModelKey(model.ID), delta)
		if first {
			use.Messages, first = 1, false
		}
		if err := s.quota.Record(context.WithoutCancel(ctx), u.ID, use); err != nil {
			s.log.Error("记录后台任务用量失败", "user_id", u.ID, "task_id", task.ID, "err", err)
		}
	}
	t := &turn{s: s, ctx: ctx, model: model, thinking: store.ThinkingOff, tools: toolset, sink: taskSink{run.Progress}, msgs: msgs,
		userID: u.ID, convID: conv.ID, maxRounds: run.MaxRounds, budget: budget, wrapUp: taskWrapUp, deadline: run.Deadline, replyFor: replyFor}
	t.rounds = task.Rounds
	if t.maxRounds > 0 {
		t.maxRounds += task.Rounds // 接着做时，之前用掉的轮数也算
	}
	t.onRound = func(t *turn) {
		// 服务商没返回用量：按字数估算这一轮，预算和计费都按估算的来。
		if !t.usageReported {
			var in int
			for _, m := range t.msgs {
				in += prompt.EstimateTokens(m.Content) + 4
			}
			t.usage.Add(llm.Usage{PromptTokens: in, CompletionTokens: 200})
		}
		charge(t)
		if run.Progress != nil {
			run.Progress.Round(used, t.rounds, t.msgs)
		}
	}
	genErr := t.run(func() error { return nil })
	if !t.usageReported && t.final != "" {
		t.usage.Add(llm.Usage{PromptTokens: prompt.EstimateTokens(t.msgs[0].Content) * 2, CompletionTokens: prompt.EstimateTokens(t.final)})
	}
	charge(t)
	if run.Progress != nil {
		run.Progress.Round(used, t.rounds, t.msgs)
	}
	bg := context.WithoutCancel(ctx)
	report := strings.TrimSpace(stripToolNotes(t.final))
	if genErr != nil || report == "" {
		if reply != nil {
			// 做到一半失败了：已经生成的文件还在，留下说明。
			reply.Content, reply.Status = "（后台任务「"+task.Title+"」没有做完，生成的文件在下面。）", store.MessageComplete
			_ = s.store.Messages().Finish(bg, reply)
		}
		if genErr == nil {
			genErr = errors.New("模型没有交出结果")
		}
		return nil, "", genErr
	}
	if _, err := replyFor(); err != nil {
		return nil, "", fmt.Errorf("保存结果: %w", err)
	}
	reply.Content, reply.Status = report, store.MessageComplete
	reply.PromptTokens, reply.CompletionTokens, reply.CachedTokens = t.usage.PromptTokens, t.usage.CompletionTokens, cachedOf(t.usage)
	if len(t.records) > 0 {
		b, _ := json.Marshal(t.records)
		reply.ToolLog = string(b)
	}
	if err := s.store.Messages().Finish(bg, reply); err != nil {
		return nil, "", fmt.Errorf("保存结果: %w", err)
	}
	if err := s.store.Conversations().Touch(bg, u.ID, conv.ID, s.now()); err != nil {
		s.log.Warn("更新对话时间失败", "err", err)
	}
	return &ProactiveResult{Conversation: conv, Message: reply, Tools: t.records}, report, nil
}
