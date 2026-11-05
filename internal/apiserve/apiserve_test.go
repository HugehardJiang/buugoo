package apiserve

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func TestOpenAIToChat(t *testing.T) {
	var r OpenAIRequest
	body := `{"model":"m","max_completion_tokens":100,"reasoning_effort":"high","response_format":{"type":"json_object"},
	 "messages":[{"role":"developer","content":"你是助手"},
	  {"role":"user","content":[{"type":"text","text":"看图"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]},
	  {"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{\"a\":1}"}}]},
	  {"role":"tool","tool_call_id":"c1","content":"结果"}],
	 "tools":[{"type":"function","function":{"name":"f","description":"d","parameters":{"type":"object"}}}]}`
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	req, err := r.ToChat()
	if err != nil {
		t.Fatal(err)
	}
	if req.MaxTokens != 100 || req.Thinking != "high" || !req.JSON || len(req.Tools) != 1 || len(req.Messages) != 4 {
		t.Fatalf("%+v", req)
	}
	m := req.Messages
	if m[0].Role != llm.RoleSystem || m[1].Content != "看图" || len(m[1].Images) != 1 || m[2].ToolCalls[0].Arguments != `{"a":1}` || m[3].Role != llm.RoleTool || m[3].ToolCallID != "c1" {
		t.Fatalf("%+v", m)
	}
	r.Messages = append(r.Messages, openAIMessage{Role: "user", Content: json.RawMessage(`[{"type":"image_url","image_url":{"url":"file:///etc/passwd"}}]`)})
	if _, err := r.ToChat(); err == nil {
		t.Fatal("只接受 data: / http(s) 图片地址")
	}
}

func TestAnthropicToChat(t *testing.T) {
	var r AnthropicRequest
	body := `{"model":"m","max_tokens":50,"system":[{"type":"text","text":"系统"}],"thinking":{"type":"enabled","budget_tokens":2000},
	 "messages":[{"role":"user","content":[{"type":"text","text":"你好"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]},
	  {"role":"assistant","content":[{"type":"thinking","thinking":"想"},{"type":"text","text":"我查一下"},{"type":"tool_use","id":"t1","name":"f","input":{"q":"x"}}]},
	  {"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"晴"}]},{"type":"text","text":"然后呢"}]}],
	 "tools":[{"name":"f","description":"d","input_schema":{"type":"object"}},{"type":"web_search_20250305","name":"web_search"}]}`
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	req, err := r.ToChat()
	if err != nil {
		t.Fatal(err)
	}
	m := req.Messages
	if req.Thinking != "low" || req.MaxTokens != 50 || len(req.Tools) != 1 || len(m) != 5 {
		t.Fatalf("%+v", req)
	}
	if m[0].Role != llm.RoleSystem || m[0].Content != "系统" || m[1].Images[0].URL != "data:image/png;base64,AAAA" ||
		m[2].Content != "我查一下" || m[2].ToolCalls[0].Arguments != `{"q":"x"}` ||
		m[3].Role != llm.RoleTool || m[3].ToolCallID != "t1" || m[3].Content != "晴" || m[4].Content != "然后呢" {
		t.Fatalf("%+v", m)
	}
	r.MaxTokens = 0
	if _, err := r.ToChat(); err == nil {
		t.Fatal("max_tokens 必填")
	}
}

func TestStreams(t *testing.T) {
	var buf bytes.Buffer
	s := NewOpenAIStream(&buf, func() {}, "id1", "m", time.Unix(0, 0), true)
	_ = s.Start()
	_ = s.Chunk(llm.Chunk{Delta: "你"})
	_ = s.Chunk(llm.Chunk{ToolCalls: []llm.ToolCallDelta{{Index: 0, ID: "c1", Name: "f", Arguments: "{"}}})
	_ = s.Finish(llm.Usage{PromptTokens: 10, CompletionTokens: 2})
	out := buf.String()
	for _, want := range []string{`"content":"你"`, `"tool_calls":[{"function":{"arguments":"{","name":"f"},"id":"c1","index":0,"type":"function"}]`, `"finish_reason":"tool_calls"`, `"total_tokens":12`, "data: [DONE]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("OpenAI 流里缺少 %s：\n%s", want, out)
		}
	}

	buf.Reset()
	a := NewAnthropicStream(&buf, func() {}, "msg1", "m")
	_ = a.Start()
	_ = a.Chunk(llm.Chunk{Reasoning: "想"})
	_ = a.Chunk(llm.Chunk{Delta: "好"})
	_ = a.Chunk(llm.Chunk{ToolCalls: []llm.ToolCallDelta{{Index: 0, ID: "t1", Name: "f", Arguments: `{"q":`}}})
	_ = a.Chunk(llm.Chunk{ToolCalls: []llm.ToolCallDelta{{Index: 0, Arguments: `1}`}}})
	_ = a.Finish(llm.Usage{PromptTokens: 10, CompletionTokens: 2, CachedTokens: 4})
	out = buf.String()
	order := []string{"message_start", `"type":"thinking"`, "thinking_delta", `"index":0,"type":"content_block_stop"`, `"type":"text"`, "text_delta",
		`"type":"tool_use"`, "input_json_delta", `"stop_reason":"tool_use"`, `"cache_read_input_tokens":4`, `"input_tokens":6`, "message_stop"}
	pos := 0
	for _, want := range order {
		i := strings.Index(out[pos:], want)
		if i < 0 {
			t.Fatalf("Anthropic 流里 %s 没有按顺序出现：\n%s", want, out)
		}
		pos += i
	}
}

