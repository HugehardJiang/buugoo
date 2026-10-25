package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/models"
)

// ---------- 聊天模型 ----------

func (s *server) listModels(c echo.Context) error {
	ms, err := s.Models.List(c.Request().Context(), modelUser(currentUser(c)))
	if err != nil {
		return err
	}
	if ms == nil {
		ms = []models.View{}
	}
	return c.JSON(http.StatusOK, map[string]any{"models": ms})
}

func (s *server) createModel(c echo.Context) error {
	var in models.Input
	if err := bind(c, &in); err != nil {
		return err
	}
	v, err := s.Models.Create(c.Request().Context(), modelUser(currentUser(c)), in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, v)
}

func (s *server) updateModel(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var in models.Input
	if err := bind(c, &in); err != nil {
		return err
	}
	v, err := s.Models.Update(c.Request().Context(), modelUser(currentUser(c)), id, in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}

func (s *server) deleteModel(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Models.Delete(c.Request().Context(), modelUser(currentUser(c)), id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *server) setDefaultModel(c echo.Context) error {
	id, err := pathID(c)
	if err != nil && c.Param("id") != "0" {
		return err
	}
	if err := s.Models.SetDefault(c.Request().Context(), modelUser(currentUser(c)), id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *server) clearDefaultModel(c echo.Context) error {
	if err := s.Models.ClearDefault(c.Request().Context(), modelUser(currentUser(c))); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// testModelInput 测试尚未保存的模型配置。
func (s *server) testModelInput(c echo.Context) error {
	var in models.Input
	if err := bind(c, &in); err != nil {
		return err
	}
	res, err := s.Models.Test(c.Request().Context(), modelUser(currentUser(c)), 0, &in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

// testModel 测试已保存的模型；请求体非空时用请求体中的配置（Key 为空则用已保存的 Key）。
func (s *server) testModel(c echo.Context) error {
	id, err := pathID(c)
	if err != nil && c.Param("id") != "0" {
		return err
	}
	in, err := optionalBody[models.Input](c)
	if err != nil {
		return err
	}
	res, err := s.Models.Test(c.Request().Context(), modelUser(currentUser(c)), id, in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

// optionalBody 解析可选的 JSON 请求体，为空时返回 nil。
func optionalBody[T any](c echo.Context) (*T, error) {
	var v T
	if err := json.NewDecoder(c.Request().Body).Decode(&v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, badRequest("请求格式错误")
	}
	return &v, nil
}

// ---------- 向量模型（全站唯一，仅管理员） ----------

func (s *server) adminGetEmbedding(c echo.Context) error {
	ctx := c.Request().Context()
	v, err := s.Models.GetEmbedding(ctx)
	if err != nil {
		return err
	}
	pending, counts, err := s.Reembed.Pending(ctx, false)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"embedding": v, "pending": pending, "counts": counts, "reembed": s.Reembed.Status()})
}

func (s *server) adminSetEmbedding(c echo.Context) error {
	var in models.EmbeddingInput
	if err := bind(c, &in); err != nil {
		return err
	}
	ctx := c.Request().Context()
	v, changed, err := s.Models.SetEmbedding(ctx, modelUser(currentUser(c)), in)
	if err != nil {
		return err
	}
	pending, _, err := s.Reembed.Pending(ctx, false)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"embedding": v, "changed": changed, "pending": pending})
}

func (s *server) adminTestEmbedding(c echo.Context) error {
	in, err := optionalBody[models.EmbeddingInput](c)
	if err != nil {
		return err
	}
	res, err := s.Models.TestEmbedding(c.Request().Context(), modelUser(currentUser(c)), in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

func (s *server) adminReembedStatus(c echo.Context) error {
	return c.JSON(http.StatusOK, s.Reembed.Status())
}

func (s *server) adminStartReembed(c echo.Context) error {
	var req struct {
		All bool `json:"all"`
	}
	if in, err := optionalBody[struct {
		All bool `json:"all"`
	}](c); err != nil {
		return err
	} else if in != nil {
		req.All = in.All
	}
	// 任务在后台运行，不随本次请求取消。
	if err := s.Reembed.Start(context.WithoutCancel(c.Request().Context()), req.All); err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, s.Reembed.Status())
}
