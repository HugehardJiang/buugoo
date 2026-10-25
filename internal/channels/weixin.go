package channels

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// 个人微信通过微信官方的 ClawBot（openclaw-weixin）接口接入：网页上扫码登录，长轮询收消息，不需要公网回调地址。
// 需要手机微信升级到最新版并且有 ClawBot 插件。协议细节参考 AstrBot 的 weixin_oc 适配器。

// weixinCDNURL 是收发图片用的 CDN（测试时替换）。
var weixinCDNURL = "https://novac2c.cdn.weixin.qq.com/c2c"

const (
	weixinBaseURL = "https://ilinkai.weixin.qq.com"

	weixinBotType     = "3"
	weixinLongPoll    = 35 * time.Second
	weixinChannelVer  = "buugoo"
	weixinSessionGone = -14 // 登录已失效，需要重新扫码

	wxItemText  = 1
	wxItemImage = 2
	wxItemVoice = 3
	wxItemFile  = 4
	wxItemVideo = 5

	wxUploadImage = 1
)

// weixinState 是保存在数据目录中的登录态。
type weixinState struct {
	Token     string            `json:"token"`
	AccountID string            `json:"account_id"`
	BaseURL   string            `json:"base_url"`
	SyncBuf   string            `json:"sync_buf"`
	Contexts  map[string]string `json:"context_tokens"` // 用户 → 最近一条消息的 context_token（发消息时需要）
	LoginAt   time.Time         `json:"login_at"`
}

// WeixinLogin 是扫码登录的进展。
type WeixinLogin struct {
	// QRCode 是二维码图片（data: URL）。
	QRCode string `json:"qrcode,omitempty"`
	// Status：wait 等待扫码、scaned 已扫码待确认、confirmed 已登录、expired 已过期、error 出错。
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// WeixinChannel 是个人微信接入。
type WeixinChannel struct {
	path string
	http *http.Client
	log  *slog.Logger

	mu       sync.Mutex
	state    weixinState
	login    *WeixinLogin
	cancel   context.CancelFunc // 取消正在进行的扫码
	loggedIn chan struct{}      // 登录成功时关闭，唤醒收消息的循环
	lastErr  string
}

// NewWeixin 创建个人微信接入，登录态保存在 dir/weixin.json。
func NewWeixin(dir string, log *slog.Logger) *WeixinChannel {
	if log == nil {
		log = slog.Default()
	}
	w := &WeixinChannel{path: filepath.Join(dir, "weixin.json"), log: log.With("channel", Weixin), loggedIn: make(chan struct{}),
		http: &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}}
	if b, err := os.ReadFile(w.path); err == nil {
		_ = json.Unmarshal(b, &w.state)
	}
	if w.state.Contexts == nil {
		w.state.Contexts = map[string]string{}
	}
	return w
}

// Name 实现 Channel。
func (w *WeixinChannel) Name() string { return Weixin }

// Status 实现 Channel。
func (w *WeixinChannel) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := Status{Name: Weixin, Label: Labels[Weixin], Connected: w.state.Token != "", Account: w.state.AccountID}
	switch {
	case w.state.Token == "":
		st.Detail = "还没有登录，在下面扫码登录"
	case w.lastErr != "":
		st.Detail = "收消息出错：" + w.lastErr
	}
	return st
}

func (w *WeixinChannel) save() {
	b, _ := json.MarshalIndent(w.state, "", "  ")
	if err := os.MkdirAll(filepath.Dir(w.path), 0o750); err != nil {
		w.log.Warn("保存微信登录态失败", "err", err)
		return
	}
	if err := os.WriteFile(w.path, b, 0o600); err != nil {
		w.log.Warn("保存微信登录态失败", "err", err)
	}
}

func (w *WeixinChannel) baseURL() string {
	if w.state.BaseURL != "" {
		return strings.TrimRight(w.state.BaseURL, "/")
	}
	return weixinBaseURL
}

