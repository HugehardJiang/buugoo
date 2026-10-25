package httpapi

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type reminderDTO struct {
	ID      uint64    `json:"id"`
	Content string    `json:"content"`
	DueAt   time.Time `json:"due_at"`
	Repeat  string    `json:"repeat"`
}

// listReminders 处理 GET /api/v1/reminders：还没到期或重复中的提醒。
func (s *server) listReminders(c echo.Context) error {
	list, err := s.Store.Reminders().ListPending(c.Request().Context(), currentUser(c).ID)
	if err != nil {
		return err
	}
	out := make([]reminderDTO, len(list))
	for i, r := range list {
		out[i] = reminderDTO{ID: r.ID, Content: r.Content, DueAt: r.DueAt, Repeat: r.Repeat}
	}
	return c.JSON(http.StatusOK, map[string]any{"reminders": out, "enabled": s.Config.Proactive.Reminders, "care": s.Config.Proactive.Care})
}

// cancelReminder 处理 DELETE /api/v1/reminders/:id。
func (s *server) cancelReminder(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Store.Reminders().Cancel(c.Request().Context(), currentUser(c).ID, id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// setProactive 处理 PUT /api/v1/me/proactive：{"on": false} 不让布谷主动找自己聊天（提醒不受影响）。
func (s *server) setProactive(c echo.Context) error {
	var req struct {
		On bool `json:"on"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest("请求格式不正确")
	}
	if err := s.Store.Users().SetProactive(c.Request().Context(), currentUser(c).ID, req.On); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"on": req.On})
}

// latestMessage 处理 GET /api/v1/conversations/:id/latest：最新一条消息的 ID，网页用来发现布谷主动发来的消息。
func (s *server) latestMessage(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	ctx, u := c.Request().Context(), currentUser(c)
	if _, err := s.Store.Conversations().Get(ctx, u.ID, id); err != nil {
		return err
	}
	latest, err := s.Store.Messages().LatestID(ctx, u.ID, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"latest_id": latest})
}
