package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// 后台长任务：查看、确认（可以调预算）、取消、接着做。任务由聊天里的模型提出（start_task 工具）。

func (s *server) routeTasks(priv *echo.Group) {
	if s.Tasks == nil {
		return
	}
	priv.GET("/bg-tasks", s.listBgTasks)
	priv.GET("/bg-tasks/:id", s.getBgTask)
	priv.POST("/bg-tasks/:id/confirm", s.confirmBgTask)
	priv.POST("/bg-tasks/:id/cancel", s.cancelBgTask)
	priv.POST("/bg-tasks/:id/resume", s.resumeBgTask)
}

func (s *server) listBgTasks(c echo.Context) error {
	ctx := c.Request().Context()
	u := currentUser(c)
	cfg := s.Tasks.Config()
	out := map[string]any{"allowed": s.Tasks.Allowed(ctx, u), "default_budget": cfg.DefaultBudget, "max_budget": cfg.MaxBudget}
	list, err := s.Tasks.List(ctx, u, 100)
	if err != nil {
		return err
	}
	out["tasks"] = list
	return c.JSON(http.StatusOK, out)
}

func (s *server) getBgTask(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	v, err := s.Tasks.Get(c.Request().Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}

func (s *server) confirmBgTask(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var req struct {
		Budget int64 `json:"budget"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if _, err := s.Tasks.Confirm(c.Request().Context(), currentUser(c), id, req.Budget); err != nil {
		return err
	}
	return s.getBgTask(c)
}

func (s *server) cancelBgTask(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Tasks.Cancel(c.Request().Context(), currentUser(c), id); err != nil {
		return err
	}
	return s.getBgTask(c)
}

func (s *server) resumeBgTask(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if _, err := s.Tasks.Resume(c.Request().Context(), currentUser(c), id); err != nil {
		return err
	}
	return s.getBgTask(c)
}
