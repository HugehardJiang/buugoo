package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// maxSummaryRunes 是摘要的长度上限（数据库中摘要列为 4000 字）。
const maxSummaryRunes = 2400

// Summarizer 把较早的消息整理成摘要。
type Summarizer interface {
	// Summarize 用 chat（这段对话用的模型）整理摘要，chat 为空时用辅助模型。
	Summarize(ctx context.Context, chat llm.ChatProvider, name, previous string, msgs []store.Message) (string, llm.Usage, error)
}

// LLMSummarizer 整理摘要；没指定模型时用辅助模型。
type LLMSummarizer struct{ LLM llm.ChatProvider }

const summaryPrompt = `你在帮 AI 朋友"%s"整理一段长对话的前情提要。下面给出已有的摘要（可能为空）和之后的对话，请写出新的摘要，替换旧摘要。

要求：
- 用简洁的要点，第三人称（"用户""%s"），不超过 1500 字
- 保留：聊过的主要话题和结论、用户提到的事实和计划、双方的约定和答应过的事、还没聊完的话题、用户的情绪变化
- 工具记录（搜索、运行代码等）只保留结论和之后可能还要用到的关键数据（数字、日期、网址、文件名），去掉过程细节
- 去掉：寒暄、重复的内容
- 较早的、不重要的内容可以压缩成一句话
- 只输出摘要本身`

// Summarize 实现 Summarizer。
func (s LLMSummarizer) Summarize(ctx context.Context, chat llm.ChatProvider, name, previous string, msgs []store.Message) (string, llm.Usage, error) {
	if chat == nil {
		chat = s.LLM
	}
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
	resp, err := chat.Chat(ctx, llm.ChatRequest{Thinking: "off", Messages: []llm.Message{
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

// Wait 等待后台任务（对话摘要）结束，用于测试与退出。
func (s *Service) Wait() { s.bg.Wait() }
