package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/cosplay"
)

// Cosplay 角色卡：每个用户导入自己的卡，管理员可以发布公共卡。新建对话时选一张，这段对话里 AI 扮演它。

// maxImportText 是粘贴或上传一张卡时最多多少字节（超过上限的卡也先解析出来，告诉用户超了多少）。
const maxImportText = 2 << 20

func cosplayUser(c echo.Context) cosplay.User {
	u := currentUser(c)
	return cosplay.User{ID: u.ID, IsAdmin: u.IsAdmin}
}

func (s *server) listCharacters(c echo.Context) error {
	list, err := s.Characters.List(c.Request().Context(), cosplayUser(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"characters": list, "max_runes": s.Characters.MaxRunes()})
}

func (s *server) getCharacter(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	u := cosplayUser(c)
	ch, err := s.Characters.Get(c.Request().Context(), u, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{
		"id": ch.ID, "name": ch.Name, "description": ch.Description, "prompt": ch.Prompt, "public": ch.Public,
		"mine": ch.UserID == u.ID, "source": ch.Source, "runes": utf8.RuneCountInString(ch.Prompt),
	})
}

func (s *server) createCharacter(c echo.Context) error {
	var in cosplay.Input
	if err := bind(c, &in); err != nil {
		return err
	}
	card, err := s.Characters.Create(c.Request().Context(), cosplayUser(c), in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, card)
}

func (s *server) updateCharacter(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var in cosplay.Input
	if err := bind(c, &in); err != nil {
		return err
	}
	card, err := s.Characters.Update(c.Request().Context(), cosplayUser(c), id, in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, card)
}

func (s *server) deleteCharacter(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Characters.Delete(c.Request().Context(), cosplayUser(c), id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// parseCharacter 处理 POST /characters/parse：解析粘贴或上传的内容，返回解析结果给用户确认（不保存）。
func (s *server) parseCharacter(c echo.Context) error {
	var req struct {
		Text     string `json:"text"`
		Filename string `json:"filename"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if len(req.Text) > maxImportText {
		return badRequest("内容太长了")
	}
	card, err := s.Characters.ParseText(req.Text, req.Filename)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"characters": []cosplay.Candidate{card}, "max_runes": s.Characters.MaxRunes()})
}

// fetchCharacters 处理 POST /characters/fetch：打开网址，解析出里面的角色卡给用户挑（不保存）。
func (s *server) fetchCharacters(c echo.Context) error {
	var req struct {
		URL string `json:"url"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	if len(strings.TrimSpace(req.URL)) > 2000 {
		return badRequest("网址太长了")
	}
	cards, err := s.Characters.FetchURL(c.Request().Context(), req.URL)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"characters": cards, "max_runes": s.Characters.MaxRunes()})
}

// characterRevisions 处理 GET /characters/:id/revisions：这张卡的修改记录（自我进化或手动修改前的样子）。
func (s *server) characterRevisions(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	list, err := s.Characters.Revisions(c.Request().Context(), cosplayUser(c), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"revisions": list})
}

// restoreCharacter 处理 POST /characters/:id/revisions/:rid/restore：恢复成某条修改记录里的样子。
func (s *server) restoreCharacter(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	rid, err := strconv.ParseUint(c.Param("rid"), 10, 64)
	if err != nil {
		return badRequest("修改记录编号不对")
	}
	card, err := s.Characters.Restore(c.Request().Context(), cosplayUser(c), id, rid)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, card)
}

// forkCharacter 处理 POST /characters/:id/fork：公共卡不能改，按填写的内容另存为自己的副本，
// 自己用原卡的对话都换成副本。
func (s *server) forkCharacter(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var in cosplay.Input
	if err := bind(c, &in); err != nil {
		return err
	}
	card, err := s.Characters.Fork(c.Request().Context(), cosplayUser(c), id, in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, card)
}
