package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/cms"
)

// 文章 CMS 接口：公开接口只读且只返回已发布文章；管理接口挂在管理后台并拦截默认密码。

func (s *server) routeCMS(api, console *echo.Group) {
	if s.CMS == nil {
		return
	}
	pub := api.Group("/cms")
	pub.GET("/posts", s.publicCMSPosts)
	pub.GET("/posts/:slug", s.publicCMSPost)

	m := console.Group("/cms", s.requireStrongConsole)
	m.GET("/posts", s.consoleCMSPosts)
	m.POST("/posts", s.consoleCreateCMSPost)
	m.PUT("/posts/:id", s.consoleUpdateCMSPost)
	m.DELETE("/posts/:id", s.consoleDeleteCMSPost)
	m.POST("/seed", s.consoleSeedCMSPosts)
}

func (s *server) publicCMSPosts(c echo.Context) error {
	limit, err := queryInt(c, "limit", 0, 1, cms.MaxPublicLimit)
	if err != nil {
		return err
	}
	featuredRaw := strings.ToLower(strings.TrimSpace(c.QueryParam("featured")))
	featured := featuredRaw == "1" || featuredRaw == "true"

	res, err := s.CMS.ListPublic(c.Request().Context(), cms.PublicListFilter{
		Category: c.QueryParam("category"),
		Tag:      c.QueryParam("tag"),
		Query:    c.QueryParam("q"),
		Featured: featured,
		Limit:    limit,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

func (s *server) publicCMSPost(c echo.Context) error {
	res, err := s.CMS.GetPublicBySlug(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

func (s *server) consoleCMSPosts(c echo.Context) error {
	list, err := s.CMS.ListAdmin(c.Request().Context(), cms.AdminListFilter{
		Status:   c.QueryParam("status"),
		Category: c.QueryParam("category"),
		Query:    c.QueryParam("q"),
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"posts": list})
}

type cmsPostPayload struct {
	Slug        string          `json:"slug"`
	Category    string          `json:"category"`
	Tags        []string        `json:"tags"`
	Title       string          `json:"title"`
	Summary     string          `json:"summary"`
	Content     string          `json:"content"`
	Author      string          `json:"author"`
	CoverURL    string          `json:"cover_url"`
	CoverTheme  string          `json:"cover_theme"`
	Status      string          `json:"status"`
	Featured    bool            `json:"featured"`
	Pinned      bool            `json:"pinned"`
	SortOrder   int             `json:"sort_order"`
	PublishedAt json.RawMessage `json:"published_at"`
}

func (p *cmsPostPayload) toInput() (cms.PostInput, error) {
	pubAt, err := parseOptionalTime(p.PublishedAt)
	if err != nil {
		return cms.PostInput{}, err
	}
	return cms.PostInput{
		Slug:        p.Slug,
		Category:    p.Category,
		Tags:        p.Tags,
		Title:       p.Title,
		Summary:     p.Summary,
		Content:     p.Content,
		Author:      p.Author,
		CoverURL:    p.CoverURL,
		CoverTheme:  p.CoverTheme,
		Status:      p.Status,
		Featured:    p.Featured,
		Pinned:      p.Pinned,
		SortOrder:   p.SortOrder,
		PublishedAt: pubAt,
	}, nil
}

func parseOptionalTime(raw json.RawMessage) (*time.Time, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var str string
	if err := json.Unmarshal(raw, &str); err != nil {
		return nil, badRequest("发布时间格式无效")
	}
	str = strings.TrimSpace(str)
	if str == "" {
		return nil, nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, str); err == nil {
			u := t.UTC()
			return &u, nil
		}
	}
	return nil, badRequest("发布时间格式无效")
}

func (s *server) consoleCreateCMSPost(c echo.Context) error {
	var payload cmsPostPayload
	if err := bind(c, &payload); err != nil {
		return err
	}
	in, err := payload.toInput()
	if err != nil {
		return err
	}
	post, err := s.CMS.Create(c.Request().Context(), in)
	if err != nil {
		return err
	}
	s.audit(c, "管理后台新建了文章", "cms_post", post.ID, "slug", post.Slug, "status", post.Status)
	return c.JSON(http.StatusCreated, map[string]any{"post": post})
}

func (s *server) consoleUpdateCMSPost(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var payload cmsPostPayload
	if err := bind(c, &payload); err != nil {
		return err
	}
	in, err := payload.toInput()
	if err != nil {
		return err
	}
	post, err := s.CMS.Update(c.Request().Context(), id, in)
	if err != nil {
		return err
	}
	s.audit(c, "管理后台修改了文章", "cms_post", post.ID, "slug", post.Slug, "status", post.Status)
	return c.JSON(http.StatusOK, map[string]any{"post": post})
}

func (s *server) consoleDeleteCMSPost(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.CMS.Delete(c.Request().Context(), id); err != nil {
		return err
	}
	s.audit(c, "管理后台删除了文章", "cms_post", id)
	return c.NoContent(http.StatusNoContent)
}

func (s *server) consoleSeedCMSPosts(c echo.Context) error {
	created, err := s.CMS.SeedDemo(c.Request().Context())
	if err != nil {
		return err
	}
	s.audit(c, "管理后台载入了示例文章", "created", created)
	return c.JSON(http.StatusOK, map[string]any{"created": created})
}
