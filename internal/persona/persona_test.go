package persona

import (
	"os"
	"path/filepath"
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
