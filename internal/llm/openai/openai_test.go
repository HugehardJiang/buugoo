package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
)

func chatCfg(url string) config.ModelConfig {
	return config.ModelConfig{BaseURL: url + "/v1/", APIKey: "k", Model: "m", Temperature: 0.5, MaxTokens: 100, Timeout: 5 * time.Second}
}

func TestChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("unexpected request %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "m" || *req.Temperature != 0.5 || req.MaxTokens != 100 || req.ResponseFormat == nil {
			t.Errorf("默认值未生效: %+v", req)
		}
		fmt.Fprint(w, `{"model":"m","choices":[{"message":{"role":"assistant","content":"你好"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
	}))
	defer srv.Close()
	resp, err := NewChat(chatCfg(srv.URL)).Chat(context.Background(), llm.ChatRequest{JSON: true, Messages: []llm.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "你好" || resp.Usage.Total() != 5 || resp.FinishReason != "stop" {
		t.Fatalf("resp=%+v", resp)
	}
}

func TestChatRetryAndErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"message":"slow down","type":"rate_limit"}}`)
		default:
			fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
		}
	}))
	defer srv.Close()
	c := NewChat(chatCfg(srv.URL))
	c.sleep = func(context.Context, time.Duration) error { return nil }
	resp, err := c.Chat(context.Background(), llm.ChatRequest{})
	if err != nil || resp.Content != "ok" || calls.Load() != 2 {
		t.Fatalf("resp=%v err=%v calls=%d", resp, err, calls.Load())
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid key","type":"auth"}}`)
	}))
	defer bad.Close()
	_, err = NewChat(chatCfg(bad.URL)).Chat(context.Background(), llm.ChatRequest{})
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 || apiErr.Message != "invalid key" {
		t.Fatalf("err=%v", err)
	}

	noKey := chatCfg(bad.URL)
	noKey.APIKey = ""
	if _, err := NewChat(noKey).Chat(context.Background(), llm.ChatRequest{}); !errors.Is(err, llm.ErrNotConfigured) {
		t.Fatalf("缺少 key 应返回 ErrNotConfigured: %v", err)
	}
}

func TestChatStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream || req.StreamOptions == nil || !req.StreamOptions.IncludeUsage {
			t.Errorf("流式参数错误: %+v", req)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range []string{
			`: keep-alive`,
			`data: {"model":"m","choices":[{"delta":{"role":"assistant","content":""}}]}`,
			`data: {"model":"m","choices":[{"delta":{"content":"你"}}]}`,
			`data:{"model":"m","choices":[{"delta":{"content":"好"},"finish_reason":"stop"}]}`,
			`data: {"model":"m","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2}}`,
			`data: [DONE]`,
		} {
			fmt.Fprintf(w, "%s\n\n", line)
			w.(http.Flusher).Flush()
		}
	}))
	defer srv.Close()
	st, err := NewChat(chatCfg(srv.URL)).ChatStream(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var text strings.Builder
	var usage *llm.Usage
	var finish string
	for {
		c, err := st.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		text.WriteString(c.Delta)
		if c.Usage != nil {
			usage = c.Usage
		}
		if c.FinishReason != "" {
			finish = c.FinishReason
		}
	}
	if text.String() != "你好" || usage == nil || usage.Total() != 6 || finish != "stop" {
		t.Fatalf("text=%q usage=%v finish=%q", text.String(), usage, finish)
	}
}

func TestChatStreamErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\ndata: {\"error\":{\"message\":\"boom\"}}\n\n")
	}))
	defer srv.Close()
	st, err := NewChat(chatCfg(srv.URL)).ChatStream(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if c, err := st.Recv(); err != nil || c.Delta != "a" {
		t.Fatalf("c=%v err=%v", c, err)
	}
	var apiErr *llm.APIError
	if _, err := st.Recv(); !errors.As(err, &apiErr) || apiErr.Message != "boom" {
		t.Fatalf("应解析流中的错误: %v", err)
	}
}

