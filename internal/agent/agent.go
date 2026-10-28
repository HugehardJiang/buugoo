// Package agent 是设置页里的「牛马」助手：用聊天的方式查看和修改个人设置、
// 开关 skill 与插件；在管理后台登录时还能改系统设置、安装 skill 和插件。
//
// 牛马和布谷完全分开：有自己的聊天记录，不读写记忆、不影响情绪；
// 用户每日用量照常计算。所有修改都先生成确认卡片，用户点了确认才执行。
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/mcp"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/secret"
	"github.com/HugehardJiang/buugoo/internal/skills"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/tools"
)

// 错误。
var (
	ErrBusy         = errors.New("牛马还在忙上一条，等它回完再发")
	ErrEmpty        = errors.New("说点什么吧")
	ErrTooLong      = errors.New("消息太长了（最多 4000 字）")
	ErrNotFound     = errors.New("没有这条修改")
	ErrHandled      = errors.New("这条修改已经处理过了")
	ErrNeedConsole  = errors.New("这是系统设置的修改，要先在这个浏览器登录管理后台")
	ErrNoToolsModel = errors.New("这个模型不支持工具调用，牛马用不了它，换一个支持工具的模型吧")
)

const (
	maxInput    = 4000
	maxRounds   = 14 // 修 skill 要看文件、试运行、改、再试，步骤多一些
	historySize = 40
	callTimeout = 3 * time.Minute
	// RoleEvent 是确认或取消修改后记下的系统消息：给牛马看，界面上不单独显示。
	RoleEvent = "event"
)

// Deps 是牛马用到的组件。Reminders、Skills、Plugins 可以为空（对应功能没开）。
type Deps struct {
	DB        *gorm.DB
	Users     store.UserRepo
	Reminders store.ReminderRepo
	Models    *models.Service
	Quota     *quota.Limiter
	Skills    *skills.Manager
	Plugins   *mcp.Manager
	Config    *config.Config
	// Box 加密保存修改的参数（里面可能有 API Key）；为空时不加密（测试用）。
	Box    *secret.Box
	Logger *slog.Logger
	// Runner 返回这个用户能用的代码沙箱（试运行 skill 用），没有时返回 nil；本身为空表示不能运行代码。
	Runner func(ctx context.Context, u *store.User) tools.Runner
}

// Service 是牛马助手。
type Service struct {
	d   Deps
	log *slog.Logger

	mu   sync.Mutex
	busy map[uint64]bool
}

// New 创建牛马助手。
func New(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &Service{d: d, log: d.Logger.With("component", "agent"), busy: map[uint64]bool{}}
}

// ToolStep 是牛马查看或提出修改的一步（界面上折叠显示）。
type ToolStep struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Summary string `json:"summary"`
}

// ActionView 是一张确认卡片。
type ActionView struct {
	ID      uint64    `json:"id"`
	Kind    string    `json:"kind"`
	Summary string    `json:"summary"`
	Admin   bool      `json:"admin"`
	Status  string    `json:"status"`
	Result  string    `json:"result,omitempty"`
	Created time.Time `json:"created_at"`
	// Option 是卡片上可以在确认前改的开关（例如安装 skill 时是否允许联网）。
	Option *OptionView `json:"option,omitempty"`
}

// OptionView 是确认卡片上的一个开关。
type OptionView struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value bool   `json:"value"`
}

