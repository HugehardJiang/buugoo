package skills

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gorm.io/gorm"

	"github.com/HugehardJiang/buugoo/internal/store"
)

// 安装方式。
const (
	SourceGitHub = "github"
	SourceZip    = "zip"
	SourcePaste  = "paste"
)

// ErrNotFound 表示没有这个 skill。
var ErrNotFound = errors.New("没有这个 skill")

// UserError 是给管理员看的安装错误（地址不对、格式不对等）。
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func userErr(format string, args ...any) error { return &UserError{Msg: fmt.Sprintf(format, args...)} }

// Manager 管理已安装的 skill：文件在 dir/<名字>/，元数据在数据库的 skills 表。
type Manager struct {
	db   *gorm.DB
	dir  string
	http *http.Client
	log  *slog.Logger
	mu   sync.Mutex // 安装、删除时串行，避免同名 skill 同时写入
}

// New 创建 Manager。dir 不存在时自动创建。
func New(db *gorm.DB, dir string, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{db: db, dir: dir, log: log.With("component", "skills"),
		http: &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}}
}

// Dir 返回 skill 的存放目录（运行代码时只读挂载进沙箱）。
func (m *Manager) Dir() string { return m.dir }

// List 返回全部已安装的 skill，按名字排序。
func (m *Manager) List(ctx context.Context) ([]store.Skill, error) {
	var list []store.Skill
	err := m.db.WithContext(ctx).Order("name").Find(&list).Error
	return list, err
}

