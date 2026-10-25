package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/persona"
)

type presetDTO struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	Summary      string   `json:"summary"`
	Catchphrases []string `json:"catchphrases"`
}

// effectivePreset 返回用户实际使用的预设（选的预设被管理员关掉时为默认）。
func (s *server) effectivePreset(id string) string {
	if p := persona.Find(id, s.Config.Persona.DisabledPresets); p != nil {
		return p.ID
	}
	return persona.DefaultPreset
}

// listPresets 处理 GET /api/v1/persona/presets：默认人格（管理后台设置的）与可选的预设性格。
func (s *server) listPresets(c echo.Context) error {
	base := s.Persona.Current()
	summary := "管理员设定的性格"
	if len(base.Personality) > 0 {
		summary = base.Personality[0]
	}
	out := []presetDTO{{ID: persona.DefaultPreset, Label: "默认", Summary: summary, Catchphrases: base.Catchphrases}}
	for _, p := range persona.Available(s.Config.Persona.DisabledPresets) {
		out = append(out, presetDTO{ID: p.ID, Label: p.Label, Summary: p.Summary, Catchphrases: p.Catchphrases})
	}
	return c.JSON(http.StatusOK, map[string]any{"current": s.effectivePreset(currentUser(c).PersonaPreset), "presets": out})
}

// setPreset 处理 PUT /api/v1/persona/preset：{"id": "gentle"}，空字符串恢复默认。下一条消息起生效。
func (s *server) setPreset(c echo.Context) error {
	var req struct {
		ID string `json:"id"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest("请求格式不正确")
	}
	if req.ID != persona.DefaultPreset && persona.Find(req.ID, s.Config.Persona.DisabledPresets) == nil {
		return badRequest("没有这种性格")
	}
	u := currentUser(c)
	if err := s.Store.Users().SetPersonaPreset(c.Request().Context(), u.ID, req.ID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"current": req.ID})
}
