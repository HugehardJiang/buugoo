package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

// fakeLLM 模拟 OpenAI 兼容服务：流式聊天、JSON 模式记忆提取与向量化。
func fakeLLM(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch r.URL.Path {
		case "/v1/embeddings":
			inputs, _ := req["input"].([]any)
			var data []map[string]any
			for i, in := range inputs {
				s, _ := in.(string)
				v := []float32{0.05, 0, 0}
				if strings.Contains(s, "猫") {
					v[1] = 1
				}
				if strings.Contains(s, "狗") {
					v[2] = 1
				}
				data = append(data, map[string]any{"index": i, "embedding": v})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		case "/v1/chat/completions":
			msgs, _ := req["messages"].([]any)
			last, _ := msgs[len(msgs)-1].(map[string]any)
			content, _ := last["content"].(string)
			if req["response_format"] != nil { // 记忆提取
				mem := ""
				switch {
				case strings.Contains(content, "用户：我养了一只猫"):
					mem = `{"type":"semantic","content":"用户养了一只猫","importance":7,"emotion":0.3,"source":"stated"}`
				case strings.Contains(content, "用户：我养了一只狗"):
					mem = `{"type":"semantic","content":"用户养了一只狗","importance":7,"emotion":0.3,"source":"stated"}`
				}
				fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}],"usage":{"prompt_tokens":30,"completion_tokens":10}}`,
					`{"memories":[`+mem+`]}`)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			if req["reasoning_effort"] != nil {
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":%q}}]}\n\n", "想一想："+req["reasoning_effort"].(string))
			}
			for _, piece := range []string{"好的", "，我知道了"} {
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", piece)
				w.(http.Flusher).Flush()
			}
			fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":8}}\n\ndata: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
}

type testApp struct {
	*App
	srv *httptest.Server
}

func newTestApp(t *testing.T, mutate func(*config.Config)) *testApp {
	t.Helper()
	llm := fakeLLM(t)
	t.Cleanup(llm.Close)
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Database.DSN = filepath.Join(cfg.DataDir, "app.db")
	cfg.LLM.Chat.BaseURL = llm.URL + "/v1"
	cfg.LLM.Chat.APIKey = "k"
	cfg.LLM.Utility = cfg.LLM.Chat
	cfg.LLM.Embedding.BaseURL = llm.URL + "/v1"
	cfg.LLM.Embedding.APIKey = "k"
	cfg.LLM.Embedding.Model = "fake-emb"
	cfg.Log.Level = "error"
	if mutate != nil {
		mutate(cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	var db *gormstore.DB
	var err error
	if dsn := os.Getenv("BUUGOO_TEST_MYSQL_DSN"); dsn != "" && cfg.Database.Driver == config.DriverMySQL {
		db, err = gormstore.Open(config.DatabaseConfig{Driver: config.DriverMySQL, DSN: dsn, MaxOpenConns: 5}, nil)
	} else {
		db, err = gormstore.Open(cfg.Database, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	a, err := build(context.Background(), cfg, testLogger(), db)
	if err != nil {
		t.Fatal(err)
	}
	a.Writer.Start(context.Background())
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(func() {
		srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Writer.Close(ctx)
		_ = a.Close()
	})
	return &testApp{App: a, srv: srv}
}

func (ta *testApp) do(t *testing.T, method, path, token string, body any) (*http.Response, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, ta.srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

type sseEvent struct {
	Event string
	Data  map[string]any
}

func parseSSE(t *testing.T, b []byte) []sseEvent {
	t.Helper()
	var out []sseEvent
	sc := bufio.NewScanner(bytes.NewReader(b))
	var ev sseEvent
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			ev.Event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev.Data); err != nil {
				t.Fatalf("SSE 数据不是 JSON: %s", line)
			}
		case line == "" && ev.Event != "":
			out = append(out, ev)
			ev = sseEvent{}
		}
	}
	return out
}

// waitMemories 等待异步记忆写入完成。
func (ta *testApp) waitMemories(t *testing.T, token string, want int) []any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, b := ta.do(t, "GET", "/api/v1/memories", token, nil)
		var r struct {
			Memories []any `json:"memories"`
			Total    int   `json:"total"`
		}
		_ = json.Unmarshal(b, &r)
		if r.Total >= want || time.Now().After(deadline) {
			if r.Total != want {
				t.Fatalf("记忆数量 = %d，期望 %d: %s", r.Total, want, b)
			}
			return r.Memories
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSingleModeEndToEnd(t *testing.T) {
	ta := newTestApp(t, nil)

	resp, b := ta.do(t, "GET", "/healthz", "", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"ok"`) {
		t.Fatalf("healthz: %d %s", resp.StatusCode, b)
	}
	_, b = ta.do(t, "GET", "/api/v1/meta", "", nil)
	if !strings.Contains(string(b), `"name":"布谷"`) || !strings.Contains(string(b), `"password_required":false`) {
		t.Fatalf("meta: %s", b)
	}

	// 无密码 single 模式：不带令牌即可聊天。
	resp, b = ta.do(t, "POST", "/api/v1/chat", "", map[string]any{"content": "我养了一只猫"})
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("chat: %d %s", resp.StatusCode, b)
	}
	events := parseSSE(t, b)
	var names []string
	var text string
	for _, e := range events {
		names = append(names, e.Event)
		if e.Event == "delta" {
			text += e.Data["text"].(string)
		}
	}
	if strings.Join(names, ",") != "start,memories,delta,delta,done" || text != "好的，我知道了" {
		t.Fatalf("events=%v text=%q", names, text)
	}

	mems := ta.waitMemories(t, "", 1)
	memID := uint64(mems[0].(map[string]any)["id"].(float64))

	// 第二轮应使用记忆。
	_, b = ta.do(t, "POST", "/api/v1/chat", "", map[string]any{"content": "我的猫今天不吃饭", "stream": false})
	var nonStream struct {
		Message  struct{ Content, Status string }
		Memories []struct{ Content string }
	}
	if err := json.Unmarshal(b, &nonStream); err != nil {
		t.Fatal(err)
	}
	if nonStream.Message.Status != "complete" || len(nonStream.Memories) != 1 || nonStream.Memories[0].Content != "用户养了一只猫" {
		t.Fatalf("非流式响应: %s", b)
	}

	_, b = ta.do(t, "GET", "/api/v1/messages?limit=10", "", nil)
	var hist struct {
		Messages []struct{ Role, Content string }
	}
	_ = json.Unmarshal(b, &hist)
	if len(hist.Messages) != 4 || hist.Messages[0].Content != "我养了一只猫" {
		t.Fatalf("history: %s", b)
	}

	resp, b = ta.do(t, "GET", "/api/v1/debug/prompt?q=猫&format=text", "", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "用户养了一只猫") || !strings.Contains(string(b), "# 你是布谷") {
		t.Fatalf("debug prompt: %d %s", resp.StatusCode, b)
	}

	resp, _ = ta.do(t, "DELETE", fmt.Sprintf("/api/v1/memories/%d", memID), "", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	resp, _ = ta.do(t, "GET", fmt.Sprintf("/api/v1/memories/%d", memID), "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("删除后应 404: %d", resp.StatusCode)
	}

	_, b = ta.do(t, "GET", "/api/v1/me", "", nil)
	if !strings.Contains(string(b), `"messages":2`) {
		t.Fatalf("me/quota: %s", b)
	}

	resp, b = ta.do(t, "POST", "/api/v1/chat", "", map[string]any{"content": "  "})
	if resp.StatusCode != 400 || !strings.Contains(string(b), "invalid_message") {
		t.Fatalf("空消息: %d %s", resp.StatusCode, b)
	}
	resp, _ = ta.do(t, "GET", "/api/v1/nope", "", nil)
	if resp.StatusCode != 404 {
		t.Fatalf("未知 API 应 404: %d", resp.StatusCode)
	}
}