// request 调用接口：JSON 请求与响应，带上 ClawBot 需要的请求头。
func (w *WeixinChannel) request(ctx context.Context, method, endpoint string, query url.Values, body any, auth bool, timeout time.Duration, extra map[string]string) (map[string]any, error) {
	w.mu.Lock()
	base, token := w.baseURL(), w.state.Token
	w.mu.Unlock()
	u := base + "/" + endpoint
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("AuthorizationType", "ilink_bot_token")
	n, _ := rand.Int(rand.Reader, big.NewInt(1<<32))
	req.Header.Set("X-WECHAT-UIN", base64.StdEncoding.EncodeToString([]byte(strconv.FormatInt(n.Int64(), 10))))
	if auth && token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := w.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s 失败：HTTP %d %s", endpoint, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	out := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("%s 返回的不是 JSON", endpoint)
		}
	}
	return out, nil
}

func num(m map[string]any, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

func str(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func apiErr(m map[string]any) error {
	if num(m, "ret") == 0 && num(m, "errcode") == 0 {
		return nil
	}
	return fmt.Errorf("ret=%d errcode=%d %s", num(m, "ret"), num(m, "errcode"), str(m, "errmsg"))
}

// ---------- 扫码登录 ----------

// StartLogin 申请登录二维码并在后台等待扫码。已经在扫码时返回当前进展。
func (w *WeixinChannel) StartLogin(ctx context.Context) (*WeixinLogin, error) {
	w.mu.Lock()
	if w.login != nil && (w.login.Status == "wait" || w.login.Status == "scaned") {
		l := *w.login
		w.mu.Unlock()
		return &l, nil
	}
	w.mu.Unlock()
	qr, img, err := w.newQRCode(ctx)
	if err != nil {
		return nil, err
	}
	lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
	}
	w.cancel = cancel
	w.login = &WeixinLogin{QRCode: img, Status: "wait"}
	l := *w.login
	w.mu.Unlock()
	go w.pollLogin(lctx, qr)
	return &l, nil
}

func (w *WeixinChannel) newQRCode(ctx context.Context) (qr, img string, err error) {
	data, err := w.request(ctx, http.MethodGet, "ilink/bot/get_bot_qrcode", url.Values{"bot_type": {weixinBotType}}, nil, false, 15*time.Second, nil)
	if err != nil {
		return "", "", fmt.Errorf("申请登录二维码失败：%w", err)
	}
	qr, content := str(data, "qrcode"), str(data, "qrcode_img_content")
	if qr == "" || content == "" {
		return "", "", errors.New("申请登录二维码失败：返回内容不完整")
	}
	png, err := qrcode.Encode(content, qrcode.Medium, 320)
	if err != nil {
		return "", "", err
	}
	return qr, "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

func (w *WeixinChannel) setLogin(f func(l *WeixinLogin)) {
	w.mu.Lock()
	if w.login != nil {
		f(w.login)
	}
	w.mu.Unlock()
}

func (w *WeixinChannel) pollLogin(ctx context.Context, qr string) {
	refreshed := 0
	for ctx.Err() == nil {
		data, err := w.request(ctx, http.MethodGet, "ilink/bot/get_qrcode_status", url.Values{"qrcode": {qr}}, nil, false,
			weixinLongPoll+5*time.Second, map[string]string{"iLink-App-ClientVersion": "1"})
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				w.log.Warn("查询扫码状态失败", "err", err)
				time.Sleep(2 * time.Second)
			}
			continue
		}
		switch status := str(data, "status"); status {
		case "confirmed":
			token := str(data, "bot_token")
			if token == "" {
				w.setLogin(func(l *WeixinLogin) { l.Status, l.Error = "error", "登录成功但没有返回凭证，请重试" })
				return
			}
			w.mu.Lock()
			w.state = weixinState{Token: token, AccountID: str(data, "ilink_bot_id"), BaseURL: str(data, "baseurl"),
				Contexts: map[string]string{}, LoginAt: time.Now()}
			w.save()
			if w.login != nil {
				w.login.Status, w.login.QRCode = "confirmed", ""
			}
			w.lastErr = ""
			select {
			case <-w.loggedIn:
			default:
				close(w.loggedIn)
			}
			w.mu.Unlock()
			w.log.Info("个人微信已登录", "account", w.state.AccountID)
			return
		case "expired":
			if refreshed++; refreshed > 3 {
				w.setLogin(func(l *WeixinLogin) { l.Status, l.QRCode = "expired", "" })
				return
			}
			nqr, img, err := w.newQRCode(ctx)
			if err != nil {
				w.setLogin(func(l *WeixinLogin) { l.Status, l.Error = "error", err.Error() })
				return
			}
			qr = nqr
			w.setLogin(func(l *WeixinLogin) { l.Status, l.QRCode = "wait", img })
		case "scaned", "scanned":
			w.setLogin(func(l *WeixinLogin) { l.Status = "scaned" })
		}
	}
	w.setLogin(func(l *WeixinLogin) {
		if l.Status == "wait" || l.Status == "scaned" {
			l.Status, l.QRCode = "expired", ""
		}
	})
}

