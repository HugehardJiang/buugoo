package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
)

// maxInstallPackages 是一次最多安装的包数。
const maxInstallPackages = 10

// baseMarker 记录已经装好的预装库，配置不变时启动不会重复安装。
const baseMarker = ".buugoo-packages"

// Library 是第三方库目录：所有人共用，通过 PYTHONUSERBASE 供代码使用。
type Library struct {
	dir      string
	maxMB    int
	indexURL string
	packages []string
	timeout  time.Duration
	log      *slog.Logger

	mu sync.Mutex // 同一时间只允许一次安装
}

func newLibrary(c config.PythonConfig, log *slog.Logger) *Library {
	return &Library{dir: c.LibDir, maxMB: c.LibMaxMB, indexURL: c.IndexURL, packages: c.Packages,
		timeout: c.InstallTimeout, log: log}
}

// Packages 返回预装的库。
func (l *Library) Packages() []string { return slices.Clone(l.packages) }

// ready 报告库目录是否存在（存在才挂载到运行代码的容器中）。
func (l *Library) ready() bool {
	if l == nil || l.dir == "" {
		return false
	}
	info, err := os.Stat(l.dir)
	return err == nil && info.IsDir()
}

// threadEnv 限制数值计算库的线程数，避免超出进程数限制、占满 CPU。prefix 为 "-e" 时生成 docker 参数。
func threadEnv(prefix string) []string {
	var out []string
	for _, k := range []string{"OPENBLAS_NUM_THREADS", "OMP_NUM_THREADS", "MKL_NUM_THREADS", "NUMEXPR_NUM_THREADS"} {
		if prefix != "" {
			out = append(out, prefix)
		}
		out = append(out, k+"=1")
	}
	return out
}

// pipEnv 是安装时传给 pip 的环境变量：关闭无用的提示，并沿用服务器的代理设置。
func pipEnv() []string {
	env := []string{"PIP_NO_CACHE_DIR=1", "PIP_DISABLE_PIP_VERSION_CHECK=1", "PIP_NO_WARN_SCRIPT_LOCATION=1", "PIP_ROOT_USER_ACTION=ignore"}
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// pipFunc 用给定的 pip 参数执行安装，返回合并的输出。
type pipFunc func(ctx context.Context, pipArgs []string) ([]byte, error)

// install 校验包名后执行安装。只装预编译的 wheel（--only-binary），安装过程中不会运行任何包的安装脚本。
func (l *Library) install(ctx context.Context, pkgs []string, pip pipFunc) (string, error) {
	if len(pkgs) == 0 {
		return "", errors.New("没有要安装的包")
	}
	if len(pkgs) > maxInstallPackages {
		return "", fmt.Errorf("一次最多安装 %d 个包", maxInstallPackages)
	}
	for _, p := range pkgs {
		if !config.ValidPackageSpec(p) {
			return "", fmt.Errorf("%q 不是合法的包名（只能是 PyPI 上的包名，可带版本号，例如 requests 或 numpy==2.1.0）", p)
		}
	}
	if !l.mu.TryLock() {
		return "", errors.New("正在安装别的包，请稍后再试")
	}
	defer l.mu.Unlock()
	if l.maxMB > 0 {
		if mb := dirSizeMB(l.dir); mb >= l.maxMB {
			return "", fmt.Errorf("库目录已经有 %d MB，达到上限 %d MB，不能再安装（管理员可以调整 tools.python.lib_max_mb）", mb, l.maxMB)
		}
	}
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return "", fmt.Errorf("创建库目录: %w", err)
	}
	args := []string{"install", "--user", "--only-binary=:all:", "--no-input", "--progress-bar", "off",
		"--upgrade-strategy", "only-if-needed"}
	if l.indexURL != "" {
		args = append(args, "--index-url", l.indexURL)
		// http 的镜像需要显式信任，否则 pip 会忽略它。
		if u, err := url.Parse(l.indexURL); err == nil && u.Scheme == "http" {
			args = append(args, "--trusted-host", u.Hostname())
		}
	}
	args = append(args, "--")
	args = append(args, pkgs...)
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	start := time.Now()
	out, err := pip(ctx, args)
	text := tail(strings.TrimSpace(string(out)), 40)
	if ctx.Err() != nil {
		return text, fmt.Errorf("安装超时（%s）", l.timeout)
	}
	if err != nil {
		return text, fmt.Errorf("安装失败")
	}
	l.log.Info("已安装第三方库", "packages", strings.Join(pkgs, " "), "took", time.Since(start).Round(time.Second))
	return text, nil
}

// ensureBase 在后台安装预装的库；预装列表与上次相同时跳过。
func (l *Library) ensureBase(ctx context.Context, install func(context.Context, []string) (string, error)) {
	if len(l.packages) == 0 {
		return
	}
	want := slices.Clone(l.packages)
	slices.Sort(want)
	var have []string
	if b, err := os.ReadFile(filepath.Join(l.dir, baseMarker)); err == nil {
		_ = json.Unmarshal(b, &have)
	}
	if slices.Equal(have, want) {
		return
	}
	l.log.Info("正在安装预装的 Python 库，完成前运行代码时可能缺少这些库", "packages", strings.Join(want, " "))
	out, err := install(ctx, want)
	if err != nil {
		l.log.Warn("安装预装的 Python 库失败，下次启动时会重试（国内服务器可以设置 tools.python.index_url）",
			"err", err, "output", tail(out, 8))
		return
	}
	b, _ := json.Marshal(want)
	_ = os.WriteFile(filepath.Join(l.dir, baseMarker), b, 0o644)
	l.log.Info("预装的 Python 库已就绪")
}

func dirSizeMB(dir string) int {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return int(total >> 20)
}

// tail 返回最后 n 行。
func tail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = append([]string{"…"}, lines[len(lines)-n:]...)
	}
	return strings.Join(lines, "\n")
}