func TestSinglePasswordMode(t *testing.T) {
	ta := newTestApp(t, func(c *config.Config) { c.Auth.AccessPassword = "letmein" })
	resp, _ := ta.do(t, "GET", "/api/v1/me", "", nil)
	if resp.StatusCode != 401 {
		t.Fatalf("应要求登录: %d", resp.StatusCode)
	}
	resp, _ = ta.do(t, "POST", "/api/v1/auth/login", "", map[string]string{"password": "wrong"})
	if resp.StatusCode != 401 {
		t.Fatalf("错误密码: %d", resp.StatusCode)
	}
	_, b := ta.do(t, "POST", "/api/v1/auth/login", "", map[string]string{"password": "letmein"})
	var sess struct{ Token string }
	_ = json.Unmarshal(b, &sess)
	resp, _ = ta.do(t, "GET", "/api/v1/me", sess.Token, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("登录后应可访问: %d", resp.StatusCode)
	}
	resp, _ = ta.do(t, "GET", "/api/v1/me", "garbage", nil)
	if resp.StatusCode != 401 {
		t.Fatalf("无效令牌: %d", resp.StatusCode)
	}
}

func multiConfig(c *config.Config) {
	c.Mode = config.ModeMulti
	c.Auth.JWTSecret = "0123456789abcdef0123456789abcdef"
	c.Auth.Registration = config.RegistrationInvite
	c.Quota.DailyMessages = 2
	if os.Getenv("BUUGOO_TEST_MYSQL_DSN") != "" {
		c.Database.Driver = config.DriverMySQL
		c.Database.DSN = os.Getenv("BUUGOO_TEST_MYSQL_DSN")
	}
}

