package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HugehardJiang/buugoo/internal/cosplay"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/tools"
)

// 自我进化：Cosplay 对话里，对方明确要求改进角色（说话方式、设定、规则）时，AI 改自己的角色提示词。
// 每轮最多改两次；改动下一条消息起生效，改之前的样子在「设置 → 角色卡」里可以恢复。

const (
	evolveName    = "improve_character"
	evolvePerTurn = 2
)

const evolveHint = "improve_character：对方明确要求你改进或调整角色（例如「以后别老用括号」「说话再凶一点」「记住你是我妹妹」「把某条设定改掉」）时，" +
	"用它把要求写进你自己的角色卡，下一条消息起生效。只有对方明确要求改设定、改说话方式时才用；平时聊天、对方只是随口吐槽时不要用。" +
	"一般用 append 追加一条简短明确的要求；要改掉卡里原有的某条规则时用 replace，old 一字不差地照抄原文。改完简短地告诉对方改了什么。"

func evolveTools(st store.Store, cards *cosplay.Service, u *store.User, conv *store.Conversation) []tools.Extra {
	calls := 0
	return []tools.Extra{{
		Def: llm.Tool{
			Name:        evolveName,
			Description: "修改你正在扮演的角色卡（自我进化）。只在对方明确要求改进或调整你的角色设定、说话方式时用，下一条消息起生效。",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"mode": map[string]any{"type": "string", "enum": []string{cosplay.EditAppend, cosplay.EditReplace},
						"description": "append：在角色卡最后追加一条要求；replace：把卡里的一段原文换成新的（新内容为空就是删掉）"},
					"text": map[string]any{"type": "string", "description": "追加的要求，或替换后的新内容。写成给角色自己看的规则，简短明确"},
					"old":  map[string]any{"type": "string", "description": "replace 时要换掉的原文，要和角色卡里的一字不差"},
					"note": map[string]any{"type": "string", "description": "一句话说明改了什么（记进修改记录，例如「少用括号」）"},
				},
				"required": []string{"mode", "text", "note"},
			},
		},
		Label: "调整角色设定",
		Hint:  evolveHint,
		Run: func(ctx context.Context, args json.RawMessage) tools.Result {
			var in struct {
				Mode, Text, Old, Note string
			}
			_ = json.Unmarshal(args, &in)
			res := tools.Result{Input: strings.TrimSpace(in.Note)}
			if calls >= evolvePerTurn {
				res.Output, res.Summary = "这一轮已经改过角色卡了，先这样，下一轮再改", "改得太多了"
				return res
			}
			calls++
			card, copied, err := cards.Revise(ctx, cosplay.User{ID: u.ID, IsAdmin: u.IsAdmin}, *conv.CharacterID,
				cosplay.Edit{Mode: in.Mode, Old: in.Old, Text: in.Text, Note: in.Note})
			if err != nil {
				res.Output, res.Summary = "没改成："+err.Error(), "没改成"
				return res
			}
			msg := "已经改好角色卡，下一条消息起按新的设定说话。"
			if copied {
				// 公共卡不能被某个人改掉：复制了一张对方自己的，这段对话换成用新卡。
				if err := st.Conversations().Update(ctx, u.ID, conv.ID, map[string]any{"character_id": card.ID, "character_name": card.Name}); err != nil {
					res.Output, res.Summary = "改好了，但这段对话没能换成新卡："+err.Error(), "没改成"
					return res
				}
				conv.CharacterID, conv.CharacterName = &card.ID, card.Name
				msg = "这是公共角色卡，已经复制了一张对方自己的卡并改好，这段对话之后用新卡，下一条消息起生效。"
			}
			res.Output = msg + "改之前的样子在「设置 → 角色卡」里可以恢复。"
			res.Summary, res.OK = fmt.Sprintf("已改：%s", in.Note), true
			if strings.TrimSpace(in.Note) == "" {
				res.Summary = "已改好"
			}
			res.Detail = in.Text
			return res
		},
	}}
}
