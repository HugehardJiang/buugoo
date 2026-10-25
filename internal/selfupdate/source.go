// Package selfupdate 实现 buugoo update：检查新版本、下载并校验、备份数据库、
// 替换二进制、执行迁移、重启服务，失败时自动回滚。
package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// DefaultRepo 是默认的 GitHub 仓库。
const DefaultRepo = "HugehardJiang/buugoo"

// ChecksumsFile 是发布产物中的校验文件名（GoReleaser 生成）。
const ChecksumsFile = "checksums.txt"

// Source 描述从哪里获取发布产物。
//
// 默认从 GitHub Releases 下载；指定 Mirror 后改从镜像下载，镜像需要提供与 GitHub Releases
// 相同的布局：
//
//	<mirror>/latest                 内容为最新版本号，例如 v1.2.3
//	<mirror>/<tag>/<文件名>          例如 <mirror>/v1.2.3/buugoo_1.2.3_linux_amd64.tar.gz
type Source struct {
	Repo   string
	Mirror string
	HTTP   *http.Client
}

// NewSource 创建下载源。mirror 为空时依次读取环境变量 BUUGOO_MIRROR。
func NewSource(repo, mirror string) Source {
	if repo == "" {
		repo = DefaultRepo
	}
	if mirror == "" {
		mirror = os.Getenv("BUUGOO_MIRROR")
	}
	return Source{
		Repo:   repo,
		Mirror: strings.TrimRight(mirror, "/"),
		HTTP: &http.Client{
			Timeout:   10 * time.Minute,
			Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second},
		},
	}
}

// Latest 返回最新发布的版本号（带 v 前缀）。
func (s Source) Latest(ctx context.Context) (string, error) {
	if s.Mirror != "" {
		body, err := s.get(ctx, s.Mirror+"/latest", 1<<10)
		if err != nil {
			return "", fmt.Errorf("从镜像获取最新版本: %w", err)
		}
		return normalize(strings.TrimSpace(string(body)))
	}
	// 使用 releases/latest 的重定向地址获取版本号，不受 GitHub API 匿名频率限制。
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://github.com/"+s.Repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	client := *s.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("获取最新版本: %w", err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/tag/")
	if resp.StatusCode/100 != 3 || i < 0 {
		return "", fmt.Errorf("获取最新版本: GitHub 返回 HTTP %d（可能还没有发布任何版本）", resp.StatusCode)
	}
	return normalize(loc[i+len("/tag/"):])
}

// URL 返回某个版本中某个文件的下载地址。
func (s Source) URL(tag, name string) string {
	if s.Mirror != "" {
		return s.Mirror + "/" + tag + "/" + name
	}
	return "https://github.com/" + s.Repo + "/releases/download/" + tag + "/" + name
}

func (s Source) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载 %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// download 把 url 下载到 dst。
func (s Source) download(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("下载 %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 %s: HTTP %d", url, resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("下载 %s: %w", url, err)
	}
	return f.Close()
}

// AssetName 返回某平台的发布包文件名，与 .goreleaser.yaml 的 name_template 保持一致。
func AssetName(tag, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("buugoo_%s_%s_%s%s", strings.TrimPrefix(tag, "v"), goos, goarch, ext)
}

// CurrentAsset 返回当前平台的发布包文件名。
func CurrentAsset(tag string) string { return AssetName(tag, runtime.GOOS, runtime.GOARCH) }

// ErrInvalidVersion 表示版本号不是合法的语义化版本。
var ErrInvalidVersion = errors.New("版本号无效")

func normalize(v string) (string, error) {
	if v != "" && v[0] != 'v' {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return "", fmt.Errorf("%w: %q", ErrInvalidVersion, v)
	}
	return v, nil
}

// Normalize 规范化版本号（补全 v 前缀并校验）。
func Normalize(v string) (string, error) { return normalize(v) }

// Newer 报告 latest 是否比 current 新。current 不是合法版本（例如开发构建 dev）时视为更旧。
func Newer(latest, current string) bool {
	if !semver.IsValid(current) {
		return true
	}
	return semver.Compare(latest, current) > 0
}
