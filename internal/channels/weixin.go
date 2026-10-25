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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// 个人微信通过微信官方的 ClawBot（openclaw-weixin）接口接入：用户在网页上扫码登录自己的微信，长轮询收消息，不需要公网回调地址。
// 需要手机微信升级到最新版并且有 ClawBot 插件。协议细节参考 AstrBot 的 weixin_oc 适配器。

// weixinCDNURL 是收发图片、文件用的 CDN，weixinBaseURL 是接口地址（测试时替换）。
var (
	weixinCDNURL  = "https://novac2c.cdn.weixin.qq.com/c2c"
	weixinBaseURL = "https://ilinkai.weixin.qq.com"
)

const (
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
	wxUploadFile  = 3
)

// weixinState 是一个微信账号保存在数据目录中的登录态。
type weixinState struct {
	Token     string            `json:"token"`
	AccountID string            `json:"account_id"`
	BaseURL   string            `json:"base_url"`
	SyncBuf   string            `json:"sync_buf"`
	Contexts  map[string]string `json:"context_tokens"` // 用户 → 最近一条消息的 context_token（发消息时需要）
	LoginAt   time.Time         `json:"login_at"`
	// OwnerPeer 是扫码登录的那个微信（ilink_user_id），它发来的消息自动算作 Owner 的。
	OwnerPeer string `json:"owner_peer,omitempty"`
}

// WeixinLogin 是扫码登录的进展。
type WeixinLogin struct {
	// QRCode 是二维码图片（data: URL）。
	QRCode string `json:"qrcode,omitempty"`
	// Status：wait 等待扫码、scaned 已扫码待确认、confirmed 已登录、expired 已过期、error 出错。
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// WeixinAccount 是一个用户登录的微信（管理后台与设置页显示）。
type WeixinAccount struct {
	UserID  uint64    `json:"user_id"`
	Account string    `json:"account"`
	LoginAt time.Time `json:"login_at"`
	Error   string    `json:"error,omitempty"`
	// Legacy 表示旧版本由管理员扫码登录、不属于某个用户的账号，靠绑定码使用。
	Legacy bool `json:"legacy,omitempty"`
}

// weixinMaxLogins 是同时进行的扫码登录数上限。
const weixinMaxLogins = 20

// WeixinChannel 是个人微信接入。ClawBot 是扫码人自己微信里的一个机器人，基本只有扫码的人自己能和它聊，
// 所以每个用户扫自己的码：每个用户一个账号，登录后自动绑定到这个用户。
type WeixinChannel struct {
	dir  string // 登录态保存在 dir/<用户 ID>.json
	http *http.Client
	log  *slog.Logger

	// OnLogin 在用户扫码登录成功、或者第一次收到扫码人的消息时调用（peer 为扫码人的微信）。
	OnLogin func(ctx context.Context, userID uint64, peer string)
	// OnLogout 在账号退出登录（用户断开、管理员踢下线、登录失效）时调用。
	OnLogout func(ctx context.Context, userID uint64, peer string)

	mu       sync.Mutex
	accounts map[uint64]*weixinAccount // 用户 ID → 账号；0 是旧版本管理员扫码登录的账号
	logins   map[uint64]*weixinLoginSession
	notices  map[uint64]string // 登录失效等需要告诉用户的事
	runCtx   context.Context
	handler  Handler
}

type weixinLoginSession struct {
	WeixinLogin
	cancel context.CancelFunc
}

type weixinAccount struct {
	w     *WeixinChannel
	owner uint64
	path  string

	mu      sync.Mutex
	state   weixinState
	lastErr string
	cancel  context.CancelFunc
}

// NewWeixin 创建个人微信接入，各个账号的登录态保存在 dir/weixin/ 中。
// 旧版本（dir/weixin.json，管理员扫码）的登录态继续可用。
func NewWeixin(dir string, log *slog.Logger) *WeixinChannel {
	if log == nil {
		log = slog.Default()
	}
	w := &WeixinChannel{dir: filepath.Join(dir, "weixin"), log: log.With("channel", Weixin),
		http:     &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}},
		accounts: map[uint64]*weixinAccount{}, logins: map[uint64]*weixinLoginSession{}, notices: map[uint64]string{}}
	if a := w.loadAccount(filepath.Join(dir, "weixin.json"), 0); a != nil {
		w.accounts[0] = a
	}
	entries, _ := os.ReadDir(w.dir)
	for _, e := range entries {
		id, err := strconv.ParseUint(strings.TrimSuffix(e.Name(), ".json"), 10, 64)
		if err != nil || id == 0 || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if a := w.loadAccount(filepath.Join(w.dir, e.Name()), id); a != nil {
			w.accounts[id] = a
		}
	}
	return w
}