func TestResponses(t *testing.T) {
	r := &llm.ChatResponse{Content: "", ToolCalls: []llm.ToolCall{{ID: "t1", Name: "f", Arguments: "坏的"}}, FinishReason: "tool_calls"}
	b, _ := json.Marshal(AnthropicResponse("msg", "m", r, llm.Usage{PromptTokens: 3}))
	if !strings.Contains(string(b), `"input":{}`) || !strings.Contains(string(b), `"stop_reason":"tool_use"`) || strings.Contains(string(b), `"type":"text"`) {
		t.Fatalf("%s", b)
	}
	b, _ = json.Marshal(OpenAIResponse("id", "m", time.Unix(0, 0), &llm.ChatResponse{Content: "嗨", Reasoning: "想", FinishReason: "stop"}, llm.Usage{PromptTokens: 3, CompletionTokens: 1, CachedTokens: 2}))
	for _, want := range []string{`"content":"嗨"`, `"reasoning_content":"想"`, `"finish_reason":"stop"`, `"cached_tokens":2`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("缺少 %s：%s", want, b)
		}
	}
}

func TestCatalogAndFind(t *testing.T) {
	views := []models.View{
		{ID: 0, Name: "内置", Model: "deepseek-flash", Scope: models.ScopeBuiltin, IsDefault: true},
		{ID: 3, Name: "DeepSeek（公共）", Model: "deepseek-flash", Scope: models.ScopePublic},
		{ID: 5, Name: "GLM", Model: "glm-5.2", Scope: models.ScopePublic},
		{ID: 9, Name: "我的", Model: "gpt-x", Scope: models.ScopePersonal},
	}
	st := Settings{Default: 1.5, Models: map[string]float64{"5": 2}}
	es := Catalog(views, st)
	if es[0].Name != "deepseek-flash@0" || es[1].Name != "deepseek-flash@3" || es[2].Name != "glm-5.2" || es[2].Scale != 2 || es[0].Scale != 1.5 || !es[3].Personal || es[3].Scale != 0 {
		t.Fatalf("%+v", es)
	}
	for model, id := range map[string]uint64{"deepseek-flash@3": 3, "DEEPSEEK-FLASH": 0, "GLM": 5, "9": 9, "": 0, "glm-5.2": 5} {
		e, ok := Find(es, model)
		if !ok || e.ID != id {
			t.Errorf("Find(%q) = %+v %v，应该是 %d", model, e, ok, id)
		}
	}
	if _, ok := Find(es, "不存在"); ok {
		t.Error("不存在的模型")
	}
}

func TestKeysAndSettings(t *testing.T) {
	ctx := context.Background()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "t.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	k := NewKeys(db.Gorm())
	raw, v, err := k.Create(ctx, 1, " 我的 脚本 ")
	if err != nil || !strings.HasPrefix(raw, KeyPrefix) || len(raw) != len(KeyPrefix)+keyBody || v.Name != "我的 脚本" || strings.Contains(v.Hint, raw[10:30]) {
		t.Fatalf("%q %+v %v", raw, v, err)
	}
	got, err := k.Lookup(ctx, raw)
	if err != nil || got.UserID != 1 || got.LastUsedAt == nil {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := k.Lookup(ctx, raw[:len(raw)-1]+"x"); err != ErrInvalidKey {
		t.Fatal("错的 Key")
	}
	if err := k.Delete(ctx, 2, v.ID); err != ErrNotFound {
		t.Fatal("不能删别人的 Key")
	}
	if err := k.Delete(ctx, 1, v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Lookup(ctx, raw); err != ErrInvalidKey {
		t.Fatal("删掉后立即失效")
	}
	for i := 0; i < MaxKeys; i++ {
		if _, _, err := k.Create(ctx, 1, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := k.Create(ctx, 1, ""); err == nil {
		t.Fatal("超过上限")
	}

	ss := NewSettingsStore(db.Meta())
	if st := ss.Get(ctx); st.Default != DefaultScale || st.Scale(3) != DefaultScale {
		t.Fatalf("默认：%+v", st)
	}
	if _, err := ss.Set(ctx, Settings{Default: 0}); err == nil {
		t.Fatal("倍率要大于 0")
	}
	if _, err := ss.Set(ctx, Settings{Default: 2, Models: map[string]float64{"3": 1, "4": 0}}); err != nil {
		t.Fatal(err)
	}
	st := NewSettingsStore(db.Meta()).Get(ctx)
	if st.Scale(3) != 1 || st.Scale(4) != 2 || len(st.Models) != 1 {
		t.Fatalf("%+v", st)
	}
}
