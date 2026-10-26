package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/HugehardJiang/buugoo/internal/agent"
	"github.com/HugehardJiang/buugoo/internal/mcp"
	"github.com/HugehardJiang/buugoo/internal/skills"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// skill、MCP 插件（管理后台安装，用户自己开关）与牛马助手。

// extensionError 把 skill、插件、牛马的错误转成接口错误，不是这几类时返回 nil。
func extensionError(err error) *apiError {
	var se *skills.UserError
	var me *mcp.InvalidError
	switch {
	case errors.As(err, &se):
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_skill", Message: se.Msg}
	case errors.As(err, &me):
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_plugin", Message: me.Msg}
	case errors.Is(err, skills.ErrNotFound), errors.Is(err, mcp.ErrNotFound), errors.Is(err, agent.ErrNotFound):
		return &apiError{Status: http.StatusNotFound, Code: "not_found", Message: err.Error()}
	case errors.Is(err, agent.ErrBusy), errors.Is(err, agent.ErrHandled):
		return &apiError{Status: http.StatusConflict, Code: "busy", Message: err.Error()}
	case errors.Is(err, agent.ErrNeedConsole):
		return &apiError{Status: http.StatusForbidden, Code: "console_required", Message: err.Error()}
	case errors.Is(err, agent.ErrEmpty), errors.Is(err, agent.ErrTooLong), errors.Is(err, agent.ErrNoToolsModel):
		return &apiError{Status: http.StatusBadRequest, Code: "invalid_message", Message: err.Error()}
	}
	return nil
}

func (s *server) routeExtensions(e *echo.Echo, api, priv, console *echo.Group) {
	if s.Skills != nil {
		console.GET("/skills", s.consoleSkills)
		console.GET("/skills/:id", s.consoleSkill)
		console.POST("/skills/github", s.consoleInstallGitHub)
		console.POST("/skills/text", s.consoleInstallText)
		console.PATCH("/skills/:id", s.consoleUpdateSkill)
		console.DELETE("/skills/:id", s.consoleRemoveSkill)
		// 上传 zip 单独设置请求体上限。
		e.POST("/api/v1/console/skills/zip", s.consoleInstallZip, middleware.BodyLimit("101M"), s.requireConsole)
	}
	if s.Plugins != nil {
		console.GET("/plugins", s.consolePlugins)
		console.POST("/plugins", s.consoleAddPlugin)
		console.PATCH("/plugins/:id", s.consoleUpdatePlugin)
		console.POST("/plugins/:id/reconnect", s.consoleReconnectPlugin)
		console.DELETE("/plugins/:id", s.consoleRemovePlugin)
	}
	priv.GET("/me/extensions", s.myExtensions)
	priv.PUT("/me/extensions", s.toggleExtension)
	if s.Agent != nil {
		priv.GET("/agent", s.agentState)
		priv.POST("/agent/messages", s.agentSend)
		priv.POST("/agent/actions/:id/confirm", s.agentConfirm)
		priv.POST("/agent/actions/:id/cancel", s.agentCancel)
		priv.PUT("/agent/model", s.agentSetModel)
		priv.DELETE("/agent", s.agentClear)
	}
}

const maxSkillZip = 100 << 20

// ---------- 管理后台：skill ----------

