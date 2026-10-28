package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/prompt"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// 上下文预算：按模型的上下文长度计算，而不是按消息条数。
//
//   - 每轮把摘要之后的全部消息放进提示词，超出预算时从最早的开始舍弃（兜底）；
//   - 一轮结束后估算这轮用了多少上下文，超过 compact_at% 时，回复照常发出，
//     下一条消息处理前先压缩：较早的消息整理进摘要，最近约 keep_recent% 的消息保留原文；
//   - 用户也可以随时手动压缩。

const (
	// maxHistoryLoad 是一次最多读取的未整理消息条数（防止关掉摘要后无限增长）。
	maxHistoryLoad = 1000
	// summaryChunkTokens 是整理摘要时一次交给辅助模型的消息量，太长的分几次整理。
	summaryChunkTokens = 24000
	// defaultReplyReserve 是模型没有设置最大输出时，给回复预留的 token。
	defaultReplyReserve = 4096
)

// ErrNothingToCompact 表示没有可以压缩的消息。
var ErrNothingToCompact = errors.New("最近的聊天还不多，不需要压缩")

// ErrCompacting 表示这段对话正在压缩。
var ErrCompacting = errors.New("正在压缩，请稍等")

// ErrSummaryDisabled 表示没有开启对话摘要。
var ErrSummaryDisabled = errors.New("管理员没有开启对话摘要（chat.summarize），不能压缩")

// contextSize 返回模型的上下文长度与提示词预算（上下文长度减去给回复预留的部分）。
func contextSize(model *models.Resolved) (size, budget int) {
	size = models.DefaultContextTokens
	reserve := defaultReplyReserve
	if model != nil {
		if model.ContextTokens > 0 {
			size = model.ContextTokens
		}
		if model.MaxTokens > 0 {
			reserve = model.MaxTokens
		}
	}
	reserve = min(reserve, size/4) + 256
	return size, size - reserve
}

func (s *Service) compactAt() int {
	if s.cfg.CompactAt <= 0 || s.cfg.CompactAt > 100 {
		return 80
	}
	return s.cfg.CompactAt
}

func (s *Service) keepRecent() int {
	if s.cfg.KeepRecent <= 0 || s.cfg.KeepRecent >= s.compactAt() {
		return min(20, s.compactAt()/2)
	}
	return s.cfg.KeepRecent
}

// unsummarized 返回摘要之后的消息（按时间正序，最多 maxHistoryLoad 条）。
func (s *Service) unsummarized(ctx context.Context, u *store.User, conv *store.Conversation) ([]store.Message, error) {
	history, err := s.store.Messages().Recent(ctx, u.ID, conv.ID, maxHistoryLoad)
	if err != nil {
		return nil, err
	}
	i := 0
	for i < len(history) && history[i].ID <= conv.SummaryUntil {
		i++
	}
	return history[i:], nil
}

// 工具记录的长度限制。
const (
	toolNoteInput  = 200
	toolNoteDetail = 800
	toolNoteTotal  = 2500
)

