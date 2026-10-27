package httpapi

import (
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/buildinfo"
	"github.com/HugehardJiang/buugoo/internal/logx"
)

// 诊断信息：网页上「复制诊断信息」用。普通用户只能看到和自己有关的最近警告和错误，
// 管理后台能看到全部；日志里像密钥的部分已经打码。

var startedAt = time.Now()

func (s *server) basicInfo() map[string]any {
	return map[string]any{
		"version":   buildinfo.Version,
		"mode":      s.Config.Mode,
		"time":      time.Now().In(s.Config.Location()).Format("2006-01-02 15:04:05 MST"),
		"uptime":    time.Since(startedAt).Round(time.Second).String(),
		"platform":  runtime.GOOS + "/" + runtime.GOARCH,
		"go":        runtime.Version(),
		"database":  s.Config.Database.Driver,
		"python":    s.Config.Tools.Python.Runner,
		"search":    s.Config.Tools.Search.Provider,
		"memory_on": s.Config.Memory.Write.Enabled,
	}
}

// myDiagnostics 处理 GET /api/v1/diagnostics：服务器信息和这个账号最近的警告、错误。
func (s *server) myDiagnostics(c echo.Context) error {
	u := currentUser(c)
	id := fmt.Sprint(u.ID)
	recent := logx.Recent.List(30, func(e logx.Entry) bool { return e.Attrs["user_id"] == id || e.Attrs["user"] == id })
	info := s.basicInfo()
	info["user_id"] = u.ID
	return c.JSON(http.StatusOK, map[string]any{"server": info, "recent": recent})
}

// consoleDiagnostics 处理 GET /api/v1/console/diagnostics：服务器信息和全部最近的警告、错误。
func (s *server) consoleDiagnostics(c echo.Context) error {
	info := s.basicInfo()
	info["config"] = s.Config.Path()
	info["data_dir"] = s.Config.DataDir
	info["restart"] = s.restartHint()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	info["memory_mb"] = mem.Alloc >> 20
	info["goroutines"] = runtime.NumGoroutine()
	if s.Channels != nil {
		info["channels"] = s.Channels.Enabled()
	}
	return c.JSON(http.StatusOK, map[string]any{"server": info, "recent": logx.Recent.List(200, nil)})
}
