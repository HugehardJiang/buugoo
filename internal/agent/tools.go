package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/mcp"
	"github.com/HugehardJiang/buugoo/internal/persona"
	"github.com/HugehardJiang/buugoo/internal/proactive"
	"github.com/HugehardJiang/buugoo/internal/settings"
	"github.com/HugehardJiang/buugoo/internal/skills"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// env 是工具运行时的上下文。
type env struct {
	s *Service
	u *store.User
	// admin 表示这个请求带着有效的管理后台登录：查看时显示全站的情况。
	admin bool
}

// spec 是牛马的一个工具。read 不为空是查看类（直接执行）；否则是修改类：
// propose 校验参数并给出卡片上的说明，用户确认后 run 执行。
type spec struct {
	name   string
	label  string
	desc   string
	params map[string]any
	// admin 表示只有登录了管理后台才提供，提出的修改也要管理后台登录才能确认。
	admin bool
	// avail 为空表示总是可用。
	avail   func(s *Service) bool
	read    func(ctx context.Context, e *env, args json.RawMessage) (string, error)
	propose func(ctx context.Context, e *env, args json.RawMessage) (summary string, params any, err error)
	run     func(ctx context.Context, e *env, params json.RawMessage) (string, error)
	// option 不为空时，确认卡片上多一个开关，用户可以在确认前改（对应参数里同名的布尔字段）。
	option *option
}

// option 是确认卡片上的一个开关。
type option struct {
	key, label string
}

// actions 是修改类工具（按名字查找，确认时用）。
var actions = map[string]*spec{}

func init() {
	for i := range specs {
		if specs[i].read == nil {
			actions[specs[i].name] = &specs[i]
		}
	}
}

// turn 是一次回复中调用工具的过程。
type turn struct {
	s       *Service
	u       *store.User
	admin   bool
	log     toolLog
	actions []*store.AgentAction
	// proposed 防止同一轮里重复提出完全一样的修改。
	proposed map[string]bool
}

func (t *turn) available() []*spec {
	var out []*spec
	for i := range specs {
		sp := &specs[i]
		if sp.admin && !t.admin {
			continue
		}
		if sp.avail != nil && !sp.avail(t.s) {
			continue
		}
		out = append(out, sp)
	}
	return out
}

func (t *turn) defs() []llm.Tool {
	var out []llm.Tool
	for _, sp := range t.available() {
		params := sp.params
		if params == nil {
			params = obj(nil)
		}
		desc := sp.desc
		if sp.read == nil {
			desc = "【修改，需用户确认】" + desc
		}
		out = append(out, llm.Tool{Name: sp.name, Description: desc, Parameters: params})
	}
	return out
}

// run 执行一次工具调用，返回交给模型的结果。
func (t *turn) run(ctx context.Context, call llm.ToolCall) string {
	var sp *spec
	for _, x := range t.available() {
		if x.name == call.Name {
			sp = x
		}
	}
	if sp == nil {
		return "没有这个工具：" + call.Name
	}
	args := json.RawMessage(call.Arguments)
	if len(strings.TrimSpace(call.Arguments)) == 0 {
		args = json.RawMessage("{}")
	}
	e := &env{s: t.s, u: t.u, admin: t.admin}
	step := ToolStep{Name: sp.name, Label: sp.label}
	defer func() { t.log.Tools = append(t.log.Tools, step) }()
	if sp.read != nil {
		out, err := sp.read(ctx, e, args)
		if err != nil {
			step.Summary = "失败"
			return "出错了：" + err.Error()
		}
		step.Summary = "已查看"
		return clip(out, 12000)
	}
	summary, params, err := sp.propose(ctx, e, args)
	if err != nil {
		step.Summary = "没有提出"
		return "不能这样改：" + err.Error()
	}
	b, _ := json.Marshal(params)
	key := sp.name + string(b)
	if t.proposed[key] {
		step.Summary = "重复"
		return "这项修改刚才已经提出过了，不要重复。"
	}
	sealed, err := t.s.seal(string(b))
	if err != nil {
		step.Summary = "失败"
		return "出错了：" + err.Error()
	}
	a := &store.AgentAction{UserID: t.u.ID, Kind: sp.name, Params: sealed, Summary: clip(summary, 900), Admin: sp.admin, Status: store.ActionPending}
	if err := t.s.d.DB.WithContext(ctx).Create(a).Error; err != nil {
		step.Summary = "失败"
		return "出错了：" + err.Error()
	}
	if t.proposed == nil {
		t.proposed = map[string]bool{}
	}
	t.proposed[key] = true
	t.actions = append(t.actions, a)
	t.log.Actions = append(t.log.Actions, a.ID)
	step.Summary = "等待确认"
	return fmt.Sprintf("已生成确认卡片 #%d「%s」，等用户点确认后才会执行。请告诉用户去点确认，不要说已经改好了。", a.ID, a.Summary)
}

// ---------- 参数 ----------

