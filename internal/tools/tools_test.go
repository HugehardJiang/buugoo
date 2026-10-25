package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	l := NewLocal(config.PythonConfig{Python: "python3", Timeout: 3 * time.Second, MemoryMB: 256, LibDir: t.TempDir()}, nil)
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

func TestLibraryInstallValidation(t *testing.T) {
	lib := newLibrary(config.PythonConfig{LibDir: t.TempDir(), LibMaxMB: 1, InstallTimeout: time.Second,
		IndexURL: "http://mirror.example/simple"}, slog.Default())
	var got []string
	pip := func(_ context.Context, args []string) ([]byte, error) {
		got = args
		return []byte("Successfully installed"), nil
	}
	ctx := context.Background()
	for _, bad := range [][]string{{}, {"--index-url=http://evil"}, {"https://evil/x.whl"}, {"../x"}, {"a;rm -rf /"},
		{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"}} {
		if _, err := lib.install(ctx, bad, pip); err == nil {
			t.Errorf("%v 应被拒绝", bad)
		}
	}
	if _, err := lib.install(ctx, []string{"numpy==2.1.0", "requests[socks]", "pandas>=2,<3"}, pip); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{"install --user --only-binary=:all:", "--index-url http://mirror.example/simple",
		"--trusted-host mirror.example", "-- numpy==2.1.0 requests[socks] pandas>=2,<3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("pip 参数缺少 %q: %s", want, joined)
		}
	}
	// 库目录超过上限后不再安装。
	_ = os.WriteFile(filepath.Join(lib.dir, "big"), make([]byte, 2<<20), 0o644)
	if _, err := lib.install(ctx, []string{"six"}, pip); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("超过上限应拒绝: %v", err)
	}
}

func TestInstallTool(t *testing.T) {
	set := New(Options{Python: fakeRunner{}, Packages: []string{"numpy"}, Installer: fakeInstaller{}})
	defs := set.Defs()
	if len(defs) != 2 || !strings.Contains(defs[0].Description, "numpy") || !strings.Contains(defs[0].Description, "install_package") {
		t.Fatalf("defs=%+v", defs)
	}
	r := set.Run(context.Background(), llm.ToolCall{Name: NameInstall, Arguments: `{"packages":["networkx"]}`})
	if !r.OK || r.Input != "networkx" || !strings.Contains(r.Output, "安装完成") {
		t.Fatalf("%+v", r)
	}
	if len(New(Options{Python: fakeRunner{}}).Defs()) != 1 {
		t.Fatal("没有 Installer 时不应提供安装工具")
	}
}

type fakeInstaller struct{}

func (fakeInstaller) Install(context.Context, []string) (string, error) {
	return "Successfully installed networkx", nil
}

