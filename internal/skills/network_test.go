package skills

import (
	"strings"
	"testing"
)

func TestDetectNetwork(t *testing.T) {
	raw := map[string]string{
		"weather/SKILL.md":       "---\nname: w\ndescription: d\n---\n见 https://github.com/x/y",
		"weather/scripts/nmc.py": "import json\nfrom urllib.request import urlopen\nAPI = 'https://www.nmc.cn/rest/province/all'\n",
		"calc/SKILL.md":          "---\nname: c\ndescription: d\n---\n文档 https://docs.example.org/a",
		"calc/scripts/add.py":    "print(1+1)\n",
		"curl/SKILL.md":          "---\nname: k\ndescription: d\n---\n运行 `curl -s wttr.in`",
	}
	tr := tree{}
	for k, v := range raw {
		tr[k] = []byte(v)
	}
	if got := detectNetwork(tr, "weather"); !strings.Contains(got, "www.nmc.cn") {
		t.Fatalf("weather: %q", got)
	}
	if got := detectNetwork(tr, "calc"); got != "" {
		t.Fatalf("calc: %q", got)
	}
	if got := detectNetwork(tr, "curl"); !strings.Contains(got, "curl") {
		t.Fatalf("curl: %q", got)
	}
}

func TestWriteFile(t *testing.T) {
	m := newManager(t)
	ctx := t.Context()
	if _, err := m.InstallText(ctx, "---\nname: demo\ndescription: 演示\n---\n说明"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.WriteFile(ctx, "demo", "scripts/a.py", []byte("print('hi')\n")); err != nil {
		t.Fatal(err)
	}
	if b, err := m.ReadFile("demo", "scripts/a.py"); err != nil || string(b) != "print('hi')\n" {
		t.Fatalf("%q %v", b, err)
	}
	if _, err := m.WriteFile(ctx, "demo", "../evil.py", []byte("x")); err == nil {
		t.Fatal("不应允许跳出 skill 文件夹")
	}
	if _, err := m.WriteFile(ctx, "demo", "SKILL.md", []byte("---\nname: other\ndescription: d\n---\n")); err == nil {
		t.Fatal("不应允许改名")
	}
	if _, err := m.WriteFile(ctx, "demo", "scripts/a.py", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadFile("demo", "scripts/a.py"); err == nil {
		t.Fatal("文件应已删除")
	}
}
