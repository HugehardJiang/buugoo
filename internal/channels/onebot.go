package channels

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/HugehardJiang/buugoo/internal/netguard"
)

// QQ 通过 OneBot v11 协议接入（NapCat、Lagrange 等）：在 NapCat 中添加「WebSocket 客户端」（反向 WebSocket），
// 地址填 ws://布谷的地址/onebot/v11/ws，令牌填 channels.qq.access_token。只处理私聊。

// OneBotPath 是反向 WebSocket 的地址。
const OneBotPath = "/onebot/v11/ws"

// OneBotChannel 是 QQ（OneBot v11）接入。
type OneBotChannel struct {
	token      string
	autoAccept bool
	log        *slog.Logger
	media      *http.Client

	mu      sync.Mutex
	conn    *websocket.Conn
	writeMu sync.Mutex
	selfID  string
	pending map[string]chan map[string]any
	seq     int
	h       Handler
	ctx     context.Context
	since   time.Time
}

// NewOneBot 创建 QQ 接入。token 是 NapCat 连接时必须带的访问令牌；autoAccept 为 true 时自动通过好友申请。
func NewOneBot(token string, autoAccept bool, log *slog.Logger) *OneBotChannel {
	if log == nil {
		log = slog.Default()
	}
	return &OneBotChannel{token: token, autoAccept: autoAccept, log: log.With("channel", QQ), pending: map[string]chan map[string]any{},
		media: &http.Client{Timeout: 60 * time.Second, Transport: netguard.PublicTransport()}}
}

// Name 实现 Channel。
func (o *OneBotChannel) Name() string { return QQ }

// Status 实现 Channel。
func (o *OneBotChannel) Status() Status {
	o.mu.Lock()
	defer o.mu.Unlock()
	st := Status{Name: QQ, Label: Labels[QQ], Connected: o.conn != nil, Account: o.selfID}
	if o.conn == nil {
		st.Detail = "等待 NapCat 连接：在 NapCat 中添加 WebSocket 客户端，地址填 ws://布谷的地址" + OneBotPath
	}
	return st
}

// Run 实现 Channel：消息来自 NapCat 的连接（ServeHTTP），这里只保存处理函数并等待退出。
func (o *OneBotChannel) Run(ctx context.Context, h Handler) {
	o.mu.Lock()
	o.h, o.ctx = h, ctx
	o.mu.Unlock()
	<-ctx.Done()
	o.mu.Lock()
	if o.conn != nil {
		_ = o.conn.Close()
	}
	o.mu.Unlock()
}

var upgrader = websocket.Upgrader{
	ReadBufferSize: 64 << 10, WriteBufferSize: 64 << 10,
	// NapCat 不是浏览器，不带 Origin；带了也不校验来源，靠令牌鉴权。
	CheckOrigin: func(*http.Request) bool { return true },
}

func (o *OneBotChannel) authorized(r *http.Request) bool {
	got := r.URL.Query().Get("access_token")
	if a := r.Header.Get("Authorization"); a != "" {
		got = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(a, "Bearer "), "Token "))
	}
	return o.token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(o.token)) == 1
}

// ServeHTTP 接受 NapCat 的反向 WebSocket 连接。新连接会替换旧连接。
func (o *OneBotChannel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !o.authorized(r) {
		http.Error(w, "access token 不正确", http.StatusUnauthorized)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(32 << 20)
	o.mu.Lock()
	if o.conn != nil {
		_ = o.conn.Close()
	}
	o.conn, o.selfID, o.since = conn, r.Header.Get("X-Self-ID"), time.Now()
	h, ctx := o.h, o.ctx
	o.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	o.log.Info("NapCat 已连接", "qq", o.selfID)
	defer func() {
		o.mu.Lock()
		if o.conn == conn {
			o.conn = nil
		}
		o.mu.Unlock()
		_ = conn.Close()
		o.log.Info("NapCat 已断开")
	}()
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var ev map[string]any
		if json.Unmarshal(data, &ev) != nil {
			continue
		}
		if echo, ok := ev["echo"].(string); ok && ev["post_type"] == nil {
			o.mu.Lock()
			ch := o.pending[echo]
			delete(o.pending, echo)
			o.mu.Unlock()
			if ch != nil {
				ch <- ev
			}
			continue
		}
		go o.event(ctx, h, ev)
	}
}

func (o *OneBotChannel) event(ctx context.Context, h Handler, ev map[string]any) {
	switch str(ev, "post_type") {
	case "message":
		if str(ev, "message_type") != "private" || h == nil {
			return // 只处理私聊
		}
		peer := str(ev, "user_id")
		if peer == "" || peer == str(ev, "self_id") {
			return
		}
		in := Inbound{Channel: QQ, PeerID: peer, At: time.Now()}
		if s, ok := ev["sender"].(map[string]any); ok {
			in.PeerName = str(s, "nickname")
		}
		in.Text, in.Images = o.parseMessage(ctx, ev["message"])
		h(ctx, in)
	case "request":
		if str(ev, "request_type") == "friend" && o.autoAccept {
			_, err := o.call(ctx, "set_friend_add_request", map[string]any{"flag": str(ev, "flag"), "approve": true})
			if err != nil {
				o.log.Warn("通过好友申请失败", "err", err)
			} else {
				o.log.Info("已自动通过好友申请", "qq", str(ev, "user_id"))
			}
		}
	}
}

