package skills

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

const weatherMD = "---\nname: weather\ndescription: 查询天气预报。用户问天气、气温、要不要带伞时使用。\n---\n# 查天气\n运行 `python scripts/weather.py 城市`。\n"

func TestParse(t *testing.T) {
	m, body, err := Parse([]byte(weatherMD), "")
	if err != nil || m.Name != "weather" || !strings.Contains(m.Description, "天气") || !strings.HasPrefix(body, "# 查天气") {
		t.Fatalf("%+v %q %v", m, body, err)
	}
	// 没有 name 时用文件夹名，并规范成小写连字符。
	if m, _, err := Parse([]byte("---\ndescription: x\n---\nbody"), "My_Skill"); err != nil || m.Name != "my-skill" {
		t.Fatalf("%+v %v", m, err)
	}
	for _, bad := range []string{"---\nname: a\n---\n没有说明", "---\nname: [\n---\n", "---\nname: 中文\ndescription: x\n---\n"} {
		if _, _, err := Parse([]byte(bad), ""); err == nil {
			t.Errorf("%q 应失败", bad)
		}
	}
}

func TestParseGitHub(t *testing.T) {
	cases := map[string]GitHubRef{
		"https://github.com/a/b":                                  {Owner: "a", Repo: "b"},
		"github.com/a/b.git":                                      {Owner: "a", Repo: "b"},
		"https://github.com/a/b/tree/main/skills/weather":         {Owner: "a", Repo: "b", Ref: "main", Path: "skills/weather"},
		"https://github.com/a/b/blob/dev/skills/weather/SKILL.md": {Owner: "a", Repo: "b", Ref: "dev", Path: "skills/weather"},
	}
	for in, want := range cases {
		if got, err := ParseGitHub(in); err != nil || got != want {
			t.Errorf("%s: %+v %v", in, got, err)
		}
	}
	for _, bad := range []string{"https://gitlab.com/a/b", "https://github.com/a", "not a url"} {
		if _, err := ParseGitHub(bad); err == nil {
			t.Errorf("%s 应失败", bad)
		}
	}
}

func tarGz(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.WriteHeader(&tar.Header{Name: "repo-abc/evil", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func newManager(t *testing.T) *Manager {
	t.Helper()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "s.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return New(db.Gorm(), filepath.Join(t.TempDir(), "skills"), nil)
}

func TestInstall(t *testing.T) {
	ctx := context.Background()
	repo := tarGz(t, map[string]string{
		"repo-abc/README.md":                         "readme",
		"repo-abc/skills/weather/SKILL.md":           weatherMD,
		"repo-abc/skills/weather/scripts/weather.py": "print('晴')",
		"repo-abc/skills/notes/SKILL.md":             "---\nname: notes\ndescription: 记笔记\n---\n记笔记",
		"repo-abc/../escape.txt":                     "x",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/a/b/tarball" && r.URL.Path != "/repos/a/b/tarball/main" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write(repo)
	}))
	defer srv.Close()
	old := githubAPI
	githubAPI = srv.URL
	defer func() { githubAPI = old }()

	m := newManager(t)
	// 仓库里有两个 skill：让管理员选。
	r, err := m.InstallGitHub(ctx, "https://github.com/a/b", "")
	if err != nil || r.Skill != nil || len(r.Candidates) != 2 || r.Candidates[1].Path != "skills/weather" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = m.InstallGitHub(ctx, "https://github.com/a/b", "skills/weather")
	if err != nil || r.Skill == nil || r.Skill.Name != "weather" || r.Skill.Files != 2 || !r.Skill.Enabled {
		t.Fatalf("%+v %v", r, err)
	}
	// 直接给出 skill 文件夹的地址。
	if r, err := m.InstallGitHub(ctx, "https://github.com/a/b/tree/main/skills/notes", ""); err != nil || r.Skill == nil || r.Skill.Name != "notes" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := m.InstallGitHub(ctx, "https://github.com/a/missing", ""); err == nil {
		t.Fatal("不存在的仓库应失败")
	}
	// API 被限流时改从网页的下载地址下载。
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a/limited/archive/HEAD.tar.gz" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write(repo)
	}))
	defer web.Close()
	oldWeb := githubWeb
	githubWeb = web.URL
	defer func() { githubWeb = oldWeb }()
	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer limited.Close()
	githubAPI = limited.URL
	if r, err := m.InstallGitHub(ctx, "https://github.com/a/limited", "skills/notes"); err != nil || r.Skill == nil || r.Skill.Name != "notes" {
		t.Fatalf("fallback: %+v %v", r, err)
	}
	if _, err := m.InstallGitHub(ctx, "https://github.com/a/other", ""); err == nil || !strings.Contains(err.Error(), "找不到") {
		t.Fatalf("fallback 404: %v", err)
	}
	githubAPI = srv.URL
	if b, err := m.ReadFile("weather", "scripts/weather.py"); err != nil || string(b) != "print('晴')" {
		t.Fatal(string(b), err)
	}
	if _, err := m.ReadFile("weather", "../notes/SKILL.md"); err == nil {
		t.Fatal("不能读到别的 skill")
	}
	if _, err := os.Stat(filepath.Join(m.Dir(), "weather", "evil")); err == nil {
		t.Fatal("链接不应解压出来")
	}

	// 重新安装时保留管理员的设置。
	on := true
	if _, err := m.Update(ctx, r.Skill.ID, nil, &on); err != nil {
		t.Fatal(err)
	}
	r2, _ := m.InstallGitHub(ctx, "https://github.com/a/b", "skills/weather")
	if !r2.Skill.AllowNetwork {
		t.Fatal("重新安装应保留联网设置")
	}

	// zip 与粘贴。
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	f, _ := zw.Create("my-tool/SKILL.md")
	_, _ = f.Write([]byte("---\ndescription: 一个工具\n---\n用法"))
	_ = zw.Close()
	if r, err := m.InstallZip(ctx, zbuf.Bytes(), ""); err != nil || r.Skill == nil || r.Skill.Name != "my-tool" {
		t.Fatalf("%+v %v", r, err)
	}
	// GitHub「Download ZIP」下载的仓库：最外层「仓库名-分支」去掉，路径和从 GitHub 安装时一样。
	zbuf.Reset()
	zw = zip.NewWriter(&zbuf)
	for name, body := range map[string]string{"b-main/skills/one/SKILL.md": "---\nname: one\ndescription: 一\n---\n", "b-main/skills/two/SKILL.md": "---\nname: two\ndescription: 二\n---\n"} {
		f, _ := zw.Create(name)
		_, _ = f.Write([]byte(body))
	}
	_ = zw.Close()
	if r, err := m.InstallZip(ctx, zbuf.Bytes(), ""); err != nil || len(r.Candidates) != 2 || r.Candidates[0].Path != "skills/one" {
		t.Fatalf("repo zip: %+v %v", r, err)
	}
	if r, err := m.InstallZip(ctx, zbuf.Bytes(), "skills/two"); err != nil || r.Skill == nil || r.Skill.Name != "two" {
		t.Fatalf("repo zip pick: %+v %v", r, err)
	}
	if s, err := m.InstallText(ctx, "---\nname: tips\ndescription: 小贴士\n---\n多喝水"); err != nil || s.Source != SourcePaste {
		t.Fatal(s, err)
	}
	if _, err := m.InstallText(ctx, "没有元数据"); err == nil {
		t.Fatal("没有 description 应失败")
	}

	// 用户关掉的不出现；管理员关掉的也不出现。
	u := &store.User{DisabledSkills: "notes"}
	names := func() string {
		var out []string
		for _, s := range m.For(ctx, u) {
			out = append(out, s.Name)
		}
		return strings.Join(out, ",")
	}
	if got := names(); got != "my-tool,tips,two,weather" {
		t.Fatal(got)
	}
	off := false
	tips, _ := m.ByName(ctx, "tips")
	_, _ = m.Update(ctx, tips.ID, &off, nil)
	if got := names(); got != "my-tool,two,weather" {
		t.Fatal(got)
	}
	if ToggleName("a,b", "a", true) != "b" || ToggleName("b", "a", false) != "b,a" {
		t.Fatal("ToggleName")
	}
	if err := m.Remove(ctx, tips.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.Dir(), "tips")); err == nil {
		t.Fatal("卸载应删除文件")
	}
}

