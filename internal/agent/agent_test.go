package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/secret"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

// fakeLLM 是 OpenAI 兼容的假模型：按顺序返回预先写好的回复，并记下收到的请求。
type fakeLLM struct {
	mu       sync.Mutex
	replies  []map[string]any
	requests []map[string]any
}

func (f *fakeLLM) say(text string) {
	f.replies = append(f.replies, map[string]any{"role": "assistant", "content": text})
}

func (f *fakeLLM) call(name string, args any) {
	b, _ := json.Marshal(args)
	f.replies = append(f.replies, map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
		map[string]any{"id": "c" + name, "type": "function", "function": map[string]any{"name": name, "arguments": string(b)}},
	}})
}

func (f *fakeLLM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.NotFound(w, r)
		return
	}
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.requests = append(f.requests, req)
	msg := map[string]any{"role": "assistant", "content": "（没有准备回复）"}
	if len(f.replies) > 0 {
		msg, f.replies = f.replies[0], f.replies[1:]
	}
	f.mu.Unlock()
	finish := "stop"
	if msg["tool_calls"] != nil {
		finish = "tool_calls"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "x", "model": "fake", "choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
	})
}

func (f *fakeLLM) last() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func toolNames(req map[string]any) []string {
	var out []string
	list, _ := req["tools"].([]any)
	for _, t := range list {
		fn := t.(map[string]any)["function"].(map[string]any)
		out = append(out, fn["name"].(string))
	}
	return out
}

