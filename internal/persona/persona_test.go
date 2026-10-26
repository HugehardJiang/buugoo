package persona

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDefaultPersona(t *testing.T) {
	p, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "布谷" || len(p.Principles) < 4 {
		t.Fatalf("默认人格不完整: %+v", p)
	}
	joined := strings.Join(p.Principles, "\n")
	for _, want := range []string{"真诚", "不同意见", "现实生活", "依赖"} {
		if !strings.Contains(joined, want) {
			t.Errorf("默认原则中缺少 %q", want)
		}
	}
}

func TestParse(t *testing.T) {
	if _, err := Parse([]byte("name: x\nidentity: y\npersonality: [a]\nprinciples: [b]\nunknown_field: 1\n")); err == nil {
		t.Fatal("未知字段应报错")
	}
	if _, err := Parse([]byte("name: x\n")); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("缺少必填字段应报错: %v", err)
	}
	p, err := Parse([]byte("name: ' 小灰 '\nidentity: 你是小灰\npersonality: [' 安静 ', '']\nprinciples: [真诚]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "小灰" || len(p.Personality) != 1 || p.Personality[0] != "安静" {
		t.Fatalf("p=%+v", p)
	}
	path := filepath.Join(t.TempDir(), "p.yaml")
	_ = os.WriteFile(path, []byte("name: a\nidentity: b\npersonality: [c]\nprinciples: [d]\n"), 0o644)
	if p, err := Load(path); err != nil || p.Name != "a" {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("文件不存在应报错")
	}
}

func TestManager(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager("", dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, src, _ := m.Raw(); src != SourceBuiltin || m.Current().Name != "布谷" {
		t.Fatalf("默认应为内置人格: %s", src)
	}
	var notified Persona
	m.OnChange(func(p Persona) { notified = p })

	if _, err := m.Save([]byte("name: 小胖\nidentity: 一只胖鸟\n")); err == nil {
		t.Fatal("缺少必填项应校验失败")
	}
	var pe *ParseError
	if _, err := m.Save([]byte("name: [")); !errors.As(err, &pe) {
		t.Fatalf("格式错误应为 ParseError: %v", err)
	}
	good := "name: 小胖\nidentity: 你是小胖\npersonality: [爱吃]\nprinciples: [真诚]\n"
	if _, err := m.Save([]byte(good)); err != nil {
		t.Fatal(err)
	}
	if m.Current().Name != "小胖" || notified.Name != "小胖" {
		t.Fatal("保存后应立即生效并通知")
	}
	// 重新启动后仍然使用网页保存的人格，即使配置文件指定了别的文件。
	other := filepath.Join(dir, "other.yaml")
	_ = os.WriteFile(other, []byte("name: 别的\nidentity: x\npersonality: [a]\nprinciples: [b]\n"), 0o600)
	m2, err := NewManager(other, dir)
	if err != nil || m2.Current().Name != "小胖" {
		t.Fatalf("网页保存的人格应优先: %v %v", m2.Current().Name, err)
	}
	// 恢复后回到配置文件指定的人格。
	if _, err := m2.Reset(); err != nil || m2.Current().Name != "别的" {
		t.Fatalf("reset: %v %v", m2.Current().Name, err)
	}
	if _, err := os.Stat(filepath.Join(dir, WebFile)); !os.IsNotExist(err) {
		t.Fatal("恢复后应删除网页保存的人格")
	}
}

func TestPresets(t *testing.T) {
	list := Presets()
	if len(list) != 5 {
		t.Fatalf("presets=%d", len(list))
	}
	base, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		got := p.Apply(base)
		if got.Name != base.Name || !strings.Contains(got.Identity, base.Name) || strings.Contains(got.Identity, "{name}") {
			t.Fatalf("%s: 名字应来自人格档案: %q", p.ID, got.Identity)
		}
		if !slices.Equal(got.Principles, base.Principles) || !slices.Equal(got.Values, base.Values) {
			t.Fatalf("%s: 原则和价值观不应改变", p.ID)
		}
		if err := got.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if Find("snarky", nil) == nil || Find("snarky", []string{"snarky"}) != nil || Find("nope", nil) != nil || Find("", nil) != nil {
		t.Fatal("Find")
	}
	if len(Available([]string{"gentle", "bubbly"})) != 3 {
		t.Fatal("Available")
	}
	// 人工智鸟：有自己的态度，情绪起伏更大，多想起几条记忆。
	alive := Find("alive", nil)
	if alive == nil || len(alive.Attitude) == 0 || alive.Emotion == nil || alive.Emotion.Temper <= 1 || alive.MemoryExtra <= 0 {
		t.Fatalf("%+v", alive)
	}
	if got := alive.Apply(base); len(got.Attitude) == 0 || !slices.Equal(got.Principles, base.Principles) {
		t.Fatal("应带上态度，原则不变")
	}
	if _, err := parsePresets([]byte("- id: a\n  label: x\n")); err == nil {
		t.Fatal("缺少字段应报错")
	}
}