type skillDTO struct {
	ID          uint64 `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	SourceURL   string `json:"source_url,omitempty"`
	Network     bool   `json:"network"`
	Enabled     bool   `json:"enabled"`
	Files       int    `json:"files"`
	Size        int64  `json:"size"`
	UpdatedAt   string `json:"updated_at"`
}

func toSkill(sk *store.Skill) skillDTO {
	return skillDTO{ID: sk.ID, Name: sk.Name, Description: sk.Description, Source: sk.Source, SourceURL: sk.SourceURL,
		Network: sk.AllowNetwork, Enabled: sk.Enabled, Files: sk.Files, Size: sk.Size, UpdatedAt: sk.UpdatedAt.Format("2006-01-02 15:04")}
}

func (s *server) consoleSkills(c echo.Context) error {
	list, err := s.Skills.List(c.Request().Context())
	if err != nil {
		return err
	}
	out := make([]skillDTO, len(list))
	for i := range list {
		out[i] = toSkill(&list[i])
	}
	return c.JSON(http.StatusOK, map[string]any{"skills": out, "sandbox": s.Config.Tools.Python.Runner})
}

// consoleSkill 返回 skill 的说明（SKILL.md 正文）和文件列表。
func (s *server) consoleSkill(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	sk, err := s.Skills.Get(c.Request().Context(), id)
	if err != nil {
		return err
	}
	body, _ := s.Skills.Body(sk.Name)
	return c.JSON(http.StatusOK, map[string]any{"skill": toSkill(sk), "body": body, "files": s.Skills.Files(sk.Name, 200)})
}

func (s *server) installResult(c echo.Context, res *skills.InstallResult) error {
	if res.Skill == nil {
		return c.JSON(http.StatusOK, map[string]any{"candidates": res.Candidates})
	}
	s.log.Warn("管理后台安装了 skill", "name", res.Skill.Name, "source", res.Skill.Source, "ip", c.RealIP())
	return c.JSON(http.StatusOK, map[string]any{"skill": toSkill(res.Skill)})
}

func (s *server) consoleInstallGitHub(c echo.Context) error {
	var req struct {
		URL  string `json:"url"`
		Path string `json:"path"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	res, err := s.Skills.InstallGitHub(c.Request().Context(), req.URL, req.Path)
	if err != nil {
		return err
	}
	return s.installResult(c, res)
}

func (s *server) consoleInstallText(c echo.Context) error {
	var req struct {
		Content string `json:"content"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	sk, err := s.Skills.InstallText(c.Request().Context(), req.Content)
	if err != nil {
		return err
	}
	return s.installResult(c, &skills.InstallResult{Skill: sk})
}

// consoleInstallZip 处理上传的 zip：表单字段 file，可选 path（找到好几个 skill 时选哪个）。
func (s *server) consoleInstallZip(c echo.Context) error {
	fh, err := c.FormFile("file")
	if err != nil {
		return badRequest("请选择 zip 文件")
	}
	f, err := fh.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSkillZip+1))
	if err != nil {
		return err
	}
	res, err := s.Skills.InstallZip(c.Request().Context(), data, strings.TrimSpace(c.FormValue("path")))
	if err != nil {
		return err
	}
	return s.installResult(c, res)
}

func (s *server) consoleUpdateSkill(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var req struct {
		Enabled *bool `json:"enabled"`
		Network *bool `json:"network"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	sk, err := s.Skills.Update(c.Request().Context(), id, req.Enabled, req.Network)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toSkill(sk))
}

func (s *server) consoleRemoveSkill(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Skills.Remove(c.Request().Context(), id); err != nil {
		return err
	}
	s.log.Warn("管理后台卸载了 skill", "id", id, "ip", c.RealIP())
	return c.NoContent(http.StatusNoContent)
}

// ---------- 管理后台：插件 ----------

func (s *server) consolePlugins(c echo.Context) error {
	views, err := s.Plugins.Views(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"plugins": views})
}

func (s *server) consoleAddPlugin(c echo.Context) error {
	var in mcp.Input
	if err := bind(c, &in); err != nil {
		return err
	}
	p, err := s.Plugins.Add(c.Request().Context(), in)
	if err != nil {
		return err
	}
	s.log.Warn("管理后台添加了插件", "name", p.Name, "transport", p.Transport, "ip", c.RealIP())
	return c.JSON(http.StatusOK, map[string]any{"id": p.ID})
}