func TestEmbeddingBatches(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req embeddingRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "bge" || req.Dimensions != 3 || req.EncodingFormat != "float" {
			t.Errorf("req=%+v", req)
		}
		type item struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}
		var data []item
		// 倒序返回，验证按 index 还原顺序。
		for i := len(req.Input) - 1; i >= 0; i-- {
			data = append(data, item{i, []float32{float32(len(req.Input[i])), 0, 1}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()
	e := NewEmbedding(config.EmbeddingConfig{BaseURL: srv.URL, APIKey: "k", Model: "bge", Dim: 3, BatchSize: 2, Timeout: time.Second})
	vecs, err := e.Embed(context.Background(), []string{"a", "bb", "ccc"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(vecs) != 3 || vecs[0][0] != 1 || vecs[2][0] != 3 {
		t.Fatalf("calls=%d vecs=%v", calls.Load(), vecs)
	}
}

func TestThinkingStyles(t *testing.T) {
	base := chatCfg("http://x")
	cases := []struct {
		style, level string
		check        func(r chatRequest) bool
	}{
		{"reasoning_effort", "high", func(r chatRequest) bool {
			return r.ReasoningEffort == "high" && r.Temperature == nil && r.MaxTokens == 0
		}},
		{"reasoning_effort", "off", func(r chatRequest) bool { return r.ReasoningEffort == "" && r.Temperature != nil && r.MaxTokens == 100 }},
		{"enable_thinking", "medium", func(r chatRequest) bool {
			return r.EnableThinking != nil && *r.EnableThinking && r.ThinkingBudget == 4096 && r.MaxTokens == 100+4096
		}},
		{"enable_thinking", "off", func(r chatRequest) bool {
			return r.EnableThinking != nil && !*r.EnableThinking && r.ThinkingBudget == 0
		}},
		{"model_switch", "low", func(r chatRequest) bool { return r.Model == "reasoner" && r.Temperature == nil }},
		{"model_switch", "off", func(r chatRequest) bool { return r.Model == "m" }},
		{"none", "high", func(r chatRequest) bool { return r.Model == "m" && r.ReasoningEffort == "" && r.EnableThinking == nil }},
	}
	for _, tc := range cases {
		cfg := base
		cfg.ThinkingStyle, cfg.ThinkingModel = tc.style, "reasoner"
		r := NewChat(cfg).build(llm.ChatRequest{Thinking: tc.level}, true)
		if !tc.check(r) {
			t.Errorf("%s/%s: %+v", tc.style, tc.level, r)
		}
	}
}

func TestStreamReasoning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"先想\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning\":\"一想\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"答案\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	st, err := NewChat(chatCfg(srv.URL)).ChatStream(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var reasoning, content string
	for {
		c, err := st.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		reasoning += c.Reasoning
		content += c.Delta
	}
	if reasoning != "先想一想" || content != "答案" {
		t.Fatalf("reasoning=%q content=%q", reasoning, content)
	}
}

func TestPublicOnlyBlocksPrivateAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("受限客户端不应连到本机")
	}))
	defer srv.Close()
	c := NewPublicOnlyChat(config.ModelConfig{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	_, err := c.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}})
	if !errors.Is(err, ErrPrivateAddress) {
		t.Fatalf("应拒绝连接本机地址: %v", err)
	}
	for _, addr := range []string{"10.0.0.1:80", "192.168.1.1:443", "169.254.169.254:80", "[::1]:80", "100.64.0.1:80", "0.0.0.0:80", "[::ffff:127.0.0.1]:80"} {
		if denyPrivate("tcp", addr, nil) == nil {
			t.Errorf("%s 应被拒绝", addr)
		}
	}
	if err := denyPrivate("tcp", "1.1.1.1:443", nil); err != nil {
		t.Errorf("公网地址应允许: %v", err)
	}
}
