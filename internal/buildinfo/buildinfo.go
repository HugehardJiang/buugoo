// Package buildinfo 保存通过 ldflags 注入的构建信息。
package buildinfo

import (
	"fmt"
	"runtime"
)

// 以下变量在构建时通过 -ldflags "-X" 注入。
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String 返回人类可读的版本信息。
func String() string {
	return fmt.Sprintf("buugoo %s (commit %s, built %s, %s/%s, %s)",
		Version, Commit, Date, runtime.GOOS, runtime.GOARCH, runtime.Version())
}

// IsDev 表示当前是否为未正式发布的开发构建。
func IsDev() bool { return Version == "dev" || Version == "" }