func (w *WeixinChannel) loadAccount(path string, owner uint64) *weixinAccount {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	a := &weixinAccount{w: w, owner: owner, path: path}
	if json.Unmarshal(b, &a.state) != nil || a.state.Token == "" {
		return nil
	}
	if a.state.Contexts == nil {
		a.state.Contexts = map[string]string{}
	}
	return a
}

// Name 实现 Channel。
func (w *WeixinChannel) Name() string { return Weixin }

// Status 实现 Channel。
func (w *WeixinChannel) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := Status{Name: Weixin, Label: Labels[Weixin], Connected: len(w.accounts) > 0}
	if n := len(w.accounts); n > 0 {
		st.Account = fmt.Sprintf("%d 个微信已连接", n)
	} else {
		st.Detail = "还没有用户连接微信。用户在「聊天软件」页里扫码连接自己的微信"
	}
	return st
}

// Accounts 返回已登录的微信账号，按登录时间排序。
func (w *WeixinChannel) Accounts() []WeixinAccount {
	w.mu.Lock()
	list := make([]*weixinAccount, 0, len(w.accounts))
	for _, a := range w.accounts {
		list = append(list, a)
	}
	w.mu.Unlock()
	out := make([]WeixinAccount, 0, len(list))
	for _, a := range list {
		out = append(out, a.info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LoginAt.Before(out[j].LoginAt) })
	return out
}

func (a *weixinAccount) info() WeixinAccount {
	a.mu.Lock()
	defer a.mu.Unlock()
	return WeixinAccount{UserID: a.owner, Account: a.state.AccountID, LoginAt: a.state.LoginAt, Error: a.lastErr, Legacy: a.owner == 0}
}

// WeixinUser 是一个用户自己的微信连接情况（设置页显示）。
type WeixinUser struct {
	Account *WeixinAccount `json:"account"`
	Login   *WeixinLogin   `json:"login"`
	// Notice 是需要告诉用户的事，例如登录已经失效。
	Notice string `json:"notice,omitempty"`
}

// User 返回用户自己的微信连接情况。
func (w *WeixinChannel) User(userID uint64) WeixinUser {
	w.mu.Lock()
	a := w.accounts[userID]
	var out WeixinUser
	if l := w.logins[userID]; l != nil {
		cp := l.WeixinLogin
		out.Login = &cp
	}
	out.Notice = w.notices[userID]
	w.mu.Unlock()
	if a != nil && userID != 0 {
		info := a.info()
		out.Account = &info
	}
	return out
}

func (a *weixinAccount) save() {
	a.mu.Lock()
	b, _ := json.MarshalIndent(a.state, "", "  ")
	a.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(a.path), 0o750); err != nil {
		a.w.log.Warn("保存微信登录态失败", "err", err)
		return
	}
	if err := os.WriteFile(a.path, b, 0o600); err != nil {
		a.w.log.Warn("保存微信登录态失败", "err", err)
	}
}

func (a *weixinAccount) baseURL() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state.BaseURL != "" {
		return strings.TrimRight(a.state.BaseURL, "/")
	}
	return weixinBaseURL
}

// request 用这个账号的凭证调用接口。
func (a *weixinAccount) request(ctx context.Context, method, endpoint string, body any, timeout time.Duration) (map[string]any, error) {
	a.mu.Lock()
	token := a.state.Token
	a.mu.Unlock()
	return a.w.request(ctx, a.baseURL(), token, method, endpoint, nil, body, timeout, nil)
}

