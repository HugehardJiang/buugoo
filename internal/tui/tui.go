// Package tui 是 buugoo chat 的终端聊天界面（bubbletea）。
package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/HugehardJiang/buugoo/internal/client"
)

// Options 是界面参数。
type Options struct {
	Client *client.Client
	Name   string // 人格名字（布谷）
	Server string
	// GlamourStyle 是 Markdown 渲染风格：dark、light 或 notty。
	GlamourStyle string
}

type entry struct {
	role     string // user | assistant | system
	content  string
	at       time.Time
	status   string
	err      string
	memories []client.UsedMemory
	// rendered 缓存渲染结果，宽度变化时失效。
	rendered      string
	renderedWidth int
}

// 流式事件。
type (
	historyMsg  struct{ msgs []client.Message }
	streamMsg   struct{ ev client.StreamEvent }
	streamEnd   struct{ err error }
	systemMsg   struct{ text string }
	errorMsg    struct{ err error }
	streamStart struct{ ch <-chan tea.Msg }
)

// 配色来自设计系统（布谷 BUUGOO tokens.json）：浅色终端用 ink / text-secondary / accent，
// 深色终端用 on-dark 系列。
var (
	colorInk       = lipgloss.AdaptiveColor{Light: "#141413", Dark: "#faf9f5"} // color-ink / color-on-dark
	colorSecondary = lipgloss.AdaptiveColor{Light: "#5e5d59", Dark: "#c9c7bf"} // color-text-secondary
	colorMuted     = lipgloss.AdaptiveColor{Light: "#87867f", Dark: "#87867f"} // color-muted
	colorAccent    = lipgloss.AdaptiveColor{Light: "#5b7a4a", Dark: "#8fa67f"} // color-accent / illo-sage
	colorDanger    = lipgloss.AdaptiveColor{Light: "#b53333", Dark: "#e07a6c"} // color-danger
	colorWarning   = lipgloss.AdaptiveColor{Light: "#a0701c", Dark: "#d8b46a"} // color-warning
	colorDivider   = lipgloss.AdaptiveColor{Light: "#d1cfc5", Dark: "#3d3d3a"} // color-hairline-strong / color-ink-hover

	styleHeader  = lipgloss.NewStyle().Bold(true).Foreground(colorInk)
	styleMuted   = lipgloss.NewStyle().Foreground(colorMuted)
	styleUser    = lipgloss.NewStyle().Bold(true).Foreground(colorSecondary)
	styleBot     = lipgloss.NewStyle().Bold(true).Foreground(colorInk)
	styleRecall  = lipgloss.NewStyle().Foreground(colorAccent)
	styleError   = lipgloss.NewStyle().Foreground(colorDanger)
	styleSystem  = lipgloss.NewStyle().Foreground(colorWarning)
	styleDivider = lipgloss.NewStyle().Foreground(colorDivider)
	styleInput   = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(colorDivider).Padding(0, 1)
)

const helpText = `可用命令：
  /memories [关键词]   查看布谷记得的事
  /forget <编号>       删除一条记忆（不可恢复）
  /recall             开关"显示本轮想起的记忆"
  /prompt <内容>       查看这句话会发给模型的完整提示词（调试）
  /clear              清空屏幕（不影响聊天记录）
  /help               显示帮助
  /quit               退出
快捷键：Enter 发送 · Ctrl+J 换行 · Esc 停止生成 · PgUp/PgDn 翻页 · Ctrl+C 退出`

// Model 是界面状态。
type Model struct {
	opt      Options
	vp       viewport.Model
	ta       textarea.Model
	sp       spinner.Model
	entries  []entry
	width    int
	height   int
	ready    bool
	stream   <-chan tea.Msg
	cancel   context.CancelFunc
	recall   bool
	renderer *glamour.TermRenderer
	rendW    int
}

// New 创建界面。
func New(opt Options) *Model {
	ta := textarea.New()
	ta.Placeholder = fmt.Sprintf("和%s说点什么…（/help 查看命令）", opt.Name)
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.CharLimit = 4000
	ta.SetHeight(2)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j", "alt+enter"))
	ta.Focus()
	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(styleMuted))
	return &Model{opt: opt, ta: ta, sp: sp, recall: true}
}

