package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
)

// maxCodeBytes 是一段代码的大小上限。
const maxCodeBytes = 64 << 10

// maxOutputBytes 是标准输出与错误输出各自保留的上限。
const maxOutputBytes = 64 << 10

// ExecResult 是代码运行结果。
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	TimedOut bool
	// Images 是代码生成的图片（保存到工作目录的图片文件，或没有保存的 matplotlib 图表）。
	Images []Image
	// ImagesSkipped 是因为数量或大小超出上限而没有收集的图片数。
	ImagesSkipped int
	// Workspace 是沙箱中工作目录有变化时打包交回的 tar.gz（本机运行时直接写在工作目录里，为空）。
	Workspace []byte
	// WorkspaceErr 说明工作目录的改动为什么没有交回。
	WorkspaceErr string
}

// RunEnv 是一次运行的文件环境。零值表示没有对话文件、不保留文件。
type RunEnv struct {
	// Workspace 是这段对话的工作目录（服务器上的路径），代码保存在当前目录的文件下次运行还在。
	Workspace string
	// Input 是本对话上传的文件复制到的目录，运行时以只读的 input/ 出现在当前目录。
	Input string
	// WorkspaceMax 是工作目录（打包压缩后）的大小上限，字节。
	WorkspaceMax int
}

// Runner 运行 Python 代码。
type Runner interface {
	Run(ctx context.Context, code string, env RunEnv) (ExecResult, error)
	// Sandboxed 报告代码是否在隔离环境中运行（断网、只读、看不到服务器文件）。
	Sandboxed() bool
}

func pythonHandler(r Runner, ws Workspace) handler {
	return func(ctx context.Context, args json.RawMessage) Result {
		var in struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(args, &in)
		res := Result{Input: in.Code}
		if strings.TrimSpace(in.Code) == "" {
			res.Output, res.Summary = "代码为空", "代码为空"
			return res
		}
		if len(in.Code) > maxCodeBytes {
			res.Output, res.Summary = "代码太长", "代码太长"
			return res
		}
		var env RunEnv
		if ws != nil {
			e, cleanup, err := ws.Prepare(ctx)
			if err != nil {
				res.Output, res.Summary = "准备运行环境失败："+err.Error(), "无法运行"
				return res
			}
			defer cleanup()
			env = e
		}
		out, err := r.Run(ctx, in.Code, env)
		if err != nil {
			res.Output, res.Summary = "无法运行代码："+err.Error(), "无法运行"
			return res
		}
		res.Images = out.Images
		if out.Workspace != nil && ws != nil {
			if err := ws.Save(ctx, out.Workspace); err != nil {
				out.WorkspaceErr = "这次保存的文件没有保留下来：" + err.Error()
			}
		}
		var b strings.Builder
		switch {
		case out.TimedOut:
			b.WriteString("运行超时，已终止。\n")
			res.Summary = "运行超时"
		case out.ExitCode != 0:
			fmt.Fprintf(&b, "退出码：%d\n", out.ExitCode)
			res.Summary = "运行出错"
		default:
			res.OK, res.Summary = true, "运行完成"
		}
		if out.Stdout != "" {
			b.WriteString("标准输出：\n" + out.Stdout + "\n")
		}
		if out.Stderr != "" {
			b.WriteString("错误输出：\n" + out.Stderr + "\n")
		}
		if len(out.Images) > 0 {
			names := make([]string, len(out.Images))
			for i, img := range out.Images {
				names[i] = img.Name
			}
			fmt.Fprintf(&b, "生成了 %d 张图片（%s），已经显示在你的回复里，不需要再贴链接或用 Markdown 插入图片。\n",
				len(out.Images), strings.Join(names, "、"))
			if res.OK {
				res.Summary = fmt.Sprintf("运行完成，生成 %d 张图片", len(out.Images))
			}
		}
		if out.ImagesSkipped > 0 {
			fmt.Fprintf(&b, "另有 %d 张图片超出数量或大小上限，没有显示（最多 %d 张，每张不超过 %d MB）。\n",
				out.ImagesSkipped, maxImages, maxImageBytes>>20)
		}
		if out.WorkspaceErr != "" {
			b.WriteString(out.WorkspaceErr + "\n")
		}
		if out.Stdout == "" && out.Stderr == "" && len(out.Images) == 0 {
			b.WriteString("（没有输出，记得用 print 打印结果）")
		}
		res.Output = truncate(strings.TrimSpace(b.String()), 8000)
		res.Detail = truncate(strings.TrimSpace(out.Stdout+"\n"+out.Stderr), 4000)
		return res
	}
}