// request 调用接口：JSON 请求与响应，带上 ClawBot 需要的请求头。token 为空时不带凭证（扫码登录）。
func (w *WeixinChannel) request(ctx context.Context, base, token, method, endpoint string, query url.Values, body any, timeout time.Duration, extra map[string]string) (map[string]any, error) {
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
	if token != "" {
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

// StartLogin 为用户申请登录二维码并在后台等待扫码。这个用户已经在扫码时返回当前进展。
func (w *WeixinChannel) StartLogin(ctx context.Context, userID uint64) (*WeixinLogin, error) {
	w.mu.Lock()
	if l := w.logins[userID]; l != nil && (l.Status == "wait" || l.Status == "scaned") {
		cp := l.WeixinLogin
		w.mu.Unlock()
		return &cp, nil
	}
	active := 0
	for _, l := range w.logins {
		if l.Status == "wait" || l.Status == "scaned" {
			active++
		}
	}
	w.mu.Unlock()
	if active >= weixinMaxLogins {
		return nil, errors.New("同时扫码的人太多了，过一会儿再试")
	}
	qr, img, err := w.newQRCode(ctx)
	if err != nil {
		return nil, err
	}
	lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
	w.mu.Lock()
	if old := w.logins[userID]; old != nil && old.cancel != nil {
		old.cancel()
	}
	l := &weixinLoginSession{WeixinLogin: WeixinLogin{QRCode: img, Status: "wait"}, cancel: cancel}
	w.logins[userID] = l
	delete(w.notices, userID)
	cp := l.WeixinLogin
	w.mu.Unlock()
	go w.pollLogin(lctx, userID, l, qr)
	return &cp, nil
}

func (w *WeixinChannel) newQRCode(ctx context.Context) (qr, img string, err error) {
	data, err := w.request(ctx, weixinBaseURL, "", http.MethodGet, "ilink/bot/get_bot_qrcode", url.Values{"bot_type": {weixinBotType}}, nil, 15*time.Second, nil)
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

func (w *WeixinChannel) setLogin(l *weixinLoginSession, f func(l *WeixinLogin)) {
	w.mu.Lock()
	f(&l.WeixinLogin)
	w.mu.Unlock()
}

func (w *WeixinChannel) pollLogin(ctx context.Context, userID uint64, l *weixinLoginSession, qr string) {
	refreshed := 0
	for ctx.Err() == nil {
		data, err := w.request(ctx, weixinBaseURL, "", http.MethodGet, "ilink/bot/get_qrcode_status", url.Values{"qrcode": {qr}}, nil,
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
				w.setLogin(l, func(l *WeixinLogin) { l.Status, l.Error = "error", "登录成功但没有返回凭证，请重试" })
				return
			}
			st := weixinState{Token: token, AccountID: str(data, "ilink_bot_id"), BaseURL: str(data, "baseurl"),
				OwnerPeer: str(data, "ilink_user_id"), Contexts: map[string]string{}, LoginAt: time.Now()}
			w.setLogin(l, func(l *WeixinLogin) { l.Status, l.QRCode = "confirmed", "" })
			w.addAccount(ctx, userID, st)
			return
		case "expired":
			if refreshed++; refreshed > 3 {
				w.setLogin(l, func(l *WeixinLogin) { l.Status, l.QRCode = "expired", "" })
				return
			}
			nqr, img, err := w.newQRCode(ctx)
			if err != nil {
				w.setLogin(l, func(l *WeixinLogin) { l.Status, l.Error = "error", err.Error() })
				return
			}
			qr = nqr
			w.setLogin(l, func(l *WeixinLogin) { l.Status, l.QRCode = "wait", img })
		case "scaned", "scanned":
			w.setLogin(l, func(l *WeixinLogin) { l.Status = "scaned" })
		}
	}
	w.setLogin(l, func(l *WeixinLogin) {
		if l.Status == "wait" || l.Status == "scaned" {
			l.Status, l.QRCode = "expired", ""
		}
	})
}

// addAccount 保存登录成功的账号（替换这个用户之前的账号）并开始收消息。
func (w *WeixinChannel) addAccount(ctx context.Context, userID uint64, st weixinState) {
	a := &weixinAccount{w: w, owner: userID, path: filepath.Join(w.dir, strconv.FormatUint(userID, 10)+".json"), state: st}
	w.mu.Lock()
	old := w.accounts[userID]
	w.accounts[userID] = a
	w.mu.Unlock()
	if old != nil {
		old.stop()
		if old.state.OwnerPeer != "" && old.state.OwnerPeer != st.OwnerPeer && w.OnLogout != nil {
			w.OnLogout(context.WithoutCancel(ctx), userID, old.state.OwnerPeer)
		}
	}
	a.save()
	w.log.Info("个人微信已登录", "user_id", userID, "account", st.AccountID)
	if st.OwnerPeer != "" && w.OnLogin != nil {
		w.OnLogin(context.WithoutCancel(ctx), userID, st.OwnerPeer)
	}
	w.start(a)
}

// LoginStatus 返回用户扫码登录的进展，没有在登录时返回 nil。
func (w *WeixinChannel) LoginStatus(userID uint64) *WeixinLogin {
	w.mu.Lock()
	defer w.mu.Unlock()
	if l := w.logins[userID]; l != nil {
		cp := l.WeixinLogin
		return &cp
	}
	return nil
}

// Logout 让用户的微信退出登录（清除保存的登录态）。reason 不为空时记下来告诉用户。
func (w *WeixinChannel) Logout(ctx context.Context, userID uint64, reason string) {
	w.mu.Lock()
	a := w.accounts[userID]
	delete(w.accounts, userID)
	if l := w.logins[userID]; l != nil {
		if l.cancel != nil {
			l.cancel()
		}
		delete(w.logins, userID)
	}
	if reason != "" {
		w.notices[userID] = reason
	} else {
		delete(w.notices, userID)
	}
	w.mu.Unlock()
	if a == nil {
		return
	}
	a.stop()
	_ = os.Remove(a.path)
	a.mu.Lock()
	peer := a.state.OwnerPeer
	a.mu.Unlock()
	if peer != "" && userID != 0 && w.OnLogout != nil {
		w.OnLogout(context.WithoutCancel(ctx), userID, peer)
	}
	w.log.Info("个人微信已退出登录", "user_id", userID)
}

func (a *weixinAccount) stop() {
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.mu.Unlock()
}

// ---------- 收消息 ----------

// Run 实现 Channel：每个已登录的账号各自长轮询收消息，新扫码登录的账号随后加入。
func (w *WeixinChannel) Run(ctx context.Context, h Handler) {
	w.mu.Lock()
	w.runCtx, w.handler = ctx, h
	list := make([]*weixinAccount, 0, len(w.accounts))
	for _, a := range w.accounts {
		list = append(list, a)
	}
	w.mu.Unlock()
	for _, a := range list {
		w.start(a)
	}
	<-ctx.Done()
}

// start 开始收这个账号的消息（Run 之前登录的账号等 Run 时再开始）。
func (w *WeixinChannel) start(a *weixinAccount) {
	w.mu.Lock()
	ctx, h := w.runCtx, w.handler
	w.mu.Unlock()
	if ctx == nil {
		return
	}
	actx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.cancel = cancel
	a.mu.Unlock()
	go a.run(actx, h)
}

func (a *weixinAccount) run(ctx context.Context, h Handler) {
	for ctx.Err() == nil {
		if err := a.poll(ctx, h); err != nil && ctx.Err() == nil {
			a.mu.Lock()
			a.lastErr = err.Error()
			a.mu.Unlock()
			a.w.log.Warn("收微信消息失败，5 秒后重试", "user_id", a.owner, "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func (a *weixinAccount) poll(ctx context.Context, h Handler) error {
	a.mu.Lock()
	buf := a.state.SyncBuf
	a.mu.Unlock()
	data, err := a.request(ctx, http.MethodPost, "ilink/bot/getupdates",
		map[string]any{"base_info": map[string]any{"channel_version": weixinChannelVer}, "get_updates_buf": buf},
		weixinLongPoll+5*time.Second)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return nil // 长轮询超时，继续
		}
		return err
	}
	if err := apiErr(data); err != nil {
		if num(data, "errcode") == weixinSessionGone {
			a.w.log.Warn("微信登录已失效，需要重新扫码", "user_id", a.owner)
			a.w.Logout(ctx, a.owner, "微信登录已经失效（可能在手机上退出了），重新扫码连接就好")
			return nil
		}
		return err
	}
	a.mu.Lock()
	a.lastErr = ""
	dirty := false
	if b := str(data, "get_updates_buf"); b != "" && b != a.state.SyncBuf {
		a.state.SyncBuf, dirty = b, true
	}
	a.mu.Unlock()
	msgs, _ := data["msgs"].([]any)
	for _, raw := range msgs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if in, ok := a.inbound(ctx, m); ok {
			dirty = true
			h(ctx, in)
		}
	}
	if dirty {
		a.save()
	}
	return nil
}

func (a *weixinAccount) inbound(ctx context.Context, m map[string]any) (Inbound, bool) {
	from := strings.TrimSpace(str(m, "from_user_id"))
	if from == "" || num(m, "message_type") == 2 { // 2 是机器人自己发的
		return Inbound{}, false
	}
	in := Inbound{Channel: Weixin, PeerID: from, At: time.Now()}
	firstOwner := false
	a.mu.Lock()
	if tok := str(m, "context_token"); tok != "" {
		a.state.Contexts[from] = tok
	}
	if a.owner != 0 {
		if a.state.OwnerPeer == "" {
			// 登录时没有返回扫码人：第一个发消息的人就是扫码人自己。
			a.state.OwnerPeer, firstOwner = from, true
		}
		if a.state.OwnerPeer == from {
			in.Owner = a.owner
		}
	}
	a.mu.Unlock()
	if firstOwner && a.w.OnLogin != nil {
		a.w.OnLogin(ctx, a.owner, from)
	}
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
			if img, err := a.w.downloadImage(ctx, it); err == nil {
				in.Files = append(in.Files, img)
			} else {
				a.w.log.Warn("下载微信图片失败", "err", err)
				texts = append(texts, "[图片]")
			}
		case wxItemFile:
			if f, err := a.w.downloadFile(ctx, it); err == nil {
				in.Files = append(in.Files, f)
			} else {
				a.w.log.Warn("下载微信文件失败", "err", err)
				texts = append(texts, "[文件]")
			}
		case wxItemVideo:
			texts = append(texts, "[视频]")
		}
	}
	in.Text = strings.TrimSpace(strings.Join(texts, "\n"))
	return in, in.Text != "" || len(in.Files) > 0
}

