package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
)

func TestNewDeepSeekOnlyForDeepSeek(t *testing.T) {
	if NewDeepSeek("https://api.deepseek.com/v1", "k") == nil {
		t.Fatal("DeepSeek 地址应提供搜索")
	}
	for _, u := range []string{"https://api.openai.com/v1", "https://api.deepseek.com.evil.cn/v1", "::bad"} {
		if NewDeepSeek(u, "k") != nil {
			t.Errorf("%s 不应提供搜索", u)
		}
	}
	if NewDeepSeek("https://api.deepseek.com/v1", "") != nil {
		t.Error("没有 Key 时不应提供搜索")
	}
}

func TestSearchAndFetch(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		switch r.URL.Path {
		case "/v1/tools/search":
			_, _ = w.Write([]byte(`{"results":[{"title":"杭州天气","url":"https://w.example/hz","summary":"明天多云，7~13°C","date":"2026-10-24T00:00:00+00:00"}]}`))
		case "/v1/tools/fetch":
			if gotBody["url"] == "https://bad.example/" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"message":"not found"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"title":"文章","content":"正文内容"}`))
		}
	}))
	defer srv.Close()
	set := New(Options{DeepSeek: newDeepSeekAt(srv.URL, "sk-ds")})
	if len(set.Defs()) != 2 {
		t.Fatalf("defs=%v", set.Defs())
	}
	ctx := context.Background()

	r := set.Run(ctx, llm.ToolCall{Name: NameSearch, Arguments: `{"query":"杭州明天天气"}`})
	if !r.OK || !strings.Contains(r.Output, "明天多云") || !strings.Contains(r.Output, "日期：2026-10-24") || gotAuth != "Bearer sk-ds" {
		t.Fatalf("search: %+v auth=%q", r, gotAuth)
	}
	if gotBody["count"] != float64(5) {
		t.Fatalf("默认返回 5 条: %v", gotBody)
	}

	r = set.Run(ctx, llm.ToolCall{Name: NameFetch, Arguments: `{"url":"https://a.example/x"}`})
	if !r.OK || r.Output != "# 文章\n\n正文内容" || r.Summary != "文章" {
		t.Fatalf("fetch: %+v", r)
	}
	r = set.Run(ctx, llm.ToolCall{Name: NameFetch, Arguments: `{"url":"https://bad.example/"}`})
	if r.OK || !strings.Contains(r.Output, "无法获取这个网页") {
		t.Fatalf("fetch 404: %+v", r)
	}
	for _, args := range []string{`{"url":"file:///etc/passwd"}`, `{"url":"https://u:p@a.example/"}`, `not json`} {
		if r := set.Run(ctx, llm.ToolCall{Name: NameFetch, Arguments: args}); r.OK {
			t.Errorf("%s 应失败", args)
		}
	}
	if r := set.Run(ctx, llm.ToolCall{Name: "rm_rf", Arguments: `{}`}); r.OK {
		t.Error("未知工具应失败")
	}
}

type fakeRunner struct{ res ExecResult }

func (f fakeRunner) Run(context.Context, string) (ExecResult, error) { return f.res, nil }
func (fakeRunner) Sandboxed() bool                                   { return true }

func TestPythonHandler(t *testing.T) {
	ctx := context.Background()
	set := New(Options{Python: fakeRunner{ExecResult{Stdout: "42\n"}}})
	r := set.Run(ctx, llm.ToolCall{Name: NamePython, Arguments: `{"code":"print(6*7)"}`})
	if !r.OK || !strings.Contains(r.Output, "42") || r.Input != "print(6*7)" {
		t.Fatalf("%+v", r)
	}
	set = New(Options{Python: fakeRunner{ExecResult{Stderr: "ZeroDivisionError", ExitCode: 1}}})
	r = set.Run(ctx, llm.ToolCall{Name: NamePython, Arguments: `{"code":"1/0"}`})
	if r.OK || !strings.Contains(r.Output, "退出码：1") || !strings.Contains(r.Output, "ZeroDivisionError") {
		t.Fatalf("%+v", r)
	}
}

func TestLocalRunner(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("没有 python3")
	}
	l := NewLocal(config.PythonConfig{Python: "python3", Timeout: 3 * time.Second, MemoryMB: 256})
	ctx := context.Background()
	res, err := l.Run(ctx, "import os\nprint(sum(range(101)), os.getcwd().startswith(os.environ['HOME']))")
	if err != nil || res.Stdout != "5050 True\n" || res.ExitCode != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	res, err = l.Run(ctx, "import time\ntime.sleep(10)")
	if err != nil || !res.TimedOut {
		t.Fatalf("应超时: %+v %v", res, err)
	}
	res, _ = l.Run(ctx, "raise SystemExit(3)")
	if res.ExitCode != 3 {
		t.Fatalf("exit=%d", res.ExitCode)
	}
	res, _ = l.Run(ctx, "print('x'*200000)")
	if !strings.HasSuffix(res.Stdout, "已截断）") {
		t.Fatal("输出应截断")
	}
}

// TestDockerRunner 需要本机有 Docker 与 python:3.12-slim 镜像，设置 BUUGOO_TEST_DOCKER=1 时运行。
func TestDockerRunner(t *testing.T) {
	if os.Getenv("BUUGOO_TEST_DOCKER") == "" {
		t.Skip("设置 BUUGOO_TEST_DOCKER=1 运行")
	}
	d := NewDocker(config.PythonConfig{Docker: "docker", Image: "python:3.12-slim", Timeout: 20 * time.Second, MemoryMB: 128}, nil)
	ctx := context.Background()
	if !d.Available(ctx) {
		t.Fatal("Docker 不可用")
	}
	res, err := d.Run(ctx, "import os, socket\nprint(os.getuid())\ntry:\n    socket.create_connection(('1.1.1.1', 80), 2)\n    print('net')\nexcept OSError:\n    print('nonet')\ntry:\n    open('/x','w')\nexcept OSError:\n    print('ro')")
	if err != nil || res.Stdout != "65534\nnonet\nro\n" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	d.timeout = 3 * time.Second
	res, err = d.Run(ctx, "while True: pass")
	if err != nil || !res.TimedOut {
		t.Fatalf("应超时: %+v %v", res, err)
	}
}
