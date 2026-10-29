package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
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
	ds := newDeepSeekAt(srv.URL, "sk-ds")
	set := New(Options{Search: ds, Fetch: ds})
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

func (f fakeRunner) Run(context.Context, string, RunEnv) (ExecResult, error) { return f.res, nil }
func (fakeRunner) Sandboxed() bool                                           { return true }

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
	res, err := l.Run(ctx, "import os\nprint(sum(range(101)), os.getcwd().startswith(os.environ['HOME']))", RunEnv{})
	if err != nil || res.Stdout != "5050 True\n" || res.ExitCode != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	res, err = l.Run(ctx, "import time\ntime.sleep(10)", RunEnv{})
	if err != nil || !res.TimedOut {
		t.Fatalf("应超时: %+v %v", res, err)
	}
	res, _ = l.Run(ctx, "raise SystemExit(3)", RunEnv{})
	if res.ExitCode != 3 {
		t.Fatalf("exit=%d", res.ExitCode)
	}
	res, _ = l.Run(ctx, "print('x'*200000)", RunEnv{})
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
	res, err := d.Run(ctx, "import os, socket\nprint(os.getuid())\ntry:\n    socket.create_connection(('1.1.1.1', 80), 2)\n    print('net')\nexcept OSError:\n    print('nonet')\ntry:\n    open('/x','w')\nexcept OSError:\n    print('ro')", RunEnv{})
	if err != nil || res.Stdout != "65534\nnonet\nro\n" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	d.timeout = 3 * time.Second
	res, err = d.Run(ctx, "while True: pass", RunEnv{})
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
	res, err := d.Run(ctx, "import tabulate\nprint(tabulate.tabulate([[1, 2]]).count('1'))\ntry:\n    open(tabulate.__file__, 'a')\nexcept OSError:\n    print('ro')", RunEnv{})
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
	res, err := l.Run(ctx, tinyPNG+"open('chart.png','wb').write(png)\nimport os\nos.makedirs('out', exist_ok=True)\nopen('out/b.png','wb').write(png)\nopen('notes.txt','w').write('x')\nprint('ok')", RunEnv{})
	if err != nil || res.Stdout != "ok\n" || len(res.Images) != 2 || res.Images[0].Name != "chart.png" || res.Images[1].Name != "b.png" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if !strings.HasPrefix(string(res.Images[0].Data), "\x89PNG") {
		t.Fatal("图片内容不对")
	}
	// 超出数量上限的图片不收集。
	res, _ = l.Run(ctx, tinyPNG+"for i in range(8):\n    open(f'{i}.png','wb').write(png)", RunEnv{})
	if len(res.Images) != maxImages || res.ImagesSkipped != 2 {
		t.Fatalf("images=%d skipped=%d", len(res.Images), res.ImagesSkipped)
	}
	// 出错时错误信息只指向用户代码，并且仍然带上已经生成的图片。
	res, _ = l.Run(ctx, tinyPNG+"open('a.png','wb').write(png)\nx = 1\n1/0", RunEnv{})
	if res.ExitCode != 1 || len(res.Images) != 1 || !strings.Contains(res.Stderr, `File "<code>", line 7`) ||
		!strings.Contains(res.Stderr, "1/0") || strings.Contains(res.Stderr, "<stdin>") {
		t.Fatalf("res=%+v", res)
	}
	// 用户代码伪造的分隔符不影响输出。
	res, _ = l.Run(ctx, "print('\\n--buugoo-images-0\\n{}')\nimport sys\nsys.exit(0)", RunEnv{})
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "--buugoo-images-0") || len(res.Images) != 0 {
		t.Fatalf("res=%+v", res)
	}
	res, _ = l.Run(ctx, "if __name__ == '__main__':\n    print('main')", RunEnv{})
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
	res, err := d.Run(ctx, "import matplotlib.pyplot as plt\nplt.plot([1, 3, 2])\nplt.title('Test')\nplt.show()\nfig2 = plt.figure()\nplt.bar(['a', 'b'], [1, 2])\nprint('done')", RunEnv{})
	if err != nil || res.ExitCode != 0 || res.Stdout != "done\n" || len(res.Images) != 2 || res.Stderr != "" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	t.Logf("画图用时 %s，图片 %d 字节", time.Since(start).Round(time.Millisecond), len(res.Images[0].Data))
	// 保存了文件时只收集文件，不再重复收集图表。
	res, err = d.Run(ctx, "import matplotlib.pyplot as plt\nplt.plot([1, 2])\nplt.savefig('line.png')", RunEnv{})
	if err != nil || len(res.Images) != 1 || res.Images[0].Name != "line.png" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestBochaAndTavily(t *testing.T) {
	var auth, path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/bocha":
			_, _ = w.Write([]byte(`{"code":200,"data":{"webPages":{"value":[{"name":"天气","url":"https://a.cn","snippet":"短","summary":"明天晴","datePublished":"2026-10-24T08:00:00+08:00"}]}}}`))
		case "/tavily":
			_, _ = w.Write([]byte(`{"results":[{"title":"News","url":"https://b.com","content":"hello"}]}`))
		case "/extract":
			_, _ = w.Write([]byte(`{"results":[{"url":"https://b.com","raw_content":"full text"}],"failed_results":[]}`))
		case "/limit":
			w.WriteHeader(432)
			_, _ = w.Write([]byte(`{"detail":{"error":"quota"}}`))
		}
	}))
	defer srv.Close()
	oldB, oldT, oldE := bochaURL, tavilyURL, tavilyExtract
	defer func() { bochaURL, tavilyURL, tavilyExtract = oldB, oldT, oldE }()
	bochaURL, tavilyURL, tavilyExtract = srv.URL+"/bocha", srv.URL+"/tavily", srv.URL+"/extract"
	ctx := context.Background()

	if NewBocha(" ") != nil || NewTavily("") != nil {
		t.Fatal("没有 Key 时不应提供")
	}
	r := New(Options{Search: NewBocha("bk")}).Run(ctx, llm.ToolCall{Name: NameSearch, Arguments: `{"query":"天气","count":3}`})
	if !r.OK || !strings.Contains(r.Output, "明天晴") || !strings.Contains(r.Output, "日期：2026-10-24") || auth != "Bearer bk" ||
		body["count"] != float64(3) || body["summary"] != true {
		t.Fatalf("bocha: %+v auth=%s body=%v", r, auth, body)
	}
	tv := NewTavily("tk")
	set := New(Options{Search: tv, Fetch: tv})
	if r := set.Run(ctx, llm.ToolCall{Name: NameSearch, Arguments: `{"query":"news"}`}); !r.OK || !strings.Contains(r.Output, "hello") || path != "/tavily" || body["max_results"] != float64(5) {
		t.Fatalf("tavily: %+v %v", r, body)
	}
	if r := set.Run(ctx, llm.ToolCall{Name: NameFetch, Arguments: `{"url":"https://b.com"}`}); !r.OK || !strings.Contains(r.Output, "full text") {
		t.Fatalf("extract: %+v", r)
	}
	tavilyURL = srv.URL + "/limit"
	if r := set.Run(ctx, llm.ToolCall{Name: NameSearch, Arguments: `{"query":"x"}`}); r.OK || !strings.Contains(r.Output, "额度用完") {
		t.Fatalf("quota: %+v", r)
	}
}

