package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/proactive"
	"github.com/HugehardJiang/buugoo/internal/store"
)

type reminderDTO struct {
	ID      uint64    `json:"id"`
	Kind    string    `json:"kind"`
	Content string    `json:"content"`
	DueAt   time.Time `json:"due_at"`
	// Time 是下次时间的本地写法（YYYY-MM-DD HH:MM），编辑时使用。
	Time        string `json:"time"`
	Repeat      string `json:"repeat"`
	Every       int    `json:"every_minutes"`
	Cron        string `json:"cron"`
	RepeatLabel string `json:"repeat_label"`
	Paused      bool   `json:"paused"`
}

func (s *server) toReminder(r *store.Reminder) reminderDTO {
	loc := s.Config.Location()
	cron := r.Cron
	if r.Repeat == store.RepeatMonthly {
		cron = "" // 每月的 cron 是自动生成的
	}
	return reminderDTO{ID: r.ID, Kind: r.Kind, Content: r.Content, DueAt: r.DueAt, Time: r.DueAt.In(loc).Format("2006-01-02 15:04"),
		Repeat: r.Repeat, Every: r.Every, Cron: cron, RepeatLabel: proactive.RepeatLabel(r, loc), Paused: r.Status == store.ReminderPaused}
}

// listReminders 处理 GET /api/v1/reminders：还在进行的提醒与定时任务。
func (s *server) listReminders(c echo.Context) error {
	list, err := s.Store.Reminders().ListPending(c.Request().Context(), currentUser(c).ID)
	if err != nil {
		return err
	}
	out := make([]reminderDTO, len(list))
	for i := range list {
		out[i] = s.toReminder(&list[i])
	}
	return c.JSON(http.StatusOK, map[string]any{"reminders": out, "enabled": s.Config.Proactive.Reminders, "care": s.Config.Proactive.Care,
		"timezone": s.Config.Location().String()})
}

type reminderReq struct {
	Kind string `json:"kind"`
	proactive.Input
}

// createReminder 处理 POST /api/v1/reminders：新建提醒或定时任务。
func (s *server) createReminder(c echo.Context) error {
	if !s.Config.Proactive.Reminders {
		return badRequest("管理员关闭了提醒功能")
	}
	var req reminderReq
	if err := c.Bind(&req); err != nil {
		return badRequest("请求格式不正确")
	}
	if req.Kind != store.KindTask {
		req.Kind = store.KindRemind
	}
	r, err := proactive.Create(c.Request().Context(), s.Store.Reminders(), currentUser(c).ID, req.Kind, req.Input, time.Now(), s.Config.Location())
	if err != nil {
		return badRequest(err.Error())
	}
	return c.JSON(http.StatusCreated, s.toReminder(r))
}

// updateReminder 处理 PUT /api/v1/reminders/:id：修改内容与时间安排。
func (s *server) updateReminder(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var req reminderReq
	if err := c.Bind(&req); err != nil {
		return badRequest("请求格式不正确")
	}
	ctx, u := c.Request().Context(), currentUser(c)
	r, err := s.Store.Reminders().Get(ctx, u.ID, id)
	if err != nil {
		return err
	}
	if err := req.Input.Build(r, time.Now(), s.Config.Location()); err != nil {
		return badRequest(err.Error())
	}
	if err := s.Store.Reminders().Update(ctx, u.ID, id, map[string]any{"content": r.Content, "due_at": r.DueAt,
		"repeat": r.Repeat, "every": r.Every, "cron": r.Cron}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, s.toReminder(r))
}

// pauseReminder 处理 POST /api/v1/reminders/:id/pause 与 /resume。
func (s *server) pauseReminder(pause bool) echo.HandlerFunc {
	return func(c echo.Context) error {
		id, err := pathID(c)
		if err != nil {
			return err
		}
		ctx, u := c.Request().Context(), currentUser(c)
		r, err := s.Store.Reminders().Get(ctx, u.ID, id)
		if err != nil {
			return err
		}
		fields := map[string]any{"status": store.ReminderPending}
		if pause {
			fields["status"] = store.ReminderPaused
		} else if now := time.Now(); !r.DueAt.After(now) {
			// 暂停期间错过的不补，从下一次开始。
			next := proactive.NextDue(r, now, s.Config.Location())
			if next == nil {
				return badRequest("这个时间已经过了，先改一下时间")
			}
			fields["due_at"] = next.UTC()
			r.DueAt = next.UTC()
		}
		if err := s.Store.Reminders().Update(ctx, u.ID, id, fields); err != nil {
			return err
		}
		r.Status = fields["status"].(string)
		return c.JSON(http.StatusOK, s.toReminder(r))
	}
}

// cancelReminder 处理 DELETE /api/v1/reminders/:id。
func (s *server) cancelReminder(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Store.Reminders().Cancel(c.Request().Context(), currentUser(c).ID, id); err != nil && !errors.Is(err, store.ErrNotFound) {
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
