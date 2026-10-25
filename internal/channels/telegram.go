package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Telegram 通过官方 Bot API 接入：在 @BotFather 创建机器人拿到 token，长轮询收消息。国内服务器需要配置代理。

// TelegramChannel 是 Telegram 接入。
type TelegramChannel struct {
	token string
	base  string
	http  *http.Client
	log   *slog.Logger

	mu      sync.Mutex
	offset  int64
	me      string
	lastErr string
	ok      bool
}

// NewTelegram 创建 Telegram 接入。apiBase 为空时使用 https://api.telegram.org；proxy 为空时使用环境变量中的代理。
func NewTelegram(token, apiBase, proxy string, log *slog.Logger) (*TelegramChannel, error) {
	if log == nil {
		log = slog.Default()
	}
	t := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("Telegram 代理地址不对: %w", err)
		}
		t.Proxy = http.ProxyURL(u)
	}
	if apiBase == "" {
		apiBase = "https://api.telegram.org"
	}
	return &TelegramChannel{token: token, base: strings.TrimRight(apiBase, "/"), http: &http.Client{Transport: t}, log: log.With("channel", Telegram)}, nil
}

// Name 实现 Channel。
func (t *TelegramChannel) Name() string { return Telegram }

// Status 实现 Channel。
func (t *TelegramChannel) Status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := Status{Name: Telegram, Label: Labels[Telegram], Connected: t.ok, Account: t.me}
	if t.lastErr != "" {
		st.Detail = t.lastErr
	}
	return st
}

type tgResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
}

func (t *TelegramChannel) call(ctx context.Context, method string, body any, timeout time.Duration, out any) error {
	b, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.base+"/bot"+t.token+"/"+method, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return t.do(req, out)
}

func (t *TelegramChannel) do(req *http.Request, out any) error {
	resp, err := t.http.Do(req)
	if err != nil {
		// 错误信息里有带 token 的网址，去掉。
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	var r tgResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if !r.OK {
		return fmt.Errorf("Telegram：%s", r.Description)
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

type tgMessage struct {
	Chat struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	From struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Username  string `json:"username"`
		IsBot     bool   `json:"is_bot"`
	} `json:"from"`
	Text    string `json:"text"`
	Caption string `json:"caption"`
	Photo   []struct {
		FileID   string `json:"file_id"`
		FileSize int    `json:"file_size"`
	} `json:"photo"`
	Voice    any `json:"voice"`
	Document any `json:"document"`
	Sticker  any `json:"sticker"`
}

// Run 实现 Channel：长轮询 getUpdates。
func (t *TelegramChannel) Run(ctx context.Context, h Handler) {
	var me struct {
		Username string `json:"username"`
	}
	for ctx.Err() == nil {
		if err := t.call(ctx, "getMe", map[string]any{}, 20*time.Second, &me); err != nil {
			t.fail("连接 Telegram 失败：" + err.Error())
			if !sleepCtx(ctx, 30*time.Second) {
				return
			}
			continue
		}
		break
	}
	t.mu.Lock()
	t.me, t.ok, t.lastErr = "@"+me.Username, true, ""
	t.mu.Unlock()
	t.log.Info("Telegram 机器人已连接", "bot", "@"+me.Username)
	for ctx.Err() == nil {
		var updates []struct {
			UpdateID int64      `json:"update_id"`
			Message  *tgMessage `json:"message"`
		}
		t.mu.Lock()
		offset := t.offset
		t.mu.Unlock()
		err := t.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 30, "allowed_updates": []string{"message"}}, 45*time.Second, &updates)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			t.fail("收消息失败：" + err.Error())
			if !sleepCtx(ctx, 5*time.Second) {
				return
			}
			continue
		}
		t.mu.Lock()
		t.ok, t.lastErr = true, ""
		t.mu.Unlock()
		for _, u := range updates {
			t.mu.Lock()
			t.offset = u.UpdateID + 1
			t.mu.Unlock()
			if m := u.Message; m != nil && m.Chat.Type == "private" && !m.From.IsBot {
				h(ctx, t.inbound(ctx, m))
			}
		}
	}
}

func (t *TelegramChannel) fail(msg string) {
	t.mu.Lock()
	t.ok, t.lastErr = false, msg
	t.mu.Unlock()
	t.log.Warn(msg)
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func (t *TelegramChannel) inbound(ctx context.Context, m *tgMessage) Inbound {
	name := strings.TrimSpace(m.From.FirstName + " " + m.From.LastName)
	if name == "" {
		name = m.From.Username
	}
	in := Inbound{Channel: Telegram, PeerID: strconv.FormatInt(m.Chat.ID, 10), PeerName: name, At: time.Now()}
	in.Text = strings.TrimSpace(m.Text + m.Caption)
	if len(m.Photo) > 0 {
		if img, err := t.download(ctx, m.Photo[len(m.Photo)-1].FileID); err == nil {
			in.Images = append(in.Images, img)
		} else {
			t.log.Warn("下载 Telegram 图片失败", "err", err)
		}
	}
	switch {
	case m.Voice != nil:
		in.Text = strings.TrimSpace(in.Text + " [语音]")
	case m.Document != nil:
		in.Text = strings.TrimSpace(in.Text + " [文件]")
	case m.Sticker != nil && in.Text == "":
		in.Text = "[表情]"
	}
	return in
}

func (t *TelegramChannel) download(ctx context.Context, fileID string) (Image, error) {
	var f struct {
		FilePath string `json:"file_path"`
	}
	if err := t.call(ctx, "getFile", map[string]any{"file_id": fileID}, 20*time.Second, &f); err != nil {
		return Image{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.base+"/file/bot"+t.token+"/"+f.FilePath, nil)
	if err != nil {
		return Image{}, err
	}
	resp, err := t.http.Do(req)
	if err != nil {
		return Image{}, errors.New("下载失败")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	return Image{Name: "telegram.jpg", Data: data}, err
}

// Send 实现 Channel。
func (t *TelegramChannel) Send(ctx context.Context, peer string, out Outbound) error {
	if out.Image == nil {
		return t.call(ctx, "sendMessage", map[string]any{"chat_id": peer, "text": out.Text}, 30*time.Second, nil)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("chat_id", peer)
	fw, _ := mw.CreateFormFile("photo", out.Image.Name)
	_, _ = fw.Write(out.Image.Data)
	_ = mw.Close()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.base+"/bot"+t.token+"/sendPhoto", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return t.do(req, nil)
}

// Typing 实现 Channel：「正在输入」每次持续 5 秒，生成期间每 4 秒续一次。
func (t *TelegramChannel) Typing(ctx context.Context, peer string) func() {
	done := make(chan struct{})
	go func() {
		tick := time.NewTicker(4 * time.Second)
		defer tick.Stop()
		for {
			_ = t.call(ctx, "sendChatAction", map[string]any{"chat_id": peer, "action": "typing"}, 10*time.Second, nil)
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}
