// Package mcp 接入 MCP（Model Context Protocol）插件：管理员添加的 MCP 服务器提供的工具，布谷聊天时可以直接用。
//
// 支持三种接入方式：在服务器上启动一个命令（stdio）、Streamable HTTP、旧版 SSE。
// 只用到协议里的工具部分：initialize → tools/list → tools/call。
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// protocolVersion 是请求的协议版本（服务器可以回一个它支持的版本）。
const protocolVersion = "2025-06-18"

// 超时。
const (
	initTimeout = 30 * time.Second
	callTimeout = 2 * time.Minute
)

// Tool 是 MCP 服务器提供的一个工具。
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Content 是工具返回的一段内容。
type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
	// resource 类型的内容
	Resource *struct {
		URI  string `json:"uri"`
		Text string `json:"text"`
	} `json:"resource"`
}

// CallResult 是 tools/call 的结果。
type CallResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("MCP 错误 %d：%s", e.Code, e.Message) }

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// transport 是一种接入方式。
type transport interface {
	// call 发出请求并等待响应。
	call(ctx context.Context, id int64, method string, params any) (json.RawMessage, error)
	notify(ctx context.Context, method string, params any) error
	close() error
}

// Client 是和一个 MCP 服务器的连接。
type Client struct {
	t      transport
	mu     sync.Mutex
	nextID int64
	// Server 是服务器报的名字与版本。
	Server string
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()
	return c.t.call(ctx, id, method, params)
}

// connect 完成初始化握手。
func connect(ctx context.Context, t transport) (*Client, error) {
	c := &Client{t: t}
	ictx, cancel := context.WithTimeout(ctx, initTimeout)
	defer cancel()
	raw, err := c.call(ictx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "buugoo", "version": "1"},
	})
	if err != nil {
		_ = t.close()
		return nil, fmt.Errorf("初始化失败：%w", err)
	}
	var info struct {
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	_ = json.Unmarshal(raw, &info)
	c.Server = strings.TrimSpace(info.ServerInfo.Name + " " + info.ServerInfo.Version)
	if err := t.notify(ictx, "notifications/initialized", nil); err != nil {
		_ = t.close()
		return nil, fmt.Errorf("初始化失败：%w", err)
	}
	return c, nil
}

// ListTools 返回服务器的全部工具（处理分页）。
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var out []Tool
	cursor := ""
	for i := 0; i < 20; i++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		cctx, cancel := context.WithTimeout(ctx, initTimeout)
		raw, err := c.call(cctx, "tools/list", params)
		cancel()
		if err != nil {
			return nil, err
		}
		var page struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("工具列表格式不对：%v", err)
		}
		out = append(out, page.Tools...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return out, nil
}

// CallTool 调用一个工具。
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (*CallResult, error) {
	if len(bytes.TrimSpace(args)) == 0 || string(bytes.TrimSpace(args)) == "null" {
		args = json.RawMessage("{}")
	}
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	raw, err := c.call(cctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, err
	}
	var res CallResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("结果格式不对：%v", err)
	}
	return &res, nil
}

// Close 断开连接（stdio 方式会结束进程）。
func (c *Client) Close() error { return c.t.close() }

// ---------- 等待响应 ----------

// waiter 按请求 ID 把响应交给等待的请求（stdio 与 SSE 方式的响应都从同一个流里来）。
type waiter struct {
	mu      sync.Mutex
	pending map[string]chan rpcMessage
	err     error
}

func newWaiter() *waiter { return &waiter{pending: map[string]chan rpcMessage{}} }

func (w *waiter) add(id int64) (chan rpcMessage, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return nil, w.err
	}
	ch := make(chan rpcMessage, 1)
	w.pending[fmt.Sprint(id)] = ch
	return ch, nil
}

func (w *waiter) remove(id int64) {
	w.mu.Lock()
	delete(w.pending, fmt.Sprint(id))
	w.mu.Unlock()
}

func (w *waiter) deliver(m rpcMessage) {
	key := strings.Trim(string(m.ID), `"`)
	w.mu.Lock()
	ch := w.pending[key]
	delete(w.pending, key)
	w.mu.Unlock()
	if ch != nil {
		ch <- m
	}
}

// fail 让所有等待中的请求失败（连接断了）。
func (w *waiter) fail(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err == nil {
		w.err = err
	}
	for k, ch := range w.pending {
		close(ch)
		delete(w.pending, k)
	}
}

