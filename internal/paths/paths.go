// Package paths 定义各平台下 Buugoo 的安装目录惯例，以及安装清单 install.json 的读写。
package paths

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// 安装范围。
const (
	ScopeSystem = "system" // 以 root/管理员身份安装到系统目录
	ScopeUser   = "user"   // 安装到当前用户目录
)

const (
	// ConfigFileName 是配置文件名。
	ConfigFileName = "config.yaml"
	// ManifestFileName 是安装清单文件名，与配置文件放在同一目录。
	ManifestFileName = "install.json"
	// ServiceName 是 systemd 服务名。
	ServiceName = "buugoo"
)

// Layout 描述一套安装目录。
type Layout struct {
	Scope     string `json:"scope"`
	BinDir    string `json:"bin_dir"`
	ConfigDir string `json:"config_dir"`
	DataDir   string `json:"data_dir"`
}

// ConfigFile 返回配置文件路径。
func (l Layout) ConfigFile() string { return filepath.Join(l.ConfigDir, ConfigFileName) }

// ManifestFile 返回安装清单路径。
func (l Layout) ManifestFile() string { return filepath.Join(l.ConfigDir, ManifestFileName) }

// Default 返回当前平台在给定范围下的默认目录。
func Default(scope string) (Layout, error) {
	home, err := os.UserHomeDir()
	if err != nil && scope == ScopeUser {
		return Layout{}, fmt.Errorf("获取用户主目录: %w", err)
	}
	return layoutFor(runtime.GOOS, scope, home, os.Getenv)
}

func layoutFor(goos, scope, home string, getenv func(string) string) (Layout, error) {
	l := Layout{Scope: scope}
	switch goos {
	case "windows":
		local := getenv("LOCALAPPDATA")
		roaming := getenv("APPDATA")
		if scope == ScopeSystem {
			pf := getenv("ProgramFiles")
			pd := getenv("ProgramData")
			if pf == "" || pd == "" {
				return Layout{}, errors.New("无法确定 ProgramFiles/ProgramData 目录")
			}
			l.BinDir = filepath.Join(pf, "buugoo")
			l.ConfigDir = filepath.Join(pd, "buugoo")
			l.DataDir = filepath.Join(pd, "buugoo", "data")
			return l, nil
		}
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		if roaming == "" {
			roaming = filepath.Join(home, "AppData", "Roaming")
		}
		l.BinDir = filepath.Join(local, "Programs", "buugoo")
		l.ConfigDir = filepath.Join(roaming, "buugoo")
		l.DataDir = filepath.Join(local, "buugoo")
	case "darwin":
		if scope == ScopeSystem {
			l.BinDir = "/usr/local/bin"
			l.ConfigDir = "/usr/local/etc/buugoo"
			l.DataDir = "/usr/local/var/buugoo"
			return l, nil
		}
		fillXDG(&l, home, getenv)
	default:
		if scope == ScopeSystem {
			l.BinDir = "/usr/local/bin"
			l.ConfigDir = "/etc/buugoo"
			l.DataDir = "/var/lib/buugoo"
			return l, nil
		}
		fillXDG(&l, home, getenv)
	}
	if scope != ScopeSystem && scope != ScopeUser {
		return Layout{}, fmt.Errorf("未知安装范围 %q", scope)
	}
	return l, nil
}

func fillXDG(l *Layout, home string, getenv func(string) string) {
	cfg := getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	data := getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	l.BinDir = filepath.Join(home, ".local", "bin")
	l.ConfigDir = filepath.Join(cfg, "buugoo")
	l.DataDir = filepath.Join(data, "buugoo")
}

// Manifest 是安装脚本写入的安装清单，update / uninstall 依据它决定操作对象。
type Manifest struct {
	Layout
	Binary      string `json:"binary"`
	Service     bool   `json:"service"`      // 是否注册了 systemd 服务
	ServiceUser string `json:"service_user"` // 服务运行用户
	Version     string `json:"version"`
	Mirror      string `json:"mirror,omitempty"`
	InstalledAt string `json:"installed_at"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	// PrevVersion 是上一次更新前的版本（对应二进制 <binary>.prev），供 buugoo update --rollback 使用。
	PrevVersion string `json:"prev_version,omitempty"`
	// LastBackup 是最近一次更新前的数据库备份路径。
	LastBackup string `json:"last_backup,omitempty"`

	path string
}

// Path 返回清单文件路径。
func (m *Manifest) Path() string { return m.path }

// ReadManifest 读取指定路径的安装清单。
func ReadManifest(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("解析安装清单 %s: %w", path, err)
	}
	m.path = path
	return &m, nil
}

// Save 写回安装清单（先写临时文件再原子替换）。
func (m *Manifest) Save() error {
	if m.path == "" {
		m.path = m.ManifestFile()
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("写入安装清单: %w", err)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		return fmt.Errorf("替换安装清单: %w", err)
	}
	return nil
}

// ErrNoManifest 表示找不到安装清单（例如通过 go install 或手动放置的二进制）。
var ErrNoManifest = errors.New("未找到安装清单 install.json")

// FindManifest 按以下顺序查找安装清单：
// BUUGOO_MANIFEST 环境变量 → 配置文件所在目录 → 用户目录 → 系统目录。
func FindManifest(configPath string) (*Manifest, error) {
	var candidates []string
	if p := os.Getenv("BUUGOO_MANIFEST"); p != "" {
		candidates = append(candidates, p)
	}
	if configPath != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(configPath), ManifestFileName))
	}
	for _, scope := range []string{ScopeUser, ScopeSystem} {
		if l, err := Default(scope); err == nil {
			candidates = append(candidates, l.ManifestFile())
		}
	}
	for _, c := range candidates {
		m, err := ReadManifest(c)
		if err == nil {
			return m, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, ErrNoManifest
}

// FindConfig 按以下顺序查找配置文件，找不到时返回空字符串：
// 显式指定 → BUUGOO_CONFIG → 当前目录 config.yaml → 用户配置目录 → 系统配置目录。
func FindConfig(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("配置文件 %s: %w", explicit, err)
		}
		return explicit, nil
	}
	if p := os.Getenv("BUUGOO_CONFIG"); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("BUUGOO_CONFIG 指向的配置文件 %s: %w", p, err)
		}
		return p, nil
	}
	candidates := []string{ConfigFileName}
	for _, scope := range []string{ScopeUser, ScopeSystem} {
		if l, err := Default(scope); err == nil {
			candidates = append(candidates, l.ConfigFile())
		}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", nil
}

// IsPrivileged 报告当前进程是否具有 root 权限（Windows 下总是返回 false，由调用方另行判断）。
func IsPrivileged() bool {
	return runtime.GOOS != "windows" && os.Geteuid() == 0
}
