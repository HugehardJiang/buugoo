package skills

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/HugehardJiang/buugoo/internal/store"
)

// maxEditBytes 是修改一个文件时内容的上限。
const maxEditBytes = 512 << 10

// CleanRel 规范 skill 里的相对路径，不安全的返回空。
func CleanRel(rel string) string { return cleanPath(strings.TrimPrefix(strings.TrimSpace(rel), "./")) }

// loadTree 读出已安装的 skill 的全部文件。
func (m *Manager) loadTree(name string) (tree, error) {
	root := filepath.Join(m.dir, name)
	t := tree{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		t[filepath.ToSlash(rel)] = b
		return nil
	})
	return t, err
}

// WriteFile 新建或改写 skill 里的一个文件（content 为 nil 表示删除这个文件），和安装一样先写到临时目录再换上去。
// 改 SKILL.md 时不能改名字。
func (m *Manager) WriteFile(ctx context.Context, name, rel string, content []byte) (*store.Skill, error) {
	s, err := m.ByName(ctx, name)
	if err != nil {
		return nil, err
	}
	clean := CleanRel(rel)
	if clean == "" {
		return nil, userErr("文件路径不对：%s", rel)
	}
	if len(content) > maxEditBytes {
		return nil, userErr("文件太大了（最多 %d KB）", maxEditBytes>>10)
	}
	t, err := m.loadTree(s.Name)
	if err != nil {
		return nil, err
	}
	if content == nil {
		if clean == "SKILL.md" {
			return nil, userErr("不能删除 SKILL.md")
		}
		if _, ok := t[clean]; !ok {
			return nil, userErr("这个 skill 里没有 %s", clean)
		}
		delete(t, clean)
	} else {
		t[clean] = content
	}
	if clean == "SKILL.md" {
		meta, _, err := Parse(content, s.Name)
		if err != nil {
			return nil, &UserError{Msg: err.Error()}
		}
		if meta.Name != s.Name {
			return nil, userErr("不能改 skill 的名字（%s → %s）", s.Name, meta.Name)
		}
	}
	return m.install(ctx, t, s.Name, s.Source, s.SourceURL)
}

// DetectInstalled 推测已安装的 skill 要不要联网（依据见 detectNetwork）。
func (m *Manager) DetectInstalled(name string) string {
	t, err := m.loadTree(name)
	if err != nil {
		return ""
	}
	return detectNetwork(t, "")
}