// downloadFile 下载私聊发来的文件。
func (w *WeixinChannel) downloadFile(ctx context.Context, it map[string]any) (File, error) {
	fi, _ := it["file_item"].(map[string]any)
	media, _ := fi["media"].(map[string]any)
	param := str(media, "encrypt_query_param")
	if param == "" {
		return File{}, errors.New("没有文件地址")
	}
	key, err := parseMediaKey(str(media, "aes_key"))
	if err != nil {
		return File{}, err
	}
	data, err := w.downloadCDN(ctx, param, key)
	if err != nil {
		return File{}, err
	}
	name := filepath.Base(strings.TrimSpace(str(fi, "file_name")))
	if name == "" || name == "." || name == "/" {
		name = "微信文件"
	}
	return File{Name: name, Data: data}, nil
}

func (w *WeixinChannel) downloadImage(ctx context.Context, it map[string]any) (File, error) {
	img, _ := it["image_item"].(map[string]any)
	media, _ := img["media"].(map[string]any)
	param := str(media, "encrypt_query_param")
	if param == "" {
		return File{}, errors.New("没有图片地址")
	}
	var key []byte
	if hexKey := str(img, "aeskey"); hexKey != "" {
		key, _ = hex.DecodeString(hexKey)
	} else if k := str(media, "aes_key"); k != "" {
		key, _ = parseMediaKey(k)
	}
	data, err := w.downloadCDN(ctx, param, key)
	if err != nil {
		return File{}, err
	}
	return File{Name: "微信图片.jpg", Data: data}, nil
}

