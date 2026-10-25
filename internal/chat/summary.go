package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// summarySlack 是触发摘要前允许多积累的消息条数：未整理的消息超过「历史条数 + summarySlack」时，
// 把较早的部分整理进摘要，只留最近的「历史条数」条原样放进提示词。
const summarySlack = 8

// maxSummaryRunes 是摘要的长度上限。
const maxSummaryRunes = 800

// Summarizer 把较早的消息整理成摘要。
type Summarizer interface {
	Summarize(ctx context.Context, name, previous string, msgs []store.Message) (string, llm.Usage, error)
}

// LLMSummarizer 用辅助模型整理摘要。
type LLMSummarizer struct{ LLM llm.ChatProvider }

const summaryPrompt = `你在帮 AI 朋友"%s"整理一段长对话的前情提要。下面给出已有的摘要（可能为空）和之后的对话，请写出新的摘要，替换旧摘要。

要求：
- 用简洁的要点，第三人称（"用户""%s"），不超过 500 字
- 保留：聊过的主要话题和结论、用户提到的事实和计划、双方的约定和答应过的事、还没聊完的话题、用户的情绪变化
- 去掉：寒暄、重复的内容、搜索结果和代码的细节
- 较早的、不重要的内容可以压缩成一句话
- 只输出摘要本身`

// Summarize 实现 Summarizer。
func (s LLMSummarizer) Summarize(ctx context.Context, name, previous string, msgs []store.Message) (string, llm.Usage, error) {
	var b strings.Builder
	if previous != "" {
		b.WriteString("【已有摘要】\n" + previous + "\n\n")
	}
	b.WriteString("【之后的对话】\n")
	for _, m := range msgs {
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		if r := []rune(text); len(r) > 1500 {
			text = string(r[:1500]) + "…"
		}
		who := "用户"
		if m.Role == store.RoleAssistant {
			who = name
		}
		fmt.Fprintf(&b, "%s（%s）：%s\n", who, m.CreatedAt.Format("01-02 15:04"), text)
	}
	resp, err := s.LLM.Chat(ctx, llm.ChatRequest{Thinking: "off", Messages: []llm.Message{
		{Role: llm.RoleSystem, Content: fmt.Sprintf(summaryPrompt, name, name)},
		{Role: llm.RoleUser, Content: b.String()},
	}})
	if err != nil {
		return "", llm.Usage{}, err
	}
	out := strings.TrimSpace(resp.Content)
	if r := []rune(out); len(r) > maxSummaryRunes {
		out = string(r[:maxSummaryRunes]) + "…"
	}
	return out, resp.Usage, nil
}

// maybeSummarize 在未整理的消息太多时，把较早的部分整理进摘要。在后台运行，失败只记录日志（下次再试）。
func (s *Service) maybeSummarize(ctx context.Context, u *store.User, convID uint64) {
	if s.summarizer == nil {
		return
	}
	s.sumMu.Lock()
	if s.summarizing[convID] {
		s.sumMu.Unlock()
		return
	}
	s.summarizing[convID] = true
	s.sumMu.Unlock()
	defer func() {
		s.sumMu.Lock()
		delete(s.summarizing, convID)
		s.sumMu.Unlock()
	}()

	conv, err := s.store.Conversations().Get(ctx, u.ID, convID)
	if err != nil {
		return
	}
	keep := s.cfg.HistoryMessages
	pending, err := s.store.Messages().After(ctx, u.ID, convID, conv.SummaryUntil, keep+summarySlack+200)
	if err != nil || len(pending) < keep+summarySlack {
		return
	}
	fold := pending[:len(pending)-keep]
	summary, usage, err := s.summarizer.Summarize(ctx, s.builder.Persona().Name, conv.Summary, fold)
	if err != nil || summary == "" {
		s.log.Warn("整理对话摘要失败，下次再试", "user_id", u.ID, "conversation_id", convID, "err", err)
		return
	}
	if err := s.quota.Record(ctx, u.ID, 0, usage.PromptTokens, usage.CompletionTokens); err != nil {
		s.log.Error("记录用量失败", "user_id", u.ID, "err", err)
	}
	if err := s.store.Conversations().Update(ctx, u.ID, convID, map[string]any{
		"summary": summary, "summary_until": fold[len(fold)-1].ID,
	}); err != nil {
		s.log.Warn("保存对话摘要失败", "conversation_id", convID, "err", err)
	}
}

// Wait 等待后台任务（对话摘要）结束，用于测试与退出。
func (s *Service) Wait() { s.bg.Wait() }
