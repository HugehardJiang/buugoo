package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChatStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":{"code":"unauthorized","message":"未登录"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: start\ndata: {}\n\n: ping\n\n")
		fmt.Fprint(w, "event: memories\ndata: {\"memories\":[{\"id\":3,\"content\":\"用户养猫\",\"source\":\"stated\",\"score\":{\"total\":2.1}}]}\n\n")
		fmt.Fprint(w, "event: delta\ndata: {\"text\":\"你\"}\n\nevent: delta\ndata: {\"text\":\"好\"}\n\n")
		fmt.Fprint(w, "event: done\ndata: {\"message\":{\"id\":9,\"role\":\"assistant\",\"content\":\"你好\",\"status\":\"complete\"}}\n\n")
	}))
	defer srv.Close()

	c := New(srv.URL+"/", "tok")
	var text strings.Builder
	var mems []UsedMemory
	msg, err := c.Chat(context.Background(), "hi", func(e StreamEvent) {
		switch e.Type {
		case "delta":
			text.WriteString(e.Text)
		case "memories":
			mems = e.Memories
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.ID != 9 || text.String() != "你好" || len(mems) != 1 || mems[0].Score.Total != 2.1 {
		t.Fatalf("msg=%+v text=%q mems=%+v", msg, text.String(), mems)
	}

	c.Token = "bad"
	if _, err := c.Chat(context.Background(), "hi", func(StreamEvent) {}); !IsUnauthorized(err) {
		t.Fatalf("应返回未登录错误: %v", err)
	}
}

func TestStreamErrorAndTruncation(t *testing.T) {
	_, err := parseStream(strings.NewReader("event: start\ndata: {}\n\nevent: error\ndata: {\"error\":{\"code\":\"llm_error\",\"message\":\"模型挂了\"}}\n\n"), func(StreamEvent) {})
	if err == nil || err.Error() != "模型挂了" {
		t.Fatalf("err=%v", err)
	}
	_, err = parseStream(strings.NewReader("event: delta\ndata: {\"text\":\"半句\"}\n\n"), func(StreamEvent) {})
	if err == nil {
		t.Fatal("没有 done 事件应报错")
	}
}

func TestCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "credentials.json")
	c, err := LoadCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("http://a", "t1"); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("权限应为 0600: %v", st.Mode())
	}
	c2, _ := LoadCredentials(path)
	if c2.Get("http://a") != "t1" {
		t.Fatal("应能读回令牌")
	}
	_ = c2.Set("http://a", "")
	c3, _ := LoadCredentials(path)
	if c3.Get("http://a") != "" {
		t.Fatal("应已删除")
	}
}