func obj(props map[string]any, required ...string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func boolean(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func integer(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func parse[T any](args json.RawMessage) (T, error) {
	var v T
	if err := json.Unmarshal(args, &v); err != nil {
		return v, errors.New("参数格式不对：" + err.Error())
	}
	return v, nil
}

func onOff(on bool) string {
	if on {
		return "打开"
	}
	return "关闭"
}

func jsonText(v any) string {
	b, _ := json.MarshalIndent(v, "", " ")
	return string(b)
}

// fresh 重新读取用户（确认时用户的设置可能已经变了）。
func (e *env) fresh(ctx context.Context) (*store.User, error) {
	return e.s.d.Users.GetByID(ctx, e.u.ID)
}

func (e *env) cfg() (disabledPresets []string) { return e.s.d.Config.Persona.DisabledPresets }

// ---------- 工具 ----------

var specs = []spec{
	{
		name: "get_my_settings", label: "查看我的设置",
		desc: "查看用户自己的设置：昵称、布谷的性格、主动找他聊天、默认模型、牛马用的模型、关掉的 skill 和插件、今天的用量。",
		read: func(ctx context.Context, e *env, _ json.RawMessage) (string, error) {
			u, err := e.fresh(ctx)
			if err != nil {
				return "", err
			}
			out := map[string]any{
				"username": u.Username, "display_name": u.DisplayName,
				"persona_preset":   presetLabel(u.PersonaPreset, e.cfg()),
				"proactive":        u.Proactive,
				"disabled_skills":  skills.SplitNames(u.DisabledSkills),
				"disabled_plugins": skills.SplitNames(u.DisabledPlugins),
			}
			if ms, err := e.s.d.Models.List(ctx, modelUser(u)); err == nil {
				for _, m := range ms {
					if m.IsDefault {
						out["default_model"] = fmt.Sprintf("%s（%s，编号 %d）", m.Name, m.Model, m.ID)
					}
				}
			}
			if m, err := e.s.Model(ctx, u); err == nil {
				out["agent_model"] = m.Name
			}
			if st, err := e.s.d.Quota.Status(ctx, u); err == nil {
				out["quota_today"] = st
			}
			return jsonText(out), nil
		},
	},
	{
		name: "list_presets", label: "查看性格",
		desc: "列出布谷可选的性格（预设），以及用户现在选的是哪个。",
		read: func(ctx context.Context, e *env, _ json.RawMessage) (string, error) {
			u, err := e.fresh(ctx)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "现在：%s\n- default：管理员设定的默认性格\n", presetLabel(u.PersonaPreset, e.cfg()))
			for _, p := range persona.Available(e.cfg()) {
				fmt.Fprintf(&b, "- %s（%s）：%s\n", p.ID, p.Label, p.Summary)
			}
			return b.String(), nil
		},
	},
	{
		name: "list_models", label: "查看模型",
		desc: "列出用户能用的模型（编号、名字、能否看图、能否用工具、是不是默认）。",
		read: func(ctx context.Context, e *env, _ json.RawMessage) (string, error) {
			ms, err := e.s.d.Models.List(ctx, modelUser(e.u))
			if err != nil {
				return "", err
			}
			if len(ms) == 0 {
				return "还没有模型。", nil
			}
			var b strings.Builder
			for _, m := range ms {
				fmt.Fprintf(&b, "- 编号 %d：%s（%s，%s）看图=%v 工具=%v", m.ID, m.Name, m.Model, m.Scope, m.Vision, m.Tools)
				if m.IsDefault {
					b.WriteString(" 【默认】")
				}
				b.WriteString("\n")
			}
			return b.String(), nil
		},
	},
	{
		name: "list_skills", label: "查看 skill",
		desc:  "列出管理员安装的 skill（布谷聊天时按需使用的技能包），以及用户自己有没有关掉。",
		avail: func(s *Service) bool { return s.d.Skills != nil },
		read: func(ctx context.Context, e *env, _ json.RawMessage) (string, error) {
			u, err := e.fresh(ctx)
			if err != nil {
				return "", err
			}
			list, err := e.s.d.Skills.List(ctx)
			if err != nil {
				return "", err
			}
			off := skills.SplitNames(u.DisabledSkills)
			var b strings.Builder
			for _, sk := range list {
				if !sk.Enabled && !e.adminView() {
					continue
				}
				fmt.Fprintf(&b, "- %s：%s（我这里%s", sk.Name, clip(sk.Description, 200), onOff(!contains(off, sk.Name)))
				if e.adminView() {
					fmt.Fprintf(&b, "；全站%s；联网%s；来源 %s %s", onOff(sk.Enabled), onOff(sk.AllowNetwork), sk.Source, sk.SourceURL)
				}
				b.WriteString("）\n")
			}
			if b.Len() == 0 {
				return "还没有安装 skill。", nil
			}
			return b.String(), nil
		},
	},
	{
		name: "list_plugins", label: "查看插件",
		desc:  "列出管理员添加的 MCP 插件（给布谷提供额外工具），以及用户自己有没有关掉。",
		avail: func(s *Service) bool { return s.d.Plugins != nil },
		read: func(ctx context.Context, e *env, _ json.RawMessage) (string, error) {
			u, err := e.fresh(ctx)
			if err != nil {
				return "", err
			}
			views, err := e.s.d.Plugins.Views(ctx)
			if err != nil {
				return "", err
			}
			off := skills.SplitNames(u.DisabledPlugins)
			var b strings.Builder
			for _, v := range views {
				if !v.Enabled && !e.adminView() {
					continue
				}
				names := make([]string, len(v.Tools))
				for i, t := range v.Tools {
					names[i] = t.Name
				}
				fmt.Fprintf(&b, "- %s（我这里%s）工具：%s", v.Name, onOff(!contains(off, v.Name)), strings.Join(names, "、"))
				if e.adminView() {
					state := "未连接"
					switch {
					case v.Connected:
						state = "已连接"
					case v.Connecting:
						state = "连接中"
					}
					fmt.Fprintf(&b, "；全站%s；%s 方式 %s %s%s", onOff(v.Enabled), state, v.Transport, v.URL, v.Command)
					if v.Error != "" {
						b.WriteString("；错误：" + clip(v.Error, 200))
					}
				}
				b.WriteString("\n")
			}
			if b.Len() == 0 {
				return "还没有插件。", nil
			}
			return b.String(), nil
		},
	},
	{
		name: "list_reminders", label: "查看提醒",
		desc:  "列出用户还在进行的提醒和定时任务（编号、时间、重复方式、是否暂停）。",
		avail: func(s *Service) bool { return s.d.Reminders != nil },
		read: func(ctx context.Context, e *env, _ json.RawMessage) (string, error) {
			list, err := e.s.d.Reminders.ListPending(ctx, e.u.ID)
			if err != nil {
				return "", err
			}
			if len(list) == 0 {
				return "没有进行中的提醒或定时任务。", nil
			}
			var b strings.Builder
			for i := range list {
				b.WriteString(proactive.Describe(&list[i], e.s.d.Config.Location()) + "\n")
			}
			return b.String(), nil
		},
	},

	// ----- 个人设置（修改） -----
	{
		name: "set_display_name", label: "改昵称",
		desc:   "修改用户的昵称（布谷怎么称呼他）。",
		params: obj(map[string]any{"name": str("新昵称，1 到 32 个字")}, "name"),
		propose: func(ctx context.Context, e *env, args json.RawMessage) (string, any, error) {
			p, err := parse[struct {
				Name string `json:"name"`
			}](args)
			p.Name = strings.TrimSpace(p.Name)
			if err == nil && (p.Name == "" || utf8.RuneCountInString(p.Name) > 32) {
				err = errors.New("昵称要 1 到 32 个字")
			}
			return "把昵称改成「" + p.Name + "」", p, err
		},
		run: func(ctx context.Context, e *env, params json.RawMessage) (string, error) {
			p, err := parse[struct {
				Name string `json:"name"`
			}](params)
			if err != nil {
				return "", err
			}
			return "昵称已改为「" + p.Name + "」", e.s.d.Users.UpdateFields(ctx, e.u.ID, map[string]any{"display_name": p.Name})
		},
	},
	{
		name: "set_preset", label: "换性格",
		desc:   "换布谷的性格（预设）。id 用 list_presets 里的，default 表示管理员设定的默认性格。",
		params: obj(map[string]any{"id": str("性格的 id")}, "id"),
		propose: func(ctx context.Context, e *env, args json.RawMessage) (string, any, error) {
			p, err := parse[struct {
				ID string `json:"id"`
			}](args)
			if err != nil {
				return "", nil, err
			}
			if p.ID == "default" {
				p.ID = persona.DefaultPreset
			}
			if p.ID != persona.DefaultPreset && persona.Find(p.ID, e.cfg()) == nil {
				return "", nil, errors.New("没有这种性格，先用 list_presets 看看")
			}
			return "把布谷的性格换成「" + presetLabel(p.ID, e.cfg()) + "」（下一条消息起生效）", p, nil
		},
		run: func(ctx context.Context, e *env, params json.RawMessage) (string, error) {
			p, err := parse[struct {
				ID string `json:"id"`
			}](params)
			if err != nil {
				return "", err
			}
			if p.ID != persona.DefaultPreset && persona.Find(p.ID, e.cfg()) == nil {
				return "", errors.New("这种性格已经不能选了")
			}
			return "性格已换成「" + presetLabel(p.ID, e.cfg()) + "」", e.s.d.Users.SetPersonaPreset(ctx, e.u.ID, p.ID)
		},
	},
	{
		name: "set_proactive", label: "主动聊天开关",
		desc:   "打开或关闭「布谷主动找我聊天」（提醒和定时任务不受影响）。",
		params: obj(map[string]any{"on": boolean("true 打开，false 关闭")}, "on"),
		propose: func(ctx context.Context, e *env, args json.RawMessage) (string, any, error) {
			p, err := parse[struct {
				On bool `json:"on"`
			}](args)
			return onOff(p.On) + "「布谷主动找我聊天」", p, err
		},
		run: func(ctx context.Context, e *env, params json.RawMessage) (string, error) {
			p, err := parse[struct {
				On bool `json:"on"`
			}](params)
			if err != nil {
				return "", err
			}
			return "已" + onOff(p.On) + "主动聊天", e.s.d.Users.SetProactive(ctx, e.u.ID, p.On)
		},
	},
	{
		name: "set_default_model", label: "换默认模型",
		desc:   "设置用户和布谷聊天默认用的模型。id 用 list_models 里的编号。",
		params: obj(map[string]any{"id": integer("模型编号")}, "id"),
		propose: func(ctx context.Context, e *env, args json.RawMessage) (string, any, error) {
			p, err := parse[struct {
				ID uint64 `json:"id"`
			}](args)
			if err != nil {
				return "", nil, err
			}
			name, err := e.modelName(ctx, p.ID)
			return "把默认模型换成「" + name + "」", p, err
		},
		run: func(ctx context.Context, e *env, params json.RawMessage) (string, error) {
			p, err := parse[struct {
				ID uint64 `json:"id"`
			}](params)
			if err != nil {
				return "", err
			}
			if err := e.s.d.Models.SetDefault(ctx, modelUser(e.u), p.ID); err != nil {
				return "", err
			}
			return "默认模型已更换（已经打开的对话如果单独选过模型，不受影响）", nil
		},
	},
	{
		name: "toggle_skill", label: "开关 skill",
		desc:   "在用户自己这里打开或关闭一个 skill（只影响他自己）。",
		avail:  func(s *Service) bool { return s.d.Skills != nil },
		params: obj(map[string]any{"name": str("skill 的名字"), "enabled": boolean("true 打开，false 关闭")}, "name", "enabled"),
		propose: func(ctx context.Context, e *env, args json.RawMessage) (string, any, error) {
			p, err := parse[toggle](args)
			if err != nil {
				return "", nil, err
			}
			if _, err := e.s.d.Skills.ByName(ctx, p.Name); err != nil {
				return "", nil, errors.New("没有叫 " + p.Name + " 的 skill")
			}
			return "在我这里" + onOff(p.Enabled) + " skill「" + p.Name + "」", p, nil
		},
		run: func(ctx context.Context, e *env, params json.RawMessage) (string, error) {
			return e.toggle(ctx, params, "disabled_skills", func(u *store.User) string { return u.DisabledSkills })
		},
	},
	{
		name: "toggle_plugin", label: "开关插件",
		desc:   "在用户自己这里打开或关闭一个插件（只影响他自己）。",
		avail:  func(s *Service) bool { return s.d.Plugins != nil },
		params: obj(map[string]any{"name": str("插件的名字"), "enabled": boolean("true 打开，false 关闭")}, "name", "enabled"),
		propose: func(ctx context.Context, e *env, args json.RawMessage) (string, any, error) {
			p, err := parse[toggle](args)
			if err != nil {
				return "", nil, err
			}
			if _, err := e.s.d.Plugins.ByName(ctx, p.Name); err != nil {
				return "", nil, errors.New("没有叫 " + p.Name + " 的插件")
			}
			return "在我这里" + onOff(p.Enabled) + "插件「" + p.Name + "」", p, nil
		},
		run: func(ctx context.Context, e *env, params json.RawMessage) (string, error) {
			return e.toggle(ctx, params, "disabled_plugins", func(u *store.User) string { return u.DisabledPlugins })
		},
	},
	{
		name: "change_reminder", label: "管理提醒",
		desc:   "暂停、恢复或取消用户的一个提醒或定时任务。id 用 list_reminders 里的编号。新建提醒请让用户直接和布谷说。",
		avail:  func(s *Service) bool { return s.d.Reminders != nil },
		params: obj(map[string]any{"id": integer("提醒编号"), "op": map[string]any{"type": "string", "enum": []string{"pause", "resume", "cancel"}}}, "id", "op"),
		propose: func(ctx context.Context, e *env, args json.RawMessage) (string, any, error) {
			p, err := parse[reminderOp](args)
			if err != nil {
				return "", nil, err
			}
			r, err := e.s.d.Reminders.Get(ctx, e.u.ID, p.ID)
			if err != nil {
				return "", nil, errors.New("没有这个提醒")
			}
			verb := map[string]string{"pause": "暂停", "resume": "恢复", "cancel": "取消"}[p.Op]
			if verb == "" {
				return "", nil, errors.New("op 只能是 pause、resume、cancel")
			}
			return verb + "「" + clip(r.Content, 60) + "」（#" + fmt.Sprint(r.ID) + "）", p, nil
		},
		run: func(ctx context.Context, e *env, params json.RawMessage) (string, error) {
			p, err := parse[reminderOp](params)
			if err != nil {
				return "", err
			}
			repo := e.s.d.Reminders
			if p.Op == "cancel" {
				return "已取消", repo.Cancel(ctx, e.u.ID, p.ID)
			}
			r, err := repo.Get(ctx, e.u.ID, p.ID)
			if err != nil {
				return "", err
			}
			fields := map[string]any{"status": store.ReminderPaused}
			if p.Op == "resume" {
				fields["status"] = store.ReminderPending
				if now := time.Now(); !r.DueAt.After(now) {
					next := proactive.NextDue(r, now, e.s.d.Config.Location())
					if next == nil {
						return "", errors.New("这个时间已经过了，先在定时任务页改一下时间")
					}
					fields["due_at"] = next.UTC()
				}
			}
			if err := repo.Update(ctx, e.u.ID, p.ID, fields); err != nil {
				return "", err
			}
			if p.Op == "pause" {
				return "已暂停", nil
			}
			return "已恢复", nil
		},
	},

	// ----- 系统（管理后台登录后） -----
	{
		name: "search_settings", label: "查系统设置", admin: true,
		desc:   "按关键词搜索系统设置项（键名、名称、说明），返回键名、类型、当前值、可选值。query 为空时列出所有分组和设置项的键名。",
		params: obj(map[string]any{"query": str("关键词，例如 搜索、上下文、python、微信")}),
		read: func(ctx context.Context, e *env, args json.RawMessage) (string, error) {
			p, _ := parse[struct {
				Query string `json:"query"`
			}](args)
			fields, err := settings.Fields(e.s.d.Config)
			if err != nil {
				return "", err
			}
			q := strings.ToLower(strings.TrimSpace(p.Query))
			var b strings.Builder
			n := 0
			for _, f := range fields {
				if q == "" {
					fmt.Fprintf(&b, "%s（%s）\n", f.Key, f.Label)
					continue
				}
				hay := strings.ToLower(f.Key + " " + f.Label + " " + f.Help + " " + f.Section + " " + f.Group)
				if !strings.Contains(hay, q) {
					continue
				}
				if n++; n > 25 {
					b.WriteString("……还有更多，换个更准确的关键词\n")
					break
				}
				fmt.Fprintf(&b, "- %s【%s】类型 %s，当前值 %v", f.Key, f.Label, f.Type, f.Value)
				if f.Type == "secret" {
					b.WriteString("（密钥只显示是否已设置）")
				}
				if f.Unit != "" {
					b.WriteString("，单位 " + f.Unit)
				}
				if len(f.Options) > 0 {
					b.WriteString("，可选 " + strings.Join(f.Options, "/"))
				}
				if f.Pending {
					fmt.Fprintf(&b, "，已保存待重启的值 %v", f.Saved)
				}
				if f.Locked {
					b.WriteString("，只能在配置文件里改")
				}
				if f.Env {
					b.WriteString("，被环境变量覆盖")
				}
				if f.Help != "" {
					b.WriteString("。说明：" + clip(f.Help, 200))
				}
				b.WriteString("\n")
			}
			if b.Len() == 0 {
				return "没有找到，换个关键词试试。", nil
			}
			return b.String(), nil
		},
	},
	{
		name: "change_settings", label: "改系统设置", admin: true,
		desc: "修改系统设置（保存后要重启服务才生效）。changes 的键是 search_settings 查到的键名，值按类型填写：" +
			"开关填 true/false，时长填 30s、5m 这种，列表填字符串数组。",
		params: obj(map[string]any{"changes": map[string]any{"type": "object", "description": "键名 → 新值", "additionalProperties": true}}, "changes"),
		propose: func(ctx context.Context, e *env, args json.RawMessage) (string, any, error) {
			p, err := parse[struct {
				Changes map[string]any `json:"changes"`
			}](args)
			if err != nil {
				return "", nil, err
			}
			if len(p.Changes) == 0 {
				return "", nil, errors.New("没有要改的设置")
			}
			if err := settings.Check(e.s.d.Config, p.Changes); err != nil {
				return "", nil, err
			}
			fields, err := settings.Fields(e.s.d.Config)
			if err != nil {
				return "", nil, err
			}
			byKey := map[string]settings.Field{}
			for _, f := range fields {
				byKey[f.Key] = f
			}
			keys := make([]string, 0, len(p.Changes))
			for k := range p.Changes {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			lines := []string{"修改系统设置（重启后生效）："}
			for _, k := range keys {
				f := byKey[k]
				nv := fmt.Sprint(p.Changes[k])
				if f.Type == "secret" {
					nv = "（新的密钥）"
				}
				lines = append(lines, fmt.Sprintf("%s：%v → %s", f.Label, f.Value, nv))
			}
			return strings.Join(lines, "\n"), p, nil
		},
		run: func(ctx context.Context, e *env, params json.RawMessage) (string, error) {
			p, err := parse[struct {
				Changes map[string]any `json:"changes"`
			}](params)
			if err != nil {
				return "", err
			}
			changed, err := settings.Save(e.s.d.Config, p.Changes)
			if err != nil {
				return "", err
			}
			e.s.log.Warn("牛马修改了系统设置，重启后生效", "keys", strings.Join(changed, ","), "user", e.u.ID)
			return "已保存 " + strings.Join(changed, "、") + "，重启服务后生效", nil
		},
	},
	{
		name: "search_skills", label: "在 GitHub 上找 skill", admin: true,
		desc: "按关键词在 GitHub 上搜含有 skill 的公开仓库（按星数排序），用于帮用户上网找 skill。关键词用英文效果更好，例如 weather、pdf、translate。" +
			"Anthropic 官方的 skill 合集在 https://github.com/anthropics/skills。搜到后用 list_repo_skills 看仓库里具体有哪些 skill。",
		avail:  func(s *Service) bool { return s.d.Skills != nil },
		params: obj(map[string]any{"query": str("关键词")}, "query"),
		read: func(ctx context.Context, e *env, args json.RawMessage) (string, error) {
			p, err := parse[struct {
				Query string `json:"query"`
			}](args)
			if err != nil {
				return "", err
			}
			hits, err := e.s.d.Skills.SearchGitHub(ctx, p.Query, 8)
			if err != nil {
				return "", err
			}
			if len(hits) == 0 {
				return "没有搜到，换个关键词（英文）试试。", nil
			}
			var b strings.Builder
			for _, h := range hits {
				fmt.Fprintf(&b, "- %s（%d 星，%s 更新）%s\n  %s\n", h.Name, h.Stars, h.Updated, clip(h.Description, 160), h.URL)
			}
			return b.String(), nil
		},
	},
	{
		name: "list_repo_skills", label: "看仓库里的 skill", admin: true,
		desc:   "下载一个 GitHub 仓库（或其中的文件夹）看看里面有哪些 skill（路径、名字、说明），不安装。",
		avail:  func(s *Service) bool { return s.d.Skills != nil },
		params: obj(map[string]any{"url": str("GitHub 地址")}, "url"),
		read: func(ctx context.Context, e *env, args json.RawMessage) (string, error) {
			p, err := parse[struct {
				URL string `json:"url"`
			}](args)
			if err != nil {
				return "", err
			}
			list, err := e.s.d.Skills.PreviewGitHub(ctx, p.URL)
			if err != nil {
				return "", err
			}
			if len(list) == 0 {
				return "这里面没有 skill（没有带 SKILL.md 的文件夹）。", nil
			}
			var b strings.Builder
			fmt.Fprintf(&b, "共 %d 个（安装时把 path 填给 install_skill）：\n", len(list))
			for _, c := range list {
				fmt.Fprintf(&b, "- path=%q 名字 %s：%s\n", c.Path, c.Name, clip(c.Description, 200))
				if c.Network != "" {
					b.WriteString("  检测：看起来要联网（" + c.Network + "）\n")
				}
			}
			return b.String(), nil
		},
	},
	{
		name: "install_skill", label: "安装 skill", admin: true,
		desc: "给全站安装 skill。二选一：url 填 GitHub 地址（仓库、tree/分支/路径 或 SKILL.md 的地址），仓库里有好几个 skill 时用 path 指定是哪个；" +
			"或者 content 直接填完整的 SKILL.md 内容（开头要有 name 和 description）。同名的会被覆盖更新。" +
			"network 填你判断这个 skill 的脚本要不要联网（查天气、调接口、下载数据等要联网），network_reason 用一句话说依据；" +
			"确认卡片上会让用户自己决定开不开联网。",
		avail: func(s *Service) bool { return s.d.Skills != nil },
		params: obj(map[string]any{"url": str("GitHub 地址"), "path": str("仓库里 skill 文件夹的路径（可选）"),
			"content": str("SKILL.md 的完整内容"), "network": boolean("你判断脚本要不要联网"), "network_reason": str("判断的依据（一句话）")}),
		option: &option{key: "network", label: "允许这个 skill 的脚本联网"},
		propose: func(ctx context.Context, e *env, args json.RawMessage) (string, any, error) {
			p, err := parse[installSkill](args)
			if err != nil {
				return "", nil, err
			}
			p.URL, p.Path = strings.TrimSpace(p.URL), strings.Trim(strings.TrimSpace(p.Path), "/")
			p.NetworkReason = clip(strings.TrimSpace(p.NetworkReason), 120)
			var s string
			detected := ""
			switch {
			case p.URL != "" && p.Content != "":
				return "", nil, errors.New("url 和 content 只填一个")
			case p.URL != "":
				if _, err := skills.ParseGitHub(p.URL); err != nil {
					return "", nil, err
				}
				s = "从 GitHub 安装 skill：" + p.URL
				if p.Path != "" {
					s += "（" + p.Path + "）"
				}
				// 下载下来看看脚本（不安装），推测要不要联网；下载失败不影响提出安装。
				if list, err := e.s.d.Skills.PreviewGitHub(ctx, p.URL); err == nil {
					for _, c := range list {
						if len(list) == 1 || c.Path == p.Path {
							detected = c.Network
						}
					}
				}
			case strings.TrimSpace(p.Content) != "":
				m, _, err := skills.Parse([]byte(p.Content), "")
				if err != nil {
					return "", nil, err
				}
				s = "安装 skill「" + m.Name + "」：" + clip(m.Description, 120)
				detected = skills.DetectText(p.Content)
			default:
				return "", nil, errors.New("要填 url 或 content")
			}
			s += "。安装后所有用户都能用。"
			p.Network = p.Network || detected != ""
			switch {
			case p.Network:
				reason := p.NetworkReason
				if detected != "" {
					reason = strings.Trim(reason+"；检测到"+detected, "；")
				}
				s += "\n看起来需要联网：" + reason + "。"
			default:
				s += "\n看起来不需要联网。"
			}
			return s, p, nil
		},
		run: func(ctx context.Context, e *env, params json.RawMessage) (string, error) {
			p, err := parse[installSkill](params)
			if err != nil {
				return "", err
			}
			var sk *store.Skill
			if p.Content != "" {
				if sk, err = e.s.d.Skills.InstallText(ctx, p.Content); err != nil {
					return "", err
				}
			} else {
				res, err := e.s.d.Skills.InstallGitHub(ctx, p.URL, p.Path)
				if err != nil {
					return "", err
				}
				if res.Skill == nil {
					names := make([]string, len(res.Candidates))
					for i, c := range res.Candidates {
						names[i] = fmt.Sprintf("%s（path=%s）", c.Name, c.Path)
					}
					return "", errors.New("仓库里有好几个 skill，没有安装，请选一个再提：" + strings.Join(names, "；"))
				}
				sk = res.Skill
			}
			net := p.Network
			if _, err := e.s.d.Skills.Update(ctx, sk.ID, nil, &net); err != nil {
				return "", err
			}
			out := fmt.Sprintf("已安装 skill「%s」（%d 个文件），脚本%s联网", sk.Name, sk.Files, map[bool]string{true: "可以", false: "不能"}[net])
			if !net {
				if d := e.s.d.Skills.DetectInstalled(sk.Name); d != "" {
					out += "（" + d + "，以后需要可以再打开）"
				}
			}
			return out, nil
		},
	},
	{
		name: "inspect_skill", label: "检查 skill", admin: true,
		desc:   "查看一个已安装的 skill：开关、能否联网、来源、文件列表、SKILL.md 全文，以及服务器推测它要不要联网。排查 skill 的问题时先看这个。",
		avail:  func(s *Service) bool { return s.d.Skills != nil },
		params: obj(map[string]any{"name": str("skill 名字")}, "name"),
		read: func(ctx context.Context, e *env, args json.RawMessage) (string, error) {
			p, err := parse[struct {
				Name string `json:"name"`
			}](args)
			if err != nil {
				return "", err
			}
			sk, err := e.s.d.Skills.ByName(ctx, p.Name)
			if err != nil {
				return "", errors.New("没有叫 " + p.Name + " 的 skill")
			}
			var b strings.Builder
			fmt.Fprintf(&b, "skill「%s」：%s\n全站%s，脚本%s联网，来源 %s %s\n", sk.Name, sk.Description, onOff(sk.Enabled),
				map[bool]string{true: "可以", false: "不能"}[sk.AllowNetwork], sk.Source, sk.SourceURL)
			if d := e.s.d.Skills.DetectInstalled(sk.Name); d != "" {
				b.WriteString("服务器推测要联网：" + d + "\n")
			}
			if e.s.d.Runner == nil || e.s.d.Runner(ctx, e.u) == nil {
				b.WriteString("注意：服务器现在不能运行代码（没有 Docker 沙箱），skill 的脚本都跑不了。\n")
			}
			if files := e.s.d.Skills.Files(sk.Name, 200); len(files) > 0 {
				b.WriteString("\n文件：\n" + strings.Join(files, "\n") + "\n")
			}
			md, _ := e.s.d.Skills.ReadFile(sk.Name, "SKILL.md")
			b.WriteString("\n--- SKILL.md ---\n" + clip(string(md), 8000))
			return b.String(), nil
		},
	},
	{
		name: "read_skill_source", label: "看 skill 的文件", admin: true,
		desc:   "读 skill 里的一个文件（脚本、参考资料）的全文，排查问题用。",
		avail:  func(s *Service) bool { return s.d.Skills != nil },
		params: obj(map[string]any{"name": str("skill 名字"), "path": str("文件路径，例如 scripts/main.py")}, "name", "path"),
		read: func(ctx context.Context, e *env, args json.RawMessage) (string, error) {
			p, err := parse[editSkillFile](args)
			if err != nil {
				return "", err
			}
			b, err := e.s.d.Skills.ReadFile(p.Name, p.Path)
			if err != nil {
				return "", err
			}
			if !utf8.Valid(b) {
				return fmt.Sprintf("%s 是二进制文件（%d 字节），不能显示。", p.Path, len(b)), nil
			}
			return withLineNumbers(clip(string(b), 30000)), nil
		},
	},
	{
		name: "test_skill", label: "试运行 skill", admin: true,
		desc: "在沙箱里试运行 skill 文件夹里的一条命令（例如 python scripts/main.py --help），看输出和报错，用来找问题、验证修好了没有。" +
			"按这个 skill 现在的联网设置运行，最多运行几十秒。",
		avail:  func(s *Service) bool { return s.d.Skills != nil && s.d.Runner != nil },
		params: obj(map[string]any{"name": str("skill 名字"), "command": str("要运行的命令")}, "name", "command"),
		read: func(ctx context.Context, e *env, args json.RawMessage) (string, error) {
			p, err := parse[struct {
				Name    string `json:"name"`
				Command string `json:"command"`
			}](args)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(p.Command) == "" || len(p.Command) > 4000 {
				return "", errors.New("命令为空或太长")
			}
			sk, err := e.s.d.Skills.ByName(ctx, p.Name)
			if err != nil {
				return "", errors.New("没有叫 " + p.Name + " 的 skill")
			}
			r := e.s.d.Runner(ctx, e.u)
			if r == nil {
				return "服务器现在不能运行代码（没有 Docker 沙箱），没法试运行。", nil
			}
			res := skills.RunScript(ctx, e.s.d.Skills, r, nil, sk, p.Command)
			out := res.Output
			if out == "" {
				out = res.Detail
			}
			return "结果：" + res.Summary + "\n" + clip(out, 8000), nil
		},
	},
	{
		name: "edit_skill_file", label: "修改 skill 的文件", admin: true,
		desc: "修复 skill：改写（或新建）skill 里的一个文件，content 填改好后的完整内容；delete 为 true 时删除这个文件。" +
			"reason 用一句话说明修什么。改 SKILL.md 时不能改 name。改之前先用 read_skill_source 看原文，改完用 test_skill 验证。",
		avail: func(s *Service) bool { return s.d.Skills != nil },
		params: obj(map[string]any{"name": str("skill 名字"), "path": str("文件路径"), "content": str("改好后的完整内容"),
			"delete": boolean("删除这个文件"), "reason": str("修什么（一句话）")}, "name", "path", "reason"),
		propose: func(ctx context.Context, e *env, args json.RawMessage) (string, any, error) {
			p, err := parse[editSkillFile](args)
			if err != nil {
				return "", nil, err
			}
			if _, err := e.s.d.Skills.ByName(ctx, p.Name); err != nil {
				return "", nil, errors.New("没有叫 " + p.Name + " 的 skill")
			}
			p.Path = skills.CleanRel(p.Path)
			if p.Path == "" {
				return "", nil, errors.New("文件路径不对")
			}
			if strings.TrimSpace(p.Reason) == "" {
				return "", nil, errors.New("reason 要说明修什么")
			}
			old, readErr := e.s.d.Skills.ReadFile(p.Name, p.Path)
			head := fmt.Sprintf("修改 skill「%s」的 %s：%s", p.Name, p.Path, clip(p.Reason, 150))
			switch {
			case p.Delete:
				if readErr != nil {
					return "", nil, errors.New("这个 skill 里没有 " + p.Path)
				}
				return fmt.Sprintf("删除 skill「%s」的 %s：%s", p.Name, p.Path, clip(p.Reason, 150)), p, nil
			case p.Content == "":
				return "", nil, errors.New("content 为空；要删除文件请用 delete")
			case readErr != nil:
				return head + fmt.Sprintf("\n（新文件，%d 行）", strings.Count(p.Content, "\n")+1), p, nil
			case string(old) == p.Content:
				return "", nil, errors.New("内容没有变化")
			}
			return head + "\n" + lineDiff(string(old), p.Content, 600), p, nil
		},
		run: func(ctx context.Context, e *env, params json.RawMessage) (string, error) {
			p, err := parse[editSkillFile](params)
			if err != nil {
				return "", err
			}
			var content []byte
			if !p.Delete {
				content = []byte(p.Content)
			}
			if _, err := e.s.d.Skills.WriteFile(ctx, p.Name, p.Path, content); err != nil {
				return "", err
			}
			if p.Delete {
				return "已删除 " + p.Path, nil
			}
			return "已保存 " + p.Path, nil
		},
	},
	{
		name: "update_skill", label: "设置 skill", admin: true,
		desc:   "给全站打开或关闭一个 skill，或者设置它的脚本能不能联网。不改的项不填。",
		avail:  func(s *Service) bool { return s.d.Skills != nil },
		params: obj(map[string]any{"name": str("skill 名字"), "enabled": boolean("全站开关"), "network": boolean("脚本能否联网")}, "name"),
		propose: func(ctx context.Context, e *env, args json.RawMessage) (string, any, error) {
			p, err := parse[updateSkill](args)
			if err != nil {
				return "", nil, err
			}
			if _, err := e.s.d.Skills.ByName(ctx, p.Name); err != nil {
				return "", nil, errors.New("没有叫 " + p.Name + " 的 skill")
			}
			var parts []string
			if p.Enabled != nil {
				parts = append(parts, "全站"+onOff(*p.Enabled))
			}
			if p.Network != nil {
				parts = append(parts, onOff(*p.Network)+"脚本联网")
			}
			if len(parts) == 0 {
				return "", nil, errors.New("没有要改的")
			}
			return "skill「" + p.Name + "」：" + strings.Join(parts, "，"), p, nil
		},
		run: func(ctx context.Context, e *env, params json.RawMessage) (string, error) {
			p, err := parse[updateSkill](params)
			if err != nil {
				return "", err
			}
			sk, err := e.s.d.Skills.ByName(ctx, p.Name)
			if err != nil {
				return "", err
			}
			_, err = e.s.d.Skills.Update(ctx, sk.ID, p.Enabled, p.Network)
			return "已更新", err
		},
	},
	{
		name: "remove_skill", label: "卸载 skill", admin: true,
		desc:   "从全站卸载一个 skill（删除文件）。",
		avail:  func(s *Service) bool { return s.d.Skills != nil },
		params: obj(map[string]any{"name": str("skill 名字")}, "name"),
		propose: func(ctx context.Context, e *env, args json.RawMessage) (string, any, error) {
			p, err := parse[named](args)
			if err != nil {
				return "", nil, err
			}
			if _, err := e.s.d.Skills.ByName(ctx, p.Name); err != nil {
				return "", nil, errors.New("没有叫 " + p.Name + " 的 skill")
			}
			return "卸载 skill「" + p.Name + "」（所有用户都不能再用）", p, nil
		},
		run: func(ctx context.Context, e *env, params json.RawMessage) (string, error) {
			p, err := parse[named](params)
			if err != nil {
				return "", err
			}
			sk, err := e.s.d.Skills.ByName(ctx, p.Name)
			if err != nil {
				return "", err
			}
			return "已卸载", e.s.d.Skills.Remove(ctx, sk.ID)
		},
	},
	{
		name: "add_plugin", label: "添加插件", admin: true,
		desc: "给全站添加一个 MCP 插件。transport 为 http（Streamable HTTP，填 url）、sse（旧的 SSE 方式，填 url）或 stdio（在服务器上运行命令，填 command 和 args，例如 npx -y @xxx/mcp-server）。" +
			"需要的 API Key 放在 headers（http/sse）或 env（stdio）里，会加密保存。",
		avail: func(s *Service) bool { return s.d.Plugins != nil },
		params: obj(map[string]any{
			"name":      str("插件名，小写字母、数字、连字符"),
			"transport": map[string]any{"type": "string", "enum": []string{"http", "sse", "stdio"}},
			"url":       str("http/sse 的地址"),
			"headers":   map[string]any{"type": "object", "description": "请求头，例如 {\"Authorization\": \"Bearer xxx\"}", "additionalProperties": map[string]any{"type": "string"}},
			"command":   str("stdio 的命令"),
			"args":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"env":       map[string]any{"type": "object", "description": "stdio 的环境变量", "additionalProperties": map[string]any{"type": "string"}},
		}, "name", "transport"),
		propose: func(ctx context.Context, e *env, args json.RawMessage) (string, any, error) {
			in, err := parse[mcp.Input](args)
			if err != nil {
				return "", nil, err
			}
			if err := in.Validate(); err != nil {
				return "", nil, err
			}
			if _, err := e.s.d.Plugins.ByName(ctx, in.Name); err == nil {
				return "", nil, errors.New("已经有叫 " + in.Name + " 的插件了")
			}
			where := in.URL
			if in.Transport == "stdio" {
				where = "运行命令 " + strings.TrimSpace(in.Command+" "+strings.Join(in.Args, " "))
			}
			keys := append(sortedKeys(in.Headers), sortedKeys(in.Env)...)
			s := fmt.Sprintf("添加插件「%s」（%s）：%s", in.Name, in.Transport, where)
			if len(keys) > 0 {
				s += "，带上 " + strings.Join(keys, "、")
			}
			return s, in, nil
		},
		run: func(ctx context.Context, e *env, params json.RawMessage) (string, error) {
			in, err := parse[mcp.Input](params)
			if err != nil {
				return "", err
			}
			if _, err := e.s.d.Plugins.Add(ctx, in); err != nil {
				return "", err
			}
			return "已添加，正在后台连接，可以过一会儿用 list_plugins 看连接状态", nil
		},
	},
	{
		name: "update_plugin", label: "开关插件（全站）", admin: true,
		desc:   "给全站打开或关闭一个插件，或者删除它。",
		avail:  func(s *Service) bool { return s.d.Plugins != nil },
		params: obj(map[string]any{"name": str("插件名"), "op": map[string]any{"type": "string", "enum": []string{"enable", "disable", "remove", "reconnect"}}}, "name", "op"),
		propose: func(ctx context.Context, e *env, args json.RawMessage) (string, any, error) {
			p, err := parse[pluginOp](args)
			if err != nil {
				return "", nil, err
			}
			if _, err := e.s.d.Plugins.ByName(ctx, p.Name); err != nil {
				return "", nil, errors.New("没有叫 " + p.Name + " 的插件")
			}
			verb := map[string]string{"enable": "全站打开", "disable": "全站关闭", "remove": "删除", "reconnect": "重新连接"}[p.Op]
			if verb == "" {
				return "", nil, errors.New("op 只能是 enable、disable、remove、reconnect")
			}
			return verb + "插件「" + p.Name + "」", p, nil
		},
		run: func(ctx context.Context, e *env, params json.RawMessage) (string, error) {
			p, err := parse[pluginOp](params)
			if err != nil {
				return "", err
			}
			pl := e.s.d.Plugins
			s, err := pl.ByName(ctx, p.Name)
			if err != nil {
				return "", err
			}
			switch p.Op {
			case "enable", "disable":
				return "已" + onOff(p.Op == "enable"), pl.SetEnabled(ctx, s.ID, p.Op == "enable")
			case "remove":
				return "已删除", pl.Remove(ctx, s.ID)
			}
			return "已重新连接", pl.Reconnect(ctx, s.ID)
		},
	},
}

type toggle struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type named struct {
	Name string `json:"name"`
}

type reminderOp struct {
	ID uint64 `json:"id"`
	Op string `json:"op"`
}

type installSkill struct {
	URL           string `json:"url,omitempty"`
	Path          string `json:"path,omitempty"`
	Content       string `json:"content,omitempty"`
	Network       bool   `json:"network"`
	NetworkReason string `json:"network_reason,omitempty"`
}

type editSkillFile struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
	Delete  bool   `json:"delete,omitempty"`
	Reason  string `json:"reason"`
}

type updateSkill struct {
	Name    string `json:"name"`
	Enabled *bool  `json:"enabled,omitempty"`
	Network *bool  `json:"network,omitempty"`
}

type pluginOp struct {
	Name string `json:"name"`
	Op   string `json:"op"`
}

func (e *env) adminView() bool { return e.admin }

func (e *env) toggle(ctx context.Context, params json.RawMessage, column string, get func(*store.User) string) (string, error) {
	p, err := parse[toggle](params)
	if err != nil {
		return "", err
	}
	u, err := e.fresh(ctx)
	if err != nil {
		return "", err
	}
	if err := e.s.d.Users.UpdateFields(ctx, u.ID, map[string]any{column: skills.ToggleName(get(u), p.Name, p.Enabled)}); err != nil {
		return "", err
	}
	return "已" + onOff(p.Enabled) + "「" + p.Name + "」", nil
}

func (e *env) modelName(ctx context.Context, id uint64) (string, error) {
	ms, err := e.s.d.Models.List(ctx, modelUser(e.u))
	if err != nil {
		return "", err
	}
	for _, m := range ms {
		if m.ID == id {
			return m.Name, nil
		}
	}
	return "", errors.New("没有这个模型，先用 list_models 看看编号")
}

func presetLabel(id string, disabled []string) string {
	if p := persona.Find(id, disabled); p != nil {
		return p.Label + "（" + p.ID + "）"
	}
	return "默认（default）"
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// withLineNumbers 给每行加上行号，方便模型说清楚改哪里。
func withLineNumbers(s string) string {
	lines := strings.Split(s, "\n")
	var b strings.Builder
	for i, l := range lines {
		fmt.Fprintf(&b, "%4d  %s\n", i+1, l)
	}
	return b.String()
}

// lineDiff 粗略列出改了哪些行（删掉的以 - 开头，加上的以 + 开头），最多 limit 个字。
func lineDiff(old, cur string, limit int) string {
	a, b := strings.Split(old, "\n"), strings.Split(cur, "\n")
	// 去掉相同的开头和结尾，中间的就是改动。
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	ja, jb := len(a), len(b)
	for ja > i && jb > i && a[ja-1] == b[jb-1] {
		ja, jb = ja-1, jb-1
	}
	var out strings.Builder
	fmt.Fprintf(&out, "（第 %d 行起，删 %d 行，加 %d 行）\n", i+1, ja-i, jb-i)
	for _, l := range a[i:ja] {
		out.WriteString("- " + l + "\n")
	}
	for _, l := range b[i:jb] {
		out.WriteString("+ " + l + "\n")
	}
	return clip(strings.TrimRight(out.String(), "\n"), limit)
}