func (s *server) consoleUpdatePlugin(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := s.Plugins.SetEnabled(c.Request().Context(), id, req.Enabled); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *server) consoleReconnectPlugin(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Plugins.Reconnect(c.Request().Context(), id); err != nil {
		if errors.Is(err, mcp.ErrNotFound) {
			return err
		}
		return &apiError{Status: http.StatusBadGateway, Code: "plugin_unreachable", Message: "连接失败：" + err.Error()}
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *server) consoleRemovePlugin(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Plugins.Remove(c.Request().Context(), id); err != nil {
		return err
	}
	s.log.Warn("管理后台删除了插件", "id", id, "ip", c.RealIP())
	return c.NoContent(http.StatusNoContent)
}

// ---------- 用户：自己开关 ----------

type extensionDTO struct {
	Kind        string `json:"kind"` // skill | plugin
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

// myExtensions 处理 GET /api/v1/me/extensions：管理员开启的 skill 和插件，以及用户自己有没有关掉。
func (s *server) myExtensions(c echo.Context) error {
	ctx, u := c.Request().Context(), currentUser(c)
	out := []extensionDTO{}
	if s.Skills != nil {
		list, err := s.Skills.List(ctx)
		if err != nil {
			return err
		}
		off := skills.SplitNames(u.DisabledSkills)
		for _, sk := range list {
			if sk.Enabled {
				out = append(out, extensionDTO{Kind: "skill", Name: sk.Name, Description: sk.Description, Enabled: !contains(off, sk.Name)})
			}
		}
	}
	if s.Plugins != nil {
		views, err := s.Plugins.Views(ctx)
		if err != nil {
			return err
		}
		off := skills.SplitNames(u.DisabledPlugins)
		for _, v := range views {
			if !v.Enabled {
				continue
			}
			names := make([]string, len(v.Tools))
			for i, t := range v.Tools {
				names[i] = t.Name
			}
			desc := "提供工具：" + strings.Join(names, "、")
			if len(names) == 0 {
				desc = "暂时没有连上"
			}
			out = append(out, extensionDTO{Kind: "plugin", Name: v.Name, Description: desc, Enabled: !contains(off, v.Name)})
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"extensions": out})
}

func (s *server) toggleExtension(c echo.Context) error {
	var req struct {
		Kind    string `json:"kind"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	ctx, u := c.Request().Context(), currentUser(c)
	var column, list string
	switch {
	case req.Kind == "skill" && s.Skills != nil:
		if _, err := s.Skills.ByName(ctx, req.Name); err != nil {
			return err
		}
		column, list = "disabled_skills", u.DisabledSkills
	case req.Kind == "plugin" && s.Plugins != nil:
		if _, err := s.Plugins.ByName(ctx, req.Name); err != nil {
			return err
		}
		column, list = "disabled_plugins", u.DisabledPlugins
	default:
		return badRequest("kind 只能是 skill 或 plugin")
	}
	if err := s.Store.Users().UpdateFields(ctx, u.ID, map[string]any{column: skills.ToggleName(list, req.Name, req.Enabled)}); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---------- 牛马助手 ----------

// consoleActive 表示这个请求带着有效的管理后台登录（牛马可以改系统设置）。
func (s *server) consoleActive(c echo.Context) bool {
	return s.console.valid(c.Request().Header.Get("X-Console-Token"))
}

func (s *server) agentState(c echo.Context) error {
	ctx, u := c.Request().Context(), currentUser(c)
	msgs, err := s.Agent.History(ctx, u)
	if err != nil {
		return err
	}
	resp := map[string]any{"messages": msgs, "admin": s.consoleActive(c), "model_id": u.AgentModelID}
	if m, err := s.Agent.Model(ctx, u); err == nil {
		resp["model"] = map[string]any{"id": m.ID, "name": m.Name, "tools": m.Tools}
	}
	return c.JSON(http.StatusOK, resp)
}

func (s *server) agentSend(c echo.Context) error {
	var req struct {
		Content string `json:"content"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	reply, err := s.Agent.Send(c.Request().Context(), currentUser(c), req.Content, s.consoleActive(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, reply)
}

func (s *server) agentConfirm(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	a, err := s.Agent.Confirm(c.Request().Context(), currentUser(c), id, s.consoleActive(c))
	if err != nil {
		return err
	}
	if a.Admin {
		s.log.Warn("牛马执行了系统修改", "kind", a.Kind, "status", a.Status, "ip", c.RealIP())
	}
	return c.JSON(http.StatusOK, a)
}

func (s *server) agentCancel(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	a, err := s.Agent.Cancel(c.Request().Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, a)
}

func (s *server) agentSetModel(c echo.Context) error {
	var req struct {
		ID *uint64 `json:"id"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := s.Agent.SetModel(c.Request().Context(), currentUser(c), req.ID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *server) agentClear(c echo.Context) error {
	if err := s.Agent.Clear(c.Request().Context(), currentUser(c)); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