func (w *waiter) wait(ctx context.Context, id int64, ch chan rpcMessage) (json.RawMessage, error) {
	select {
	case <-ctx.Done():
		w.remove(id)
		return nil, fmt.Errorf("等待响应超时：%w", ctx.Err())
	case m, ok := <-ch:
		if !ok {
			w.mu.Lock()
			err := w.err
			w.mu.Unlock()
			return nil, fmt.Errorf("连接断了：%w", err)
		}
		if m.Error != nil {
			return nil, m.Error
		}
		return m.Result, nil
	}
}

func request(id int64, method string, params any) ([]byte, error) {
	m := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		m["params"] = params
	}
	return json.Marshal(m)
}

func notification(method string, params any) ([]byte, error) {
	m := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		m["params"] = params
	}
	return json.Marshal(m)
}

// handleIncoming 处理服务器发来的消息：响应交给等待的请求；服务器的请求（如 ping）回一个应答。
func handleIncoming(w *waiter, m rpcMessage, reply func([]byte)) {
	switch {
	case m.Method == "" && len(m.ID) > 0:
		w.deliver(m)
	case m.Method != "" && len(m.ID) > 0:
		var resp []byte
		if m.Method == "ping" {
			resp, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{}})
		} else {
			resp, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32601, "message": "not supported"}})
		}
		reply(resp)
	}
	// 通知（日志、进度等）忽略。
}

// ---------- stdio ----------

type stdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	w      *waiter
	wmu    sync.Mutex
	stderr *tail
	done   chan struct{}
}

// tail 保存进程最后输出的一段错误信息，连接失败时给管理员看。
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4096 {
		t.buf = t.buf[len(t.buf)-4096:]
	}
	t.mu.Unlock()
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

// dialStdio 启动命令并通过标准输入输出通信（每行一条 JSON 消息）。
func dialStdio(ctx context.Context, command string, args []string, env map[string]string) (*Client, error) {
	cmd := exec.Command(command, args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	setProcessGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	st := &stdioTransport{cmd: cmd, stdin: stdin, w: newWaiter(), stderr: &tail{}, done: make(chan struct{})}
	cmd.Stderr = st.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动命令失败：%w", err)
	}
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64<<10), 16<<20)
		for sc.Scan() {
			var m rpcMessage
			if json.Unmarshal(sc.Bytes(), &m) != nil {
				continue // 有的服务器会往标准输出打日志
			}
			handleIncoming(st.w, m, st.write)
		}
		err := cmd.Wait()
		msg := "进程退出了"
		if err != nil {
			msg = "进程退出了：" + err.Error()
		}
		if e := st.stderr.String(); e != "" {
			msg += "\n" + e
		}
		st.w.fail(errors.New(msg))
		close(st.done)
	}()
	c, err := connect(ctx, st)
	if err != nil {
		if e := st.stderr.String(); e != "" {
			return nil, fmt.Errorf("%w\n%s", err, e)
		}
		return nil, err
	}
	return c, nil
}

func (s *stdioTransport) write(b []byte) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, _ = s.stdin.Write(append(b, '\n'))
}

func (s *stdioTransport) call(ctx context.Context, id int64, method string, params any) (json.RawMessage, error) {
	b, err := request(id, method, params)
	if err != nil {
		return nil, err
	}
	ch, err := s.w.add(id)
	if err != nil {
		return nil, err
	}
	s.write(b)
	return s.w.wait(ctx, id, ch)
}

func (s *stdioTransport) notify(_ context.Context, method string, params any) error {
	b, err := notification(method, params)
	if err != nil {
		return err
	}
	s.write(b)
	return nil
}

func (s *stdioTransport) close() error {
	_ = s.stdin.Close()
	select {
	case <-s.done:
		return nil
	case <-time.After(3 * time.Second):
	}
	killProcessGroup(s.cmd)
	<-s.done
	return nil
}

// ---------- Streamable HTTP ----------

type httpTransport struct {
	url     string
	headers map[string]string
	client  *http.Client
	mu      sync.Mutex
	session string
}

func dialHTTP(ctx context.Context, client *http.Client, rawURL string, headers map[string]string) (*Client, error) {
	return connect(ctx, &httpTransport{url: rawURL, headers: headers, client: client})
}

func (h *httpTransport) post(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", protocolVersion)
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	h.mu.Lock()
	if h.session != "" {
		req.Header.Set("Mcp-Session-Id", h.session)
	}
	h.mu.Unlock()
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	if s := resp.Header.Get("Mcp-Session-Id"); s != "" {
		h.mu.Lock()
		h.session = s
		h.mu.Unlock()
	}
	return resp, nil
}

