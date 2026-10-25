package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/HugehardJiang/buugoo/internal/paths"
	"github.com/HugehardJiang/buugoo/internal/selfupdate"
	"github.com/HugehardJiang/buugoo/internal/service"
)

func newUninstallCmd(g *globalOpts) *cobra.Command {
	var purge, yes bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "卸载 buugoo（默认保留配置和数据）",
		Long: `卸载 buugoo：停止并移除 systemd 服务、删除二进制。

默认保留配置文件和数据（数据库、备份），并打印它们的位置，重新安装后可以继续使用。
--purge 会连同配置和数据一起删除，执行前必须确认；非交互模式下需要同时加 -y。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runUninstall(cmd, g, purge, yes) },
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "同时删除配置和数据（不可恢复）")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "非交互模式，自动确认")
	return cmd
}

func runUninstall(cmd *cobra.Command, g *globalOpts, purge, yes bool) error {
	out := cmd.OutOrStdout()
	m, err := paths.FindManifest(g.configPath)
	if errors.Is(err, paths.ErrNoManifest) {
		return errors.New("未找到安装清单 install.json：这个 buugoo 不是通过安装脚本安装的，请直接删除二进制文件")
	}
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" && m.Scope == paths.ScopeSystem && !paths.IsPrivileged() {
		return errors.New("系统级安装需要 root 权限，请使用 sudo buugoo uninstall")
	}

	if purge {
		msg := fmt.Sprintf("将永久删除配置目录 %s 和数据目录 %s（包括所有聊天记录、记忆和备份），无法恢复。确定继续吗", m.ConfigDir, m.DataDir)
		if !yes && !isTerminal(os.Stdin) {
			return errors.New("非交互模式下 --purge 需要同时加 -y")
		}
		if !selfupdate.TerminalConfirm(cmd.InOrStdin(), cmd.ErrOrStderr(), yes)(msg) {
			return errors.New("已取消")
		}
	}

	var errs []error
	var done []string
	if m.Service {
		svc := service.New(paths.ServiceName)
		if err := svc.Remove(cmd.Context()); err != nil && !errors.Is(err, service.ErrUnsupported) {
			errs = append(errs, err)
		} else {
			done = append(done, "已停止并移除 systemd 服务 "+paths.ServiceName)
		}
	}
	for _, p := range []string{m.Binary + ".prev", m.Binary + ".prev.bak", m.Binary + ".new", m.Binary + ".swap"} {
		_ = os.Remove(p)
	}
	if err := removeSelf(m.Binary); err != nil {
		errs = append(errs, fmt.Errorf("删除二进制 %s: %w", m.Binary, err))
	} else {
		done = append(done, "已删除二进制 "+m.Binary)
	}

	if purge {
		for _, dir := range uniqueDirs(m.ConfigDir, m.DataDir) {
			if err := os.RemoveAll(dir); err != nil {
				errs = append(errs, fmt.Errorf("删除 %s: %w", dir, err))
			} else {
				done = append(done, "已删除 "+dir)
			}
		}
		if m.ServiceUser != "" && runtime.GOOS == "linux" {
			if _, err := exec.LookPath("userdel"); err == nil {
				if b, err := exec.Command("userdel", m.ServiceUser).CombinedOutput(); err != nil {
					errs = append(errs, fmt.Errorf("删除系统用户 %s: %v: %s", m.ServiceUser, err, strings.TrimSpace(string(b))))
				} else {
					done = append(done, "已删除系统用户 "+m.ServiceUser)
				}
			}
		}
	} else if err := os.Remove(m.Path()); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("删除安装清单: %w", err))
	}

	fmt.Fprintln(out, "卸载结果：")
	for _, d := range done {
		fmt.Fprintln(out, "  ✓ "+d)
	}
	for _, e := range errs {
		fmt.Fprintln(out, "  ✗ "+e.Error())
	}
	if !purge {
		fmt.Fprintf(out, "已保留：\n  配置 %s\n  数据 %s\n重新安装后会继续使用这些配置和数据；如需彻底删除，请手动删除以上目录或重新安装后执行 buugoo uninstall --purge。\n",
			m.ConfigFile(), m.DataDir)
	}
	return errors.Join(errs...)
}

// removeSelf 删除二进制。Windows 不能删除正在运行的 exe，改为重命名后在下次重启时不再使用。
func removeSelf(bin string) error {
	err := os.Remove(bin)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if runtime.GOOS == "windows" {
		trash := filepath.Join(os.TempDir(), fmt.Sprintf("buugoo-uninstalled-%d.exe", os.Getpid()))
		return os.Rename(bin, trash)
	}
	return err
}

func uniqueDirs(dirs ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		if d == "" || d == "/" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}
