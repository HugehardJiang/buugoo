package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HugehardJiang/buugoo/internal/bgtask"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/tools"
)

// 提出后台长任务（Router 那一层）：聊天里遇到比较大的事，聊天的模型不自己硬做，而是提出一个后台任务。
// 网页上会出现确认卡片（可以调预算），对方确认后才开始；在聊天软件里对方没法点卡片，要先口头问过、对方答应了再提。

const startTaskName = "start_task"

func taskTools(tasks *bgtask.Service, u *store.User, conv *store.Conversation) []tools.Extra {
	inApp := conv.Channel != ""
	when := "网页上会给对方一张确认卡片，对方确认后才开始；提出后简短地告诉对方要做什么、请对方在卡片上确认。"
	if inApp {
		when = "现在是在聊天软件里，对方点不了确认卡片：先问对方要不要放到后台做（说清楚大概要做什么），对方明确答应了再调用，调用后直接开始。"
	}
	hint := "start_task：对方要你做的事比较大、要很多步（例如调研一个话题写成报告、批量处理很多文件、对比很多资料、做一份完整的方案），" +
		"一两次搜索或运行代码做不完时，用它把事情放到后台慢慢做，做完结果会发到这段对话里。简单的事直接做，不要用。" + when
	return []tools.Extra{{
		Def: llm.Tool{
			Name:        startTaskName,
			Description: "把一件比较大的事放到后台慢慢做（可以用更多轮搜索、读网页、运行代码、执行命令），做完把结果发回这段对话。" + when,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": map[string]any{"type": "string", "description": "任务的简短标题，20 字以内"},
					"goal": map[string]any{"type": "string", "description": "给后台的详细说明：要做什么、对方的具体要求、要交出什么（报告、表格、文件格式等），" +
						"以及这段对话里相关的背景（后台看不到聊天记录，要写全）"},
				},
				"required": []string{"title", "goal"},
			},
		},
		Label: "提出后台任务",
		Hint:  hint,
		Run: func(ctx context.Context, args json.RawMessage) tools.Result {
			var in struct{ Title, Goal string }
			_ = json.Unmarshal(args, &in)
			res := tools.Result{Input: strings.TrimSpace(in.Title)}
			t, err := tasks.Propose(ctx, u, conv.ID, in.Title, in.Goal, 0, inApp)
			if err != nil {
				res.Output, res.Summary = "没提成："+err.Error(), "没提成"
				return res
			}
			// Detail 放任务编号，网页据此显示确认卡片。
			b, _ := json.Marshal(map[string]any{"task_id": t.ID})
			res.Detail, res.OK = string(b), true
			if inApp {
				res.Output = fmt.Sprintf("后台任务已经开始了（编号 %d），做完结果会发过来。简短地告诉对方开始做了。", t.ID)
				res.Summary = "已开始"
			} else {
				res.Output = fmt.Sprintf("已经给对方列出确认卡片（任务编号 %d），对方确认后才开始。简短地告诉对方要做什么、请对方确认。", t.ID)
				res.Summary = "已列出确认卡片"
			}
			return res
		},
	}}
}