func (h *httpTransport) call(ctx context.Context, id int64, method string, params any) (json.RawMessage, error) {
	b, err := request(id, method, params)
	if err != nil {
		return nil, err
	}
	resp, err := h.post(ctx, b)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	want := fmt.Sprint(id)
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		// 响应以 SSE 流返回：读到这个请求的响应为止。
		var found *rpcMessage
		err := readSSE(resp.Body, func(event, data string) bool {
			var m rpcMessage
			if json.Unmarshal([]byte(data), &m) != nil {
				return true
			}
			if m.Method == "" && strings.Trim(string(m.ID), `"`) == want {
				found = &m
				return false
			}
			return true
		})
		if found == nil {
			if err == nil {
				err = errors.New("服务器没有返回响应")
			}
			return nil, err
		}
		if found.Error != nil {
			return nil, found.Error
		}
		return found.Result, nil
	}
	var m rpcMessage
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&m); err != nil {
		return nil, fmt.Errorf("响应格式不对：%v", err)
	}
	if m.Error != nil {
		return nil, m.Error
	}
	return m.Result, nil
}

func (h *httpTransport) notify(ctx context.Context, method string, params any) error {
	b, err := notification(method, params)
	if err != nil {
		return err
	}
	resp, err := h.post(ctx, b)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func (h *httpTransport) close() error {
	h.mu.Lock()
	session := h.session
	h.mu.Unlock()
	if session == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, h.url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Mcp-Session-Id", session)
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	if resp, err := h.client.Do(req); err == nil {
		resp.Body.Close()
	}
	return nil
}

// readSSE 逐个读 SSE 事件，fn 返回 false 时停止。
func readSSE(r io.Reader, fn func(event, data string) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	event := ""
	var data []string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if len(data) > 0 {
				if !fn(event, strings.Join(data, "\n")) {
					return nil
				}
			}
			event, data = "", nil
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return io.EOF
}

// ---------- 旧版 SSE ----------

type sseTransport struct {
	client   *http.Client
	headers  map[string]string
	endpoint string
	w        *waiter
	cancel   context.CancelFunc
}

// dialSSE 打开 SSE 流，服务器先发来 endpoint 事件（发消息的地址），之后的响应都从这个流回来。
func dialSSE(ctx context.Context, client *http.Client, rawURL string, headers map[string]string) (*Client, error) {
	base, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	sctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(sctx, http.MethodGet, rawURL, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	// SSE 流是长连接，不能用带总超时的客户端。
	stream := &http.Client{Transport: client.Transport}
	resp, err := stream.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	st := &sseTransport{client: client, headers: headers, w: newWaiter(), cancel: cancel}
	endpoint := make(chan string, 1)
	go func() {
		defer resp.Body.Close()
		err := readSSE(resp.Body, func(event, data string) bool {
			if event == "endpoint" {
				if u, err := base.Parse(strings.TrimSpace(data)); err == nil {
					select {
					case endpoint <- u.String():
					default:
					}
				}
				return true
			}
			var m rpcMessage
			if json.Unmarshal([]byte(data), &m) == nil {
				handleIncoming(st.w, m, func(b []byte) { _ = st.postBody(context.Background(), b) })
			}
			return true
		})
		if err == nil || errors.Is(err, io.EOF) {
			err = errors.New("SSE 连接断开了")
		}
		st.w.fail(err)
	}()
	select {
	case st.endpoint = <-endpoint:
	case <-time.After(initTimeout):
		cancel()
		return nil, errors.New("服务器没有发来 endpoint 事件（可能不是 SSE 方式，试试 Streamable HTTP）")
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}
	return connect(ctx, st)
}

func (s *sseTransport) postBody(ctx context.Context, b []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func (s *sseTransport) call(ctx context.Context, id int64, method string, params any) (json.RawMessage, error) {
	b, err := request(id, method, params)
	if err != nil {
		return nil, err
	}
	ch, err := s.w.add(id)
	if err != nil {
		return nil, err
	}
	if err := s.postBody(ctx, b); err != nil {
		s.w.remove(id)
		return nil, err
	}
	return s.w.wait(ctx, id, ch)
}

func (s *sseTransport) notify(ctx context.Context, method string, params any) error {
	b, err := notification(method, params)
	if err != nil {
		return err
	}
	return s.postBody(ctx, b)
}

func (s *sseTransport) close() error {
	s.cancel()
	return nil
}