// limitedBuffer 只保留前 n 字节。
type limitedBuffer struct {
	buf bytes.Buffer
	n   int
	cut bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.n - l.buf.Len(); room > 0 {
		if len(p) > room {
			l.buf.Write(p[:room])
			l.cut = true
		} else {
			l.buf.Write(p)
		}
	} else if len(p) > 0 {
		l.cut = true
	}
	return len(p), nil
}

func (l *limitedBuffer) String() string {
	s := l.buf.String()
	if l.cut {
		s += "\n…（输出过长，已截断）"
	}
	return s
}

func exitCode(err error) (int, error) {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return 0, err
}

// ---------- Docker ----------

// Docker 在一次性容器中运行代码：断网、只读根文件系统、限制内存/CPU/进程数、非 root、去掉所有能力。
type Docker struct {
	bin, image string
	timeout    time.Duration
	memoryMB   int
	lib        *Library
	log        *slog.Logger

	mu        sync.Mutex
	checkedAt time.Time
	ok        bool
}

// NewDocker 创建 Docker 运行器。
func NewDocker(c config.PythonConfig, log *slog.Logger) *Docker {
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "python")
	return &Docker{bin: c.Docker, image: c.Image, timeout: c.Timeout, memoryMB: c.MemoryMB, lib: newLibrary(c, log), log: log}
}

// Library 返回第三方库目录。
func (d *Docker) Library() *Library { return d.lib }

// Sandboxed 实现 Runner。
func (d *Docker) Sandboxed() bool { return true }

// Available 报告 Docker 是否可用（结果缓存一分钟）。
func (d *Docker) Available(ctx context.Context) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if time.Since(d.checkedAt) < time.Minute {
		return d.ok
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	d.ok = exec.CommandContext(ctx, d.bin, "version", "--format", "{{.Server.Version}}").Run() == nil
	d.checkedAt = time.Now()
	return d.ok
}

// Prepare 在镜像不存在时拉取，避免第一次运行代码时等待下载而超时。
func (d *Docker) Prepare(ctx context.Context) {
	if !d.Available(ctx) {
		d.log.Warn("Docker 不可用，运行代码功能已关闭（可在 tools.python 中改用 local 或 off）")
		return
	}
	if exec.CommandContext(ctx, d.bin, "image", "inspect", d.image).Run() != nil {
		d.log.Info("正在拉取运行代码用的镜像", "image", d.image)
		if out, err := exec.CommandContext(ctx, d.bin, "pull", d.image).CombinedOutput(); err != nil {
			d.log.Warn("拉取镜像失败，运行代码时会再次尝试", "image", d.image, "err", err, "output", strings.TrimSpace(string(out)))
			return
		}
		d.log.Info("镜像已就绪", "image", d.image)
	}
	d.lib.ensureBase(ctx, d.Install)
	d.warmFonts(ctx)
}

// warmFonts 预先建好 matplotlib 的字体缓存（服务器字体可能变了，所以每次启动都重建），
// 运行代码时复制一份使用。没有装 matplotlib 时什么也不做。
func (d *Docker) warmFonts(ctx context.Context) {
	if !d.lib.ready() {
		return
	}
	cache := containerLib + "/" + mplCacheDir
	args := []string{"run", "--rm", "--network", "none", "--read-only", "--tmpfs", "/tmp:rw,size=64m,mode=1777",
		"--memory", "512m", "--cpus", "1", "--pids-limit", "64", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"-v", d.lib.dir + ":" + containerLib, "-e", "HOME=/tmp", "-e", "PYTHONUSERBASE=" + containerLib, "-e", "MPLCONFIGDIR=" + cache}
	args = append(args, d.fontArgs()...)
	if uid, gid := os.Getuid(), os.Getgid(); uid >= 0 {
		args = append(args, "--user", fmt.Sprintf("%d:%d", uid, gid))
	}
	script := "import importlib.util, shutil, sys\n" +
		"if importlib.util.find_spec('matplotlib') is None: sys.exit(0)\n" +
		"shutil.rmtree('" + cache + "', ignore_errors=True)\n" +
		"import matplotlib.font_manager\n"
	args = append(args, d.image, "python3", "-c", script)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(ctx, d.bin, args...).CombinedOutput(); err != nil {
		d.log.Warn("建立 matplotlib 字体缓存失败，画图时可能会慢一些", "err", err, "output", tail(strings.TrimSpace(string(out)), 8))
	}
}