// toolNote 把一条回复调用过的工具整理成要点（工具、输入、结果），放回历史，模型之后还能用上当时的结果。
func toolNote(m *store.Message) string {
	if m.Role != store.RoleAssistant || m.ToolLog == "" {
		return ""
	}
	var evs []ToolEvent
	if json.Unmarshal([]byte(m.ToolLog), &evs) != nil {
		return ""
	}
	var b strings.Builder
	for _, ev := range evs {
		if ev.Status == "running" {
			continue
		}
		label := ev.Label
		if label == "" {
			label = ev.Name
		}
		b.WriteString("- " + label)
		if in := clip(oneLine(ev.Input), toolNoteInput); in != "" {
			b.WriteString("「" + in + "」")
		}
		if ev.Summary != "" {
			b.WriteString("：" + ev.Summary)
		}
		if d := clip(strings.TrimSpace(ev.Detail), toolNoteDetail); d != "" {
			b.WriteString("\n  " + strings.ReplaceAll(d, "\n", "\n  "))
		}
		for _, f := range append(append([]ToolImage{}, ev.Images...), ev.Files...) {
			b.WriteString("\n  （发给了对方：" + f.Filename + "）")
		}
		b.WriteString("\n")
		if b.Len() > toolNoteTotal*3 {
			break
		}
	}
	return clip(strings.TrimSpace(b.String()), toolNoteTotal)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// 工具记录的开头与结尾标记。
const (
	toolNoteStart = "〔工具记录"
	toolNoteEnd   = "〔工具记录结束〕"
)

// withToolNotes 把工具记录放回历史：附在引出这次回复的那条用户消息后面，并标明是系统附上的、对方看不到。
// 不放进助手自己的消息里，否则模型会照着这个格式把记录写进新的回复。前面没有用户消息时才放在助手消息前面。
func withToolNotes(history []store.Message) []store.Message {
	out := make([]store.Message, len(history))
	copy(out, history)
	for i := range out {
		if out[i].Role == store.RoleAssistant {
			out[i].Content = stripToolNotes(out[i].Content) // 以前照抄进回复里的记录不再放回历史
		}
		note := toolNote(&out[i])
		if note == "" {
			continue
		}
		j := i - 1
		for j >= 0 && out[j].Role != store.RoleUser {
			j--
		}
		if j >= 0 {
			out[j].Content += "\n\n" + toolNoteStart + "：系统附上的，不是对方说的，对方也看不到。你回复这条消息时用过的工具和结果要点〕\n" + note + "\n" + toolNoteEnd
		} else {
			out[i].Content = toolNoteStart + "：系统附上的，对方看不到。你回复前用过的工具和结果要点〕\n" + note + "\n" + toolNoteEnd + "\n\n" + out[i].Content
		}
	}
	return out
}

// StripToolNotes 去掉回复里照抄的工具记录（网页显示以前的消息时用）。
func StripToolNotes(text string) string { return stripToolNotes(text) }

// stripToolNotes 去掉模型照抄进回复里的工具记录。有结尾标记时去掉整段；没有结尾时去掉开头那行和紧跟着的列表。
func stripToolNotes(text string) string {
	// 模型偶尔会把附上的〔此刻〕信息照抄进回复，一并去掉。
	for {
		start := strings.Index(text, prompt.NowStart)
		if start < 0 {
			break
		}
		end := strings.Index(text[start:], prompt.NowEnd)
		if end < 0 {
			text = text[:start]
			break
		}
		text = text[:start] + text[start+end+len(prompt.NowEnd):]
	}
	if !strings.Contains(text, toolNoteStart) {
		return strings.TrimSpace(text)
	}
	for {
		start := strings.Index(text, toolNoteStart)
		if start < 0 {
			break
		}
		if end := strings.Index(text[start:], toolNoteEnd); end >= 0 {
			text = text[:start] + text[start+end+len(toolNoteEnd):]
			continue
		}
		lines := strings.Split(text[start:], "\n")
		k := 1
		for k < len(lines) {
			l := strings.TrimSpace(lines[k])
			if l == "" || strings.HasPrefix(l, "-") || strings.HasPrefix(l, "•") || strings.HasPrefix(lines[k], " ") {
				k++
				continue
			}
			break
		}
		text = text[:start] + strings.Join(lines[k:], "\n")
	}
	// 去掉留下的多余空行。
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(text)
}

// messageTokens 估算一条消息占用的上下文。
func messageTokens(m *store.Message) int {
	return prompt.EstimateTokens(m.Content) + prompt.EstimateTokens(toolNote(m)) + 4
}

// recordContext 记录这一轮用了多少上下文；超过阈值（或者已经有消息因为放不下被舍弃）时标记下一条消息前先压缩。
// 上下文很小的模型给回复预留的部分占比大，阈值不超过可用预算的 95%。
func (s *Service) recordContext(ctx context.Context, u *store.User, conv *store.Conversation, model *models.Resolved, used int, dropped bool) {
	size, budget := contextSize(model)
	limit := min(size*s.compactAt()/100, budget*95/100)
	pending := s.summarizer != nil && (used >= limit || dropped)
	conv.ContextUsed, conv.CompactPending = used, pending
	if err := s.store.Conversations().Update(ctx, u.ID, conv.ID, map[string]any{"context_used": used, "compact_pending": pending}); err != nil {
		s.log.Warn("记录上下文用量失败", "conversation_id", conv.ID, "err", err)
	}
}

// ContextStatus 是一段对话的上下文用量。
type ContextStatus struct {
	// Used 是最近一轮大约用了多少上下文（token），Size 是模型的上下文长度。
	Used    int `json:"used"`
	Size    int `json:"size"`
	Percent int `json:"percent"`
	// CompactAt 是自动压缩的阈值（百分比），KeepRecent 是压缩后保留原文的比例。
	CompactAt  int  `json:"compact_at"`
	KeepRecent int  `json:"keep_recent"`
	Pending    bool `json:"pending"`
	// Summary 是现在的前情提要，Summarized 表示有消息已经整理进摘要。
	Summary    string `json:"summary"`
	Summarized bool   `json:"summarized"`
	Enabled    bool   `json:"enabled"`
	Model      string `json:"model"`
}

// Context 返回对话的上下文用量。还没统计过（旧对话）时按摘要和未整理的消息估算。
func (s *Service) Context(ctx context.Context, u *store.User, convID uint64) (*ContextStatus, error) {
	conv, err := s.conversation(ctx, u, convID)
	if err != nil {
		return nil, err
	}
	model, err := s.models.Resolve(ctx, modelUser(u), conv.ModelID)
	if err != nil && !errors.Is(err, models.ErrNoModel) {
		return nil, err
	}
	size, _ := contextSize(model)
	used := conv.ContextUsed
	if used == 0 {
		if msgs, err := s.unsummarized(ctx, u, conv); err == nil && len(msgs) > 0 {
			used = prompt.EstimateTokens(conv.Summary) + 1500
			for i := range msgs {
				used += messageTokens(&msgs[i])
			}
		}
	}
	st := &ContextStatus{Used: used, Size: size, Percent: min(100, used*100/max(1, size)), CompactAt: s.compactAt(), KeepRecent: s.keepRecent(),
		Pending: conv.CompactPending, Summary: conv.Summary, Summarized: conv.SummaryUntil > 0, Enabled: s.summarizer != nil}
	if model != nil {
		st.Model = model.Name
	}
	return st, nil
}

// Compact 压缩对话的上下文：较早的消息整理进摘要，最近约 keep_recent% 的消息保留原文。
func (s *Service) Compact(ctx context.Context, u *store.User, convID uint64) (*ContextStatus, error) {
	if s.summarizer == nil {
		return nil, ErrSummaryDisabled
	}
	conv, err := s.conversation(ctx, u, convID)
	if err != nil {
		return nil, err
	}
	model, err := s.models.Resolve(ctx, modelUser(u), conv.ModelID)
	if err != nil && !errors.Is(err, models.ErrNoModel) {
		return nil, err
	}
	if err := s.compact(ctx, u, conv, model); err != nil {
		return nil, err
	}
	return s.Context(ctx, u, conv.ID)
}

// compact 是压缩的实现：同一段对话同时只压缩一次。
func (s *Service) compact(ctx context.Context, u *store.User, conv *store.Conversation, model *models.Resolved) error {
	s.sumMu.Lock()
	if s.summarizing[conv.ID] {
		s.sumMu.Unlock()
		return ErrCompacting
	}
	s.summarizing[conv.ID] = true
	s.sumMu.Unlock()
	defer func() {
		s.sumMu.Lock()
		delete(s.summarizing, conv.ID)
		s.sumMu.Unlock()
	}()

	msgs, err := s.unsummarized(ctx, u, conv)
	if err != nil {
		return err
	}
	size, _ := contextSize(model)
	keepBudget := size * s.keepRecent() / 100
	// 从最新往前保留原文，直到用完保留的预算；最后一轮（至少一条）总是保留。
	keep, kept := len(msgs), 0
	for i := len(msgs) - 1; i >= 0; i-- {
		t := messageTokens(&msgs[i])
		if kept+t > keepBudget && keep < len(msgs) {
			break
		}
		kept += t
		keep = i
	}
	fold := msgs[:keep]
	if len(fold) == 0 {
		if conv.CompactPending {
			_ = s.store.Conversations().Update(ctx, u.ID, conv.ID, map[string]any{"compact_pending": false})
			conv.CompactPending = false
		}
		return ErrNothingToCompact
	}
	summary := conv.Summary
	folded := withToolNotes(fold)
	for start := 0; start < len(folded); {
		end, tokens := start, 0
		for end < len(folded) && (end == start || tokens+prompt.EstimateTokens(folded[end].Content) <= summaryChunkTokens) {
			tokens += prompt.EstimateTokens(folded[end].Content)
			end++
		}
		next, usage, err := s.summarizer.Summarize(ctx, s.builder.Persona().Name, summary, folded[start:end])
		if err != nil || next == "" {
			if err == nil {
				err = errors.New("辅助模型返回了空的摘要")
			}
			return fmt.Errorf("整理摘要失败：%w", err)
		}
		if err := s.quota.Record(ctx, u.ID, quota.Tokens(quota.KeyUtility, usage)); err != nil {
			s.log.Error("记录用量失败", "user_id", u.ID, "err", err)
		}
		summary = next
		start = end
	}
	// 压缩后的用量：摘要 + 保留的原文 + 固定部分（人格、记忆等，按压缩前的用量估算）。
	foldedTokens := 0
	for i := range fold {
		foldedTokens += messageTokens(&fold[i])
	}
	used := max(prompt.EstimateTokens(summary)+kept, conv.ContextUsed-foldedTokens+prompt.EstimateTokens(summary)-prompt.EstimateTokens(conv.Summary))
	fields := map[string]any{"summary": summary, "summary_until": fold[len(fold)-1].ID, "compact_pending": false, "context_used": used}
	if err := s.store.Conversations().Update(ctx, u.ID, conv.ID, fields); err != nil {
		return err
	}
	conv.Summary, conv.SummaryUntil, conv.CompactPending, conv.ContextUsed = summary, fold[len(fold)-1].ID, false, used
	s.log.Info("已压缩对话上下文", "user_id", u.ID, "conversation_id", conv.ID, "folded", len(fold), "kept", len(msgs)-len(fold))
	return nil
}