func TestTools(t *testing.T) {
	ctx := context.Background()
	m := newManager(t)
	_, _ = m.InstallText(ctx, weatherMD)
	list := m.For(ctx, &store.User{})
	ex := Tools(m, list, nil, nil)
	if len(ex) != 2 || !strings.Contains(ex[0].Hint, "weather：查询天气预报") {
		t.Fatalf("没有运行代码时只能读说明：%d %q", len(ex), ex[0].Hint)
	}
	r := ex[0].Run(ctx, json.RawMessage(`{"name":"weather"}`))
	if !r.OK || !strings.Contains(r.Output, "python scripts/weather.py") || !strings.Contains(r.Output, "不能运行代码") {
		t.Fatal(r.Output)
	}
	if r := ex[0].Run(ctx, json.RawMessage(`{"name":"nope"}`)); r.OK {
		t.Fatal("没有的 skill")
	}
	if Tools(m, nil, nil, nil) != nil {
		t.Fatal("没有 skill 时不提供工具")
	}
	if !strings.Contains(scriptCode("weather", `python a.py "北京"`), `"python a.py \"北京\""`) {
		t.Fatal(scriptCode("weather", "x"))
	}
}

func TestSearchAndPreview(t *testing.T) {
	ctx := context.Background()
	repo := tarGz(t, map[string]string{
		"x-1/skills/weather/SKILL.md": weatherMD,
		"x-1/skills/notes/SKILL.md":   "---\nname: notes\ndescription: 记笔记\n---\n",
	})
	var gotQ string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/repositories":
			gotQ = r.URL.Query().Get("q")
			_, _ = w.Write([]byte(`{"items":[{"full_name":"a/b","html_url":"https://github.com/a/b","description":"天气 skill","stargazers_count":42,"pushed_at":"2026-10-01T00:00:00Z"}]}`))
		case "/repos/a/b/tarball":
			_, _ = w.Write(repo)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	old := githubAPI
	githubAPI = srv.URL
	defer func() { githubAPI = old }()
	m := newManager(t)

	hits, err := m.SearchGitHub(ctx, "weather", 5)
	if err != nil || len(hits) != 1 || hits[0].Name != "a/b" || hits[0].Stars != 42 || hits[0].Updated != "2026-10-01" {
		t.Fatalf("%+v %v", hits, err)
	}
	if gotQ != "weather skill" {
		t.Errorf("q = %q", gotQ)
	}
	if _, err := m.SearchGitHub(ctx, " ", 5); err == nil {
		t.Error("空关键词应失败")
	}
	cands, err := m.PreviewGitHub(ctx, "https://github.com/a/b")
	if err != nil || len(cands) != 2 || cands[1].Path != "skills/weather" {
		t.Fatalf("%+v %v", cands, err)
	}
	if list, _ := m.List(ctx); len(list) != 0 {
		t.Error("预览不应安装")
	}
}
