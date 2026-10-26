//go:build !windows

package mcp

import (
	"os/exec"
	"syscall"
)

// setProcessGroup 让命令在单独的进程组里运行，结束时连同它启动的子进程（如 npx 启动的 node）一起结束。
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