var (
	cqCode  = regexp.MustCompile(`\[CQ:([a-z_]+)((?:,[^\]]*)?)\]`)
	cqParam = regexp.MustCompile(`,([a-z_]+)=([^,\]]*)`)
)

// parseMessage 解析消息段（数组格式或 CQ 码字符串格式），返回文字与图片。
func (o *OneBotChannel) parseMessage(ctx context.Context, msg any) (string, []Image) {
	var segs []map[string]any
	switch m := msg.(type) {
	case []any:
		for _, s := range m {
			if seg, ok := s.(map[string]any); ok {
				segs = append(segs, seg)
			}
		}
	case string:
		last := 0
		for _, loc := range cqCode.FindAllStringSubmatchIndex(m, -1) {
			if t := m[last:loc[0]]; t != "" {
				segs = append(segs, map[string]any{"type": "text", "data": map[string]any{"text": html.UnescapeString(t)}})
			}
			data := map[string]any{}
			for _, p := range cqParam.FindAllStringSubmatch(m[loc[4]:loc[5]], -1) {
				data[p[1]] = html.UnescapeString(p[2])
			}
			segs = append(segs, map[string]any{"type": m[loc[2]:loc[3]], "data": data})
			last = loc[1]
		}
		if t := m[last:]; t != "" {
			segs = append(segs, map[string]any{"type": "text", "data": map[string]any{"text": html.UnescapeString(t)}})
		}
	}
	var texts []string
	var images []Image
	for _, seg := range segs {
		data, _ := seg["data"].(map[string]any)
		switch str(seg, "type") {
		case "text":
			texts = append(texts, str(data, "text"))
		case "image":
			if img, err := o.download(ctx, str(data, "url")); err == nil {
				images = append(images, img)
			} else {
				texts = append(texts, "[图片]")
			}
		case "record":
			texts = append(texts, "[语音]")
		case "file":
			texts = append(texts, "[文件]")
		case "face":
			texts = append(texts, "[表情]")
		}
	}
	return strings.TrimSpace(strings.Join(texts, "")), images
}

func (o *OneBotChannel) download(ctx context.Context, u string) (Image, error) {
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return Image{}, errors.New("不是网络图片")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Image{}, err
	}
	resp, err := o.media.Do(req)
	if err != nil {
		return Image{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Image{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 30<<20))
	return Image{Name: "QQ图片.jpg", Data: data}, err
}

// call 调用 OneBot 动作并等待结果。
func (o *OneBotChannel) call(ctx context.Context, action string, params map[string]any) (map[string]any, error) {
	o.mu.Lock()
	conn := o.conn
	if conn == nil {
		o.mu.Unlock()
		return nil, errors.New("NapCat 没有连接")
	}
	o.seq++
	echo := "buugoo-" + strconv.Itoa(o.seq)
	ch := make(chan map[string]any, 1)
	o.pending[echo] = ch
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		delete(o.pending, echo)
		o.mu.Unlock()
	}()
	b, _ := json.Marshal(map[string]any{"action": action, "params": params, "echo": echo})
	o.writeMu.Lock()
	_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	err := conn.WriteMessage(websocket.TextMessage, b)
	o.writeMu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, errors.New("NapCat 没有响应")
	case resp := <-ch:
		if st := str(resp, "status"); st == "failed" || num(resp, "retcode") != 0 {
			return resp, fmt.Errorf("%s 失败：retcode=%d %s", action, num(resp, "retcode"), str(resp, "wording")+str(resp, "message"))
		}
		return resp, nil
	}
}

// Send 实现 Channel。
func (o *OneBotChannel) Send(ctx context.Context, peer string, out Outbound) error {
	uid, err := strconv.ParseInt(peer, 10, 64)
	if err != nil {
		return fmt.Errorf("QQ 号不对：%s", peer)
	}
	var seg map[string]any
	if out.Image != nil {
		seg = map[string]any{"type": "image", "data": map[string]any{"file": "base64://" + base64.StdEncoding.EncodeToString(out.Image.Data)}}
	} else {
		seg = map[string]any{"type": "text", "data": map[string]any{"text": out.Text}}
	}
	_, err = o.call(ctx, "send_private_msg", map[string]any{"user_id": uid, "message": []any{seg}})
	return err
}

// Typing 实现 Channel（OneBot 没有「正在输入」）。
func (o *OneBotChannel) Typing(context.Context, string) func() { return func() {} }