// Run 启动界面并阻塞直到退出。
func Run(ctx context.Context, opt Options) error {
	p := tea.NewProgram(New(opt), tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx))
	_, err := p.Run()
	if err != nil && ctx.Err() != nil {
		return nil
	}
	return err
}

// Init 实现 tea.Model。
func (m *Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.sp.Tick, m.loadHistory())
}

func (m *Model) loadHistory() tea.Cmd {
	return func() tea.Msg {
		msgs, _, err := m.opt.Client.Messages(context.Background(), 0, 30)
		if err != nil {
			return errorMsg{fmt.Errorf("加载聊天记录失败: %w", err)}
		}
		return historyMsg{msgs}
	}
}

// Update 实现 tea.Model。
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ta.SetWidth(max(10, msg.Width-4))
		vpHeight := max(3, msg.Height-lipgloss.Height(m.header())-lipgloss.Height(m.footer()))
		if !m.ready {
			m.vp = viewport.New(msg.Width, vpHeight)
			m.vp.KeyMap = viewportKeys()
			m.ready = true
		} else {
			m.vp.Width, m.vp.Height = msg.Width, vpHeight
		}
		m.refresh(true)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "esc":
			if m.cancel != nil {
				m.cancel()
			}
			return m, nil
		case "enter":
			if m.stream != nil {
				return m, nil
			}
			text := strings.TrimSpace(m.ta.Value())
			if text == "" {
				return m, nil
			}
			m.ta.Reset()
			if strings.HasPrefix(text, "/") {
				return m, m.command(text)
			}
			return m, m.send(text)
		}

	case historyMsg:
		hist := make([]entry, 0, len(msg.msgs))
		for _, h := range msg.msgs {
			hist = append(hist, entry{role: h.Role, content: h.Content, at: h.CreatedAt, status: h.Status})
		}
		m.entries = append(hist, m.entries...)
		if len(m.entries) == 0 {
			m.entries = append(m.entries, entry{role: "system", content: fmt.Sprintf("这是你和%s的第一次聊天，打个招呼吧。输入 /help 查看命令。", m.opt.Name), at: time.Now()})
		}
		m.refresh(true)

	case streamStart:
		m.stream = msg.ch
		cmds = append(cmds, waitStream(m.stream))

	case streamMsg:
		last := &m.entries[len(m.entries)-1]
		switch msg.ev.Type {
		case "memories":
			last.memories = msg.ev.Memories
		case "delta":
			last.content += msg.ev.Text
		case "done":
			last.status = msg.ev.Message.Status
			last.content = msg.ev.Message.Content
		}
		last.rendered = ""
		m.refresh(m.atBottom())
		cmds = append(cmds, waitStream(m.stream))

	case streamEnd:
		last := &m.entries[len(m.entries)-1]
		if msg.err != nil {
			if errorsIsCanceled(msg.err) {
				last.status = "interrupted"
			} else {
				last.status = "error"
				last.err = msg.err.Error()
			}
		}
		if last.status == "" {
			last.status = "complete"
		}
		last.rendered = ""
		m.stream, m.cancel = nil, nil
		m.refresh(true)

	case systemMsg:
		m.entries = append(m.entries, entry{role: "system", content: msg.text, at: time.Now()})
		m.refresh(true)

	case errorMsg:
		m.entries = append(m.entries, entry{role: "system", err: msg.err.Error(), at: time.Now()})
		m.refresh(true)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		cmds = append(cmds, cmd)
		if m.stream != nil {
			m.refresh(m.atBottom())
		}
	}

	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	cmds = append(cmds, cmd)
	if m.ready {
		m.vp, cmd = m.vp.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func viewportKeys() viewport.KeyMap {
	k := viewport.DefaultKeyMap()
	// 方向键与字母键留给输入框，只保留翻页键。
	k.Up = key.NewBinding(key.WithKeys("ctrl+up"))
	k.Down = key.NewBinding(key.WithKeys("ctrl+down"))
	k.PageUp = key.NewBinding(key.WithKeys("pgup"))
	k.PageDown = key.NewBinding(key.WithKeys("pgdown"))
	k.HalfPageUp = key.NewBinding(key.WithKeys("ctrl+u"))
	k.HalfPageDown = key.NewBinding(key.WithKeys("ctrl+d"))
	k.Left = key.NewBinding(key.WithDisabled())
	k.Right = key.NewBinding(key.WithDisabled())
	return k
}

func errorsIsCanceled(err error) bool {
	return err == context.Canceled || strings.Contains(err.Error(), context.Canceled.Error())
}

func (m *Model) atBottom() bool { return !m.ready || m.vp.AtBottom() }

// send 发送消息并开始接收流式事件。
func (m *Model) send(text string) tea.Cmd {
	now := time.Now()
	m.entries = append(m.entries,
		entry{role: "user", content: text, at: now, status: "complete"},
		entry{role: "assistant", at: now},
	)
	m.refresh(true)
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	ch := make(chan tea.Msg, 64)
	go func() {
		defer close(ch)
		_, err := m.opt.Client.Chat(ctx, text, func(ev client.StreamEvent) { ch <- streamMsg{ev} })
		ch <- streamEnd{err}
	}()
	return func() tea.Msg { return streamStart{ch} }
}

func waitStream(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *Model) command(text string) tea.Cmd {
	fields := strings.Fields(text)
	cmd, args := fields[0], strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	c := m.opt.Client
	switch cmd {
	case "/quit", "/exit", "/q":
		return tea.Quit
	case "/help":
		return func() tea.Msg { return systemMsg{helpText} }
	case "/clear":
		m.entries = nil
		m.refresh(true)
		return nil
	case "/recall":
		m.recall = !m.recall
		state := "关闭"
		if m.recall {
			state = "开启"
		}
		return func() tea.Msg { return systemMsg{"显示想起的记忆：" + state} }
	case "/memories":
		return func() tea.Msg {
			mems, total, err := c.Memories(context.Background(), args, 30)
			if err != nil {
				return errorMsg{err}
			}
			if total == 0 {
				return systemMsg{fmt.Sprintf("%s还没有记住什么。", m.opt.Name)}
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%s记得的事（共 %d 条，显示最近 %d 条；/forget <编号> 删除）：", m.opt.Name, total, len(mems))
			for _, mem := range mems {
				fmt.Fprintf(&b, "\n  #%-5d [%s·重要性%d] %s", mem.ID, sourceLabel(mem.Source), mem.Importance, mem.Content)
			}
			return systemMsg{b.String()}
		}
	case "/forget":
		id, err := strconv.ParseUint(strings.TrimPrefix(args, "#"), 10, 64)
		if err != nil {
			return func() tea.Msg { return systemMsg{"用法：/forget <编号>，编号可通过 /memories 查看"} }
		}
		return func() tea.Msg {
			if err := c.DeleteMemory(context.Background(), id); err != nil {
				return errorMsg{err}
			}
			return systemMsg{fmt.Sprintf("已删除记忆 #%d", id)}
		}
	case "/prompt":
		if args == "" {
			return func() tea.Msg { return systemMsg{"用法：/prompt <内容>"} }
		}
		return func() tea.Msg {
			out, err := c.DebugPrompt(context.Background(), args)
			if err != nil {
				return errorMsg{err}
			}
			return systemMsg{out}
		}
	default:
		return func() tea.Msg { return systemMsg{"未知命令 " + cmd + "，输入 /help 查看可用命令"} }
	}
}

func sourceLabel(s string) string {
	if s == "stated" {
		return "你说过"
	}
	return "推测"
}

// refresh 重新生成对话内容；toBottom 为 true 时滚动到底部。
func (m *Model) refresh(toBottom bool) {
	if !m.ready {
		return
	}
	m.vp.SetContent(m.renderEntries())
	if toBottom {
		m.vp.GotoBottom()
	}
}

func (m *Model) markdown(s string, width int) string {
	plain := lipgloss.NewStyle().PaddingLeft(2).Width(width)
	if m.opt.GlamourStyle == "notty" || width < 20 {
		return plain.Render(s)
	}
	if m.renderer == nil || m.rendW != width {
		r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(m.opt.GlamourStyle), glamour.WithWordWrap(width-4))
		if err != nil {
			return plain.Render(s)
		}
		m.renderer, m.rendW = r, width
	}
	out, err := m.renderer.Render(s)
	if err != nil {
		return plain.Render(s)
	}
	return strings.Trim(out, "\n")
}

func (m *Model) renderEntries() string {
	width := max(20, m.width-2)
	var b strings.Builder
	lastDay := ""
	for i := range m.entries {
		e := &m.entries[i]
		if day := e.at.Local().Format("2006-01-02"); day != lastDay && e.role != "system" {
			lastDay = day
			label := dayLabel(e.at)
			pad := max(0, (width-lipgloss.Width(label)-2)/2)
			b.WriteString(styleDivider.Render(strings.Repeat("─", pad) + " " + label + " " + strings.Repeat("─", pad)))
			b.WriteString("\n\n")
		}
		b.WriteString(m.renderEntry(e, width))
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) renderEntry(e *entry, width int) string {
	streaming := e.role == "assistant" && e.status == ""
	if !streaming && e.rendered != "" && e.renderedWidth == width {
		return e.rendered
	}
	var b strings.Builder
	t := e.at.Local().Format("15:04")
	body := lipgloss.NewStyle().PaddingLeft(2).Width(width)
	switch e.role {
	case "user":
		b.WriteString(styleUser.Render("你") + " " + styleMuted.Render(t) + "\n")
		b.WriteString(body.Render(e.content))
	case "assistant":
		b.WriteString(styleBot.Render(m.opt.Name) + " " + styleMuted.Render(t))
		switch e.status {
		case "interrupted":
			b.WriteString(styleMuted.Render(" · 已中断"))
		case "error":
			b.WriteString(styleError.Render(" · " + firstNonEmpty(e.err, "生成失败")))
		}
		b.WriteString("\n")
		if m.recall && len(e.memories) > 0 {
			var mem strings.Builder
			fmt.Fprintf(&mem, "想起了 %d 件事：", len(e.memories))
			for _, x := range e.memories {
				fmt.Fprintf(&mem, "\n · [%s] %s", sourceLabel(x.Source), x.Content)
			}
			b.WriteString(lipgloss.NewStyle().PaddingLeft(2).Width(width).Inherit(styleRecall).Render(mem.String()) + "\n")
		}
		switch {
		case streaming && e.content == "":
			b.WriteString("  " + m.sp.View())
		case streaming:
			b.WriteString(body.Render(e.content + " " + m.sp.View()))
		case e.content != "":
			// glamour 自带 2 列左边距，与用户消息的缩进对齐。
			b.WriteString(m.markdown(e.content, width))
		}
	default:
		text := e.content
		style := styleSystem
		if e.err != "" {
			text, style = e.err, styleError
		}
		b.WriteString(lipgloss.NewStyle().Width(width).Inherit(style).Render(text))
	}
	out := b.String()
	if !streaming {
		e.rendered, e.renderedWidth = out, width
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

var weekdays = [...]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}

func dayLabel(t time.Time) string {
	t = t.Local()
	now := time.Now()
	y, mo, d := now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, time.Local)
	switch day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local); {
	case day.Equal(today):
		return "今天"
	case day.Equal(today.AddDate(0, 0, -1)):
		return "昨天"
	case t.Year() == now.Year():
		return fmt.Sprintf("%d月%d日 %s", t.Month(), t.Day(), weekdays[t.Weekday()])
	default:
		return fmt.Sprintf("%d年%d月%d日", t.Year(), t.Month(), t.Day())
	}
}

func (m *Model) header() string {
	title := styleHeader.Render("BUUGOO " + m.opt.Name)
	info := styleMuted.Render(" · " + m.opt.Server)
	return lipgloss.NewStyle().Padding(0, 1).Render(title+info) + "\n"
}

func (m *Model) footer() string {
	hint := "Enter 发送 · Ctrl+J 换行 · /help 命令 · Ctrl+C 退出"
	if m.stream != nil {
		hint = "正在回复… Esc 停止"
	}
	return styleInput.Render(m.ta.View()) + "\n" + styleMuted.Render(" "+hint)
}

// View 实现 tea.Model。
func (m *Model) View() string {
	if !m.ready {
		return "正在连接…"
	}
	return m.header() + m.vp.View() + "\n" + m.footer()
}
