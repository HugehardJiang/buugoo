package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/store"
)

// avatarRegensPerYear 是每人每年能重新生成头像的次数。
const avatarRegensPerYear = 5

// avatarLeft 返回今年还能重新生成头像几次。
func (s *server) avatarLeft(u *store.User) int {
	if u.AvatarYear != time.Now().In(s.Config.Location()).Year() {
		return avatarRegensPerYear
	}
	return max(0, avatarRegensPerYear-u.AvatarRegens)
}

// profile 处理 GET /api/v1/me/profile：个人中心的资料。
func (s *server) profile(c echo.Context) error {
	u := currentUser(c)
	return c.JSON(http.StatusOK, map[string]any{"user": toUser(u), "avatar_left": s.avatarLeft(u), "avatar_limit": avatarRegensPerYear})
}

// updateProfile 处理 PUT /api/v1/me/profile：改昵称。
func (s *server) updateProfile(c echo.Context) error {
	var req struct {
		DisplayName string `json:"display_name"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	name := strings.TrimSpace(req.DisplayName)
	if utf8.RuneCountInString(name) > 30 || strings.ContainsAny(name, "\r\n\t") {
		return badRequest("昵称最多 30 个字，不能换行")
	}
	ctx, u := c.Request().Context(), currentUser(c)
	if err := s.Store.Users().UpdateFields(ctx, u.ID, map[string]any{"display_name": name}); err != nil {
		return err
	}
	u.DisplayName = name
	return c.JSON(http.StatusOK, map[string]any{"user": toUser(u)})
}

// regenerateAvatar 处理 POST /api/v1/me/avatar：随机换一个头像，每年最多 5 次。
func (s *server) regenerateAvatar(c echo.Context) error {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	seed := hex.EncodeToString(b)
	ctx, u := c.Request().Context(), currentUser(c)
	used, err := s.Store.Users().RegenerateAvatar(ctx, u.ID, seed, time.Now().In(s.Config.Location()).Year(), avatarRegensPerYear)
	if errors.Is(err, store.ErrAvatarLimit) {
		return &apiError{Status: http.StatusTooManyRequests, Code: "avatar_limit", Message: "今年 5 次重新生成的机会已经用完了，明年再来吧"}
	}
	if err != nil {
		return err
	}
	u.AvatarSeed = seed
	return c.JSON(http.StatusOK, map[string]any{"user": toUser(u), "avatar_left": max(0, avatarRegensPerYear-used)})
}
