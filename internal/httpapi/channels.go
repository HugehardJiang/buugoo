package httpapi

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/channels"
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

// myChannels 处理 GET /api/v1/channels：开启了哪些聊天软件，以及自己的绑定。
func (s *server) myChannels(c echo.Context) error {
	out := map[string]any{"channels": []channelDTO{}, "bindings": []bindingDTO{}}
	if s.Channels == nil {
		return c.JSON(http.StatusOK, out)
	}
	var chs []channelDTO
	for _, st := range s.Channels.Statuses() {
		chs = append(chs, channelDTO{Name: st.Name, Label: st.Label, Account: st.Account, Connected: st.Connected})
	}
	list, err := s.Store.Bindings().ListByUser(c.Request().Context(), currentUser(c).ID)
	if err != nil {
		return err
	}
	bs := make([]bindingDTO, len(list))
	for i, b := range list {
		bs[i] = bindingDTO{ID: b.ID, Channel: b.Channel, Label: channels.Labels[b.Channel], PeerName: b.PeerName,
			CreatedAt: b.CreatedAt, LastMessageAt: b.LastMessageAt}
	}
	out["channels"], out["bindings"] = chs, bs
	return c.JSON(http.StatusOK, out)
}

// newBindCode 处理 POST /api/v1/channels/code：生成绑定码（10 分钟有效），在聊天软件里发给布谷即可绑定。
func (s *server) newBindCode(c echo.Context) error {
	if s.Channels == nil || len(s.Channels.Enabled()) == 0 {
		return badRequest("管理员还没有开启聊天软件接入")
	}
	code, exp := s.Channels.NewCode(currentUser(c).ID)
	return c.JSON(http.StatusOK, map[string]any{"code": code, "expires_at": exp})
}

// unbind 处理 DELETE /api/v1/channels/bindings/:id。
func (s *server) unbind(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.Store.Bindings().Delete(c.Request().Context(), currentUser(c).ID, id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// consoleChannels 处理 GET /api/v1/console/channels：各个聊天软件的连接状态。
func (s *server) consoleChannels(c echo.Context) error {
	out := map[string]any{"channels": []channels.Status{}, "onebot_path": channels.OneBotPath}
	if s.Channels != nil {
		if st := s.Channels.Statuses(); st != nil {
			out["channels"] = st
		}
		if w, ok := s.Channels.Channel(channels.Weixin).(*channels.WeixinChannel); ok {
			out["weixin_login"] = w.LoginStatus()
		}
	}
	return c.JSON(http.StatusOK, out)
}

func (s *server) weixin() (*channels.WeixinChannel, error) {
	if s.Channels != nil {
		if w, ok := s.Channels.Channel(channels.Weixin).(*channels.WeixinChannel); ok {
			return w, nil
		}
	}
	return nil, badRequest("没有开启个人微信接入：在「系统设置 → 聊天软件」中打开「个人微信」并重启服务")
}

// consoleWeixinLogin 处理 POST /api/v1/console/channels/weixin/login：申请登录二维码。
func (s *server) consoleWeixinLogin(c echo.Context) error {
	w, err := s.weixin()
	if err != nil {
		return err
	}
	l, err := w.StartLogin(c.Request().Context())
	if err != nil {
		return &apiError{Status: http.StatusBadGateway, Code: "weixin_error", Message: err.Error()}
	}
	return c.JSON(http.StatusOK, l)
}

// consoleWeixinLogout 处理 POST /api/v1/console/channels/weixin/logout。
func (s *server) consoleWeixinLogout(c echo.Context) error {
	w, err := s.weixin()
	if err != nil {
		return err
	}
	w.Logout()
	return c.NoContent(http.StatusNoContent)
}