func TestDirectFetch(t *testing.T) {
	page := `<html><head><title>布谷的博客</title><style>.x{}</style><script>alert(1)</script></head>
<body><nav>首页 关于</nav><article><h1>今天</h1><p>吃了 <b>三碗</b>饭。</p><ul><li>一</li><li>二</li></ul></article><footer>版权</footer></body></html>`
	p, err := extractHTML(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "布谷的博客" || !strings.Contains(p.Content, "# 今天") || !strings.Contains(p.Content, "吃了 三碗 饭。") ||
		!strings.Contains(p.Content, "- 一") || strings.Contains(p.Content, "alert") || strings.Contains(p.Content, "首页") || strings.Contains(p.Content, "版权") {
		t.Fatalf("%+v", p)
	}
	// 不能访问内网和本机。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("secret")) }))
	defer srv.Close()
	r := New(Options{Fetch: NewDirect()}).Run(context.Background(), llm.ToolCall{Name: NameFetch, Arguments: `{"url":"` + srv.URL + `"}`})
	if r.OK || !strings.Contains(r.Output, "内网") {
		t.Fatalf("应拒绝本机地址: %+v", r)
	}
}

func newTestWorkspace(t *testing.T, max int) *DirWorkspace {
	t.Helper()
	root := t.TempDir()
	_ = os.Chmod(root, 0o755)
	src := filepath.Join(root, "uploads", "data.csv")
	_ = os.MkdirAll(filepath.Dir(src), 0o750)
	if err := os.WriteFile(src, []byte("a,b\n1,2\n3,4\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	return &DirWorkspace{Dir: filepath.Join(root, "ws", "1", "7"), Staging: filepath.Join(root, "ws", ".staging"), Max: max,
		Inputs: []InputFile{{Name: "data.csv", Path: src}, {Name: "data.csv", Path: src}}}
}

// 运行代码能读到对话里上传的文件，写在当前目录的文件下次运行还在。
func testWorkspaceRunner(t *testing.T, r Runner, ws *DirWorkspace) {
	t.Helper()
	ctx := context.Background()
	set := New(Options{Python: r, Workspace: ws, Inputs: InputNames(ws.Inputs)})
	// 文件列表每轮会变，放在 Context 里，不放进工具说明（工具说明要每轮一样，缓存才能命中）。
	if d := set.Defs()[0].Description; strings.Contains(d, "data.csv") || !strings.Contains(d, "input/ 目录（只读）") {
		t.Fatalf("description: %s", d)
	}
	if c := strings.Join(set.Context(), ";"); !strings.Contains(c, "input/ 里有对方发的文件：data.csv、data (2).csv") {
		t.Fatalf("context: %s", c)
	}
	run := func(code string) Result {
		t.Helper()
		b, _ := json.Marshal(map[string]string{"code": code})
		return set.Run(ctx, llm.ToolCall{Name: NamePython, Arguments: string(b)})
	}
	if r := run("import csv, os\nrows = list(csv.reader(open('input/data.csv')))\nprint(len(rows), sorted(os.listdir('input')))\nos.makedirs('out', exist_ok=True)\nopen('out/sum.txt','w').write('10')\nopen('note.txt','w').write('hi')"); !r.OK || !strings.Contains(r.Output, "3 ['data (2).csv', 'data.csv']") {
		t.Fatalf("run1: %+v", r)
	}
	if r := run("print(open('out/sum.txt').read(), open('note.txt').read())\ntry:\n    open('input/data.csv','a')\nexcept OSError:\n    print('ro')"); !r.OK || !strings.Contains(r.Output, "10 hi") {
		t.Fatalf("run2: %+v", r)
	}
	if got := ListWorkspace(ws.Dir, 10); strings.Join(got, ",") != "note.txt,out/" {
		t.Fatalf("list: %v", got)
	}
	if entries, _ := os.ReadDir(ws.Staging); len(entries) != 0 {
		t.Fatalf("临时目录应清理: %v", entries)
	}
}

func TestLocalWorkspace(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("没有 python3")
	}
	ws := newTestWorkspace(t, 1<<20)
	testWorkspaceRunner(t, NewLocal(config.PythonConfig{Python: "python3", Timeout: 5 * time.Second, MemoryMB: 256, LibDir: t.TempDir()}, nil), ws)
}

func TestDockerWorkspace(t *testing.T) {
	if os.Getenv("BUUGOO_TEST_DOCKER") == "" {
		t.Skip("设置 BUUGOO_TEST_DOCKER=1 运行")
	}
	d := NewDocker(config.PythonConfig{Docker: "docker", Image: "python:3.12-slim", Timeout: 20 * time.Second, MemoryMB: 256}, nil)
	ws := newTestWorkspace(t, 1<<20)
	testWorkspaceRunner(t, d, ws)
	// 超过大小上限的改动不保存，原来的文件还在。
	set := New(Options{Python: d, Workspace: ws})
	r := set.Run(context.Background(), llm.ToolCall{Name: NamePython, Arguments: `{"code":"import os\nopen('big.bin','wb').write(os.urandom(3<<20))\nprint('done')"}`})
	if !strings.Contains(r.Output, "超过上限") {
		t.Fatalf("应提示超过上限: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(ws.Dir, "big.bin")); !os.IsNotExist(err) {
		t.Fatal("超限的文件不应保存")
	}
	if _, err := os.Stat(filepath.Join(ws.Dir, "note.txt")); err != nil {
		t.Fatal("原来的文件应保留")
	}
	// 空间用满时不保存。
	ws.CanSave = func(context.Context) error { return fmt.Errorf("文件空间已经用满") }
	r = set.Run(context.Background(), llm.ToolCall{Name: NamePython, Arguments: `{"code":"open('x.txt','w').write('x')"}`})
	if !strings.Contains(r.Output, "用满") {
		t.Fatalf("%+v", r)
	}
}

func TestUntarRejectsUnsafe(t *testing.T) {
	mk := func(entries ...tar.Header) []byte {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for _, h := range entries {
			h := h
			if h.Typeflag == tar.TypeReg {
				h.Size = 2
			}
			_ = tw.WriteHeader(&h)
			if h.Typeflag == tar.TypeReg {
				_, _ = tw.Write([]byte("ok"))
			}
		}
		_ = tw.Close()
		_ = gz.Close()
		return buf.Bytes()
	}
	for _, name := range []string{"../evil", "/etc/x", "a/../../evil"} {
		if err := untar(mk(tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644}), t.TempDir(), 1<<20); err == nil {
			t.Errorf("%s 应拒绝", name)
		}
	}
	dir := t.TempDir()
	if err := untar(mk(tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}, tar.Header{Name: "a/b.txt", Typeflag: tar.TypeReg, Mode: 0o755}), dir, 1<<20); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "link")); !os.IsNotExist(err) {
		t.Fatal("链接应忽略")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a", "b.txt")); string(b) != "ok" {
		t.Fatal("普通文件应解压")
	}
}