func setup(t *testing.T) (*Service, *fakeLLM, *store.User, *config.Config, *gormstore.DB) {
	t.Helper()
	ctx := context.Background()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "a.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	fake := &fakeLLM{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("data_dir: "+dir+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg.LLM.Chat.BaseURL, cfg.LLM.Chat.APIKey, cfg.LLM.Chat.Model, cfg.LLM.Chat.Tools = srv.URL+"/v1", "k", "fake", true
	box, _ := secret.New(make([]byte, 32))
	ms, err := models.NewService(ctx, db, box, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	u, err := db.Users().EnsureOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s := New(Deps{DB: db.Gorm(), Users: db.Users(), Reminders: db.Reminders(), Models: ms, Box: box,
		Quota: quota.New(cfg.Quota, db.Usage(), cfg.Location()), Config: cfg})
	return s, fake, u, cfg, db
}

func TestPersonalChange(t *testing.T) {
	s, fake, u, _, db := setup(t)
	ctx := context.Background()

	fake.call("get_my_settings", map[string]any{})
	fake.call("set_preset", map[string]any{"id": "nope"})
	fake.call("set_display_name", map[string]any{"name": "小明"})
	fake.say("好的，点确认就改。")
	reply, err := s.Send(ctx, u, "把我昵称改成小明", false)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Content != "好的，点确认就改。" || len(reply.Actions) != 1 || len(reply.Tools) != 3 {
		t.Fatalf("reply = %+v", reply)
	}
	names := toolNames(fake.last())
	if strings.Contains(strings.Join(names, ","), "change_settings") || !strings.Contains(strings.Join(names, ","), "set_display_name") {
		t.Errorf("tools without console = %v", names)
	}
	// 工具结果交给了模型：无效的性格没有生成卡片。
	msgs := fake.last()["messages"].([]any)
	var outputs []string
	for _, m := range msgs {
		if mm := m.(map[string]any); mm["role"] == "tool" {
			outputs = append(outputs, mm["content"].(string))
		}
	}
	if len(outputs) != 3 || !strings.Contains(outputs[0], `"username": "owner"`) || !strings.HasPrefix(outputs[1], "不能这样改") || !strings.Contains(outputs[2], "确认卡片") {
		t.Errorf("tool outputs = %q", outputs)
	}

	// 还没确认，不会改。
	if got, _ := db.Users().GetByID(ctx, u.ID); got.DisplayName != "" {
		t.Fatalf("changed before confirm: %q", got.DisplayName)
	}
	// 参数加密保存。
	var raw store.AgentAction
	db.Gorm().First(&raw, reply.Actions[0].ID)
	if strings.Contains(raw.Params, "小明") {
		t.Errorf("params not sealed: %q", raw.Params)
	}
	a, err := s.Confirm(ctx, u, reply.Actions[0].ID, false, nil)
	if err != nil || a.Status != store.ActionDone {
		t.Fatalf("confirm = %+v %v", a, err)
	}
	if got, _ := db.Users().GetByID(ctx, u.ID); got.DisplayName != "小明" {
		t.Errorf("display name = %q", got.DisplayName)
	}
	if _, err := s.Confirm(ctx, u, a.ID, false, nil); !errors.Is(err, ErrHandled) {
		t.Errorf("confirm twice = %v", err)
	}

	// 历史：不显示系统消息，卡片在回复里且状态更新了；下一次请求里模型能看到确认结果。
	h, err := s.History(ctx, u)
	if err != nil || len(h) != 2 || h[1].Actions[0].Status != store.ActionDone {
		t.Fatalf("history = %+v %v", h, err)
	}
	fake.say("改好了")
	if _, err := s.Send(ctx, u, "好了吗", false); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(fake.last()["messages"])
	if !strings.Contains(string(b), "【系统】用户确认并执行了修改") {
		t.Errorf("event not in history: %s", b)
	}

	// 用量计入每日额度。
	st, _ := s.d.Quota.Status(ctx, u)
	if st.Tokens != 5*15 {
		t.Errorf("tokens = %d", st.Tokens)
	}

	// 取消。
	fake.call("set_proactive", map[string]any{"on": false})
	fake.say("请确认")
	reply, err = s.Send(ctx, u, "别主动找我", false)
	if err != nil {
		t.Fatal(err)
	}
	if a, err := s.Cancel(ctx, u, reply.Actions[0].ID); err != nil || a.Status != store.ActionCanceled {
		t.Fatalf("cancel = %+v %v", a, err)
	}
	if got, _ := db.Users().GetByID(ctx, u.ID); !got.Proactive {
		t.Error("canceled change was applied")
	}

	if err := s.Clear(ctx, u); err != nil {
		t.Fatal(err)
	}
	if h, _ := s.History(ctx, u); len(h) != 0 {
		t.Errorf("after clear = %d", len(h))
	}
}

func TestSystemChange(t *testing.T) {
	s, fake, u, cfg, _ := setup(t)
	ctx := context.Background()

	fake.call("search_settings", map[string]any{"query": "压缩"})
	fake.call("change_settings", map[string]any{"changes": map[string]any{"chat.compact_at": 1000}})
	fake.call("change_settings", map[string]any{"changes": map[string]any{"chat.compact_at": 70}})
	fake.say("请确认")
	reply, err := s.Send(ctx, u, "压缩阈值改成 70", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Actions) != 1 || !reply.Actions[0].Admin || !strings.Contains(reply.Actions[0].Summary, "80 → 70") {
		t.Fatalf("reply = %+v", reply)
	}
	b, _ := json.Marshal(fake.last()["messages"])
	if !strings.Contains(string(b), "chat.compact_at") {
		t.Errorf("search result missing: %s", b)
	}
	if _, err := s.Confirm(ctx, u, reply.Actions[0].ID, false, nil); !errors.Is(err, ErrNeedConsole) {
		t.Errorf("confirm without console = %v", err)
	}
	a, err := s.Confirm(ctx, u, reply.Actions[0].ID, true, nil)
	if err != nil || a.Status != store.ActionDone {
		t.Fatalf("confirm = %+v %v", a, err)
	}
	data, err := os.ReadFile(cfg.SettingsPath())
	if err != nil || !strings.Contains(string(data), "compact_at: 70") {
		t.Errorf("settings file = %q %v", data, err)
	}
	if cfg.Chat.CompactAt != 80 {
		t.Error("running config changed before restart")
	}
}

func TestNoToolsModel(t *testing.T) {
	s, _, u, cfg, _ := setup(t)
	cfg.LLM.Chat.Tools = false
	if _, err := s.Send(context.Background(), u, "hi", false); !errors.Is(err, ErrNoToolsModel) {
		t.Errorf("err = %v", err)
	}
	if _, err := s.Send(context.Background(), u, "  ", false); !errors.Is(err, ErrEmpty) {
		t.Errorf("err = %v", err)
	}
}
