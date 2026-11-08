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

// TestDockerImageFonts：服务器字体不能盖住镜像自带的中文字体（盖住后 LibreOffice 转出的 PDF 中文是空白）。
// 设置 BUUGOO_TEST_FONT_IMAGE 为一个在 /usr/share/fonts 里带中文字体的镜像时才跑。
func TestDockerImageFonts(t *testing.T) {
	img := os.Getenv("BUUGOO_TEST_FONT_IMAGE")
	if img == "" {
		t.Skip("设置 BUUGOO_TEST_FONT_IMAGE 才跑")
	}
	cfg := config.Default().Tools.Python
	cfg.Image, cfg.LibDir, cfg.Packages = img, t.TempDir(), nil
	d := NewDocker(cfg, nil)
	d.checkImageFonts(context.Background())
	if !d.HasCJKFont() {
		t.Fatal("应检测到镜像自带的中文字体")
	}
	s := New(Options{Python: d})
	res := s.Run(context.Background(), llm.ToolCall{ID: "1", Name: NameShell, Arguments: `{"command":"find /usr/share/fonts -iname '*cjk*'"}`})
	if !strings.Contains(res.Output, "CJK") {
		t.Fatalf("运行时镜像的中文字体被盖住了：%+v", res)
	}
}
