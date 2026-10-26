package mcp

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/secret"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/tools"
)

// ErrNotFound 表示没有这个插件。
var ErrNotFound = errors.New("没有这个插件")

// InvalidError 是管理员填写的内容不对。
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

// retryAfter 是连接失败后多久再试（避免每轮对话都去连一个连不上的插件，拖慢回复）。
const retryAfter = 30 * time.Second

// maxOutput 是工具结果交给模型的最大字数。
const maxOutput = 20000

// Input 是添加插件时填写的内容。
type Input struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
}

// ToolView 是插件提供的一个工具（管理后台显示）。
type ToolView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ServerView 是插件在管理后台的样子：配置（密钥只给出名字）与连接状态。
type ServerView struct {
	ID         uint64     `json:"id"`
	Name       string     `json:"name"`
	Transport  string     `json:"transport"`
	Command    string     `json:"command,omitempty"`
	Args       []string   `json:"args,omitempty"`
	URL        string     `json:"url,omitempty"`
	EnvKeys    []string   `json:"env_keys,omitempty"`
	HeaderKeys []string   `json:"header_keys,omitempty"`
	Enabled    bool       `json:"enabled"`
	Connected  bool       `json:"connected"`
	Connecting bool       `json:"connecting"`
	Server     string     `json:"server,omitempty"`
	Tools      []ToolView `json:"tools"`
	Error      string     `json:"error,omitempty"`
}

// Manager 管理 MCP 插件：配置在数据库，连接在需要时建立并保持。
type Manager struct {
	db   *gorm.DB
	box  *secret.Box
	log  *slog.Logger
	http *http.Client

	mu    sync.Mutex
	conns map[uint64]*conn
}

type conn struct {
	mu         sync.Mutex
	client     *Client
	tools      []Tool
	err        string
	lastTry    time.Time
	connecting bool
}

// New 创建 Manager。
func New(db *gorm.DB, box *secret.Box, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{db: db, box: box, log: log.With("component", "mcp"), conns: map[uint64]*conn{},
		http: &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}}
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// Validate 检查填写的内容。
func (in *Input) Validate() error {
	in.Name = strings.ToLower(strings.TrimSpace(in.Name))
	in.Command, in.URL = strings.TrimSpace(in.Command), strings.TrimSpace(in.URL)
	if !nameRe.MatchString(in.Name) {
		return &InvalidError{"名字只能用小写字母、数字和连字符，最多 40 个字符，例如 amap、weather"}
	}
	switch in.Transport {
	case store.MCPStdio:
		if in.Command == "" {
			return &InvalidError{"要填写启动命令，例如 npx"}
		}
		in.URL, in.Headers = "", nil
	case store.MCPHTTP, store.MCPSSE:
		u, err := url.Parse(in.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return &InvalidError{"要填写 http(s) 开头的地址"}
		}
		in.Command, in.Args, in.Env = "", nil, nil
	default:
		return &InvalidError{"接入方式只能是 stdio、http、sse"}
	}
	for k := range in.Env {
		if strings.TrimSpace(k) == "" || strings.ContainsAny(k, "= ") {
			return &InvalidError{"环境变量名不对：" + k}
		}
	}
	for k := range in.Headers {
		if strings.TrimSpace(k) == "" || strings.ContainsAny(k, ": \r\n") {
			return &InvalidError{"请求头名字不对：" + k}
		}
	}
	return nil
}

func (m *Manager) seal(v map[string]string) (string, error) {
	if len(v) == 0 {
		return "", nil
	}
	b, _ := json.Marshal(v)
	return m.box.Seal(string(b))
}

func (m *Manager) open(sealed string) map[string]string {
	if sealed == "" {
		return nil
	}
	plain, err := m.box.Open(sealed)
	if err != nil {
		return nil
	}
	var v map[string]string
	_ = json.Unmarshal([]byte(plain), &v)
	return v
}

// Add 添加插件，并在后台开始连接。
func (m *Manager) Add(ctx context.Context, in Input) (*store.MCPServer, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	var n int64
	m.db.WithContext(ctx).Model(&store.MCPServer{}).Where("name = ?", in.Name).Count(&n)
	if n > 0 {
		return nil, &InvalidError{"已经有叫 " + in.Name + " 的插件了"}
	}
	env, err := m.seal(in.Env)
	if err != nil {
		return nil, err
	}
	headers, err := m.seal(in.Headers)
	if err != nil {
		return nil, err
	}
	args, _ := json.Marshal(in.Args)
	if len(in.Args) == 0 {
		args = nil
	}
	s := &store.MCPServer{Name: in.Name, Transport: in.Transport, Command: in.Command, Args: string(args),
		EnvEnc: env, URL: in.URL, HeadersEnc: headers, Enabled: true}
	if err := m.db.WithContext(ctx).Create(s).Error; err != nil {
		return nil, err
	}
	go m.connect(context.Background(), *s)
	return s, nil
}

