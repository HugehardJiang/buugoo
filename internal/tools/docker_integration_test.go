package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
)

// TestDockerShell 在真的 Docker 里跑一遍命令行工具和后台任务的资源档（设置 BUUGOO_TEST_DOCKER=1 时才跑）。
func TestDockerShell(t *testing.T) {
	if os.Getenv("BUUGOO_TEST_DOCKER") == "" {
		t.Skip("设置 BUUGOO_TEST_DOCKER=1 才跑")
	}
	cfg := config.Default().Tools.Python
	cfg.Timeout, cfg.TaskTimeout, cfg.LibDir = 5*time.Second, 20*time.Second, t.TempDir()
	cfg.Packages = nil
	d := NewDocker(cfg, nil)
	run := func(o Options, name, args string) Result {
		o.Python = d
		s := New(o)
		return s.Run(context.Background(), llm.ToolCall{ID: "1", Name: name, Arguments: args})
	}
	cmd, _ := json.Marshal(map[string]string{"command": "echo 你好; id -u; cat /proc/self/cgroup >/dev/null; nproc; exit 3"})
	res := run(Options{}, NameShell, string(cmd))
	if !strings.Contains(res.Output, "你好") || !strings.Contains(res.Output, "65534") || !strings.Contains(res.Output, "退出码：3") || res.Input == "" {
		t.Fatalf("命令行：%+v", res)
	}
	// 聊天档 5 秒就超时，任务档能跑完 8 秒。
	sleep := `{"command":"sleep 8; echo done"}`
	if res := run(Options{}, NameShell, sleep); !strings.Contains(res.Output, "超时") {
		t.Fatalf("聊天档应超时：%+v", res)
	}
	if res := run(Options{Heavy: true}, NameShell, sleep); !strings.Contains(res.Output, "done") {
		t.Fatalf("任务档应跑完：%+v", res)
	}
	// 默认断网。
	if res := run(Options{Heavy: true}, NameShell, `{"command":"python3 -c \"import socket; socket.create_connection(('1.1.1.1', 53), 3)\" && echo online || echo offline"}`); !strings.Contains(res.Output, "offline") {
		t.Fatalf("应断网：%+v", res)
	}
}
