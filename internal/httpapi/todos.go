package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/todo"
)

// 待办清单：网页上增删改、打勾（聊天时布谷用工具读写同一份）。

func (s *server) listTodos(c echo.Context) error {
	list, err := s.Todos.List(c.Request().Context(), currentUser(c).ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"items": s.Todos.Items(list...)})
}

func (s *server) createTodo(c echo.Context) error {
	var req struct {
		Title string `json:"title"`
		Note  string `json:"note"`
		Due   string `json:"due"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	t, err := s.Todos.Create(c.Request().Context(), currentUser(c).ID, req.Title, req.Note, req.Due)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, s.Todos.Items(*t)[0])
}

func (s *server) updateTodo(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var req struct {
		Title *string `json:"title"`
		Note  *string `json:"note"`
		Due   *string `json:"due"`
		Done  *bool   `json:"done"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	ctx, uid := c.Request().Context(), currentUser(c).ID
	t, err := s.Todos.Update(ctx, uid, id, todo.Patch{Title: req.Title, Note: req.Note, Due: req.Due})
	if err != nil {
		return err
	}
	if req.Done != nil && *req.Done != t.Done {
		if t, err = s.Todos.SetDone(ctx, uid, id, *req.Done); err != nil {
			return err
		}
	}
	return c.JSON(http.StatusOK, s.Todos.Items(*t)[0])
}

func (s *server) deleteTodo(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Todos.Delete(c.Request().Context(), currentUser(c).ID, id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// clearDoneTodos 删掉所有做完的。
func (s *server) clearDoneTodos(c echo.Context) error {
	n, err := s.Todos.ClearDone(c.Request().Context(), currentUser(c).ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"deleted": n})
}
