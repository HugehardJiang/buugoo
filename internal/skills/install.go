package skills

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

// 下载和解压的限制。
const (
	maxArchiveBytes = 100 << 20
	maxExtractBytes = 200 << 20
	maxExtractFiles = 20000
)

// tree 是解压出来的文件：相对路径（用 / 分隔）→ 内容。
type tree map[string][]byte

// Candidate 是在压缩包或仓库里找到的一个 skill（一个仓库里可能有好几个）。
type Candidate struct {
	// Path 是 skill 文件夹在压缩包里的路径（根目录为空）。
	Path        string `json:"path"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// cleanPath 规范压缩包里的路径，不安全的（绝对路径、..）返回空。
func cleanPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if strings.HasPrefix(p, "/") {
		return ""
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return "" // 带 .. 的一律不要，不能指望 Clean 之后还在原来的文件夹里
		}
	}
	c := path.Clean(p)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return ""
	}
	return c
}

func skipPath(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if part == ".git" || part == "__MACOSX" || part == "node_modules" || part == ".DS_Store" {
			return true
		}
	}
	return false
}

// readTarGz 读取 tar.gz，只保留普通文件。
func readTarGz(r io.Reader) (tree, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("不是有效的 tar.gz：%v", err)
	}
	tr := tar.NewReader(gz)
	out := tree{}
	total := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("解压失败：%v", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue // 目录、链接等一律忽略
		}
		p := cleanPath(h.Name)
		if p == "" || skipPath(p) || h.Size > MaxSkillBytes {
			continue
		}
		if len(out) >= maxExtractFiles || total+int(h.Size) > maxExtractBytes {
			return nil, errors.New("压缩包太大了")
		}
		data, err := io.ReadAll(io.LimitReader(tr, MaxSkillBytes+1))
		if err != nil {
			return nil, fmt.Errorf("解压失败：%v", err)
		}
		total += len(data)
		out[p] = data
	}
}

// readZip 读取 zip，只保留普通文件。
func readZip(data []byte) (tree, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("不是有效的 zip：%v", err)
	}
	out := tree{}
	total := 0
	for _, f := range zr.File {
		if !f.Mode().IsRegular() {
			continue
		}
		p := cleanPath(f.Name)
		if p == "" || skipPath(p) || f.UncompressedSize64 > MaxSkillBytes {
			continue
		}
		if len(out) >= maxExtractFiles || total+int(f.UncompressedSize64) > maxExtractBytes {
			return nil, errors.New("压缩包太大了")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("解压失败：%v", err)
		}
		b, err := io.ReadAll(io.LimitReader(rc, MaxSkillBytes+1))
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("解压失败：%v", err)
		}
		total += len(b)
		out[p] = b
	}
	return out, nil
}

// stripTop 去掉所有文件共同的最外层文件夹（GitHub 打包时会加一层「仓库名-版本」）。
func stripTop(t tree) tree {
	top := ""
	for p := range t {
		first, _, ok := strings.Cut(p, "/")
		if !ok {
			return t // 根目录下直接有文件
		}
		if top == "" {
			top = first
		} else if top != first {
			return t
		}
	}
	if top == "" {
		return t
	}
	out := tree{}
	for p, b := range t {
		out[strings.TrimPrefix(p, top+"/")] = b
	}
	return out
}

func isSkillFile(p string) bool {
	base := path.Base(p)
	return base == "SKILL.md" || base == "skill.md"
}

// discover 找出 under（为空表示整个压缩包）里所有含 SKILL.md 的文件夹。
func discover(t tree, under string) []Candidate {
	under = strings.Trim(under, "/")
	var out []Candidate
	for p, b := range t {
		if !isSkillFile(p) {
			continue
		}
		dir := path.Dir(p)
		if dir == "." {
			dir = ""
		}
		if under != "" && dir != under && !strings.HasPrefix(dir, under+"/") {
			continue
		}
		fallback := path.Base(dir)
		if dir == "" {
			fallback = ""
		}
		m, _, err := Parse(b, fallback)
		if err != nil {
			continue
		}
		out = append(out, Candidate{Path: dir, Name: m.Name, Description: m.Description})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// sub 取出一个 skill 文件夹里的文件（路径相对于这个文件夹）。
func sub(t tree, dir string) (tree, error) {
	out := tree{}
	total := 0
	for p, b := range t {
		rel := p
		if dir != "" {
			var ok bool
			if rel, ok = strings.CutPrefix(p, dir+"/"); !ok {
				continue
			}
		}
		total += len(b)
		out[rel] = b
	}
	switch {
	case len(out) > maxSkillFiles:
		return nil, fmt.Errorf("这个 skill 的文件太多（最多 %d 个）", maxSkillFiles)
	case total > MaxSkillBytes:
		return nil, fmt.Errorf("这个 skill 太大（最多 %d MB）", MaxSkillBytes>>20)
	}
	if b, ok := out["skill.md"]; ok && out["SKILL.md"] == nil {
		out["SKILL.md"] = b
		delete(out, "skill.md")
	}
	return out, nil
}

// GitHubRef 是解析出来的 GitHub 地址。
type GitHubRef struct {
	Owner, Repo, Ref, Path string
}

// ParseGitHub 解析 GitHub 地址：仓库首页、tree/<分支>/<路径>（skill 文件夹）、blob/<分支>/<路径>/SKILL.md。
// 分支名按一段处理（main、master 这类），带 / 的分支名不支持。
func ParseGitHub(raw string) (GitHubRef, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Host != "github.com" && u.Host != "www.github.com") {
		return GitHubRef{}, errors.New("请填写 github.com 上的地址，例如 https://github.com/用户/仓库/tree/main/skills/weather")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return GitHubRef{}, errors.New("地址里要有用户名和仓库名")
	}
	g := GitHubRef{Owner: parts[0], Repo: strings.TrimSuffix(parts[1], ".git")}
	if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
		g.Ref = parts[3]
		g.Path = strings.Join(parts[4:], "/")
		if parts[2] == "blob" {
			g.Path = path.Dir(g.Path)
			if g.Path == "." {
				g.Path = ""
			}
		}
	}
	for _, s := range []string{g.Owner, g.Repo, g.Ref} {
		if strings.ContainsAny(s, "?#\\ ") {
			return GitHubRef{}, errors.New("地址格式不对")
		}
	}
	return g, nil
}

// githubAPI 是下载仓库压缩包的地址（测试时替换）。
var githubAPI = "https://api.github.com"

// downloadGitHub 下载仓库（指定分支，为空时用默认分支）的 tar.gz 并解压。
func downloadGitHub(ctx context.Context, client *http.Client, g GitHubRef) (tree, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/tarball", githubAPI, url.PathEscape(g.Owner), url.PathEscape(g.Repo))
	if g.Ref != "" {
		u += "/" + url.PathEscape(g.Ref)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "buugoo")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连不上 GitHub：%v", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, errors.New("GitHub 上找不到这个仓库或分支（私有仓库不支持）")
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return nil, errors.New("GitHub 暂时拒绝了下载（可能是请求太频繁），过一会儿再试，或者下载 zip 后上传")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("从 GitHub 下载失败：HTTP %d", resp.StatusCode)
	}
	t, err := readTarGz(io.LimitReader(resp.Body, maxArchiveBytes))
	if err != nil {
		return nil, err
	}
	return stripTop(t), nil
}
