package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Workspace 是一段对话运行代码时的文件环境：上传的文件（只读的 input/）与保留下来的工作目录。
type Workspace interface {
	// Prepare 准备一次运行的环境，cleanup 在运行结束后调用。
	Prepare(ctx context.Context) (env RunEnv, cleanup func(), err error)
	// Save 保存沙箱交回的工作目录（tar.gz）。
	Save(ctx context.Context, data []byte) error
}

// InputFile 是对话中上传的一个文件。
type InputFile struct {
	// Name 是在 input/ 中的文件名（原始文件名，重名时自动加序号）。
	Name string
	// Path 是文件在服务器上的位置。
	Path string
}

// 工作目录的限制。
const (
	maxInputBytes      = 200 << 20 // 一次运行最多复制进 input/ 的总字节数
	maxWorkspaceFiles  = 5000
	workspaceUnpackMul = 8 // 解压后的大小最多是上限的几倍（tar.gz 压缩前）
)

// DirWorkspace 把工作目录保存在服务器的一个目录中。
type DirWorkspace struct {
	// Dir 是这段对话的工作目录。
	Dir string
	// Staging 是复制上传文件的临时目录所在位置（Docker 需要能访问它）。
	Staging string
	Inputs  []InputFile
	// Max 是工作目录打包压缩后的大小上限（字节）。
	Max int
	// CanSave 在保存前检查（例如文件空间是否已满），返回错误时不保存。
	CanSave func(ctx context.Context) error
}

// InputNames 返回 input/ 中的文件名。
func InputNames(inputs []InputFile) []string {
	names := make([]string, len(inputs))
	used := map[string]int{}
	for i, f := range inputs {
		names[i] = uniqueName(used, f.Name)
	}
	return names
}

// uniqueName 去掉路径并在重名时加序号：a.csv、a (2).csv。
func uniqueName(used map[string]int, name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		name = "file" + name
	}
	key := strings.ToLower(name)
	used[key]++
	if n := used[key]; n > 1 {
		ext := filepath.Ext(name)
		name = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), n, ext)
		used[strings.ToLower(name)]++
	}
	return name
}

// Prepare 实现 Workspace：把上传的文件复制到临时目录（权限 0644，沙箱里的 nobody 用户可以读）。
func (w *DirWorkspace) Prepare(ctx context.Context) (RunEnv, func(), error) {
	env := RunEnv{Workspace: w.Dir, WorkspaceMax: w.Max}
	if len(w.Inputs) == 0 {
		return env, func() {}, nil
	}
	if err := os.MkdirAll(w.Staging, 0o755); err != nil {
		return env, nil, err
	}
	dir, err := os.MkdirTemp(w.Staging, "input-")
	if err != nil {
		return env, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	_ = os.Chmod(dir, 0o755)
	var total int64
	for i, name := range InputNames(w.Inputs) {
		if ctx.Err() != nil {
			cleanup()
			return env, nil, ctx.Err()
		}
		n, err := copyFile(w.Inputs[i].Path, filepath.Join(dir, name), maxInputBytes-total)
		if err != nil {
			continue // 文件不见了等情况跳过，不影响运行
		}
		total += n
	}
	env.Input = dir
	return env, cleanup, nil
}

func copyFile(src, dst string, room int64) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return 0, errors.New("不是普通文件")
	}
	if info.Size() > room {
		return 0, errors.New("超过大小上限")
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return n, err
}

// Save 实现 Workspace：安全地解压到新目录（只接受普通文件和目录，拒绝链接与越界路径），再替换旧目录。
func (w *DirWorkspace) Save(ctx context.Context, data []byte) error {
	if w.Max > 0 && len(data) > w.Max {
		return fmt.Errorf("超过 %d MB 的上限", w.Max>>20)
	}
	if w.CanSave != nil {
		if err := w.CanSave(ctx); err != nil {
			return err
		}
	}
	parent := filepath.Dir(w.Dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, ".ws-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := untar(data, tmp, int64(max(w.Max, 1<<20))*workspaceUnpackMul); err != nil {
		return err
	}
	_ = os.Chmod(tmp, 0o755)
	old := w.Dir + ".old"
	_ = os.RemoveAll(old)
	if err := os.Rename(w.Dir, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(tmp, w.Dir); err != nil {
		_ = os.Rename(old, w.Dir)
		return err
	}
	_ = os.RemoveAll(old)
	return nil
}

func untar(data []byte, dst string, limit int64) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("工作目录格式不正确: %w", err)
	}
	tr := tar.NewReader(gz)
	var total int64
	files := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("工作目录格式不正确: %w", err)
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "./"))
		if name == "." || path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "\x00") {
			return fmt.Errorf("工作目录中有不安全的路径 %q", h.Name)
		}
		if files++; files > maxWorkspaceFiles {
			return fmt.Errorf("工作目录里的文件太多（最多 %d 个）", maxWorkspaceFiles)
		}
		target := filepath.Join(dst, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if total += h.Size; total > limit {
				return errors.New("工作目录解压后太大")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, io.LimitReader(tr, h.Size))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
			_ = os.Chtimes(target, h.ModTime, h.ModTime)
		default:
			// 链接、设备等一律忽略。
		}
	}
}

