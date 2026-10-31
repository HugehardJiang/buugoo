package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/timefmt"
	"github.com/HugehardJiang/buugoo/internal/tools"
)

// 回看记忆的出处：每条记忆记着它来自哪段对话的哪条消息（提示词里的「#编号」）。
// 只有记忆里的一句话不够、对方又在问那次聊天的具体细节时才翻原来的聊天记录。
// 为了省额度：每轮最多翻一次，只取出处前后几条消息，每条和总长度都有上限；
// 工具说明是固定的，不会让提示词缓存失效。

const (
	recallName    = "recall_chat"
	recallBefore  = 4    // 出处之前取几条（含出处那条）
	recallAfter   = 5    // 出处之后取几条
	recallPerMsg  = 200  // 每条消息最多多少字
	recallTotal   = 2000 // 一次最多返回多少字
	recallPerTurn = 1    // 每轮回复最多翻几次
)

const recallHint = "recall_chat：记忆后面的「#编号」可以用来翻看那条记忆出自哪段聊天。" +
	"只有对方明确问到过去某次聊天的具体细节（原话、当时怎么说的、具体数字），而记忆里那一句又不够回答时才用；" +
	"记忆已经够用、或者只是顺口提到时不要用。每轮最多用一次。"

func recallTools(st store.Store, userID uint64, loc *time.Location) []tools.Extra {
	calls := 0
	return []tools.Extra{{
		Def: llm.Tool{
			Name:        recallName,
			Description: "翻看一条记忆出自哪段聊天：返回那段聊天里出处前后的几条消息（只读）。很少需要，只在记忆不够回答对方问的具体细节时用。",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"memory_id": map[string]any{"type": "integer", "description": "记忆后面的 # 编号"},
				},
				"required": []string{"memory_id"},
			},
		},
		Label: "翻看聊天记录",
		Hint:  recallHint,
		Run: func(ctx context.Context, args json.RawMessage) tools.Result {
			var in struct {
				MemoryID uint64 `json:"memory_id"`
			}
			_ = json.Unmarshal(args, &in)
			res := tools.Result{Input: fmt.Sprintf("#%d", in.MemoryID)}
			if calls >= recallPerTurn {
				res.Output, res.Summary = "这一轮已经翻看过聊天记录了，用已有的信息回答吧", "已翻看过"
				return res
			}
			calls++
			out, summary := recall(ctx, st, userID, in.MemoryID, loc)
			res.Output, res.Summary, res.OK = out, summary, summary == "找到了"
			return res
		},
	}}
}

func recall(ctx context.Context, st store.Store, userID, memoryID uint64, loc *time.Location) (string, string) {
	m, err := st.Memories().Get(ctx, userID, memoryID)
	if err != nil || m.SourceMessageID == nil || m.ConversationID == nil {
		return "这条记忆没有记下出处（可能是整理合并出来的，或者编号不对），只能用记忆里的内容回答", "没有出处"
	}
	conv, err := st.Conversations().Get(ctx, userID, *m.ConversationID)
	if err != nil {
		return "那段聊天已经被删掉了，只能用记忆里的内容回答", "聊天已删除"
	}
	src := *m.SourceMessageID
	before, err := st.Messages().Before(ctx, userID, conv.ID, src+1, recallBefore)
	if err != nil {
		return "读取聊天记录失败了，先用记忆里的内容回答", "读取失败"
	}
	after, err := st.Messages().After(ctx, userID, conv.ID, src, recallAfter)
	if err != nil {
		return "读取聊天记录失败了，先用记忆里的内容回答", "读取失败"
	}
	msgs := append(before, after...)
	if len(msgs) == 0 {
		return "那几条消息已经被删掉了，只能用记忆里的内容回答", "消息已删除"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "记忆 #%d 出自这段聊天（%s）：\n", memoryID, timefmt.Date(msgs[0].CreatedAt.In(loc)))
	for _, msg := range msgs {
		text := strings.Join(strings.Fields(msg.Content), " ")
		if text == "" {
			continue
		}
		if r := []rune(text); len(r) > recallPerMsg {
			text = string(r[:recallPerMsg]) + "…"
		}
		who := "对方"
		if msg.Role == store.RoleAssistant {
			who = "你"
		}
		line := fmt.Sprintf("%s %s：%s\n", msg.CreatedAt.In(loc).Format("15:04"), who, text)
		if b.Len()+len(line) > recallTotal*3 { // 按字节粗算（中文一个字约 3 字节）
			b.WriteString("……（后面的省略了）\n")
			break
		}
		b.WriteString(line)
	}
	return b.String(), "找到了"
}
