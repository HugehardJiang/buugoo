package httpapi

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/opencode"
)

// OpenCode Go：管理后台用站长的 Key 一键添加模型（默认只给授权用户用）并写好计费价格；
// 普通用户用自己的 Key 一键添加个人模型；目录定时从官网同步，也可以手动检查。

type importedModel struct {
	ID         uint64 `json:"id"`
	Model      string `json:"model"`
	Name       string `json:"name"`
	Restricted bool   `json:"restricted"`
}

func (s *server) consoleOpenCode(c echo.Context) error {
	ctx := c.Request().Context()
	imported, err := s.OpenCode.Imported(ctx)
	if err != nil {
		return err
	}
	list := []importedModel{}
	for _, m := range imported {
		list = append(list, importedModel{ID: m.ID, Model: m.Model, Name: m.Name, Restricted: m.Restricted})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"catalog": s.OpenCode.Catalog(ctx), "base_url": opencode.BaseURL, "options": s.OpenCode.Options(ctx),
		"imported": list, "last_sync": s.OpenCode.LastSync(ctx), "billing": s.Billing != nil,
	})
}

func (s *server) consoleOpenCodeImport(c echo.Context) error {
	var req struct {
		APIKey string   `json:"api_key"`
		Models []string `json:"models"`
		opencode.Options
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	res, err := s.OpenCode.Import(c.Request().Context(), req.APIKey, req.Models, req.Options)
	if err != nil {
		return err
	}
	s.audit(c, "管理后台导入了 OpenCode Go 模型", "added", res.Added, "updated", res.Updated, "tier", req.Tier)
	msg := fmt.Sprintf("新添加 %d 个，更新 %d 个", res.Added, res.Updated)
	if res.Priced > 0 {
		msg += fmt.Sprintf("，写好了 %d 个的价格", res.Priced)
	}
	return c.JSON(http.StatusOK, map[string]any{"message": msg, "peak_note": res.PeakNote})
}

// consoleOpenCodeSync 立即从官网检查模型和价格。
func (s *server) consoleOpenCodeSync(c echo.Context) error {
	return c.JSON(http.StatusOK, s.OpenCode.Sync(c.Request().Context()))
}

// consoleOpenCodeApply 按最新目录更新已添加模型的计费价格。
func (s *server) consoleOpenCodeApply(c echo.Context) error {
	if err := s.OpenCode.ApplyPrices(c.Request().Context()); err != nil {
		return err
	}
	s.audit(c, "管理后台按 OpenCode 官网最新价格更新了计费")
	return c.NoContent(http.StatusNoContent)
}

func (s *server) consoleOpenCodeOptions(c echo.Context) error {
	var o opencode.Options
	if err := bind(c, &o); err != nil {
		return err
	}
	if err := s.OpenCode.SaveOptions(c.Request().Context(), o); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// openCodeCatalog 给普通用户看的目录（不含站长的价格设置）。
func (s *server) openCodeCatalog(c echo.Context) error {
	ctx := c.Request().Context()
	snap := s.OpenCode.Catalog(ctx)
	type item struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Vision bool   `json:"vision,omitempty"`
		Free   bool   `json:"free,omitempty"`
		Note   string `json:"note,omitempty"`
	}
	items := make([]item, 0, len(snap.Models))
	for _, m := range snap.Models {
		items = append(items, item{m.ID, m.Name, m.Vision, m.Free, m.Note})
	}
	mine, err := s.OpenCode.Mine(ctx, modelUser(currentUser(c)))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"models": items, "mine": mine, "date": snap.Date, "base_url": opencode.BaseURL})
}

// addMyOpenCode 用用户自己的 Key 一键添加选中的模型。
func (s *server) addMyOpenCode(c echo.Context) error {
	var req struct {
		APIKey string   `json:"api_key"`
		Models []string `json:"models"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	added, updated, err := s.OpenCode.AddPersonal(c.Request().Context(), modelUser(currentUser(c)), req.APIKey, req.Models)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"added": added, "updated": updated})
}

// consoleOpenCodeRemove 一键删除站长添加的 OpenCode 公共模型（连同计费价格）。
func (s *server) consoleOpenCodeRemove(c echo.Context) error {
	var req struct {
		Models []string `json:"models"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	n, err := s.OpenCode.Remove(c.Request().Context(), models.User{IsAdmin: true}, true, req.Models)
	if err != nil {
		return err
	}
	s.audit(c, "管理后台删除了 OpenCode Go 模型", "removed", n)
	return c.JSON(http.StatusOK, map[string]any{"removed": n})
}

// removeMyOpenCode 一键删除用户自己添加的 OpenCode 模型。
func (s *server) removeMyOpenCode(c echo.Context) error {
	var req struct {
		Models []string `json:"models"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	n, err := s.OpenCode.Remove(c.Request().Context(), modelUser(currentUser(c)), false, req.Models)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"removed": n})
}