// downloadCDN 从 CDN 下载媒体，key 不为空时解密。
func (w *WeixinChannel) downloadCDN(ctx context.Context, param string, key []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, weixinCDNURL+"/download?encrypted_query_param="+url.QueryEscape(param), nil)
	if err != nil {
		return nil, err
	}
	resp, err := w.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFileBytes+aes.BlockSize))
	if err != nil {
		return nil, err
	}
	if len(key) == 16 {
		return aesECBDecrypt(key, data)
	}
	return data, nil
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

// account 找到能给 peer 发消息的账号：收到过 peer 消息的那个（有多个时用登录最晚的）。
func (w *WeixinChannel) account(peer string) (*weixinAccount, string) {
	w.mu.Lock()
	list := make([]*weixinAccount, 0, len(w.accounts))
	for _, a := range w.accounts {
		list = append(list, a)
	}
	w.mu.Unlock()
	var best *weixinAccount
	var bestTok string
	var bestAt time.Time
	for _, a := range list {
		a.mu.Lock()
		tok, at, owner := a.state.Contexts[peer], a.state.LoginAt, a.state.OwnerPeer == peer
		a.mu.Unlock()
		if (tok != "" || owner) && (best == nil || at.After(bestAt)) {
			best, bestTok, bestAt = a, tok, at
		}
	}
	return best, bestTok
}

