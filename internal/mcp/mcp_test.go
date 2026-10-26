package mcp

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/secret"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

// TestMain 让测试程序自己充当 stdio 插件（FAKE_MCP=stdio 时）。
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_MCP") == "stdio" {
		fakeStdio()
		return
	}
	os.Exit(m.Run())
}

var pixel = base64.StdEncoding.EncodeToString([]byte("\x89PNG fake"))

// respond 是假插件的逻辑：通知返回 nil。
func respond(m rpcMessage) *rpcMessage {
	if len(m.ID) == 0 {
		return nil
	}
	out := &rpcMessage{JSONRPC: "2.0", ID: m.ID}
	var result any
	switch m.Method {
	case "initialize":
		result = map[string]any{"protocolVersion": protocolVersion, "capabilities": map[string]any{},
			"serverInfo": map[string]any{"name": "fake", "version": "1.0"}}
	case "tools/list":
		var p struct {
			Cursor string `json:"cursor"`
		}
		_ = json.Unmarshal(m.Params, &p)
		if p.Cursor == "" {
			result = map[string]any{"nextCursor": "2", "tools": []any{
				map[string]any{"name": "echo", "description": "原样返回", "inputSchema": map[string]any{"type": "object",
					"properties": map[string]any{"text": map[string]any{"type": "string"}}}},
				map[string]any{"name": "fail", "description": "总是报错"},
			}}
		} else {
			result = map[string]any{"tools": []any{map[string]any{"name": "pic", "description": "返回图片"}}}
		}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(m.Params, &p)
		switch p.Name {
		case "echo":
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": fmt.Sprint("echo:", p.Arguments["text"], os.Getenv("FAKE_SECRET"))}}}
		case "fail":
			result = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "坏了"}}}
		case "pic":
			result = map[string]any{"content": []any{map[string]any{"type": "image", "data": pixel, "mimeType": "image/png"}}}
		default:
			out.Error = &rpcError{Code: -32602, Message: "没有这个工具"}
		}
	default:
		out.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	if result != nil {
		out.Result, _ = json.Marshal(result)
	}
	return out
}

func fakeStdio() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var m rpcMessage
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if r := respond(m); r != nil {
			b, _ := json.Marshal(r)
			fmt.Fprintf(os.Stdout, "%s\n", b)
		}
	}
}

// fakeHTTP 是 Streamable HTTP 插件：tools/call 用 SSE 回复，其他用 JSON。
func fakeHTTP(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodDelete {
			return
		}
		var m rpcMessage
		_ = json.NewDecoder(r.Body).Decode(&m)
		if m.Method != "initialize" && r.Header.Get("Mcp-Session-Id") != "s1" {
			http.Error(w, "no session", http.StatusBadRequest)
			return
		}
		res := respond(m)
		if res == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		b, _ := json.Marshal(res)
		if m.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "s1")
		}
		if m.Method == "tools/call" {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
}