// LoginStatus 返回扫码登录的进展，没有在登录时返回 nil。
func (w *WeixinChannel) LoginStatus() *WeixinLogin {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.login == nil {
		return nil
	}
	l := *w.login
	return &l
}

// Logout 退出登录（清除保存的登录态）。
func (w *WeixinChannel) Logout() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		w.cancel()
	}
	w.state = weixinState{Contexts: map[string]string{}}
	w.login = nil
	w.loggedIn = make(chan struct{})
	w.save()
}

// ---------- 收消息 ----------

// Run 实现 Channel：没有登录时等待扫码，登录后长轮询收消息。
func (w *WeixinChannel) Run(ctx context.Context, h Handler) {
	for ctx.Err() == nil {
		w.mu.Lock()
		token, wait := w.state.Token, w.loggedIn
		w.mu.Unlock()
		if token == "" {
			select {
			case <-ctx.Done():
				return
			case <-wait:
			case <-time.After(time.Minute):
			}
			continue
		}
		if err := w.poll(ctx, h); err != nil && ctx.Err() == nil {
			w.mu.Lock()
			w.lastErr = err.Error()
			w.mu.Unlock()
			w.log.Warn("收微信消息失败，5 秒后重试", "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func (w *WeixinChannel) poll(ctx context.Context, h Handler) error {
	w.mu.Lock()
	buf := w.state.SyncBuf
	w.mu.Unlock()
	data, err := w.request(ctx, http.MethodPost, "ilink/bot/getupdates", nil,
		map[string]any{"base_info": map[string]any{"channel_version": weixinChannelVer}, "get_updates_buf": buf},
		true, weixinLongPoll+5*time.Second, nil)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil // 长轮询超时，继续
		}
		return err
	}
	if err := apiErr(data); err != nil {
		if num(data, "errcode") == weixinSessionGone {
			w.log.Warn("微信登录已失效，需要重新扫码")
			w.Logout()
			return nil
		}
		return err
	}
	w.mu.Lock()
	w.lastErr = ""
	dirty := false
	if b := str(data, "get_updates_buf"); b != "" && b != w.state.SyncBuf {
		w.state.SyncBuf, dirty = b, true
	}
	w.mu.Unlock()
	msgs, _ := data["msgs"].([]any)
	for _, raw := range msgs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if in, ok := w.inbound(ctx, m); ok {
			dirty = true
			h(ctx, in)
		}
	}
	if dirty {
		w.mu.Lock()
		w.save()
		w.mu.Unlock()
	}
	return nil
}