// send_file 只能发工作目录（和 input/）里的普通文件。
func TestSendFile(t *testing.T) {
	ws := newTestWorkspace(t, 1<<20)
	_ = os.MkdirAll(filepath.Join(ws.Dir, "out"), 0o755)
	_ = os.WriteFile(filepath.Join(ws.Dir, "out", "结果.csv"), []byte("x,y\n"), 0o644)
	_ = os.WriteFile(filepath.Join(filepath.Dir(ws.Dir), "secret.txt"), []byte("no"), 0o644)
	_ = os.Symlink("/etc/passwd", filepath.Join(ws.Dir, "link"))
	set := New(Options{Python: fakeRunner{}, Workspace: ws})
	run := func(paths ...string) Result {
		b, _ := json.Marshal(map[string]any{"paths": paths})
		return set.Run(context.Background(), llm.ToolCall{Name: NameSend, Arguments: string(b)})
	}
	r := run("out/结果.csv", "/work/input/data (2).csv")
	if !r.OK || len(r.Files) != 2 || r.Files[0].Name != "结果.csv" || string(r.Files[0].Data) != "x,y\n" || r.Files[1].Name != "data (2).csv" {
		t.Fatalf("%+v", r)
	}
	for _, bad := range []string{"../secret.txt", "../../1/secret.txt", "link", "out", "nothing.txt", ""} {
		if r := run(bad); r.OK {
			t.Errorf("%q 不应能发送: %+v", bad, r)
		}
	}
	if r := run("a", "b", "c", "d", "e", "f"); r.OK {
		t.Fatal("一次最多 5 个")
	}
	// 没有工作目录时不提供。
	for _, d := range New(Options{Python: fakeRunner{}}).Defs() {
		if d.Name == NameSend {
			t.Fatal("没有工作目录时不应提供 send_file")
		}
	}
}