// MessageView 是一条聊天记录。
type MessageView struct {
	ID        uint64       `json:"id"`
	Role      string       `json:"role"`
	Content   string       `json:"content"`
	Tools     []ToolStep   `json:"tools,omitempty"`
	Actions   []ActionView `json:"actions,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
}

// toolLog 是助手消息里记下的工具过程（JSON 存在 ToolLog）。
type toolLog struct {
	Tools   []ToolStep `json:"tools,omitempty"`
	Actions []uint64   `json:"actions,omitempty"`
}

func (s *Service) toAction(a *store.AgentAction) ActionView {
	v := ActionView{ID: a.ID, Kind: a.Kind, Summary: a.Summary, Admin: a.Admin, Status: a.Status, Result: a.Result, Created: a.CreatedAt}
	if sp := actions[a.Kind]; sp != nil && sp.option != nil {
		if plain, err := s.open(a.Params); err == nil {
			var m map[string]any
			if json.Unmarshal([]byte(plain), &m) == nil {
				v.Option = &OptionView{Key: sp.option.key, Label: sp.option.label, Value: m[sp.option.key] == true}
			}
		}
	}
	return v
}

// History 返回最近的聊天记录（不含系统消息），确认卡片放在提出它的那条回复里。
func (s *Service) History(ctx context.Context, u *store.User) ([]MessageView, error) {
	msgs, err := s.recent(ctx, u.ID, historySize*2)
	if err != nil {
		return nil, err
	}
	var ids []uint64
	logs := make([]toolLog, len(msgs))
	for i := range msgs {
		if msgs[i].ToolLog != "" {
			_ = json.Unmarshal([]byte(msgs[i].ToolLog), &logs[i])
			ids = append(ids, logs[i].Actions...)
		}
	}
	actions := map[uint64]*store.AgentAction{}
	if len(ids) > 0 {
		var list []store.AgentAction
		if err := s.d.DB.WithContext(ctx).Where("user_id = ? AND id IN ?", u.ID, ids).Find(&list).Error; err != nil {
			return nil, err
		}
		for i := range list {
			actions[list[i].ID] = &list[i]
		}
	}
	out := []MessageView{}
	for i, m := range msgs {
		if m.Role == RoleEvent {
			continue
		}
		v := MessageView{ID: m.ID, Role: m.Role, Content: m.Content, Tools: logs[i].Tools, CreatedAt: m.CreatedAt}
		for _, id := range logs[i].Actions {
			if a := actions[id]; a != nil {
				v.Actions = append(v.Actions, s.toAction(a))
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// recent 按时间顺序返回最近 n 条记录。
func (s *Service) recent(ctx context.Context, userID uint64, n int) ([]store.AgentMessage, error) {
	var msgs []store.AgentMessage
	if err := s.d.DB.WithContext(ctx).Where("user_id = ?", userID).Order("id DESC").Limit(n).Find(&msgs).Error; err != nil {
		return nil, err
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

// Clear 清空聊天记录和还没处理的修改。
func (s *Service) Clear(ctx context.Context, u *store.User) error {
	return s.d.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", u.ID).Delete(&store.AgentMessage{}).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", u.ID).Delete(&store.AgentAction{}).Error
	})
}

func modelUser(u *store.User) models.User { return models.User{ID: u.ID, IsAdmin: u.IsAdmin} }

// Model 返回牛马用的模型：用户选的，没选（或已经不能用）时用默认模型。
func (s *Service) Model(ctx context.Context, u *store.User) (*models.Resolved, error) {
	return s.d.Models.Resolve(ctx, modelUser(u), u.AgentModelID)
}

// SetModel 设置牛马用的模型，nil 表示跟随默认模型。
func (s *Service) SetModel(ctx context.Context, u *store.User, id *uint64) error {
	if id != nil {
		ok, err := s.d.Models.Visible(ctx, modelUser(u), *id)
		if err != nil {
			return err
		}
		if !ok {
			return store.ErrNotFound
		}
	}
	return s.d.Users.UpdateFields(ctx, u.ID, map[string]any{"agent_model_id": id})
}

func (s *Service) lock(userID uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy[userID] {
		return false
	}
	s.busy[userID] = true
	return true
}

func (s *Service) unlock(userID uint64) {
	s.mu.Lock()
	delete(s.busy, userID)
	s.mu.Unlock()
}

// Send 发一条消息给牛马，返回它的回复（含提出的修改）。admin 表示这个请求带着有效的管理后台登录。
func (s *Service) Send(ctx context.Context, u *store.User, text string, admin bool) (*MessageView, error) {
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return nil, ErrEmpty
	case utf8.RuneCountInString(text) > maxInput:
		return nil, ErrTooLong
	}
	if !s.lock(u.ID) {
		return nil, ErrBusy
	}
	defer s.unlock(u.ID)

	model, err := s.Model(ctx, u)
	if err != nil {
		return nil, err
	}
	if !model.Tools {
		return nil, ErrNoToolsModel
	}
	if !model.Personal {
		if err := s.d.Quota.Check(ctx, u); err != nil {
			return nil, err
		}
	}
	history, err := s.recent(ctx, u.ID, historySize)
	if err != nil {
		return nil, err
	}
	userMsg := &store.AgentMessage{UserID: u.ID, Role: llm.RoleUser, Content: text}
	if err := s.d.DB.WithContext(ctx).Create(userMsg).Error; err != nil {
		return nil, err
	}

	t := &turn{s: s, u: u, admin: admin}
	msgs := append([]llm.Message{{Role: llm.RoleSystem, Content: s.systemPrompt(u, admin)}}, toLLM(history)...)
	now := time.Now().In(s.d.Config.Location()).Format("2006-01-02 15:04 Mon")
	msgs = appendMsg(msgs, llm.Message{Role: llm.RoleUser, Content: "〔现在是 " + now + "〕\n" + text})
	defs := t.defs()

	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var usage llm.Usage
	reply := ""
	for round := 0; ; round++ {
		req := llm.ChatRequest{Messages: msgs, Thinking: "off", Tools: defs}
		if round == maxRounds {
			req.Tools = nil // 最后一轮不再给工具，让它总结
		}
		resp, err := model.Chat.Chat(cctx, req)
		if err != nil {
			s.record(ctx, u, model, usage)
			// 没有回复就当这条没发过（网页会把内容放回输入框），已经提出的修改卡片保留。
			s.d.DB.WithContext(context.WithoutCancel(ctx)).Delete(&store.AgentMessage{}, userMsg.ID)
			return nil, fmt.Errorf("牛马没有回应：%w", err)
		}
		usage.Add(resp.Usage)
		if len(resp.ToolCalls) == 0 || req.Tools == nil {
			reply = strings.TrimSpace(resp.Content)
			break
		}
		msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: resp.Content, Reasoning: resp.Reasoning, ToolCalls: resp.ToolCalls})
		for _, call := range resp.ToolCalls {
			out := t.run(cctx, call)
			msgs = append(msgs, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: out})
		}
	}
	s.record(ctx, u, model, usage)
	if reply == "" {
		reply = "好了。"
		if len(t.log.Actions) > 0 {
			reply = "我把要改的内容列在下面了，确认没问题就点「确认」。"
		}
	}
	m := &store.AgentMessage{UserID: u.ID, Role: llm.RoleAssistant, Content: reply}
	if len(t.log.Tools) > 0 || len(t.log.Actions) > 0 {
		b, _ := json.Marshal(t.log)
		m.ToolLog = string(b)
	}
	if err := s.d.DB.WithContext(ctx).Create(m).Error; err != nil {
		return nil, err
	}
	v := &MessageView{ID: m.ID, Role: m.Role, Content: m.Content, Tools: t.log.Tools, CreatedAt: m.CreatedAt}
	for _, a := range t.actions {
		v.Actions = append(v.Actions, s.toAction(a))
	}
	return v, nil
}

func (s *Service) record(ctx context.Context, u *store.User, model *models.Resolved, usage llm.Usage) {
	if model.Personal || usage.Total() == 0 {
		return
	}
	if err := s.d.Quota.Record(context.WithoutCancel(ctx), u.ID, quota.Tokens(quota.ModelKey(model.ID), usage)); err != nil {
		s.log.Warn("记录用量失败", "err", err)
	}
}

// toLLM 把聊天记录转成模型消息：系统消息作为用户一方的说明，相邻的同一方合并（有的服务商要求一问一答交替）。
func toLLM(history []store.AgentMessage) []llm.Message {
	var out []llm.Message
	for _, m := range history {
		switch m.Role {
		case llm.RoleUser:
			out = appendMsg(out, llm.Message{Role: llm.RoleUser, Content: m.Content})
		case RoleEvent:
			out = appendMsg(out, llm.Message{Role: llm.RoleUser, Content: "【系统】" + m.Content})
		case llm.RoleAssistant:
			c := m.Content
			var l toolLog
			if m.ToolLog != "" && json.Unmarshal([]byte(m.ToolLog), &l) == nil && len(l.Actions) > 0 {
				c += fmt.Sprintf("\n（这次提出了 %d 项修改，编号 %s）", len(l.Actions), joinIDs(l.Actions))
			}
			out = appendMsg(out, llm.Message{Role: llm.RoleAssistant, Content: c})
		}
	}
	// 历史的开头必须是用户一方。
	for len(out) > 0 && out[0].Role != llm.RoleUser {
		out = out[1:]
	}
	return out
}

func appendMsg(list []llm.Message, m llm.Message) []llm.Message {
	if n := len(list); n > 0 && list[n-1].Role == m.Role && m.Role != llm.RoleSystem {
		list[n-1].Content += "\n\n" + m.Content
		return list
	}
	return append(list, m)
}

func joinIDs(ids []uint64) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = fmt.Sprintf("#%d", id)
	}
	return strings.Join(s, "、")
}

func (s *Service) systemPrompt(u *store.User, admin bool) string {
	var b strings.Builder
	b.WriteString(`你是「牛马」，布谷（一个 AI 朋友应用）设置页里的办事助手。你只负责帮用户查看和修改设置，不陪聊，不扮演布谷。
和你的聊天不会进入布谷的记忆，也不会影响布谷的情绪。

做事的规矩：
- 先用查看类工具弄清现状，再动手；不知道的设置不要瞎猜。
- 所有修改都只能通过修改类工具「提出」：系统会给用户显示一张确认卡片，用户点「确认」后才真正执行。提出之后告诉用户去点确认，绝对不要说已经改好了。
- 同一项修改不要重复提出。用户确认或取消后，你会收到一条【系统】消息告诉你结果。
- 回答用中文，简短直接，像个靠谱的同事。用户问的事你做不到就直说，并告诉他去哪里手动改。
- 用户的密码不能由你修改，让对方去「个人中心」自己改。
`)
	if admin {
		b.WriteString(`
现在用户在这个浏览器登录了管理后台，你还可以：查看和修改系统设置（改完要重启服务才生效）、给全站安装或卸载 skill、添加或删除 MCP 插件。
修改系统设置前先用 search_settings 查到准确的键名和当前值。
用户让你上网找 skill 时：先用 search_skills 搜，再用 list_repo_skills 看仓库里有哪些，挑最合适的一个用 install_skill 提出安装（带上 path），
并告诉用户你选的是哪个、为什么。安装前判断它的脚本要不要联网（看说明和 list_repo_skills 的检测结果：查天气、调接口、下载数据都要联网），
把判断填进 install_skill 的 network 和 network_reason；确认卡片上有「允许联网」的开关，由用户自己决定，告诉用户你的建议。
用户说某个 skill 有问题（报错、超时、结果不对）时帮他修：
1. inspect_skill 看说明和文件，read_skill_source 看相关脚本；
2. 用 test_skill 跑一下复现问题（命令照说明里的写，参数尽量简单）；
3. 找出原因再动手：没开联网就用 update_skill 提出打开；脚本的 bug、过时的接口地址、没有超时设置等用 edit_skill_file 改（content 填完整的新文件）；
   不确定新的接口地址时可以先 test_skill 试一下能不能访问；
4. 用户确认修改后，再用 test_skill 验证。服务器网络本身访问不了某个网站（境外服务器访问国内网站、被墙等）这种修不了的，直接说明原因，建议换别的 skill。
`)
	} else {
		b.WriteString(`
现在用户没有登录管理后台，你只能改他自己的设置。他问到系统设置、安装 skill 或插件时，告诉他在这个聊天框上方输入管理员密码登录管理后台（或者去「设置 → 管理后台」登录），登录后再说一次。
`)
	}
	// 现在的时间附在最新一条消息前面（系统提示词每轮不变，服务商的提示词缓存才能命中）。
	fmt.Fprintf(&b, "\n用户：%s。最新一条消息前面〔〕里是现在的时间。\n", displayName(u))
	return b.String()
}

func displayName(u *store.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

// Confirm 执行一项修改。admin 表示这个请求带着有效的管理后台登录。
// options 是用户在卡片上改过的开关（可以为空）。
func (s *Service) Confirm(ctx context.Context, u *store.User, id uint64, admin bool, options map[string]bool) (*ActionView, error) {
	a, err := s.pending(ctx, u, id)
	if err != nil {
		return nil, err
	}
	if a.Admin && !admin {
		return nil, ErrNeedConsole
	}
	def, ok := actions[a.Kind]
	if !ok {
		return nil, ErrNotFound
	}
	// 先占住，防止连点两次执行两遍。
	res := s.d.DB.WithContext(ctx).Model(&store.AgentAction{}).Where("id = ? AND status = ?", a.ID, store.ActionPending).
		Update("status", "running")
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrHandled
	}
	var result string
	params, err := s.open(a.Params)
	if err == nil && def.option != nil {
		if v, ok := options[def.option.key]; ok {
			params, err = setParam(params, def.option.key, v)
		}
	}
	if err == nil {
		result, err = def.run(ctx, &env{s: s, u: u, admin: admin}, json.RawMessage(params))
	}
	a.Status, a.Result = store.ActionDone, result
	if err != nil {
		a.Status, a.Result = store.ActionFailed, err.Error()
	}
	if a.Result == "" {
		a.Result = "已完成"
	}
	a.Result = clip(a.Result, 1900)
	bg := context.WithoutCancel(ctx)
	if err := s.d.DB.WithContext(bg).Model(&store.AgentAction{}).Where("id = ?", a.ID).
		Updates(map[string]any{"status": a.Status, "result": a.Result}).Error; err != nil {
		return nil, err
	}
	verb := "确认并执行了"
	if a.Status == store.ActionFailed {
		verb = "确认了，但执行失败"
	}
	s.event(bg, u.ID, fmt.Sprintf("用户%s修改 #%d「%s」：%s", verb, a.ID, a.Summary, a.Result))
	s.log.Info("牛马执行了修改", "user", u.ID, "kind", a.Kind, "status", a.Status, "admin", a.Admin)
	v := s.toAction(a)
	return &v, nil
}

// Cancel 取消一项修改。
func (s *Service) Cancel(ctx context.Context, u *store.User, id uint64) (*ActionView, error) {
	a, err := s.pending(ctx, u, id)
	if err != nil {
		return nil, err
	}
	res := s.d.DB.WithContext(ctx).Model(&store.AgentAction{}).Where("id = ? AND status = ?", a.ID, store.ActionPending).
		Update("status", store.ActionCanceled)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrHandled
	}
	a.Status = store.ActionCanceled
	s.event(ctx, u.ID, fmt.Sprintf("用户取消了修改 #%d「%s」", a.ID, a.Summary))
	v := s.toAction(a)
	return &v, nil
}

func (s *Service) pending(ctx context.Context, u *store.User, id uint64) (*store.AgentAction, error) {
	var a store.AgentAction
	if err := s.d.DB.WithContext(ctx).Where("id = ? AND user_id = ?", id, u.ID).First(&a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if a.Status != store.ActionPending {
		return nil, ErrHandled
	}
	return &a, nil
}

func (s *Service) event(ctx context.Context, userID uint64, text string) {
	if err := s.d.DB.WithContext(ctx).Create(&store.AgentMessage{UserID: userID, Role: RoleEvent, Content: text}).Error; err != nil {
		s.log.Warn("记录牛马消息失败", "err", err)
	}
}

func (s *Service) seal(plain string) (string, error) {
	if s.d.Box == nil {
		return plain, nil
	}
	return s.d.Box.Seal(plain)
}

func (s *Service) open(sealed string) (string, error) {
	if s.d.Box == nil {
		return sealed, nil
	}
	return s.d.Box.Open(sealed)
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// setParam 改参数（JSON 对象）里的一个字段。
func setParam(params, key string, v any) (string, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(params), &m); err != nil {
		return "", err
	}
	m[key] = v
	b, err := json.Marshal(m)
	return string(b), err
}