// Get 返回插件。
func (m *Manager) Get(ctx context.Context, id uint64) (*store.MCPServer, error) {
	var s store.MCPServer
	if err := m.db.WithContext(ctx).First(&s, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

// ByName 按名字返回插件。
func (m *Manager) ByName(ctx context.Context, name string) (*store.MCPServer, error) {
	var s store.MCPServer
	if err := m.db.WithContext(ctx).Where("name = ?", name).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

func (m *Manager) list(ctx context.Context) ([]store.MCPServer, error) {
	var list []store.MCPServer
	err := m.db.WithContext(ctx).Order("name").Find(&list).Error
	return list, err
}

// SetEnabled 开启或关闭插件（关闭时断开连接）。
func (m *Manager) SetEnabled(ctx context.Context, id uint64, on bool) error {
	s, err := m.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := m.db.WithContext(ctx).Model(&store.MCPServer{}).Where("id = ?", id).Update("enabled", on).Error; err != nil {
		return err
	}
	m.drop(id)
	if on {
		s.Enabled = true
		go m.connect(context.Background(), *s)
	}
	return nil
}

// Reconnect 断开后重新连接（管理员修好了服务器之后用）。
func (m *Manager) Reconnect(ctx context.Context, id uint64) error {
	s, err := m.Get(ctx, id)
	if err != nil {
		return err
	}
	m.drop(id)
	_, _, err = m.connect(ctx, *s)
	return err
}

// Remove 删除插件。
func (m *Manager) Remove(ctx context.Context, id uint64) error {
	if _, err := m.Get(ctx, id); err != nil {
		return err
	}
	m.drop(id)
	return m.db.WithContext(ctx).Delete(&store.MCPServer{}, id).Error
}

func (m *Manager) entry(id uint64) *conn {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.conns[id]
	if c == nil {
		c = &conn{}
		m.conns[id] = c
	}
	return c
}

// drop 断开并忘掉一个插件的连接。
func (m *Manager) drop(id uint64) {
	m.mu.Lock()
	c := m.conns[id]
	delete(m.conns, id)
	m.mu.Unlock()
	if c != nil {
		c.mu.Lock()
		if c.client != nil {
			_ = c.client.Close()
		}
		c.mu.Unlock()
	}
}

// errConnecting 表示另一个请求正在连接这个插件（例如刚启动时 npx 还在下载），这次先不用它。
var errConnecting = errors.New("正在连接，稍后再试")

// connect 确保连上插件，返回连接与工具列表。连接失败后 retryAfter 之内不再重试；
// 正在连接时直接返回 errConnecting，不让聊天等着。
func (m *Manager) connect(ctx context.Context, s store.MCPServer) (*Client, []Tool, error) {
	c := m.entry(s.ID)
	c.mu.Lock()
	switch {
	case c.client != nil:
		defer c.mu.Unlock()
		return c.client, c.tools, nil
	case c.connecting:
		c.mu.Unlock()
		return nil, nil, errConnecting
	case c.err != "" && time.Since(c.lastTry) < retryAfter:
		defer c.mu.Unlock()
		return nil, nil, errors.New(c.err)
	}
	c.connecting, c.lastTry = true, time.Now()
	c.mu.Unlock()

	client, err := m.dial(ctx, s)
	var list []Tool
	if err == nil {
		if list, err = client.ListTools(ctx); err != nil {
			_ = client.Close()
			client = nil
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.connecting = false
	m.mu.Lock()
	current := m.conns[s.ID] == c
	m.mu.Unlock()
	if !current { // 连接期间插件被关掉或删掉了
		if client != nil {
			_ = client.Close()
		}
		return nil, nil, ErrNotFound
	}
	if err != nil {
		c.err = err.Error()
		m.log.Warn("连接插件失败", "name", s.Name, "err", err)
		return nil, nil, err
	}
	c.client, c.tools, c.err = client, list, ""
	m.log.Info("已连接插件", "name", s.Name, "server", client.Server, "tools", len(list))
	return client, list, nil
}

func (m *Manager) dial(ctx context.Context, s store.MCPServer) (*Client, error) {
	switch s.Transport {
	case store.MCPStdio:
		var args []string
		if s.Args != "" {
			_ = json.Unmarshal([]byte(s.Args), &args)
		}
		return dialStdio(ctx, s.Command, args, m.open(s.EnvEnc))
	case store.MCPHTTP:
		return dialHTTP(ctx, m.http, s.URL, m.open(s.HeadersEnc))
	case store.MCPSSE:
		return dialSSE(ctx, m.http, s.URL, m.open(s.HeadersEnc))
	}
	return nil, fmt.Errorf("不支持的接入方式：%s", s.Transport)
}

// broken 在调用出错（连接断了，不是工具自己报的错）时断开，下次重新连接。
func (m *Manager) broken(id uint64, client *Client) {
	c := m.entry(id)
	c.mu.Lock()
	if c.client == client {
		_ = client.Close()
		c.client, c.tools = nil, nil
	}
	c.mu.Unlock()
}

// Start 在后台连接所有开启的插件。
func (m *Manager) Start(ctx context.Context) {
	list, err := m.list(ctx)
	if err != nil {
		return
	}
	for _, s := range list {
		if s.Enabled {
			go m.connect(ctx, s)
		}
	}
}

// Close 断开所有插件（stdio 插件的进程会结束）。
func (m *Manager) Close() {
	m.mu.Lock()
	ids := make([]uint64, 0, len(m.conns))
	for id := range m.conns {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.drop(id)
	}
}

// Views 返回全部插件与连接状态。
func (m *Manager) Views(ctx context.Context) ([]ServerView, error) {
	list, err := m.list(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ServerView, 0, len(list))
	for _, s := range list {
		v := ServerView{ID: s.ID, Name: s.Name, Transport: s.Transport, Command: s.Command, URL: s.URL, Enabled: s.Enabled, Tools: []ToolView{}}
		if s.Args != "" {
			_ = json.Unmarshal([]byte(s.Args), &v.Args)
		}
		v.EnvKeys, v.HeaderKeys = keys(m.open(s.EnvEnc)), keys(m.open(s.HeadersEnc))
		m.mu.Lock()
		c := m.conns[s.ID]
		m.mu.Unlock()
		if c != nil {
			c.mu.Lock()
			if c.client != nil {
				v.Connected, v.Server = true, c.client.Server
				for _, t := range c.tools {
					v.Tools = append(v.Tools, ToolView{Name: t.Name, Description: t.Description})
				}
			}
			v.Error, v.Connecting = c.err, c.connecting
			c.mu.Unlock()
		}
		out = append(out, v)
	}
	return out, nil
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// For 返回这个用户能用的插件：管理员开启的，减去用户自己关掉的。
func (m *Manager) For(ctx context.Context, disabled []string) []store.MCPServer {
	list, err := m.list(ctx)
	if err != nil {
		return nil
	}
	var out []store.MCPServer
	for _, s := range list {
		if s.Enabled && !contains(disabled, s.Name) {
			out = append(out, s)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

var toolNameRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// toolName 是插件工具交给模型时的名字：mcp_<插件>_<工具>，最长 64 个字符。
func toolName(server, tool string) string {
	n := toolNameRe.ReplaceAllString("mcp_"+strings.ReplaceAll(server, "-", "_")+"_"+tool, "_")
	if len(n) > 64 {
		sum := sha1.Sum([]byte(n))
		n = n[:55] + "_" + hex.EncodeToString(sum[:4])
	}
	return n
}

// Tools 返回这些插件的工具（连不上的插件跳过）。
func (m *Manager) Tools(ctx context.Context, servers []store.MCPServer) []tools.Extra {
	var out []tools.Extra
	var hint []string
	for _, s := range servers {
		client, list, err := m.connect(ctx, s)
		if err != nil || len(list) == 0 {
			continue
		}
		hint = append(hint, s.Name)
		for _, t := range list {
			out = append(out, m.extra(s, client, t))
		}
	}
	if len(out) > 0 {
		out[0].Hint = "名字以 mcp_ 开头的工具来自管理员安装的插件（" + strings.Join(hint, "、") + "），按工具说明在需要时使用"
	}
	return out
}

func (m *Manager) extra(s store.MCPServer, client *Client, t Tool) tools.Extra {
	params := t.InputSchema
	if params == nil {
		params = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	desc := strings.TrimSpace(t.Description)
	if desc == "" {
		desc = t.Name
	}
	return tools.Extra{
		Def:   llm.Tool{Name: toolName(s.Name, t.Name), Description: "【插件 " + s.Name + "】" + clip(desc, 1000), Parameters: params},
		Label: s.Name + " · " + t.Name,
		Run: func(ctx context.Context, args json.RawMessage) tools.Result {
			res := tools.Result{Input: clip(string(args), 300)}
			r, err := client.CallTool(ctx, t.Name, args)
			if err != nil {
				var re *rpcError
				if !errors.As(err, &re) {
					m.broken(s.ID, client)
				}
				res.Output, res.Summary = "调用插件失败："+err.Error(), "调用失败"
				return res
			}
			var b strings.Builder
			for _, c := range r.Content {
				switch c.Type {
				case "text":
					b.WriteString(c.Text + "\n")
				case "image":
					if data, err := base64.StdEncoding.DecodeString(c.Data); err == nil && len(data) < 5<<20 {
						res.Images = append(res.Images, tools.Image{Name: t.Name + imageExt(c.MimeType), Data: data})
					}
				case "resource":
					if c.Resource != nil {
						b.WriteString(c.Resource.Text + "\n")
					}
				}
			}
			res.Output = clip(strings.TrimSpace(b.String()), maxOutput)
			if res.Output == "" && len(res.Images) == 0 {
				res.Output = "（没有返回内容）"
			}
			res.Detail = clip(res.Output, 3000)
			res.OK = !r.IsError
			res.Summary = "完成"
			if r.IsError {
				res.Summary = "插件报错"
			}
			return res
		},
	}
}

func imageExt(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	return ".png"
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