// TestDockerInstall 需要 Docker 与一个可访问的软件源：BUUGOO_TEST_DOCKER=1 BUUGOO_TEST_PIP_INDEX=http://…/simple，
// 软件源中需要有 tabulate。
func TestDockerInstall(t *testing.T) {
	index := os.Getenv("BUUGOO_TEST_PIP_INDEX")
	if os.Getenv("BUUGOO_TEST_DOCKER") == "" || index == "" {
		t.Skip("设置 BUUGOO_TEST_DOCKER=1 与 BUUGOO_TEST_PIP_INDEX 运行")
	}
	lib := t.TempDir()
	_ = os.Chmod(lib, 0o755)
	d := NewDocker(config.PythonConfig{Docker: "docker", Image: "python:3.12-slim", Timeout: 20 * time.Second, MemoryMB: 256,
		LibDir: lib, LibMaxMB: 100, InstallTimeout: 2 * time.Minute, IndexURL: index}, nil)
	ctx := context.Background()
	if out, err := d.Install(ctx, []string{"tabulate"}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	res, err := d.Run(ctx, "import tabulate\nprint(tabulate.tabulate([[1, 2]]).count('1'))\ntry:\n    open(tabulate.__file__, 'a')\nexcept OSError:\n    print('ro')")
	if err != nil || res.Stdout != "1\nro\n" {
		t.Fatalf("运行时应能 import 且库目录只读: %+v %v", res, err)
	}
}

// tinyPNG 是生成一张 1×1 PNG 的 Python 代码。
const tinyPNG = `import struct, zlib
def chunk(t, d):
    return struct.pack('>I', len(d)) + t + d + struct.pack('>I', zlib.crc32(t + d))
png = b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', 1, 1, 8, 2, 0, 0, 0)) + chunk(b'IDAT', zlib.compress(b'\x00\xff\x00\x00')) + chunk(b'IEND', b'')
`

func TestLocalRunnerImages(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("没有 python3")
	}
	l := NewLocal(config.PythonConfig{Python: "python3", Timeout: 5 * time.Second, MemoryMB: 256, LibDir: t.TempDir()}, nil)
	ctx := context.Background()
	res, err := l.Run(ctx, tinyPNG+"open('chart.png','wb').write(png)\nimport os\nos.makedirs('out', exist_ok=True)\nopen('out/b.png','wb').write(png)\nopen('notes.txt','w').write('x')\nprint('ok')")
	if err != nil || res.Stdout != "ok\n" || len(res.Images) != 2 || res.Images[0].Name != "chart.png" || res.Images[1].Name != "b.png" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if !strings.HasPrefix(string(res.Images[0].Data), "\x89PNG") {
		t.Fatal("图片内容不对")
	}
	// 超出数量上限的图片不收集。
	res, _ = l.Run(ctx, tinyPNG+"for i in range(8):\n    open(f'{i}.png','wb').write(png)")
	if len(res.Images) != maxImages || res.ImagesSkipped != 2 {
		t.Fatalf("images=%d skipped=%d", len(res.Images), res.ImagesSkipped)
	}
	// 出错时错误信息只指向用户代码，并且仍然带上已经生成的图片。
	res, _ = l.Run(ctx, tinyPNG+"open('a.png','wb').write(png)\nx = 1\n1/0")
	if res.ExitCode != 1 || len(res.Images) != 1 || !strings.Contains(res.Stderr, `File "<code>", line 7`) ||
		!strings.Contains(res.Stderr, "1/0") || strings.Contains(res.Stderr, "<stdin>") {
		t.Fatalf("res=%+v", res)
	}
	// 用户代码伪造的分隔符不影响输出。
	res, _ = l.Run(ctx, "print('\\n--buugoo-images-0\\n{}')\nimport sys\nsys.exit(0)")
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "--buugoo-images-0") || len(res.Images) != 0 {
		t.Fatalf("res=%+v", res)
	}
	res, _ = l.Run(ctx, "if __name__ == '__main__':\n    print('main')")
	if res.Stdout != "main\n" {
		t.Fatalf("res=%+v", res)
	}
}

func TestPythonHandlerImages(t *testing.T) {
	set := New(Options{Python: fakeRunner{ExecResult{Images: []Image{{Name: "a.png", Data: []byte("x")}}, ImagesSkipped: 1}}})
	r := set.Run(context.Background(), llm.ToolCall{Name: NamePython, Arguments: `{"code":"plot()"}`})
	if !r.OK || len(r.Images) != 1 || !strings.Contains(r.Output, "生成了 1 张图片（a.png）") ||
		!strings.Contains(r.Output, "另有 1 张") || strings.Contains(r.Output, "没有输出") || r.Summary != "运行完成，生成 1 张图片" {
		t.Fatalf("%+v", r)
	}
}

// TestDockerPlot 安装 matplotlib 后在沙箱中画图，图片应被收集。
func TestDockerPlot(t *testing.T) {
	index := os.Getenv("BUUGOO_TEST_PIP_INDEX")
	if os.Getenv("BUUGOO_TEST_DOCKER") == "" || index == "" {
		t.Skip("设置 BUUGOO_TEST_DOCKER=1 与 BUUGOO_TEST_PIP_INDEX 运行")
	}
	lib := t.TempDir()
	_ = os.Chmod(lib, 0o755)
	d := NewDocker(config.PythonConfig{Docker: "docker", Image: "python:3.12-slim", Timeout: 30 * time.Second, MemoryMB: 256,
		LibDir: lib, LibMaxMB: 500, InstallTimeout: 5 * time.Minute, IndexURL: index}, nil)
	ctx := context.Background()
	if out, err := d.Install(ctx, []string{"matplotlib"}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(lib, mplCacheDir)); err != nil {
		t.Fatalf("应建好字体缓存: %v", err)
	}
	start := time.Now()
	res, err := d.Run(ctx, "import matplotlib.pyplot as plt\nplt.plot([1, 3, 2])\nplt.title('Test')\nplt.show()\nfig2 = plt.figure()\nplt.bar(['a', 'b'], [1, 2])\nprint('done')")
	if err != nil || res.ExitCode != 0 || res.Stdout != "done\n" || len(res.Images) != 2 || res.Stderr != "" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	t.Logf("画图用时 %s，图片 %d 字节", time.Since(start).Round(time.Millisecond), len(res.Images[0].Data))
	// 保存了文件时只收集文件，不再重复收集图表。
	res, err = d.Run(ctx, "import matplotlib.pyplot as plt\nplt.plot([1, 2])\nplt.savefig('line.png')")
	if err != nil || len(res.Images) != 1 || res.Images[0].Name != "line.png" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
