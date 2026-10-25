package selfupdate

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/paths"
	"github.com/HugehardJiang/buugoo/internal/service"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

// Options 是更新参数。
type Options struct {
	Version   string // 指定版本，为空表示最新版本
	CheckOnly bool   // 只检查不更新
	Force     bool   // 即使版本相同也重新安装
	RestoreDB bool   // 回滚时恢复数据库备份（不询问）
}

// Updater 执行更新与回滚。
type Updater struct {
	Source   Source
	Current  string           // 当前版本
	Binary   string           // 要替换的二进制路径
	Manifest *paths.Manifest  // 可能为空（非脚本安装）
	Config   *config.Config   // 用于备份与健康检查
	Service  *service.Manager // 为空表示没有注册服务
	Out      io.Writer
	// Confirm 询问用户，返回是否同意；非交互模式下由调用方决定默认行为。
	Confirm func(msg string) bool
	// ConfirmRestore 询问回滚时是否恢复数据库（会丢失更新后的数据），-y 不会自动同意。
	ConfirmRestore func(msg string) bool

	healthTimeout time.Duration
}

func (u *Updater) logf(format string, args ...any) { fmt.Fprintf(u.Out, format+"\n", args...) }

func (u *Updater) prevPath() string { return u.Binary + ".prev" }

// ErrInContainer 表示在容器中运行，应使用镜像更新。
var ErrInContainer = errors.New("Docker 部署请使用 docker compose pull && docker compose up -d 更新")