// Install 在一个可以联网的临时容器中安装库：只装预编译包（不运行安装脚本），
// 以服务用户身份写入库目录。运行代码的容器只能只读地使用这些库，并且仍然断网。
func (d *Docker) Install(ctx context.Context, pkgs []string) (string, error) {
	return d.lib.install(ctx, pkgs, func(ctx context.Context, pipArgs []string) ([]byte, error) {
		var rnd [6]byte
		_, _ = rand.Read(rnd[:])
		args := []string{"run", "--rm", "--name", "buugoo-pip-" + hex.EncodeToString(rnd[:]),
			"--read-only", "--tmpfs", "/tmp:rw,size=1g,mode=1777",
			"--memory", "1g", "--cpus", "2", "--pids-limit", "256",
			"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
			"-v", d.lib.dir + ":" + containerLib, "-e", "HOME=/tmp", "-e", "PYTHONUSERBASE=" + containerLib}
		for _, e := range pipEnv() {
			args = append(args, "-e", e)
		}
		if uid, gid := os.Getuid(), os.Getgid(); uid >= 0 {
			args = append(args, "--user", fmt.Sprintf("%d:%d", uid, gid))
		}
		args = append(args, d.image, "python3", "-m", "pip")
		out, err := exec.CommandContext(ctx, d.bin, append(args, pipArgs...)...).CombinedOutput()
		if err == nil {
			// matplotlib 也可能作为别的库的依赖装进来，所以每次安装后都更新字体缓存。
			d.warmFonts(context.WithoutCancel(ctx))
		}
		return out, err
	})
}

// containerLib 是库目录在容器中的挂载位置。
const containerLib = "/opt/pylib"

// Run 实现 Runner。
func (d *Docker) Run(ctx context.Context, code string, env RunEnv) (ExecResult, error) {
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	name := "buugoo-py-" + hex.EncodeToString(rnd[:])
	mem := strconv.Itoa(d.memoryMB) + "m"
	args := []string{"run", "--rm", "-i", "--name", name,
		"--network", "none", "--read-only", "--tmpfs", "/tmp:rw,size=64m,mode=1777",
		"--memory", mem, "--memory-swap", mem, "--cpus", "1", "--pids-limit", "64",
		"--user", "65534:65534", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"-e", "HOME=/tmp", "-e", "PYTHONIOENCODING=utf-8"}
	args = append(args, threadEnv("-e")...)
	// 工作目录是有大小上限的内存盘：上次保存的文件只读挂载进来复制一份，结束后有变化再打包交回，
	// 代码无法直接写服务器的磁盘。
	wd := "/tmp"
	if env.Workspace != "" {
		wd = "/work"
		size := env.WorkspaceMax>>20 + 32
		args = append(args, "--tmpfs", fmt.Sprintf("/work:rw,size=%dm,mode=1777", size),
			"-e", "BUUGOO_WS_SYNC=1", "-e", "BUUGOO_WS_MAX="+strconv.Itoa(env.WorkspaceMax))
		if info, err := os.Stat(env.Workspace); err == nil && info.IsDir() {
			args = append(args, "-v", env.Workspace+":/ws_src:ro", "-e", "BUUGOO_WS_SRC=/ws_src")
		}
	}
	if env.Input != "" {
		args = append(args, "-v", env.Input+":"+wd+"/input:ro")
	}
	args = append(args, "--workdir", wd)
	args = append(args, d.fontArgs()...)
	if d.lib.ready() {
		// 第三方库只读挂载；通过 PYTHONUSERBASE 让解释器找到它们（因此不能用 -I）。
		args = append(args, "-v", d.lib.dir+":"+containerLib+":ro", "-e", "PYTHONUSERBASE="+containerLib,
			"-e", "BUUGOO_MPL_CACHE="+containerLib+"/"+mplCacheDir)
	}
	args = append(args, d.image, "python3", "-u", "-")
	runCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, d.bin, args...)
	script, boundary := wrapCode(code)
	cmd.Stdin = strings.NewReader(script)
	stdout, stderr := newStdout(env.WorkspaceMax), &limitedBuffer{n: maxOutputBytes}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	res := ExecResult{Stderr: stderr.String()}
	var t runTail
	res.Stdout, t = splitTail(stdout, boundary)
	res.Images, res.ImagesSkipped, res.Workspace, res.WorkspaceErr = t.Images, t.Skipped, t.Workspace, t.WorkspaceErr
	if runCtx.Err() != nil {
		// 结束 docker 客户端进程不会停止容器，需要显式 kill。
		killCtx, kc := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		_ = exec.CommandContext(killCtx, d.bin, "kill", name).Run()
		kc()
		res.TimedOut = ctx.Err() == nil
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		return res, nil
	}
	if err != nil {
		code, err := exitCode(err)
		if err != nil {
			return res, fmt.Errorf("启动 Docker 失败：%w", err)
		}
		// 125 表示 docker 自身出错（例如镜像不存在、无法拉取）。
		if code == 125 {
			return res, fmt.Errorf("Docker 运行失败：%s", strings.TrimSpace(res.Stderr))
		}
		res.ExitCode = code
	}
	return res, nil
}

// ---------- 本机 Python ----------

