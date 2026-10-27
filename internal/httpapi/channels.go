package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/channels"
	"github.com/HugehardJiang/buugoo/internal/store"
)

type bindingDTO struct {
	ID            uint64     `json:"id"`
	Channel       string     `json:"channel"`
	Label         string     `json:"label"`
	PeerName      string     `json:"peer_name"`
	CreatedAt     time.Time  `json:"created_at"`
	LastMessageAt *time.Time `json:"last_message_at"`
}

type channelDTO struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	// Account 是机器人的账号（QQ 号、Telegram 用户名），用户据此找到它。
	Account   string `json:"account,omitempty"`
	Connected bool   `json:"connected"`
}

// myChannels 处理 GET /api/v1/channels：开启了哪些聊天软件、自己的绑定，以及自己的微信连接情况。
func (s *server) myChannels(c echo.Context) error {
	out := map[string]any{"channels": []channelDTO{}, "bindings": []bindingDTO{}, "weixin": nil}
	if s.Channels == nil {
		return c.JSON(http.StatusOK, out)
	}
	u := currentUser(c)
	var chs []channelDTO
	for _, st := range s.Channels.Statuses() {
		if st.Name == channels.Weixin {
			st.Account = "" // 微信是每个人连自己的，不显示别人的账号数
		}
		chs = append(chs, channelDTO{Name: st.Name, Label: st.Label, Account: st.Account, Connected: st.Connected})
	}
	list, err := s.Store.Bindings().ListByUser(c.Request().Context(), u.ID)
	if err != nil {
		return err
	}
	bs := make([]bindingDTO, len(list))
	for i, b := range list {
		bs[i] = bindingDTO{ID: b.ID, Channel: b.Channel, Label: channels.Labels[b.Channel], PeerName: b.PeerName,
			CreatedAt: b.CreatedAt, LastMessageAt: b.LastMessageAt}
	}
	out["channels"], out["bindings"] = chs, bs
	if w := s.Channels.Weixin(); w != nil {
		out["weixin"] = w.User(u.ID)
	}
	return c.JSON(http.StatusOK, out)
}

// newBindCode 处理 POST /api/v1/channels/code：生成绑定码（10 分钟有效），在 QQ、Telegram 里发给布谷即可绑定。
func (s *server) newBindCode(c echo.Context) error {
	if s.Channels == nil || (s.Channels.Channel(channels.QQ) == nil && s.Channels.Channel(channels.Telegram) == nil) {
		return badRequest("管理员没有开启 QQ 或 Telegram 接入")
	}
	code, exp := s.Channels.NewCode(currentUser(c).ID)
	return c.JSON(http.StatusOK, map[string]any{"code": code, "expires_at": exp})
}

// unbind 处理 DELETE /api/v1/channels/bindings/:id。自己扫码连接的微信同时退出登录。
func (s *server) unbind(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	ctx, u := c.Request().Context(), currentUser(c)
	list, err := s.Store.Bindings().ListByUser(ctx, u.ID)
	if err != nil {
		return err
	}
	for _, b := range list {
		if b.ID == id && b.Channel == channels.Weixin && s.Channels != nil {
			if w := s.Channels.Weixin(); w != nil {
				if acc := w.User(u.ID).Account; acc != nil {
					w.Logout(ctx, u.ID, "")
				}
			}
		}
	}
	if err := s.Store.Bindings().Delete(ctx, u.ID, id); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *server) weixin() (*channels.WeixinChannel, error) {
	if s.Channels != nil {
		if w := s.Channels.Weixin(); w != nil {
			return w, nil
		}
	}
	return nil, badRequest("管理员没有开启个人微信接入")
}

// weixinLogin 处理 POST /api/v1/channels/weixin/login：申请登录二维码，用自己的微信扫码连接。
func (s *server) weixinLogin(c echo.Context) error {
	w, err := s.weixin()
	if err != nil {
		return err
	}
	l, err := w.StartLogin(c.Request().Context(), currentUser(c).ID)
	if err != nil {
		return &apiError{Status: http.StatusFailedDependency, Code: "weixin_error", Message: err.Error()}
	}
	return c.JSON(http.StatusOK, l)
}

// weixinLogout 处理 POST /api/v1/channels/weixin/logout：断开自己的微信。
func (s *server) weixinLogout(c echo.Context) error {
	w, err := s.weixin()
	if err != nil {
		return err
	}
	w.Logout(c.Request().Context(), currentUser(c).ID, "")
	return c.NoContent(http.StatusNoContent)
}

type weixinAccountDTO struct {
	channels.WeixinAccount
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// consoleChannels 处理 GET /api/v1/console/channels：各个聊天软件的连接状态，以及用户连接的微信。
func (s *server) consoleChannels(c echo.Context) error {
	out := map[string]any{"channels": []channels.Status{}, "onebot_path": channels.OneBotPath, "weixin_accounts": []weixinAccountDTO{}}
	if s.Channels == nil {
		return c.JSON(http.StatusOK, out)
	}
	if st := s.Channels.Statuses(); st != nil {
		out["channels"] = st
	}
	if w := s.Channels.Weixin(); w != nil {
		list := []weixinAccountDTO{}
		for _, a := range w.Accounts() {
			d := weixinAccountDTO{WeixinAccount: a}
			if a.UserID != 0 {
				if u, err := s.Store.Users().GetByID(c.Request().Context(), a.UserID); err == nil {
					d.Username, d.DisplayName = u.Username, u.DisplayName
				}
			}
			list = append(list, d)
		}
		out["weixin_accounts"] = list
	}
	return c.JSON(http.StatusOK, out)
}

// consoleWeixinKick 处理 POST /api/v1/console/channels/weixin/:id/logout：断开某个用户的微信（id 为用户 ID，0 是旧版本的账号）。
func (s *server) consoleWeixinKick(c echo.Context) error {
	w, err := s.weixin()
	if err != nil {
		return err
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return badRequest("id 无效")
	}
	w.Logout(c.Request().Context(), id, "管理员断开了你的微信连接")
	return c.NoContent(http.StatusNoContent)
}
