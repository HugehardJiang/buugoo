package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/settings"
)

// 网页管理后台：用配置文件中的 admin.password 登录，查看与修改系统设置。
// 修改写入 <data_dir>/settings.yaml，需要管理员自己重启服务后生效。

const consoleTTL = 8 * time.Hour

type consoleSessions struct {
	mu     sync.Mutex
	tokens map[string]time.Time
}

func (cs *consoleSessions) issue() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	tok := hex.EncodeToString(b[:])
	cs.mu.Lock()
	defer cs.mu.Unlock()
	now := time.Now()
	for t, exp := range cs.tokens {
		if now.After(exp) {
			delete(cs.tokens, t)
		}
	}
	cs.tokens[tok] = now.Add(consoleTTL)
	return tok
}

func (cs *consoleSessions) valid(tok string) bool {
	if tok == "" {
		return false
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	exp, ok := cs.tokens[tok]
	if !ok || time.Now().After(exp) {
		delete(cs.tokens, tok)
		return false
	}
	return true
}

func (cs *consoleSessions) revoke(tok string) {
	cs.mu.Lock()
	delete(cs.tokens, tok)
	cs.mu.Unlock()
}

func (s *server) defaultAdminPassword() bool {
	return s.Config.Admin.Password == config.DefaultAdminPassword
}

func (s *server) consoleLogin(c echo.Context) error {
	var req struct {
		Password string `json:"password"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	want := s.Config.Admin.Password
	if want == "" || subtle.ConstantTimeCompare([]byte(req.Password), []byte(want)) != 1 {
		s.log.Warn("管理后台登录失败", "ip", c.RealIP())
		return &apiError{Status: http.StatusUnauthorized, Code: "invalid_credentials", Message: "管理员密码不正确"}
	}
	s.log.Info("管理后台登录", "ip", c.RealIP())
	return c.JSON(http.StatusOK, map[string]any{"token": s.console.issue(), "default_password": s.defaultAdminPassword()})
}

func (s *server) requireConsole(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if !s.console.valid(c.Request().Header.Get("X-Console-Token")) {
			return &apiError{Status: http.StatusUnauthorized, Code: "console_unauthorized", Message: "请先输入管理员密码"}
		}
		return next(c)
	}
}

func (s *server) consoleLogout(c echo.Context) error {
	s.console.revoke(c.Request().Header.Get("X-Console-Token"))
	return c.NoContent(http.StatusNoContent)
}

// restartHint 根据部署方式给出重启命令。
func (s *server) restartHint() string {
	p := s.Config.Path()
	switch {
	case strings.HasPrefix(p, "/etc/buugoo/"):
		return "sudo systemctl restart buugoo"
	case fileExists("/.dockerenv"):
		return "docker compose restart（或 docker restart 容器名）"
	default:
		return "停止正在运行的 buugoo serve，然后重新启动"
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (s *server) consoleSettings(c echo.Context) error {
	fields, err := settings.Fields(s.Config)
	if err != nil {
		return err
	}
	pending := 0
	for _, f := range fields {
		if f.Pending {
			pending++
		}
	}
	return c.JSON(http.StatusOK, map[string]any{
		"fields":           fields,
		"groups":           settings.GroupLabels,
		"pending":          pending,
		"settings_path":    s.Config.SettingsPath(),
		"config_path":      s.Config.Path(),
		"default_password": s.defaultAdminPassword(),
		"restart_hint":     s.restartHint(),
	})
}

func (s *server) consoleSave(c echo.Context) error {
	var req struct {
		Changes map[string]any `json:"changes"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	changed, err := settings.Save(s.Config, req.Changes)
	var ve *settings.ValidationError
	if errors.As(err, &ve) {
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_setting", Message: ve.Error()}
	}
	if err != nil {
		return err
	}
	// 只记录改了哪些项，不记录值（可能包含密钥）。
	s.log.Warn("管理后台修改了设置，重启后生效", "keys", strings.Join(changed, ","), "ip", c.RealIP())
	return c.JSON(http.StatusOK, map[string]any{"changed": changed, "restart_hint": s.restartHint()})
}

func (s *server) consoleDiscard(c echo.Context) error {
	var req struct {
		Keys []string `json:"keys"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := settings.Discard(s.Config, req.Keys); err != nil {
		return err
	}
	s.log.Warn("管理后台恢复了设置，重启后生效", "keys", strings.Join(req.Keys, ","), "ip", c.RealIP())
	return c.NoContent(http.StatusNoContent)
}