// Get 按 ID 返回 skill。
func (m *Manager) Get(ctx context.Context, id uint64) (*store.Skill, error) {
	var s store.Skill
	if err := m.db.WithContext(ctx).First(&s, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

// ByName 按名字返回 skill。
func (m *Manager) ByName(ctx context.Context, name string) (*store.Skill, error) {
	var s store.Skill
	if err := m.db.WithContext(ctx).Where("name = ?", name).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

// For 返回这个用户能用的 skill：管理员开启的，减去用户自己关掉的。
func (m *Manager) For(ctx context.Context, u *store.User) []store.Skill {
	list, err := m.List(ctx)
	if err != nil {
		m.log.Warn("读取 skill 失败", "err", err)
		return nil
	}
	off := SplitNames(u.DisabledSkills)
	out := list[:0]
	for _, s := range list {
		if s.Enabled && !contains(off, s.Name) {
			out = append(out, s)
		}
	}
	return out
}

// SplitNames 解析用户关掉的名字列表（逗号分隔）。
func SplitNames(s string) []string {
	var out []string
	for _, n := range strings.Split(s, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// ToggleName 在名字列表里加上或去掉一个名字（关掉 enabled=false 时加上）。
func ToggleName(list string, name string, enabled bool) string {
	names := SplitNames(list)
	var out []string
	for _, n := range names {
		if n != name {
			out = append(out, n)
		}
	}
	if !enabled {
		out = append(out, name)
	}
	return strings.Join(out, ",")
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// InstallResult 是一次安装的结果：装好了一个 skill，或者找到了好几个需要选。
type InstallResult struct {
	Skill      *store.Skill `json:"skill,omitempty"`
	Candidates []Candidate  `json:"candidates,omitempty"`
}

// InstallGitHub 从 GitHub 安装。pick 是要装的 skill 在仓库里的路径（找到好几个时由调用方选）。
func (m *Manager) InstallGitHub(ctx context.Context, rawURL, pick string) (*InstallResult, error) {
	g, err := ParseGitHub(rawURL)
	if err != nil {
		return nil, &UserError{Msg: err.Error()}
	}
	t, err := downloadGitHub(ctx, m.http, g)
	if err != nil {
		return nil, &UserError{Msg: err.Error()}
	}
	under := g.Path
	if pick != "" {
		under = pick
	}
	return m.installTree(ctx, t, under, pick != "", SourceGitHub, strings.TrimSpace(rawURL))
}

// InstallZip 从上传的 zip 安装。
func (m *Manager) InstallZip(ctx context.Context, data []byte, pick string) (*InstallResult, error) {
	if len(data) > maxArchiveBytes {
		return nil, userErr("压缩包太大（最多 %d MB）", maxArchiveBytes>>20)
	}
	t, err := readZip(data)
	if err != nil {
		return nil, &UserError{Msg: err.Error()}
	}
	// GitHub 的「Download ZIP」会套一层「仓库名-分支」：去掉，路径和从 GitHub 安装时一样。
	// 最外层本身就是 skill 文件夹时保留，它的名字在 SKILL.md 没写 name 时要用。
	if s := stripTop(t); !isSkillRoot(s) {
		t = s
	}
	return m.installTree(ctx, t, pick, pick != "", SourceZip, "")
}

func isSkillRoot(t tree) bool {
	return t["SKILL.md"] != nil || t["skill.md"] != nil
}

// InstallText 用粘贴的 SKILL.md 安装（只有说明、不带脚本的 skill）。
func (m *Manager) InstallText(ctx context.Context, content string) (*store.Skill, error) {
	if len(content) > 512<<10 {
		return nil, userErr("SKILL.md 太长了")
	}
	return m.install(ctx, tree{"SKILL.md": []byte(content)}, "", SourcePaste, "")
}

func (m *Manager) installTree(ctx context.Context, t tree, under string, exact bool, source, sourceURL string) (*InstallResult, error) {
	cands := discover(t, under)
	if exact {
		var keep []Candidate
		for _, c := range cands {
			if c.Path == strings.Trim(under, "/") {
				keep = append(keep, c)
			}
		}
		cands = keep
	}
	switch len(cands) {
	case 0:
		return nil, userErr("没有找到 skill：需要一个文件夹里有 SKILL.md，开头写上 name 和 description")
	case 1:
		files, err := sub(t, cands[0].Path)
		if err != nil {
			return nil, &UserError{Msg: err.Error()}
		}
		s, err := m.install(ctx, files, path.Base(cands[0].Path), source, sourceURL)
		if err != nil {
			return nil, err
		}
		return &InstallResult{Skill: s}, nil
	default:
		return &InstallResult{Candidates: cands}, nil
	}
}

// install 把一个 skill 的文件写到 dir/<名字>/ 并记到数据库。已经装过同名的就更新（保留开关和联网设置）。
// folder 是 skill 原来的文件夹名，SKILL.md 里没写 name 时用它。
func (m *Manager) install(ctx context.Context, files tree, folder, source, sourceURL string) (*store.Skill, error) {
	if folder == "." || folder == "/" {
		folder = ""
	}
	meta, _, err := Parse(files["SKILL.md"], folder)
	if err != nil {
		return nil, &UserError{Msg: err.Error()}
	}
	var size int64
	for _, b := range files {
		size += int64(len(b))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return nil, err
	}
	// 先写到临时目录，再换上去：安装中途失败不会留下一半的文件。
	tmp := filepath.Join(m.dir, ".tmp-"+meta.Name+"-"+randomHex())
	if err := writeTree(tmp, files); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	final := filepath.Join(m.dir, meta.Name)
	old := final + ".old-" + randomHex()
	if _, err := os.Stat(final); err == nil {
		if err := os.Rename(final, old); err != nil {
			_ = os.RemoveAll(tmp)
			return nil, err
		}
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Rename(old, final)
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	_ = os.RemoveAll(old)

	var s store.Skill
	err = m.db.WithContext(ctx).Where("name = ?", meta.Name).First(&s).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		s = store.Skill{Name: meta.Name, Enabled: true}
	case err != nil:
		return nil, err
	}
	s.Description, s.Source, s.SourceURL, s.Files, s.Size = meta.Description, source, sourceURL, len(files), size
	if err := m.db.WithContext(ctx).Save(&s).Error; err != nil {
		return nil, err
	}
	m.log.Info("已安装 skill", "name", s.Name, "source", source, "files", len(files))
	return &s, nil
}

// writeTree 写出文件：目录 0755、文件 0644（沙箱里的用户只能读）。
func writeTree(dir string, files tree) error {
	for rel, b := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if !strings.HasPrefix(p, dir+string(filepath.Separator)) {
			return fmt.Errorf("文件路径不对：%s", rel)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func randomHex() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Update 修改 skill 的开关与是否允许联网。
func (m *Manager) Update(ctx context.Context, id uint64, enabled, network *bool) (*store.Skill, error) {
	s, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{}
	if enabled != nil {
		fields["enabled"], s.Enabled = *enabled, *enabled
	}
	if network != nil {
		fields["allow_network"], s.AllowNetwork = *network, *network
	}
	if len(fields) > 0 {
		if err := m.db.WithContext(ctx).Model(&store.Skill{}).Where("id = ?", id).Updates(fields).Error; err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Remove 卸载 skill：删除文件和记录。
func (m *Manager) Remove(ctx context.Context, id uint64) error {
	s, err := m.Get(ctx, id)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.RemoveAll(filepath.Join(m.dir, s.Name)); err != nil {
		return err
	}
	return m.db.WithContext(ctx).Delete(&store.Skill{}, id).Error
}

// Body 返回 SKILL.md 的正文（不含开头的元数据）。
func (m *Manager) Body(name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(m.dir, name, "SKILL.md"))
	if err != nil {
		return "", ErrNotFound
	}
	_, body, err := Parse(b, name)
	return body, err
}

// Files 返回 skill 里的文件（相对路径，不含 SKILL.md），最多 n 个。
func (m *Manager) Files(name string, n int) []string {
	root := filepath.Join(m.dir, name)
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel != "SKILL.md" {
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	if len(out) > n {
		out = append(out[:n], fmt.Sprintf("……共 %d 个文件", len(out)))
	}
	return out
}

// ReadFile 读出 skill 里的一个文件（不允许跳出这个 skill 的文件夹）。
func (m *Manager) ReadFile(name, rel string) ([]byte, error) {
	clean := path.Clean("/" + strings.ReplaceAll(rel, "\\", "/"))[1:]
	if clean == "" {
		return nil, errors.New("文件名为空")
	}
	root := filepath.Join(m.dir, name)
	p := filepath.Join(root, filepath.FromSlash(clean))
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("这个 skill 里没有 %s", rel)
	}
	return os.ReadFile(p)
}