// resetMySQL 清空共享 MySQL 测试库中的数据（其他包的测试也会写入）。
func resetMySQL(t *testing.T, ta *testApp) {
	t.Helper()
	for _, q := range []string{"DELETE FROM attachments", "DELETE FROM memory_usages", "DELETE FROM memories", "DELETE FROM messages", "DELETE FROM conversations",
		"DELETE FROM model_configs", "DELETE FROM usage_daily", "DELETE FROM invites", "DELETE FROM users",
		"DELETE FROM app_meta WHERE meta_key = 'models.embedding'"} {
		if err := ta.DB.Gorm().Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestMultiModeIsolationAndQuota(t *testing.T) {
	ta := newTestApp(t, multiConfig)
	ctx := context.Background()
	if os.Getenv("BUUGOO_TEST_MYSQL_DSN") != "" {
		// MySQL 测试库在用例之间共享，先清空数据。
		resetMySQL(t, ta)
	}

	admin, err := ta.Auth.CreateUser(ctx, "admin", "adminpass", true)
	if err != nil {
		t.Fatal(err)
	}
	_ = admin
	_, b := ta.do(t, "POST", "/api/v1/auth/login", "", map[string]string{"username": "admin", "password": "adminpass"})
	var adminSess struct{ Token string }
	_ = json.Unmarshal(b, &adminSess)

	resp, b := ta.do(t, "POST", "/api/v1/auth/register", "", map[string]string{"username": "alice", "password": "password1"})
	if resp.StatusCode != 400 || !strings.Contains(string(b), "invalid_invite") {
		t.Fatalf("无邀请码注册: %d %s", resp.StatusCode, b)
	}
	resp, b = ta.do(t, "POST", "/api/v1/admin/invites", adminSess.Token, map[string]any{"max_uses": 2, "ttl_hours": 24})
	if resp.StatusCode != 201 {
		t.Fatalf("创建邀请码: %d %s", resp.StatusCode, b)
	}
	var inv struct{ Code string }
	_ = json.Unmarshal(b, &inv)

	register := func(name string) string {
		resp, b := ta.do(t, "POST", "/api/v1/auth/register", "", map[string]string{"username": name, "password": "password1", "invite_code": inv.Code})
		if resp.StatusCode != 201 {
			t.Fatalf("注册 %s: %d %s", name, resp.StatusCode, b)
		}
		var s struct{ Token string }
		_ = json.Unmarshal(b, &s)
		return s.Token
	}
	alice, bob := register("alice"), register("bob")

	resp, _ = ta.do(t, "GET", "/api/v1/admin/invites", alice, nil)
	if resp.StatusCode != 403 {
		t.Fatalf("普通用户不能访问管理接口: %d", resp.StatusCode)
	}

	ta.do(t, "POST", "/api/v1/chat", alice, map[string]any{"content": "我养了一只猫"})
	ta.do(t, "POST", "/api/v1/chat", bob, map[string]any{"content": "我养了一只狗"})
	aliceMems := ta.waitMemories(t, alice, 1)
	ta.waitMemories(t, bob, 1)
	if aliceMems[0].(map[string]any)["content"] != "用户养了一只猫" {
		t.Fatalf("alice 的记忆: %v", aliceMems)
	}
	aliceMemID := uint64(aliceMems[0].(map[string]any)["id"].(float64))

	// bob 不能读取或删除 alice 的记忆，也看不到她的消息。
	resp, _ = ta.do(t, "GET", fmt.Sprintf("/api/v1/memories/%d", aliceMemID), bob, nil)
	if resp.StatusCode != 404 {
		t.Fatalf("越权读取应 404: %d", resp.StatusCode)
	}
	resp, _ = ta.do(t, "DELETE", fmt.Sprintf("/api/v1/memories/%d", aliceMemID), bob, nil)
	if resp.StatusCode != 404 {
		t.Fatalf("越权删除应 404: %d", resp.StatusCode)
	}
	_, b = ta.do(t, "GET", "/api/v1/messages", bob, nil)
	if strings.Contains(string(b), "猫") {
		t.Fatalf("bob 不应看到 alice 的消息: %s", b)
	}
	// bob 聊猫时不应检索到 alice 的记忆。
	_, b = ta.do(t, "POST", "/api/v1/chat", bob, map[string]any{"content": "你知道我的猫吗", "stream": false})
	if strings.Contains(string(b), "用户养了一只猫") {
		t.Fatalf("不应检索到其他用户的记忆: %s", b)
	}

	// 每日限额 2 条：bob 已发 2 条，第 3 条应被拒绝。
	resp, b = ta.do(t, "POST", "/api/v1/chat", bob, map[string]any{"content": "再聊一句"})
	if resp.StatusCode != 429 || !strings.Contains(string(b), "quota_exceeded") {
		t.Fatalf("应超出限额: %d %s", resp.StatusCode, b)
	}
	// 管理员默认豁免。
	for i := 0; i < 3; i++ {
		resp, _ = ta.do(t, "POST", "/api/v1/chat", adminSess.Token, map[string]any{"content": "管理员测试", "stream": false})
		if resp.StatusCode != 200 {
			t.Fatalf("管理员应豁免限额: %d", resp.StatusCode)
		}
	}

	// debug 接口在 multi 模式默认关闭。
	resp, _ = ta.do(t, "GET", "/api/v1/debug/prompt?q=x", adminSess.Token, nil)
	if resp.StatusCode != 404 {
		t.Fatalf("multi 模式 debug 应关闭: %d", resp.StatusCode)
	}

	// 退出所有设备后旧令牌失效。
	resp, _ = ta.do(t, "POST", "/api/v1/auth/logout-all", alice, nil)
	if resp.StatusCode != 204 {
		t.Fatalf("logout-all: %d", resp.StatusCode)
	}
	resp, _ = ta.do(t, "GET", "/api/v1/me", alice, nil)
	if resp.StatusCode != 401 {
		t.Fatalf("旧令牌应失效: %d", resp.StatusCode)
	}
}

func TestWebFallback(t *testing.T) {
	ta := newTestApp(t, nil)
	resp, b := ta.do(t, "GET", "/memories", "", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "<html") {
		t.Fatalf("前端路由应返回页面: %d", resp.StatusCode)
	}
}

func TestConversationsAndModelsAPI(t *testing.T) {
	ta := newTestApp(t, multiConfig)
	ctx := context.Background()
	if os.Getenv("BUUGOO_TEST_MYSQL_DSN") != "" {
		resetMySQL(t, ta)
	}
	if _, err := ta.Auth.CreateUser(ctx, "admin", "adminpass", true); err != nil {
		t.Fatal(err)
	}
	if _, err := ta.Auth.CreateUser(ctx, "alice", "password1", false); err != nil {
		t.Fatal(err)
	}
	if _, err := ta.Auth.CreateUser(ctx, "bob", "password1", false); err != nil {
		t.Fatal(err)
	}
	login := func(name, pass string) string {
		_, b := ta.do(t, "POST", "/api/v1/auth/login", "", map[string]string{"username": name, "password": pass})
		var s struct{ Token string }
		_ = json.Unmarshal(b, &s)
		return s.Token
	}
	admin, alice, bob := login("admin", "adminpass"), login("alice", "password1"), login("bob", "password1")
	llmURL := ta.Config.LLM.Chat.BaseURL

	// 模型列表：内置模型。
	_, b := ta.do(t, "GET", "/api/v1/models", alice, nil)
	if !strings.Contains(string(b), `"scope":"builtin"`) || strings.Contains(string(b), `"k"`) {
		t.Fatalf("models: %s", b)
	}

	// alice 添加自己的模型（思考方式 reasoning_effort）。
	model := map[string]any{"name": "我的 DS", "base_url": llmURL, "api_key": "sk-alice-0123456789", "model": "fake", "thinking_style": "reasoning_effort"}
	resp, b := ta.do(t, "POST", "/api/v1/models", alice, model)
	if resp.StatusCode != 201 || strings.Contains(string(b), "sk-alice-0123456789") {
		t.Fatalf("create model: %d %s", resp.StatusCode, b)
	}
	var mv struct {
		ID    uint64
		Scope string
	}
	_ = json.Unmarshal(b, &mv)
	if mv.Scope != "personal" {
		t.Fatalf("scope=%s", mv.Scope)
	}
	model["public"] = true
	if resp, _ := ta.do(t, "POST", "/api/v1/models", alice, model); resp.StatusCode != 403 {
		t.Fatalf("普通用户不能添加公共模型: %d", resp.StatusCode)
	}
	if resp, _ := ta.do(t, "PUT", fmt.Sprintf("/api/v1/models/%d", mv.ID), bob, model); resp.StatusCode != 404 {
		t.Fatalf("bob 不能修改 alice 的模型: %d", resp.StatusCode)
	}

	// 新建对话并选择该模型与思考等级。
	resp, b = ta.do(t, "POST", "/api/v1/conversations", alice, map[string]any{"model_id": mv.ID, "thinking": "high"})
	if resp.StatusCode != 201 {
		t.Fatalf("create conversation: %d %s", resp.StatusCode, b)
	}
	var conv struct {
		ID       uint64
		ModelID  *uint64 `json:"model_id"`
		Thinking string
	}
	_ = json.Unmarshal(b, &conv)
	if conv.ModelID == nil || *conv.ModelID != mv.ID || conv.Thinking != "high" {
		t.Fatalf("conv: %s", b)
	}
	if resp, _ := ta.do(t, "POST", "/api/v1/conversations", bob, map[string]any{"model_id": mv.ID}); resp.StatusCode != 400 {
		t.Fatalf("bob 不能使用 alice 的模型: %d", resp.StatusCode)
	}

	// 默认禁止普通用户的模型连接本机地址。
	resp, b = ta.do(t, "POST", "/api/v1/chat", alice, map[string]any{"conversation_id": conv.ID, "content": "你好"})
	if resp.StatusCode != 400 || !strings.Contains(string(b), "private_address") {
		t.Fatalf("应拒绝内网地址: %d %s", resp.StatusCode, b)
	}
	ta.Config.Security.AllowPrivateModelURLs = true

	// 自带 Key 不计入限额（每日 2 条）：连续发 3 条都应成功，并带有思考过程。
	for i := 0; i < 3; i++ {
		resp, b = ta.do(t, "POST", "/api/v1/chat", alice, map[string]any{"conversation_id": conv.ID, "content": "我养了一只猫"})
		if resp.StatusCode != 200 {
			t.Fatalf("chat %d: %d %s", i, resp.StatusCode, b)
		}
	}
	events := parseSSE(t, b)
	var reasoning string
	for _, e := range events {
		if e.Event == "reasoning" {
			reasoning += e.Data["text"].(string)
		}
		if e.Event == "start" && e.Data["model"].(map[string]any)["name"] != "我的 DS" {
			t.Fatalf("start: %v", e.Data)
		}
	}
	if reasoning != "想一想：high" {
		t.Fatalf("reasoning=%q events=%v", reasoning, events)
	}
	_, b = ta.do(t, "GET", "/api/v1/me", alice, nil)
	if !strings.Contains(string(b), `"messages":0`) {
		t.Fatalf("自带 Key 不应计入用量: %s", b)
	}

	// 消息历史按对话区分，并保存思考过程。
	_, b = ta.do(t, "GET", fmt.Sprintf("/api/v1/conversations/%d/messages", conv.ID), alice, nil)
	if !strings.Contains(string(b), `"reasoning":"想一想：high"`) || !strings.Contains(string(b), `"model":"fake"`) {
		t.Fatalf("messages: %s", b)
	}
	if resp, _ := ta.do(t, "GET", fmt.Sprintf("/api/v1/conversations/%d/messages", conv.ID), bob, nil); resp.StatusCode != 404 {
		t.Fatalf("bob 不能读 alice 的对话: %d", resp.StatusCode)
	}

	// 改名、改回默认模型、关闭思考。
	resp, b = ta.do(t, "PATCH", fmt.Sprintf("/api/v1/conversations/%d", conv.ID), alice, map[string]any{"title": "养猫", "model_id": nil, "thinking": "off"})
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"title":"养猫"`) || !strings.Contains(string(b), `"model_id":null`) {
		t.Fatalf("patch: %d %s", resp.StatusCode, b)
	}
	// 使用公共（内置）模型时计入限额。
	ta.do(t, "POST", "/api/v1/chat", alice, map[string]any{"conversation_id": conv.ID, "content": "嗯", "stream": false})
	_, b = ta.do(t, "GET", "/api/v1/me", alice, nil)
	if !strings.Contains(string(b), `"messages":1`) {
		t.Fatalf("公共模型应计入用量: %s", b)
	}

	_, b = ta.do(t, "GET", "/api/v1/conversations", alice, nil)
	var list struct{ Conversations []struct{ ID uint64 } }
	_ = json.Unmarshal(b, &list)
	if len(list.Conversations) != 1 {
		t.Fatalf("conversations: %s", b)
	}
	if resp, _ := ta.do(t, "DELETE", fmt.Sprintf("/api/v1/conversations/%d", conv.ID), alice, nil); resp.StatusCode != 204 {
		t.Fatalf("delete conversation: %d", resp.StatusCode)
	}
	// 删除对话不影响记忆。
	ta.waitMemories(t, alice, 1)

	// 向量模型：仅管理员。
	if resp, _ := ta.do(t, "GET", "/api/v1/admin/embedding", alice, nil); resp.StatusCode != 403 {
		t.Fatalf("普通用户不能查看向量模型: %d", resp.StatusCode)
	}
	resp, b = ta.do(t, "GET", "/api/v1/admin/embedding", admin, nil)
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"source":"config"`) || !strings.Contains(string(b), `"pending":0`) {
		t.Fatalf("embedding: %d %s", resp.StatusCode, b)
	}
	resp, b = ta.do(t, "PUT", "/api/v1/admin/embedding", admin, map[string]any{"base_url": llmURL, "model": "fake-emb-2"})
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"changed":true`) || !strings.Contains(string(b), `"pending":1`) {
		t.Fatalf("set embedding: %d %s", resp.StatusCode, b)
	}
	resp, _ = ta.do(t, "POST", "/api/v1/admin/embedding/reembed", admin, nil)
	if resp.StatusCode != 202 {
		t.Fatalf("reembed: %d", resp.StatusCode)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, b = ta.do(t, "GET", "/api/v1/admin/embedding/reembed", admin, nil)
		if strings.Contains(string(b), `"running":false`) || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(string(b), `"done":1`) {
		t.Fatalf("reembed status: %s", b)
	}
	_, b = ta.do(t, "GET", "/api/v1/admin/embedding", admin, nil)
	if !strings.Contains(string(b), `"pending":0`) || !strings.Contains(string(b), `"source":"web"`) {
		t.Fatalf("embedding after reembed: %s", b)
	}
}

func (ta *testApp) upload(t *testing.T, token, name string, data []byte) (*http.Response, []byte) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", ta.srv.URL+"/api/v1/uploads", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func TestUploadsAPI(t *testing.T) {
	ta := newTestApp(t, func(c *config.Config) { c.Uploads.MaxFileMB = 1; c.Tools.Python.Runner = config.PythonOff })

	resp, b := ta.upload(t, "", "备忘.md", []byte("# 购物清单\n- 猫粮"))
	if resp.StatusCode != 201 || !strings.Contains(string(b), `"kind":"document"`) {
		t.Fatalf("upload: %d %s", resp.StatusCode, b)
	}
	var att struct{ ID uint64 }
	_ = json.Unmarshal(b, &att)

	resp, b = ta.upload(t, "", "app.exe", []byte{0x4d, 0x5a, 0, 0})
	if resp.StatusCode != 400 || !strings.Contains(string(b), "invalid_file") {
		t.Fatalf("应拒绝二进制文件: %d %s", resp.StatusCode, b)
	}
	resp, _ = ta.upload(t, "", "big.txt", bytes.Repeat([]byte("a"), 2<<20))
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("应拒绝过大的文件: %d", resp.StatusCode)
	}

	// 先上传再删除（用户在输入框里移除文件）。
	_, b = ta.upload(t, "", "x.txt", []byte("临时"))
	var tmp struct{ ID uint64 }
	_ = json.Unmarshal(b, &tmp)
	if resp, _ := ta.do(t, "DELETE", fmt.Sprintf("/api/v1/uploads/%d", tmp.ID), "", nil); resp.StatusCode != 204 {
		t.Fatalf("delete pending: %d", resp.StatusCode)
	}

	resp, b = ta.do(t, "POST", "/api/v1/chat", "", map[string]any{"content": "", "attachment_ids": []uint64{att.ID}})
	if resp.StatusCode != 200 {
		t.Fatalf("chat: %d %s", resp.StatusCode, b)
	}
	_, b = ta.do(t, "GET", "/api/v1/messages", "", nil)
	if !strings.Contains(string(b), `"filename":"备忘.md"`) {
		t.Fatalf("消息应带附件: %s", b)
	}
	// 已发送的附件不能删除，可以下载。
	if resp, _ := ta.do(t, "DELETE", fmt.Sprintf("/api/v1/uploads/%d", att.ID), "", nil); resp.StatusCode != 404 {
		t.Fatalf("已发送的附件不能删除: %d", resp.StatusCode)
	}
	resp, b = ta.do(t, "GET", fmt.Sprintf("/api/v1/uploads/%d", att.ID), "", nil)
	if resp.StatusCode != 200 || string(b) != "# 购物清单\n- 猫粮" || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") ||
		resp.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("download: %d %v %s", resp.StatusCode, resp.Header, b)
	}
	_, b = ta.do(t, "GET", "/api/v1/meta", "", nil)
	if !strings.Contains(string(b), `"max_files":8`) {
		t.Fatalf("meta: %s", b)
	}
}
