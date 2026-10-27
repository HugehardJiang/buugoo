package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/store"
)

// 备忘录：网页上查看和编辑（聊天时布谷用工具读写同一份）。

type memoDTO struct {
	ID        uint64 `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at"`
}

func toMemo(p *store.MemoPage) memoDTO {
	return memoDTO{ID: p.ID, Title: p.Title, Content: p.Content, UpdatedAt: p.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")}
}

func (s *server) listMemos(c echo.Context) error {
	list, err := s.Memos.List(c.Request().Context(), currentUser(c).ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"pages": list})
}

func (s *server) getMemo(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	p, err := s.Memos.Get(c.Request().Context(), currentUser(c).ID, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toMemo(p))
}

func (s *server) createMemo(c echo.Context) error {
	var req struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	p, err := s.Memos.Create(c.Request().Context(), currentUser(c).ID, req.Title, req.Content)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toMemo(p))
}

func (s *server) updateMemo(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var req struct {
		Title   *string `json:"title"`
		Content *string `json:"content"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	p, err := s.Memos.Update(c.Request().Context(), currentUser(c).ID, id, req.Title, req.Content)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toMemo(p))
}

func (s *server) deleteMemo(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Memos.Delete(c.Request().Context(), currentUser(c).ID, id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// compactMemories 处理 POST /api/v1/memories/compact：把相近的记忆合并（设置页「整理记忆」）。
func (s *server) compactMemories(c echo.Context) error {
	r, err := s.Compactor.Compact(c.Request().Context(), currentUser(c).ID, true)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, r)
}