// Local 用本机解释器运行代码：在临时目录中运行，清空环境变量，限制运行时间与内存（类 Unix 系统）。
// 代码仍然可以访问网络和服务用户能读到的文件，只适合自己使用。
type Local struct {
	python   string
	timeout  time.Duration
	memoryMB int
	lib      *Library
}

// NewLocal 创建本机运行器。
func NewLocal(c config.PythonConfig, log *slog.Logger) *Local {
	if log == nil {
		log = slog.Default()
	}
	return &Local{python: c.Python, timeout: c.Timeout, memoryMB: c.MemoryMB, lib: newLibrary(c, log.With("component", "python"))}
}

// Library 返回第三方库目录。
func (l *Local) Library() *Library { return l.lib }

// Prepare 在后台安装预装的库。
func (l *Local) Prepare(ctx context.Context) { l.lib.ensureBase(ctx, l.Install) }

// Install 用本机解释器的 pip 把库装进库目录（只装预编译包）。
func (l *Local) Install(ctx context.Context, pkgs []string) (string, error) {
	return l.lib.install(ctx, pkgs, func(ctx context.Context, pipArgs []string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, l.python, append([]string{"-m", "pip"}, pipArgs...)...)
		cmd.Env = append(l.env(os.TempDir()), pipEnv()...)
		// 系统自带的 Python 可能禁止 pip 安装（PEP 668）；库装在单独的目录，不会影响系统。
		cmd.Env = append(cmd.Env, "PIP_BREAK_SYSTEM_PACKAGES=1")
		return cmd.CombinedOutput()
	})
}

func (l *Local) env(dir string) []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "TMPDIR=" + dir, "LANG=C.UTF-8", "PYTHONIOENCODING=utf-8",
		"PYTHONUSERBASE=" + l.lib.dir}
	env = append(env, threadEnv("")...)
	if runtime.GOOS == "windows" {
		env = append(env, "SYSTEMROOT="+os.Getenv("SYSTEMROOT"), "TEMP="+dir, "TMP="+dir, "APPDATA="+dir)
	}
	return env
}

// Sandboxed 实现 Runner。
func (l *Local) Sandboxed() bool { return false }

// Available 报告解释器是否存在。
func (l *Local) Available() bool {
	_, err := exec.LookPath(l.python)
	return err == nil
}

// Run 实现 Runner。
func (l *Local) Run(ctx context.Context, code string, env RunEnv) (ExecResult, error) {
	dir, err := os.MkdirTemp("", "buugoo-py-")
	if err != nil {
		return ExecResult{}, err
	}
	defer os.RemoveAll(dir)
	// 本机运行不在沙箱里：直接在这段对话的工作目录中运行，input/ 是指向上传文件的链接。
	wd := dir
	if env.Workspace != "" {
		if err := os.MkdirAll(env.Workspace, 0o755); err != nil {
			return ExecResult{}, err
		}
		wd = env.Workspace
	}
	if env.Input != "" {
		link := filepath.Join(wd, "input")
		_ = os.RemoveAll(link)
		if err := os.Symlink(env.Input, link); err == nil {
			defer os.Remove(link)
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	var cmd *exec.Cmd
	// 不用 -I：需要通过 PYTHONUSERBASE 找到库目录；环境变量已经清空，只保留必要的几项。
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(runCtx, l.python, "-u", "-")
	} else {
		// 用 shell 的 ulimit 限制虚拟内存与 CPU 时间，再 exec 解释器。
		limits := fmt.Sprintf("ulimit -v %d 2>/dev/null; ulimit -t %d 2>/dev/null; exec \"$0\" -u -",
			l.memoryMB*1024, int(l.timeout.Seconds())+1)
		cmd = exec.CommandContext(runCtx, "/bin/sh", "-c", limits, l.python)
	}
	cmd.Dir = wd
	// matplotlib 的字体缓存放在库目录，不用每次运行都重新扫描字体。
	cmd.Env = append(l.env(dir), "MPLCONFIGDIR="+filepath.Join(l.lib.dir, mplCacheDir))
	script, boundary := wrapCode(code)
	cmd.Stdin = strings.NewReader(script)
	stdout, stderr := newStdout(0), &limitedBuffer{n: maxOutputBytes}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 2 * time.Second
	err = cmd.Run()
	res := ExecResult{Stderr: stderr.String()}
	var t runTail
	res.Stdout, t = splitTail(stdout, boundary)
	res.Images, res.ImagesSkipped = t.Images, t.Skipped
	if runCtx.Err() != nil {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		res.TimedOut = true
		return res, nil
	}
	if err != nil {
		code, err := exitCode(err)
		if err != nil {
			return res, fmt.Errorf("启动 Python 失败：%w", err)
		}
		res.ExitCode = code
	}
	return res, nil
}
