package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/notice"
	"github.com/HugehardJiang/buugoo/internal/proactive"
)

// 强制弹窗通知：管理员在后台发布，用户打开网页时弹出，看够秒数才能关。

func (s *server) routeNotices(priv, console *echo.Group) {
	if s.Notices == nil {
		return
	}
	priv.GET("/notices", s.noticeHistory)
	priv.GET("/notices/pending", s.pendingNotices)
	priv.POST("/notices/:id/read", s.readNotice)
	m := console.Group("", s.requireStrongConsole)
	m.GET("/notices", s.consoleNotices)
	m.POST("/notices", s.consoleCreateNotice)
	m.PUT("/notices/:id", s.consoleUpdateNotice)
	m.DELETE("/notices/:id", s.consoleDeleteNotice)
	m.GET("/proactive/observed", s.consoleObserved)
}

func (s *server) pendingNotices(c echo.Context) error {
	// 管理员临时登录排查问题时不弹，也不算用户看过。
	if impersonated(c) {
		return c.JSON(http.StatusOK, map[string]any{"notices": []notice.Item{}})
	}
	list, err := s.Notices.Pending(c.Request().Context(), currentUser(c).ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"notices": list})
}

func (s *server) readNotice(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if impersonated(c) {
		return c.NoContent(http.StatusNoContent)
	}
	if err := s.Notices.MarkRead(c.Request().Context(), currentUser(c).ID, id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *server) noticeHistory(c echo.Context) error {
	list, err := s.Notices.History(c.Request().Context(), currentUser(c).ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"notices": list})
}

func (s *server) consoleNotices(c echo.Context) error {
	list, err := s.Notices.AdminList(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"notices": list})
}

func (s *server) consoleCreateNotice(c echo.Context) error {
	var in notice.Input
	if err := bind(c, &in); err != nil {
		return err
	}
	n, err := s.Notices.Create(c.Request().Context(), in)
	if err != nil {
		return err
	}
	s.audit(c, "管理后台发布了通知", "notice", n.ID, "title", n.Title)
	return c.JSON(http.StatusOK, n)
}

func (s *server) consoleUpdateNotice(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var req struct {
		notice.Input
		// Resend 为 true 时所有人重新弹一次。
		Resend bool `json:"resend"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	n, err := s.Notices.Update(c.Request().Context(), id, req.Input, req.Resend)
	if err != nil {
		return err
	}
	s.audit(c, "管理后台修改了通知", "notice", id, "title", n.Title, "resend", req.Resend)
	return c.JSON(http.StatusOK, n)
}

func (s *server) consoleDeleteNotice(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Notices.Delete(c.Request().Context(), id); err != nil {
		return err
	}
	s.audit(c, "管理后台撤回了通知", "notice", id)
	return c.NoContent(http.StatusNoContent)
}

// consoleObserved 处理 GET /console/proactive/observed：主动关心「只观察」模式下本来会发的消息。
func (s *server) consoleObserved(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{"mode": s.Config.Proactive.CareMode, "care": s.Config.Proactive.Care,
		"observations": proactive.Observed(c.Request().Context(), s.Store.Meta())})
}
