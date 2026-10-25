package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/timefmt"
)

// Candidate 是 LLM 从一轮对话中提取出的候选记忆。
type Candidate struct {
	Type       string  `json:"type"`
	Content    string  `json:"content"`
	Importance int     `json:"importance"`
	Emotion    float64 `json:"emotion"`
	Source     string  `json:"source"`
}

// ExtractInput 是记忆提取的输入。
type ExtractInput struct {
	UserName       string
	UserMessage    string
	AssistantReply string
	// Context 是本轮之前的几条消息，帮助理解指代。
	Context []llm.Message
	// Known 是已知的相关记忆，避免重复提取。
	Known []string
	Now   time.Time
}

// Extractor 从对话中提取值得记住的内容并打分。chat 为本轮对话使用的模型，为空时使用默认模型。
type Extractor interface {
	Extract(ctx context.Context, chat llm.ChatProvider, in ExtractInput) ([]Candidate, llm.Usage, error)
}

// LLMExtractor 用 LLM 实现 Extractor。
type LLMExtractor struct {
	fallback llm.ChatProvider
}

// NewLLMExtractor 创建提取器。fallback 在任务没有指定模型时使用（可为空）。
func NewLLMExtractor(fallback llm.ChatProvider) *LLMExtractor {
	return &LLMExtractor{fallback: fallback}
}

const maxMemoryRunes = 300

const extractSystemPrompt = `你是一个记忆整理助手，负责从"用户"与 AI 朋友"布谷"的一轮对话中，提取值得长期记住的、关于用户的信息。

只提取关于用户本人及其生活的信息，例如：个人情况、经历的事件、计划与目标、喜好与厌恶、习惯、人际关系、健康状况、情绪状态及其原因、重要日期。
不要提取：寒暄、闲聊中没有信息量的内容、关于布谷自己的内容、通用知识、已知记忆中已经有的信息。

每条记忆需要给出：
- type："episodic"（情景记忆：某个时间发生的具体事件，例如"10 月 24 日和室友吵了一架"）或 "semantic"（语义记忆：相对稳定的事实，例如"用户对花生过敏"）
- content：一句完整、独立可理解的中文陈述，以"用户"作主语，不超过 100 字；涉及时间时写出具体日期而不是"今天""昨天"
- importance：1-10 的整数。1-3 琐碎小事；4-6 有一定价值的日常信息；7-8 重要的个人信息或事件；9-10 极其重要（重大人生事件、健康安全、核心身份）
- emotion：0-1 的小数，表示这件事对用户的情绪强度（0 为平淡，1 为极其强烈）
- source：
  - "stated"：用户在本轮对话中亲口明确说出的
  - "inferred"：你根据对话推测的、用户并没有直接说出的（必须谨慎，只有把握较大时才提取，并在 content 中体现推测语气，例如"用户可能……"）
  布谷说的话不能作为 stated 的依据。

严格只输出一个 JSON 对象，格式为：
{"memories":[{"type":"semantic","content":"...","importance":5,"emotion":0.2,"source":"stated"}]}
没有值得记住的内容时输出 {"memories":[]}。`

// Extract 实现 Extractor。
func (e *LLMExtractor) Extract(ctx context.Context, chat llm.ChatProvider, in ExtractInput) ([]Candidate, llm.Usage, error) {
	if chat == nil {
		chat = e.fallback
	}
	if chat == nil {
		return nil, llm.Usage{}, errors.New("没有可用于记忆提取的模型")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "当前时间：%s\n", timefmt.Full(in.Now))
	if in.UserName != "" {
		fmt.Fprintf(&b, "用户称呼：%s\n", in.UserName)
	}
	if len(in.Known) > 0 {
		b.WriteString("\n已知记忆（不要重复提取）：\n")
		for _, k := range in.Known {
			fmt.Fprintf(&b, "- %s\n", k)
		}
	}
	if len(in.Context) > 0 {
		b.WriteString("\n此前的对话（仅供理解上下文，不要从中提取）：\n")
		for _, m := range in.Context {
			fmt.Fprintf(&b, "%s：%s\n", speaker(m.Role), truncate(m.Content, 500))
		}
	}
	b.WriteString("\n本轮对话：\n")
	fmt.Fprintf(&b, "用户：%s\n", in.UserMessage)
	fmt.Fprintf(&b, "布谷：%s\n", in.AssistantReply)

	// 记忆提取不开启思考，降低成本。
	resp, err := chat.Chat(ctx, llm.ChatRequest{
		JSON:     true,
		Thinking: "off",
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: extractSystemPrompt},
			{Role: llm.RoleUser, Content: b.String()},
		},
	})
	if err != nil {
		return nil, llm.Usage{}, fmt.Errorf("调用记忆提取模型: %w", err)
	}
	cands, err := ParseCandidates(resp.Content)
	if err != nil {
		return nil, resp.Usage, err
	}
	return cands, resp.Usage, nil
}

func speaker(role string) string {
	if role == llm.RoleAssistant {
		return "布谷"
	}
	return "用户"
}

// ParseCandidates 解析并校验模型输出，丢弃不合法的条目。
func ParseCandidates(raw string) ([]Candidate, error) {
	raw = strings.TrimSpace(raw)
	// 兼容模型用 ```json 代码块包裹输出的情况。
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
	}
	if i, j := strings.Index(raw, "{"), strings.LastIndex(raw, "}"); i >= 0 && j > i {
		raw = raw[i : j+1]
	} else {
		return nil, errors.New("记忆提取结果不是 JSON 对象")
	}
	var parsed struct {
		Memories []struct {
			Type       string      `json:"type"`
			Content    string      `json:"content"`
			Importance json.Number `json:"importance"`
			Emotion    json.Number `json:"emotion"`
			Source     string      `json:"source"`
		} `json:"memories"`
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("解析记忆提取结果: %w", err)
	}
	out := make([]Candidate, 0, len(parsed.Memories))
	for _, m := range parsed.Memories {
		c := Candidate{
			Type:    strings.ToLower(strings.TrimSpace(m.Type)),
			Content: strings.TrimSpace(m.Content),
			Source:  strings.ToLower(strings.TrimSpace(m.Source)),
		}
		if c.Content == "" {
			continue
		}
		if c.Type != store.MemoryEpisodic && c.Type != store.MemorySemantic {
			continue
		}
		// 来源不明确时按推断处理，宁可低估也不把推测当成事实。
		if c.Source != store.SourceStated {
			c.Source = store.SourceInferred
		}
		imp, err := m.Importance.Float64()
		if err != nil {
			continue
		}
		c.Importance = int(clamp(imp+0.5, 1, 10))
		if emo, err := m.Emotion.Float64(); err == nil {
			c.Emotion = clamp(emo, 0, 1)
		}
		c.Content = truncate(c.Content, maxMemoryRunes)
		out = append(out, c)
	}
	return out, nil
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}