// ListWorkspace 返回工作目录第一层的文件和目录名（目录带 /），最多 n 个，用于告诉模型有哪些文件。
func ListWorkspace(dir string, n int) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "input" {
			continue
		}
		if e.IsDir() {
			name += "/"
		}
		out = append(out, name)
	}
	sort.Strings(out)
	if len(out) > n {
		out = append(out[:n], fmt.Sprintf("……等共 %d 项", len(out)))
	}
	return out
}

// FileSource 是能读出工作目录中文件的 Workspace（发送文件用）。
type FileSource interface {
	// ReadFile 读出工作目录中的一个文件（不超过 max 字节）。input/ 中的文件也可以读。
	ReadFile(name string, max int64) ([]byte, error)
}

// maxSendFiles 是一次最多发送的文件数。
const maxSendFiles = 5

// ReadFile 实现 FileSource：只允许工作目录里的普通文件，不跟随链接。
func (w *DirWorkspace) ReadFile(name string, max int64) ([]byte, error) {
	clean := path.Clean("/" + strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))[1:]
	clean = strings.TrimPrefix(clean, "work/")
	if clean == "" || clean == "." {
		return nil, errors.New("文件名为空")
	}
	if rest, ok := strings.CutPrefix(clean, "input/"); ok {
		names := InputNames(w.Inputs)
		for i, n := range names {
			if n == rest {
				return readLimited(w.Inputs[i].Path, max)
			}
		}
		return nil, errors.New("input/ 里没有这个文件")
	}
	p := filepath.Join(w.Dir, filepath.FromSlash(clean))
	// 逐级检查，不允许经过链接。
	cur := w.Dir
	for _, part := range strings.Split(clean, "/") {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return nil, errors.New("没有这个文件（要先用 run_python 保存到当前目录）")
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, errors.New("不能发送链接")
		}
	}
	return readLimited(p, max)
}

func readLimited(p string, max int64) ([]byte, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, errors.New("没有这个文件")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("这不是文件")
	}
	if info.Size() > max {
		return nil, fmt.Errorf("文件太大（%.1f MB），不能超过 %d MB", float64(info.Size())/(1<<20), max>>20)
	}
	if info.Size() == 0 {
		return nil, errors.New("文件是空的")
	}
	return os.ReadFile(p)
}

// maxSendBytes 是发送的单个文件大小上限（保存时还会按上传大小上限检查）。
var maxSendBytes int64 = 50 << 20

func sendHandler(src FileSource) func(ctx context.Context, args json.RawMessage) Result {
	return func(ctx context.Context, args json.RawMessage) Result {
		var in struct {
			Paths []string `json:"paths"`
			Path  string   `json:"path"`
		}
		_ = json.Unmarshal(args, &in)
		if in.Path != "" {
			in.Paths = append(in.Paths, in.Path)
		}
		res := Result{Input: strings.Join(in.Paths, "、")}
		if len(in.Paths) == 0 {
			res.Output, res.Summary = "没有指定要发送的文件", "没有指定文件"
			return res
		}
		if len(in.Paths) > maxSendFiles {
			res.Output, res.Summary = fmt.Sprintf("一次最多发送 %d 个文件", maxSendFiles), "文件太多"
			return res
		}
		var b strings.Builder
		for _, p := range in.Paths {
			data, err := src.ReadFile(p, maxSendBytes)
			if err != nil {
				fmt.Fprintf(&b, "%s 没有发送：%s\n", p, err)
				continue
			}
			res.Files = append(res.Files, Image{Name: path.Base(strings.ReplaceAll(p, "\\", "/")), Data: data})
		}
		if len(res.Files) > 0 {
			res.OK = true
			fmt.Fprintf(&b, "已经把 %d 个文件附在你的回复里发给对方，不需要再贴链接。", len(res.Files))
			res.Summary = fmt.Sprintf("发送 %d 个文件", len(res.Files))
		} else {
			res.Summary = "没有发送"
		}
		res.Output = strings.TrimSpace(b.String())
		return res
	}
}