func (w *WeixinChannel) inbound(ctx context.Context, m map[string]any) (Inbound, bool) {
	from := strings.TrimSpace(str(m, "from_user_id"))
	if from == "" || num(m, "message_type") == 2 { // 2 是机器人自己发的
		return Inbound{}, false
	}
	if tok := str(m, "context_token"); tok != "" {
		w.mu.Lock()
		w.state.Contexts[from] = tok
		w.mu.Unlock()
	}
	in := Inbound{Channel: Weixin, PeerID: from, At: time.Now()}
	items, _ := m["item_list"].([]any)
	var texts []string
	for _, raw := range items {
		it, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch num(it, "type") {
		case wxItemText:
			if t, ok := it["text_item"].(map[string]any); ok {
				texts = append(texts, strings.TrimSpace(str(t, "text")))
			}
		case wxItemVoice:
			// 微信会把语音转成文字。
			if v, ok := it["voice_item"].(map[string]any); ok && str(v, "text") != "" {
				texts = append(texts, str(v, "text"))
			} else {
				texts = append(texts, "[语音]")
			}
		case wxItemImage:
			if img, err := w.downloadImage(ctx, it); err == nil {
				in.Images = append(in.Images, img)
			} else {
				w.log.Warn("下载微信图片失败", "err", err)
				texts = append(texts, "[图片]")
			}
		case wxItemFile:
			texts = append(texts, "[文件]")
		case wxItemVideo:
			texts = append(texts, "[视频]")
		}
	}
	in.Text = strings.TrimSpace(strings.Join(texts, "\n"))
	return in, in.Text != "" || len(in.Images) > 0
}

func (w *WeixinChannel) downloadImage(ctx context.Context, it map[string]any) (Image, error) {
	img, _ := it["image_item"].(map[string]any)
	media, _ := img["media"].(map[string]any)
	param := str(media, "encrypt_query_param")
	if param == "" {
		return Image{}, errors.New("没有图片地址")
	}
	var key []byte
	if hexKey := str(img, "aeskey"); hexKey != "" {
		key, _ = hex.DecodeString(hexKey)
	} else if k := str(media, "aes_key"); k != "" {
		key, _ = parseMediaKey(k)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, weixinCDNURL+"/download?encrypted_query_param="+url.QueryEscape(param), nil)
	if err != nil {
		return Image{}, err
	}
	resp, err := w.http.Do(req)
	if err != nil {
		return Image{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return Image{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 30<<20))
	if err != nil {
		return Image{}, err
	}
	if len(key) == 16 {
		if data, err = aesECBDecrypt(key, data); err != nil {
			return Image{}, err
		}
	}
	return Image{Name: "微信图片.jpg", Data: data}, nil
}

// parseMediaKey 解析 base64 编码的 AES 密钥：可能直接是 16 字节，也可能是 32 位十六进制字符串。
func parseMediaKey(v string) ([]byte, error) {
	v = strings.TrimSpace(v)
	if m := len(v) % 4; m != 0 {
		v += strings.Repeat("=", 4-m)
	}
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return nil, err
	}
	if len(b) == 16 {
		return b, nil
	}
	if len(b) == 32 {
		return hex.DecodeString(string(b))
	}
	return nil, errors.New("不支持的密钥格式")
}

func aesECBDecrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, errors.New("密文长度不对")
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += aes.BlockSize {
		block.Decrypt(out[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
	}
	if n := int(out[len(out)-1]); n > 0 && n <= aes.BlockSize && bytes.Equal(out[len(out)-n:], bytes.Repeat([]byte{byte(n)}, n)) {
		out = out[:len(out)-n]
	}
	return out, nil
}

func aesECBEncrypt(key, data []byte) []byte {
	block, _ := aes.NewCipher(key)
	pad := aes.BlockSize - len(data)%aes.BlockSize
	data = append(append([]byte{}, data...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += aes.BlockSize {
		block.Encrypt(out[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
	}
	return out
}

// ---------- 发消息 ----------

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Send 实现 Channel。需要对方之前发过消息（用它的 context_token 回复）。
func (w *WeixinChannel) Send(ctx context.Context, peer string, out Outbound) error {
	w.mu.Lock()
	tok := w.state.Contexts[peer]
	loggedIn := w.state.Token != ""
	w.mu.Unlock()
	if !loggedIn {
		return errors.New("微信没有登录")
	}
	if tok == "" {
		return errors.New("对方还没有在微信里发过消息，暂时不能主动发")
	}
	var item map[string]any
	if out.Image != nil {
		var err error
		if item, err = w.uploadImage(ctx, peer, out.Image.Data); err != nil {
			return err
		}
	} else {
		item = map[string]any{"type": wxItemText, "text_item": map[string]any{"text": out.Text}}
	}
	data, err := w.request(ctx, http.MethodPost, "ilink/bot/sendmessage", nil, map[string]any{
		"base_info": map[string]any{"channel_version": weixinChannelVer},
		"msg": map[string]any{
			"from_user_id": "", "to_user_id": peer, "client_id": randomHex(16), "message_type": 2, "message_state": 2,
			"context_token": tok, "item_list": []any{item},
		},
	}, true, 30*time.Second, nil)
	if err != nil {
		return err
	}
	return apiErr(data)
}

func (w *WeixinChannel) uploadImage(ctx context.Context, peer string, raw []byte) (map[string]any, error) {
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	keyHex := hex.EncodeToString(key)
	sum := md5.Sum(raw)
	cipherSize := len(raw) + (aes.BlockSize - len(raw)%aes.BlockSize)
	fileKey := randomHex(16)
	data, err := w.request(ctx, http.MethodPost, "ilink/bot/getuploadurl", nil, map[string]any{
		"filekey": fileKey, "media_type": wxUploadImage, "to_user_id": peer, "rawsize": len(raw), "rawfilemd5": hex.EncodeToString(sum[:]),
		"filesize": cipherSize, "no_need_thumb": true, "aeskey": keyHex,
		"base_info": map[string]any{"channel_version": weixinChannelVer},
	}, true, 30*time.Second, nil)
	if err != nil {
		return nil, err
	}
	if err := apiErr(data); err != nil {
		return nil, err
	}
	upURL := str(data, "upload_full_url")
	if upURL == "" {
		param := str(data, "upload_param")
		if param == "" {
			return nil, errors.New("没有拿到图片上传地址")
		}
		upURL = weixinCDNURL + "/upload?encrypted_query_param=" + url.QueryEscape(param) + "&filekey=" + url.QueryEscape(fileKey)
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, upURL, bytes.NewReader(aesECBEncrypt(key, raw)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := w.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	download := resp.Header.Get("x-encrypted-param")
	if resp.StatusCode != http.StatusOK || download == "" {
		return nil, fmt.Errorf("上传图片失败：HTTP %d %s", resp.StatusCode, resp.Header.Get("x-error-message"))
	}
	return map[string]any{"type": wxItemImage, "image_item": map[string]any{
		"media":    map[string]any{"encrypt_query_param": download, "aes_key": base64.StdEncoding.EncodeToString([]byte(keyHex)), "encrypt_type": 1},
		"mid_size": cipherSize,
	}}, nil
}

// Typing 实现 Channel：显示「对方正在输入」，每 5 秒续一次。
func (w *WeixinChannel) Typing(ctx context.Context, peer string) func() {
	w.mu.Lock()
	tok := w.state.Contexts[peer]
	w.mu.Unlock()
	if tok == "" {
		return func() {}
	}
	data, err := w.request(ctx, http.MethodPost, "ilink/bot/getconfig", nil, map[string]any{
		"ilink_user_id": peer, "context_token": tok, "base_info": map[string]any{"channel_version": weixinChannelVer},
	}, true, 10*time.Second, nil)
	ticket := str(data, "typing_ticket")
	if err != nil || apiErr(data) != nil || ticket == "" {
		return func() {}
	}
	send := func(status int) {
		_, _ = w.request(context.WithoutCancel(ctx), http.MethodPost, "ilink/bot/sendtyping", nil, map[string]any{
			"ilink_user_id": peer, "typing_ticket": ticket, "status": status, "base_info": map[string]any{"channel_version": weixinChannelVer},
		}, true, 10*time.Second, nil)
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		send(1)
		for {
			select {
			case <-done:
				send(2)
				return
			case <-ctx.Done():
				return
			case <-t.C:
				send(1)
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}