// fakeSSE 是旧的 SSE 插件：GET /sse 打开流，POST /msg 发消息，响应从流里回来。
func fakeSSE(t *testing.T) *httptest.Server {
	var mu sync.Mutex
	streams := map[string]chan []byte{}
	n := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		id := fmt.Sprint(n)
		ch := make(chan []byte, 16)
		streams[id] = ch
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: endpoint\ndata: /msg?sid=%s\n\n", id)
		w.(http.Flusher).Flush()
		for {
			select {
			case b := <-ch:
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("/msg", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ch := streams[r.URL.Query().Get("sid")]
		mu.Unlock()
		if ch == nil {
			http.Error(w, "no stream", http.StatusNotFound)
			return
		}
		var m rpcMessage
		_ = json.NewDecoder(r.Body).Decode(&m)
		if res := respond(m); res != nil {
			b, _ := json.Marshal(res)
			ch <- b
		}
		w.WriteHeader(http.StatusAccepted)
	})
	return httptest.NewServer(mux)
}

func checkClient(t *testing.T, c *Client) {
	t.Helper()
	ctx := context.Background()
	if c.Server != "fake 1.0" && !strings.Contains(c.Server, "fake") {
		t.Errorf("server = %q", c.Server)
	}
	list, err := c.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[2].Name != "pic" {
		t.Fatalf("tools = %+v", list)
	}
	r, err := c.CallTool(ctx, "echo", json.RawMessage(`{"text":"hi"}`))
	if err != nil || len(r.Content) != 1 || !strings.HasPrefix(r.Content[0].Text, "echo:hi") {
		t.Fatalf("call = %+v, %v", r, err)
	}
	if _, err := c.CallTool(ctx, "nope", nil); err == nil {
		t.Error("unknown tool should fail")
	}
}

func TestStdio(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := dialStdio(ctx, os.Args[0], nil, map[string]string{"FAKE_MCP": "stdio", "FAKE_SECRET": "!"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	checkClient(t, c)
	r, _ := c.CallTool(ctx, "echo", json.RawMessage(`{"text":"x"}`))
	if r == nil || r.Content[0].Text != "echo:x!" {
		t.Errorf("env not passed: %+v", r)
	}
}

func TestStreamableHTTP(t *testing.T) {
	srv := fakeHTTP(t)
	defer srv.Close()
	ctx := context.Background()
	if _, err := dialHTTP(ctx, srv.Client(), srv.URL, nil); err == nil {
		t.Error("missing auth should fail")
	}
	c, err := dialHTTP(ctx, srv.Client(), srv.URL, map[string]string{"Authorization": "Bearer k"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	checkClient(t, c)
}

func TestSSE(t *testing.T) {
	srv := fakeSSE(t)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := dialSSE(ctx, srv.Client(), srv.URL+"/sse", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	checkClient(t, c)
}

func TestToolName(t *testing.T) {
	if n := toolName("amap-maps", "maps.weather"); n != "mcp_amap_maps_maps_weather" {
		t.Errorf("name = %q", n)
	}
	long := toolName("x", strings.Repeat("a", 100))
	if len(long) != 64 || long == toolName("x", strings.Repeat("a", 99)) {
		t.Errorf("long name = %q", long)
	}
}

func TestManager(t *testing.T) {
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "m.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	box, _ := secret.New(make([]byte, 32))
	m := New(db.Gorm(), box, nil)
	defer m.Close()
	srv := fakeHTTP(t)
	defer srv.Close()
	m.http = srv.Client()
	ctx := context.Background()

	bad := []Input{
		{Name: "Bad Name", Transport: store.MCPHTTP, URL: srv.URL},
		{Name: "x", Transport: store.MCPStdio},
		{Name: "x", Transport: store.MCPHTTP, URL: "ftp://x"},
		{Name: "x", Transport: "ws", URL: srv.URL},
	}
	for _, in := range bad {
		if _, err := m.Add(ctx, in); err == nil {
			t.Errorf("%+v should be rejected", in)
		}
	}
	s, err := m.Add(ctx, Input{Name: "Fake", Transport: store.MCPHTTP, URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer k"}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "fake" || strings.Contains(s.HeadersEnc, "Bearer") {
		t.Fatalf("stored = %+v", s)
	}
	if _, err := m.Add(ctx, Input{Name: "fake", Transport: store.MCPHTTP, URL: srv.URL}); err == nil {
		t.Error("duplicate name should fail")
	}
	stdio, err := m.Add(ctx, Input{Name: "local", Transport: store.MCPStdio, Command: os.Args[0], Env: map[string]string{"FAKE_MCP": "stdio"}})
	if err != nil {
		t.Fatal(err)
	}

	waitConnected(t, m, 2)

	servers := m.For(ctx, []string{"local"})
	if len(servers) != 1 || servers[0].Name != "fake" {
		t.Fatalf("for = %+v", servers)
	}
	extras := m.Tools(ctx, m.For(ctx, nil))
	if len(extras) != 6 || !strings.Contains(extras[0].Hint, "fake") {
		t.Fatalf("extras = %d", len(extras))
	}
	byName := map[string]int{}
	for i, e := range extras {
		byName[e.Def.Name] = i
	}
	run := func(name, args string) (out string, ok bool, images int) {
		i, found := byName[name]
		if !found {
			t.Fatalf("no tool %s", name)
		}
		r := extras[i].Run(ctx, json.RawMessage(args))
		return r.Output, r.OK, len(r.Images)
	}
	if out, ok, _ := run("mcp_fake_echo", `{"text":"天气"}`); !ok || out != "echo:天气" {
		t.Errorf("echo = %q %v", out, ok)
	}
	if out, ok, _ := run("mcp_local_fail", `{}`); ok || out != "坏了" {
		t.Errorf("fail = %q %v", out, ok)
	}
	if _, ok, n := run("mcp_fake_pic", `{}`); !ok || n != 1 {
		t.Errorf("pic = %v %d", ok, n)
	}

	views, err := m.Views(ctx)
	if err != nil || len(views) != 2 {
		t.Fatalf("views = %+v %v", views, err)
	}
	if v := views[0]; v.Name != "fake" || !v.Connected || len(v.Tools) != 3 || len(v.HeaderKeys) != 1 || v.HeaderKeys[0] != "Authorization" {
		t.Errorf("view = %+v", v)
	}

	// 关掉之后不再提供，连接断开。
	if err := m.SetEnabled(ctx, stdio.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := m.For(ctx, nil); len(got) != 1 {
		t.Errorf("after disable = %+v", got)
	}

	// 连不上的插件：记下错误，短时间内不重试，也不影响别的插件。
	dead, err := m.Add(ctx, Input{Name: "dead", Transport: store.MCPHTTP, URL: "http://127.0.0.1:1/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ { // 等后台的第一次连接失败
		if v, _ := m.Views(ctx); len(v) == 2 && v[0].Error != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := m.Tools(ctx, m.For(ctx, nil)); len(got) != 3 {
		t.Errorf("with dead = %d", len(got))
	}
	views, _ = m.Views(ctx)
	for _, v := range views {
		if v.Name == "dead" && (v.Connected || v.Error == "") {
			t.Errorf("dead view = %+v", v)
		}
	}
	if err := m.Remove(ctx, dead.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(ctx, dead.ID); err != ErrNotFound {
		t.Errorf("remove twice = %v", err)
	}
}

func waitConnected(t *testing.T, m *Manager, n int) {
	t.Helper()
	for i := 0; i < 500; i++ {
		views, _ := m.Views(context.Background())
		ok := 0
		for _, v := range views {
			if v.Connected {
				ok++
			}
		}
		if ok >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("plugins did not connect")
}
