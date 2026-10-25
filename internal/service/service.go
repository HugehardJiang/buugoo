// Package service 管理 Linux 上的 systemd 服务（启动、停止、移除）。其他平台返回 ErrUnsupported。
package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrUnsupported 表示当前平台不支持服务管理。
var ErrUnsupported = errors.New("当前系统不支持 systemd 服务管理")

// UnitDir 是系统级 unit 文件目录。
const UnitDir = "/etc/systemd/system"

// Manager 通过 systemctl 管理服务。
type Manager struct {
	Name string
	// run 便于测试替换。
	run func(ctx context.Context, args ...string) (string, error)
}

// New 创建服务管理器。
func New(name string) *Manager {
	return &Manager{Name: name, run: systemctl}
}

func systemctl(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

// Available 报告当前系统是否可用 systemd。
func Available() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	_, err := os.Stat("/run/systemd/system")
	return err == nil
}

func (m *Manager) unit() string { return m.Name + ".service" }

// UnitPath 返回 unit 文件路径。
func (m *Manager) UnitPath() string { return filepath.Join(UnitDir, m.unit()) }

// Installed 报告 unit 文件是否存在。
func (m *Manager) Installed() bool {
	_, err := os.Stat(m.UnitPath())
	return err == nil
}

// Active 报告服务是否正在运行。
func (m *Manager) Active(ctx context.Context) bool {
	if !Available() {
		return false
	}
	out, _ := m.run(ctx, "is-active", m.unit())
	return out == "active"
}

func (m *Manager) do(ctx context.Context, args ...string) error {
	if !Available() {
		return ErrUnsupported
	}
	out, err := m.run(ctx, args...)
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return nil
}

// Start 启动服务。先清除失败计数，避免更新、回滚连续重启触发 systemd 的启动频率限制。
func (m *Manager) Start(ctx context.Context) error {
	if Available() {
		_, _ = m.run(ctx, "reset-failed", m.unit())
	}
	return m.do(ctx, "start", m.unit())
}

// Stop 停止服务。
func (m *Manager) Stop(ctx context.Context) error { return m.do(ctx, "stop", m.unit()) }

// Remove 停止、禁用并删除 unit 文件。
func (m *Manager) Remove(ctx context.Context) error {
	if !Available() {
		return ErrUnsupported
	}
	var errs []error
	if _, err := m.run(ctx, "disable", "--now", m.unit()); err != nil && m.Active(ctx) {
		errs = append(errs, fmt.Errorf("停止服务失败: %w", err))
	}
	if err := os.Remove(m.UnitPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("删除 %s: %w", m.UnitPath(), err))
	}
	if _, err := m.run(ctx, "daemon-reload"); err != nil {
		errs = append(errs, fmt.Errorf("systemctl daemon-reload: %w", err))
	}
	return errors.Join(errs...)
}
