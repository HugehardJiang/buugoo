package httpapi

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/dbmove"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

// 管理后台 → 数据库：把 SQLite 一键迁移到 MySQL。

// maintenance 在迁移数据库期间拦下所有会改数据的请求（管理后台查看迁移进度的除外）。
func (s *server) maintenance(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if s.DBMove == nil || !s.DBMove.Paused() {
			return next(c)
		}
		switch c.Request().Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return next(c)
		}
		if p := c.Request().URL.Path; p == "/api/v1/console/login" || strings.HasPrefix(p, "/api/v1/console/database") {
			return next(c)
		}
		return dbmove.ErrMaintenance
	}
}

func (s *server) consoleDatabase(c echo.Context) error {
	info := map[string]any{
		"driver":     s.Config.Database.Driver,
		"status":     s.DBMove.Status(),
		"supervised": s.Supervised,
		"restart":    s.restartHint(),
	}
	if err := s.DBMove.Check(); err != nil {
		info["blocked"] = err.Error()
	}
	if p := gormstore.SQLitePath(s.Config.Database.DSN); p != "" {
		info["sqlite_path"] = p
		if st, err := os.Stat(p); err == nil {
			info["sqlite_bytes"] = st.Size()
		}
	}
	return c.JSON(http.StatusOK, info)
}

func (s *server) consoleDatabaseTest(c echo.Context) error {
	var t dbmove.Target
	if err := bind(c, &t); err != nil {
		return err
	}
	res, err := s.DBMove.Test(c.Request().Context(), t)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

func (s *server) consoleDatabaseMigrate(c echo.Context) error {
	var t dbmove.Target
	if err := bind(c, &t); err != nil {
		return err
	}
	// 迁移前再测一次，免得半路才发现连不上。
	if _, err := s.DBMove.Test(c.Request().Context(), t); err != nil {
		return err
	}
	onDone := func() {
		if s.Supervised && s.Restart != nil {
			// 留几秒让网页拿到「完成」的状态，再重启。
			time.AfterFunc(3*time.Second, s.Restart)
		}
	}
	if err := s.DBMove.Start(t, onDone); err != nil {
		return err
	}
	s.log.Warn("管理后台开始把数据库迁移到 MySQL", "ip", c.RealIP())
	return c.JSON(http.StatusAccepted, s.DBMove.Status())
}