// dockerImage 报告是否运行在官方 Docker 镜像中：在容器内且没有安装清单。
// 在容器里用安装脚本安装的（有清单）仍可自更新。
func (u *Updater) dockerImage() bool {
	if u.Manifest != nil {
		return false
	}
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

// Check 返回目标版本以及是否需要更新。
func (u *Updater) Check(ctx context.Context, opt Options) (string, bool, error) {
	var tag string
	var err error
	if opt.Version != "" {
		tag, err = Normalize(opt.Version)
	} else {
		tag, err = u.Source.Latest(ctx)
	}
	if err != nil {
		return "", false, err
	}
	return tag, opt.Force || Newer(tag, u.Current), nil
}

// Update 执行更新：下载并校验 → 备份数据库 → 停止服务 → 替换二进制 → 迁移 → 启动服务并检查，
// 任一步失败自动回滚。
func (u *Updater) Update(ctx context.Context, opt Options) error {
	if u.dockerImage() {
		return ErrInContainer
	}
	tag, need, err := u.Check(ctx, opt)
	if err != nil {
		return err
	}
	if !need {
		u.logf("当前已是最新版本 %s", u.Current)
		return nil
	}
	if opt.CheckOnly {
		u.logf("有新版本可用：%s（当前 %s）。运行 buugoo update 进行更新。", tag, u.Current)
		return nil
	}
	if err := checkWritable(filepath.Dir(u.Binary)); err != nil {
		return err
	}
	u.logf("准备从 %s 更新到 %s", u.Current, tag)

	tmp, err := os.MkdirTemp("", "buugoo-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	newBin, err := u.fetch(ctx, tag, tmp)
	if err != nil {
		return err
	}

	backup, err := BackupDatabase(ctx, u.Config, strings.TrimPrefix(u.Current, "v"), u.Confirm)
	if err != nil {
		return err
	}
	if backup.Path != "" {
		u.logf("已备份数据库：%s", backup.Path)
	}
	u.chownData()

	wasActive := u.Service != nil && u.Service.Active(ctx)
	if wasActive {
		u.logf("停止服务 %s", u.Service.Name)
		if err := u.Service.Stop(ctx); err != nil {
			return fmt.Errorf("停止服务: %w", err)
		}
	}

	if err := u.replace(newBin); err != nil {
		u.startBack(ctx, wasActive)
		return fmt.Errorf("替换二进制: %w", err)
	}
	u.logf("已替换 %s（旧版本保留为 %s）", u.Binary, u.prevPath())

	if err := u.migrate(ctx); err != nil {
		return u.rollbackAfterFailure(ctx, err, backup, wasActive)
	}
	u.chownData()
	if wasActive {
		if err := u.Service.Start(ctx); err != nil {
			return u.rollbackAfterFailure(ctx, fmt.Errorf("启动服务: %w", err), backup, wasActive)
		}
		if err := u.waitHealthy(ctx, tag); err != nil {
			return u.rollbackAfterFailure(ctx, err, backup, wasActive)
		}
		u.logf("服务已启动并通过健康检查")
	}

	_ = os.Remove(u.prevPath() + ".bak")
	if u.Manifest != nil {
		u.Manifest.PrevVersion = u.Current
		u.Manifest.Version = tag
		u.Manifest.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if backup.Path != "" {
			u.Manifest.LastBackup = backup.Path
		}
		if err := u.Manifest.Save(); err != nil {
			u.logf("警告：更新安装清单失败：%v", err)
		}
	}
	u.logf("更新完成：%s → %s", u.Current, tag)
	if !wasActive && u.Service == nil {
		u.logf("如果 buugoo serve 正在运行，请重启它以使用新版本。")
	}
	return nil
}

// fetch 下载、校验并解压新版本，返回解压出的二进制路径。
func (u *Updater) fetch(ctx context.Context, tag, dir string) (string, error) {
	asset := CurrentAsset(tag)
	archive := filepath.Join(dir, asset)
	u.logf("下载 %s", u.Source.URL(tag, asset))
	if err := u.Source.download(ctx, u.Source.URL(tag, asset), archive); err != nil {
		return "", err
	}
	sumsPath := filepath.Join(dir, ChecksumsFile)
	if err := u.Source.download(ctx, u.Source.URL(tag, ChecksumsFile), sumsPath); err != nil {
		return "", err
	}
	f, err := os.Open(sumsPath)
	if err != nil {
		return "", err
	}
	sums, err := ParseChecksums(f)
	f.Close()
	if err != nil {
		return "", err
	}
	want, ok := sums[asset]
	if !ok {
		return "", fmt.Errorf("%w：校验文件中没有 %s", ErrChecksumMismatch, asset)
	}
	if err := VerifyFile(archive, want); err != nil {
		return "", err
	}
	u.logf("SHA256 校验通过")
	name := "buugoo"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(dir, name)
	if err := ExtractBinary(archive, bin); err != nil {
		return "", err
	}
	// 确认新版本能在本机运行，且版本号正确。
	out, err := exec.CommandContext(ctx, bin, "version", "--short").Output()
	if err != nil {
		return "", fmt.Errorf("新版本无法运行: %w", err)
	}
	if got := strings.TrimSpace(string(out)); got != tag {
		return "", fmt.Errorf("新版本的版本号为 %q，与 %s 不符", got, tag)
	}
	return bin, nil
}

// replace 把当前二进制保留为 .prev，再原子地放入新版本。
// Windows 允许重命名正在运行的 exe，但不允许覆盖，因此先改名再写入。
func (u *Updater) replace(newBin string) error {
	staged := u.Binary + ".new"
	if err := copyFile(newBin, staged); err != nil {
		return err
	}
	if err := os.Chmod(staged, 0o755); err != nil {
		return err
	}
	// 已有的 .prev（更早的版本）先暂存，更新失败回滚时还原，保证 --rollback 仍然可用。
	_ = os.Remove(u.prevPath() + ".bak")
	if err := os.Rename(u.prevPath(), u.prevPath()+".bak"); err != nil && !errors.Is(err, os.ErrNotExist) {
		os.Remove(staged)
		return err
	}
	if err := os.Rename(u.Binary, u.prevPath()); err != nil {
		os.Remove(staged)
		_ = os.Rename(u.prevPath()+".bak", u.prevPath())
		return err
	}
	if err := os.Rename(staged, u.Binary); err != nil {
		// 尽量恢复原状。
		_ = os.Rename(u.prevPath(), u.Binary)
		_ = os.Rename(u.prevPath()+".bak", u.prevPath())
		return err
	}
	return nil
}

func (u *Updater) migrate(ctx context.Context) error {
	args := []string{"migrate"}
	if p := u.Config.Path(); p != "" {
		args = append(args, "--config", p)
	}
	u.logf("执行数据库迁移")
	cmd := exec.CommandContext(ctx, u.Binary, args...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("数据库迁移失败: %w: %s", err, strings.TrimSpace(string(b)))
	}
	return nil
}

// rollbackAfterFailure 在更新失败后恢复旧二进制与数据库备份，并重新启动服务。
func (u *Updater) rollbackAfterFailure(ctx context.Context, cause error, b *Backup, wasActive bool) error {
	u.logf("更新失败：%v", cause)
	u.logf("正在自动回滚到 %s…", u.Current)
	var errs []error
	if u.Service != nil && u.Service.Active(ctx) {
		if err := u.Service.Stop(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if err := os.Rename(u.prevPath(), u.Binary); err != nil {
		errs = append(errs, fmt.Errorf("恢复旧二进制: %w", err))
	} else if err := os.Rename(u.prevPath()+".bak", u.prevPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("恢复更早的版本: %w", err))
	}
	if b != nil && b.Kind == BackupSQLite {
		if err := RestoreSQLite(b); err != nil {
			errs = append(errs, err)
		} else {
			u.logf("已从 %s 恢复数据库", b.Path)
		}
	} else if b != nil && b.Kind == BackupMySQL {
		u.logf("MySQL 数据库未自动恢复。如迁移已部分执行，请用备份恢复：mysql <库名> < %s", b.Path)
	}
	u.chownData()
	u.startBack(ctx, wasActive)
	if len(errs) > 0 {
		return fmt.Errorf("更新失败且回滚出错（%v）: %w", errors.Join(errs...), cause)
	}
	return fmt.Errorf("更新失败，已回滚到 %s: %w", u.Current, cause)
}

func (u *Updater) startBack(ctx context.Context, wasActive bool) {
	if !wasActive {
		return
	}
	if err := u.Service.Start(ctx); err != nil {
		u.logf("警告：重新启动服务失败：%v", err)
	}
}

// Rollback 切换回上一个版本（buugoo update --rollback）。当前版本会保留为 .prev，可以再切换回来。
func (u *Updater) Rollback(ctx context.Context, opt Options) error {
	if u.dockerImage() {
		return ErrInContainer
	}
	prev := u.prevPath()
	if _, err := os.Stat(prev); err != nil {
		return fmt.Errorf("没有可回滚的旧版本（%s 不存在）", prev)
	}
	if err := checkWritable(filepath.Dir(u.Binary)); err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, prev, "version", "--short").Output()
	if err != nil {
		return fmt.Errorf("旧版本无法运行: %w", err)
	}
	prevVersion := strings.TrimSpace(string(out))

	var restore *Backup
	if u.Manifest != nil && u.Manifest.LastBackup != "" && u.Config.Database.Driver == config.DriverSQLite {
		if _, err := os.Stat(u.Manifest.LastBackup); err == nil {
			msg := fmt.Sprintf("是否把数据库恢复为更新前的备份 %s？（更新之后产生的新数据会丢失）", u.Manifest.LastBackup)
			if opt.RestoreDB || (u.ConfirmRestore != nil && u.ConfirmRestore(msg)) {
				restore = &Backup{Kind: BackupSQLite, Path: u.Manifest.LastBackup, DBPath: gormstore.SQLitePath(u.Config.Database.DSN)}
			}
		}
	}

	wasActive := u.Service != nil && u.Service.Active(ctx)
	if wasActive {
		if err := u.Service.Stop(ctx); err != nil {
			return fmt.Errorf("停止服务: %w", err)
		}
	}
	swap := u.Binary + ".swap"
	if err := os.Rename(u.Binary, swap); err != nil {
		u.startBack(ctx, wasActive)
		return err
	}
	if err := os.Rename(prev, u.Binary); err != nil {
		_ = os.Rename(swap, u.Binary)
		u.startBack(ctx, wasActive)
		return err
	}
	if err := os.Rename(swap, prev); err != nil {
		u.logf("警告：保留当前版本为 %s 失败：%v", prev, err)
	}
	u.logf("已切换到 %s（%s 保留为 %s）", prevVersion, u.Current, prev)
	if restore != nil {
		if err := RestoreSQLite(restore); err != nil {
			return err
		}
		u.logf("已从 %s 恢复数据库", restore.Path)
	}
	u.chownData()
	if wasActive {
		if err := u.Service.Start(ctx); err != nil {
			return fmt.Errorf("启动服务: %w", err)
		}
		if err := u.waitHealthy(ctx, prevVersion); err != nil {
			return err
		}
		u.logf("服务已启动并通过健康检查")
	}
	if u.Manifest != nil {
		u.Manifest.PrevVersion, u.Manifest.Version = u.Current, prevVersion
		u.Manifest.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := u.Manifest.Save(); err != nil {
			u.logf("警告：更新安装清单失败：%v", err)
		}
	}
	u.logf("回滚完成：%s → %s", u.Current, prevVersion)
	return nil
}

// waitHealthy 轮询 /healthz，直到返回期望的版本。
func (u *Updater) waitHealthy(ctx context.Context, want string) error {
	url := healthURL(u.Config.Server.Listen)
	timeout := u.healthTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second}
	var last string
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			var body struct {
				Status  string `json:"status"`
				Version string `json:"version"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && body.Version == want {
				return nil
			}
			last = fmt.Sprintf("HTTP %d，版本 %q", resp.StatusCode, body.Version)
		} else {
			last = err.Error()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("健康检查失败（%s）：%s", url, last)
}

func healthURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://127.0.0.1:8080/healthz"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

// chownData 以 root 身份运行时，把数据目录交还给服务用户（备份、迁移、恢复可能产生 root 所有的文件）。
func (u *Updater) chownData() {
	if runtime.GOOS == "windows" || os.Geteuid() != 0 || u.Manifest == nil || u.Manifest.ServiceUser == "" {
		return
	}
	usr, err := user.Lookup(u.Manifest.ServiceUser)
	if err != nil {
		return
	}
	uid, _ := strconv.Atoi(usr.Uid)
	gid, _ := strconv.Atoi(usr.Gid)
	_ = filepath.Walk(u.Config.DataDir, func(p string, _ os.FileInfo, err error) error {
		if err == nil {
			_ = os.Lchown(p, uid, gid)
		}
		return nil
	})
}

func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".buugoo-write-test-")
	if err != nil {
		if runtime.GOOS != "windows" {
			return fmt.Errorf("没有权限写入 %s，请使用 sudo 运行", dir)
		}
		return fmt.Errorf("没有权限写入 %s", dir)
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// TerminalConfirm 返回一个从终端读取 y/N 的确认函数；assumeYes 为 true 时总是同意。
func TerminalConfirm(in io.Reader, out io.Writer, assumeYes bool) func(string) bool {
	r := bufio.NewReader(in)
	return func(msg string) bool {
		if assumeYes {
			fmt.Fprintf(out, "%s：已通过 -y 确认\n", msg)
			return true
		}
		fmt.Fprintf(out, "%s [y/N] ", msg)
		line, _ := r.ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes", "是":
			return true
		}
		return false
	}
}