// Send 实现 Channel。需要对方之前发过消息（用它的 context_token 回复）。
func (w *WeixinChannel) Send(ctx context.Context, peer string, out Outbound) error {
	a, tok := w.account(peer)
	if a == nil {
		return errors.New("这个微信没有连接")
	}
	if tok == "" {
		return errors.New("对方还没有在微信里发过消息，暂时不能主动发")
	}
	var item map[string]any
	if out.File != nil {
		var err error
		if item, err = a.upload(ctx, peer, *out.File); err != nil {
			return err
		}
	} else {
		item = map[string]any{"type": wxItemText, "text_item": map[string]any{"text": out.Text}}
	}
	data, err := a.request(ctx, http.MethodPost, "ilink/bot/sendmessage", map[string]any{
		"base_info": map[string]any{"channel_version": weixinChannelVer},
		"msg": map[string]any{
			"from_user_id": "", "to_user_id": peer, "client_id": randomHex(16), "message_type": 2, "message_state": 2,
			"context_token": tok, "item_list": []any{item},
		},
	}, 30*time.Second)
	if err != nil {
		return err
	}
	return apiErr(data)
}

// upload 把图片或文件加密上传到 CDN，返回消息里的一项。
func (a *weixinAccount) upload(ctx context.Context, peer string, f File) (map[string]any, error) {
	raw := f.Data
	image := f.IsImage()
	mediaType := wxUploadFile
	if image {
		mediaType = wxUploadImage
	}
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	keyHex := hex.EncodeToString(key)
	sum := md5.Sum(raw)
	cipherSize := len(raw) + (aes.BlockSize - len(raw)%aes.BlockSize)
	fileKey := randomHex(16)
	data, err := a.request(ctx, http.MethodPost, "ilink/bot/getuploadurl", map[string]any{
		"filekey": fileKey, "media_type": mediaType, "to_user_id": peer, "rawsize": len(raw), "rawfilemd5": hex.EncodeToString(sum[:]),
		"filesize": cipherSize, "no_need_thumb": true, "aeskey": keyHex,
		"base_info": map[string]any{"channel_version": weixinChannelVer},
	}, 30*time.Second)
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
			return nil, errors.New("没有拿到上传地址")
		}
		upURL = weixinCDNURL + "/upload?encrypted_query_param=" + url.QueryEscape(param) + "&filekey=" + url.QueryEscape(fileKey)
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, upURL, bytes.NewReader(aesECBEncrypt(key, raw)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := a.w.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	download := resp.Header.Get("x-encrypted-param")
	if resp.StatusCode != http.StatusOK || download == "" {
		return nil, fmt.Errorf("上传失败：HTTP %d %s", resp.StatusCode, resp.Header.Get("x-error-message"))
	}
	media := map[string]any{"encrypt_query_param": download, "aes_key": base64.StdEncoding.EncodeToString([]byte(keyHex)), "encrypt_type": 1}
	if image {
		return map[string]any{"type": wxItemImage, "image_item": map[string]any{"media": media, "mid_size": cipherSize}}, nil
	}
	return map[string]any{"type": wxItemFile, "file_item": map[string]any{"media": media, "file_name": f.Name, "len": strconv.Itoa(len(raw))}}, nil
}

// Typing 实现 Channel：显示「对方正在输入」，每 5 秒续一次。
func (w *WeixinChannel) Typing(ctx context.Context, peer string) func() {
	a, tok := w.account(peer)
	if tok == "" {
		return func() {}
	}
	data, err := a.request(ctx, http.MethodPost, "ilink/bot/getconfig", map[string]any{
		"ilink_user_id": peer, "context_token": tok, "base_info": map[string]any{"channel_version": weixinChannelVer},
	}, 10*time.Second)
	ticket := str(data, "typing_ticket")
	if err != nil || apiErr(data) != nil || ticket == "" {
		return func() {}
	}
	send := func(status int) {
		_, _ = a.request(context.WithoutCancel(ctx), http.MethodPost, "ilink/bot/sendtyping", map[string]any{
			"ilink_user_id": peer, "typing_ticket": ticket, "status": status, "base_info": map[string]any{"channel_version": weixinChannelVer},
		}, 10*time.Second)
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
