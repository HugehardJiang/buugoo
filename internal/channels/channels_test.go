package channels

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/HugehardJiang/buugoo/internal/chat"
	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func TestSplitAndPlain(t *testing.T) {
	got := SplitBubbles("哈哈 真的吗\n\n那后来呢\n\n```py\nx=1\n\ny=2\n```\n\n- 一\n- 二")
	if len(got) != 2 || got[0] != "哈哈 真的吗" || !strings.Contains(got[1], "y=2") || !strings.Contains(got[1], "- 二") {
		t.Fatalf("%q", got)
	}
	if p := PlainText("## 标题\n**加粗** 和 `代码`，看 [这里](https://a.cn/x)"); p != "标题\n加粗 和 代码，看 这里 https://a.cn/x" {
		t.Fatalf("%q", p)
	}
}

type fakeChannel struct {
	mu   sync.Mutex
	sent []string
}

func (f *fakeChannel) Name() string                          { return Telegram }
func (f *fakeChannel) Run(context.Context, Handler)          {}
func (f *fakeChannel) Typing(context.Context, string) func() { return func() {} }
func (f *fakeChannel) Status() Status                        { return Status{Name: Telegram} }
func (f *fakeChannel) Send(_ context.Context, peer string, o Outbound) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, peer+":"+o.Text)
	return nil
}
func (f *fakeChannel) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.sent...)
}

type fakeChat struct {
	db    *gormstore.DB
	mu    sync.Mutex
	sends []chat.SendInput
	fail  error
}

func (f *fakeChat) Send(ctx context.Context, u *store.User, in chat.SendInput, _ chat.Sink) (*chat.Result, error) {
	f.mu.Lock()
	f.sends = append(f.sends, in)
	f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	return &chat.Result{Reply: &store.Message{Content: "**好呀**\n\n我在呢"}}, nil
}

func (f *fakeChat) CreateConversation(ctx context.Context, u *store.User, p chat.ConversationPatch) (*store.Conversation, error) {
	c := &store.Conversation{UserID: u.ID, Thinking: "off", Channel: p.Channel}
	return c, f.db.Conversations().Create(ctx, c)
}

func (f *fakeChat) Compact(context.Context, *store.User, uint64) (*chat.ContextStatus, error) {
	return &chat.ContextStatus{Percent: 12}, nil
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("超时")
}

func TestHubBindAndChat(t *testing.T) {
	ctx := context.Background()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "h.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_ = db.Migrate(ctx)
	u, _ := db.Users().EnsureOwner(ctx)
	fc, ch := &fakeChat{db: db}, &fakeChannel{}
	h := NewHub(db, fc, nil, func() string { return "布谷" }, nil)
	h.Add(ch)

	// 没绑定：只回绑定说明，而且 10 分钟内不重复发。
	h.handle(ctx, Inbound{Channel: Telegram, PeerID: "42", Text: "你好"})
	waitFor(t, func() bool { return len(ch.messages()) == 1 })
	if !strings.Contains(ch.messages()[0], "绑定码") {
		t.Fatal(ch.messages())
	}
	h.handle(ctx, Inbound{Channel: Telegram, PeerID: "42", Text: "在吗"})
	time.Sleep(mergeWindow + 200*time.Millisecond)
	if len(ch.messages()) != 1 || len(fc.sends) != 0 {
		t.Fatal("没绑定时不应聊天、不应重复发说明")
	}
	// 错误的绑定码。
	h.handle(ctx, Inbound{Channel: Telegram, PeerID: "42", Text: "123456"})
	waitFor(t, func() bool { return len(ch.messages()) == 2 })
	// 正确的绑定码。
	code, _ := h.NewCode(u.ID)
	h.handle(ctx, Inbound{Channel: Telegram, PeerID: "42", PeerName: "小明", Text: "绑定 " + code})
	waitFor(t, func() bool { return len(ch.messages()) == 3 })
	b, err := db.Bindings().GetByPeer(ctx, Telegram, "42")
	if err != nil || b.UserID != u.ID || b.PeerName != "小明" {
		t.Fatalf("binding=%+v err=%v", b, err)
	}
	if _, ok := h.useCode(code); ok {
		t.Fatal("绑定码只能用一次")
	}

	// 连着发的两条合并成一轮；回复拆成两条，去掉 Markdown。
	h.handle(ctx, Inbound{Channel: Telegram, PeerID: "42", Text: "今天好累"})
	h.handle(ctx, Inbound{Channel: Telegram, PeerID: "42", Text: "加班到十点"})
	waitFor(t, func() bool { return len(ch.messages()) == 5 })
	if len(fc.sends) != 1 || fc.sends[0].Content != "今天好累\n加班到十点" || fc.sends[0].ConversationID == 0 {
		t.Fatalf("sends=%+v", fc.sends)
	}
	if m := ch.messages(); m[3] != "42:好呀" || m[4] != "42:我在呢" {
		t.Fatalf("%q", m)
	}
	// 同一个绑定一直用同一段对话。
	h.handle(ctx, Inbound{Channel: Telegram, PeerID: "42", Text: "嗯"})
	waitFor(t, func() bool { return len(ch.messages()) == 7 })
	if fc.sends[1].ConversationID != fc.sends[0].ConversationID {
		t.Fatal("应使用同一段对话")
	}

	// 主动消息发到这段对话绑定的聊天软件。
	conv, _ := db.Conversations().Get(ctx, u.ID, fc.sends[0].ConversationID)
	h.Deliver(ctx, u, &chat.ProactiveResult{Conversation: conv, Message: &store.Message{Content: "面试怎么样啦"}}, false)
	if m := ch.messages(); m[len(m)-1] != "42:面试怎么样啦" {
		t.Fatalf("%q", m)
	}
	other := &store.Conversation{UserID: u.ID}
	_ = db.Conversations().Create(ctx, other)
	n := len(ch.messages())
	h.Deliver(ctx, u, &chat.ProactiveResult{Conversation: other, Message: &store.Message{Content: "x"}}, false)
	if len(ch.messages()) != n {
		t.Fatal("其他对话的主动关心不应发到聊天软件")
	}
	// 提醒和定时任务：在网页上设的（写进网页的对话）也发到绑定的聊天软件。
	h.Deliver(ctx, u, &chat.ProactiveResult{Conversation: other, Message: &store.Message{Content: "该吃药啦"}}, true)
	if m := ch.messages(); len(m) != n+1 || m[n] != "42:该吃药啦" {
		t.Fatalf("%q", m)
	}

	// 出错时说人话；解绑。
	fc.fail = errors.New("boom")
	h.handle(ctx, Inbound{Channel: Telegram, PeerID: "42", Text: "在吗"})
	waitFor(t, func() bool { m := ch.messages(); return strings.Contains(m[len(m)-1], "出了点问题") })
	h.handle(ctx, Inbound{Channel: Telegram, PeerID: "42", Text: "解绑"})
	waitFor(t, func() bool {
		_, err := db.Bindings().GetByPeer(ctx, Telegram, "42")
		return errors.Is(err, store.ErrNotFound)
	})
	if conv.Channel != Telegram {
		t.Fatalf("聊天软件里的对话应标记来源：%q", conv.Channel)
	}

	// 自己扫码登录的账号（Owner）：不用绑定码，直接绑定并聊天；已经绑给别人的改绑过来。
	fc.fail = nil
	u2 := &store.User{Username: "u2", PasswordHash: "x"}
	if err := db.Users().Create(ctx, u2); err != nil {
		t.Fatal(err)
	}
	_ = db.Bindings().Create(ctx, &store.ChannelBinding{UserID: u.ID, Channel: Telegram, PeerID: "wx1"})
	sends := len(fc.sends)
	h.handle(ctx, Inbound{Channel: Telegram, PeerID: "wx1", Text: "早", Owner: u2.ID})
	waitFor(t, func() bool { fc.mu.Lock(); defer fc.mu.Unlock(); return len(fc.sends) == sends+1 })
	if b, err := db.Bindings().GetByPeer(ctx, Telegram, "wx1"); err != nil || b.UserID != u2.ID {
		t.Fatalf("%+v %v", b, err)
	}
	// 自己扫码的账号不能发「解绑」解除（要去网页断开）。
	h.handle(ctx, Inbound{Channel: Telegram, PeerID: "wx1", Text: "解绑", Owner: u2.ID})
	waitFor(t, func() bool { m := ch.messages(); return strings.Contains(m[len(m)-1], "断开") })
}

func TestOneBot(t *testing.T) {
	ob := NewOneBot("secret-token", true, nil)
	got := make(chan Inbound, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ob.Run(ctx, func(_ context.Context, in Inbound) { got <- in })
	time.Sleep(20 * time.Millisecond)
	srv := httptest.NewServer(ob)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + OneBotPath

	if _, resp, err := websocket.DefaultDialer.Dial(wsURL, nil); err == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatal("没有令牌应拒绝")
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Authorization": {"Bearer secret-token"}, "X-Self-ID": {"10001"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	send := func(v any) {
		b, _ := json.Marshal(v)
		_ = conn.WriteMessage(websocket.TextMessage, b)
	}
	// 私聊（数组格式与 CQ 码格式）、群聊（忽略）。
	send(map[string]any{"post_type": "message", "message_type": "group", "user_id": 7, "message": "群消息"})
	send(map[string]any{"post_type": "message", "message_type": "private", "user_id": 12345, "self_id": 10001,
		"sender": map[string]any{"nickname": "小红"}, "message": []any{map[string]any{"type": "text", "data": map[string]any{"text": "你好"}}}})
	send(map[string]any{"post_type": "message", "message_type": "private", "user_id": 12345, "message": "在吗&amp;[CQ:face,id=1]"})
	in := <-got
	if in.PeerID != "12345" || in.PeerName != "小红" || in.Text != "你好" {
		t.Fatalf("%+v", in)
	}
	if in = <-got; in.Text != "在吗&[表情]" {
		t.Fatalf("%+v", in)
	}
	if ob.Status().Account != "10001" || !ob.Status().Connected {
		t.Fatalf("%+v", ob.Status())
	}

	// 发消息：NapCat 收到 send_private_msg，回复结果。
	done := make(chan error, 1)
	go func() { done <- ob.Send(context.Background(), "12345", Outbound{Text: "收到"}) }()
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var call struct {
		Action string         `json:"action"`
		Params map[string]any `json:"params"`
		Echo   string         `json:"echo"`
	}
	_ = json.Unmarshal(data, &call)
	if call.Action != "send_private_msg" || call.Params["user_id"] != float64(12345) || !strings.Contains(string(data), "收到") {
		t.Fatalf("%s", data)
	}
	send(map[string]any{"status": "ok", "retcode": 0, "echo": call.Echo})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// 好友申请自动通过。
	send(map[string]any{"post_type": "request", "request_type": "friend", "user_id": 9, "flag": "f1"})
	_, data, _ = conn.ReadMessage()
	if !strings.Contains(string(data), "set_friend_add_request") || !strings.Contains(string(data), `"flag":"f1"`) {
		t.Fatalf("%s", data)
	}
	select {
	case in := <-got:
		t.Fatalf("群消息不应处理: %+v", in)
	default:
	}
}

func TestWeixin(t *testing.T) {
	var mu sync.Mutex
	var sent []map[string]any
	cdn := map[string][]byte{}
	key := []byte("0123456789abcdef")
	cdn["in1"] = aesECBEncrypt(key, []byte("a,b\n1,2\n"))
	polls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/c2c/download":
			mu.Lock()
			data := cdn[r.URL.Query().Get("encrypted_query_param")]
			mu.Unlock()
			_, _ = w.Write(data)
			return
		case "/c2c/upload":
			mu.Lock()
			cdn["up"] = body
			mu.Unlock()
			w.Header().Set("x-encrypted-param", "up")
			return
		}
		if r.Header.Get("AuthorizationType") != "ilink_bot_token" || r.Header.Get("X-WECHAT-UIN") == "" {
			w.WriteHeader(400)
			return
		}
		switch r.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			_, _ = w.Write([]byte(`{"qrcode":"q1","qrcode_img_content":"https://weixin.qq.com/x/q1"}`))
		case "/ilink/bot/get_qrcode_status":
			_, _ = w.Write([]byte(`{"status":"confirmed","bot_token":"tok7","ilink_bot_id":"bot7","ilink_user_id":"wx_u1","baseurl":"` + srv.URL + `"}`))
		case "/ilink/bot/getupdates":
			if r.Header.Get("Authorization") != "Bearer tok7" {
				w.WriteHeader(401)
				return
			}
			mu.Lock()
			polls++
			first := polls == 1
			mu.Unlock()
			if !first {
				time.Sleep(50 * time.Millisecond)
				_, _ = w.Write([]byte(`{"ret":0,"msgs":[]}`))
				return
			}
			k := base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(key)))
			_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"b1","msgs":[` +
				`{"from_user_id":"wx_u1","context_token":"ctx1","item_list":[{"type":1,"text_item":{"text":"早"}},{"type":3,"voice_item":{"text":"语音转的字"}},` +
				`{"type":4,"file_item":{"file_name":"表.csv","media":{"encrypt_query_param":"in1","aes_key":"` + k + `"}}}]},` +
				`{"from_user_id":"wx_other","context_token":"ctx2","item_list":[{"type":1,"text_item":{"text":"我是别人"}}]},` +
				`{"from_user_id":"wx_bot","message_type":2,"item_list":[{"type":1,"text_item":{"text":"自己发的"}}]}]}`))
		case "/ilink/bot/getuploadurl":
			_, _ = w.Write([]byte(`{"ret":0,"upload_param":"p1"}`))
		case "/ilink/bot/sendmessage":
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			mu.Lock()
			sent = append(sent, m)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"ret":0}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	oldBase, oldCDN := weixinBaseURL, weixinCDNURL
	weixinBaseURL, weixinCDNURL = srv.URL, srv.URL+"/c2c"
	defer func() { weixinBaseURL, weixinCDNURL = oldBase, oldCDN }()

	dir := t.TempDir()
	w := NewWeixin(dir, nil)
	logins := make(chan string, 4)
	logouts := make(chan string, 4)
	w.OnLogin = func(_ context.Context, uid uint64, peer string) { logins <- fmt.Sprint(uid, peer) }
	w.OnLogout = func(_ context.Context, uid uint64, peer string) { logouts <- fmt.Sprint(uid, peer) }
	got := make(chan Inbound, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, func(_ context.Context, in Inbound) { got <- in })
	time.Sleep(20 * time.Millisecond)

	// 用户 7 扫码登录自己的微信：自动绑定扫码人。
	l, err := w.StartLogin(ctx, 7)
	if err != nil || !strings.HasPrefix(l.QRCode, "data:image/png;base64,") {
		t.Fatal(l, err)
	}
	if v := <-logins; v != "7wx_u1" {
		t.Fatal(v)
	}
	in := <-got
	if in.PeerID != "wx_u1" || in.Owner != 7 || in.Text != "早\n语音转的字" || len(in.Files) != 1 ||
		in.Files[0].Name != "表.csv" || string(in.Files[0].Data) != "a,b\n1,2\n" {
		t.Fatalf("%+v", in)
	}
	// 别人发来的消息不算这个用户的（要用绑定码）。
	if other := <-got; other.PeerID != "wx_other" || other.Owner != 0 {
		t.Fatalf("%+v", other)
	}
	if u := w.User(7); u.Account == nil || u.Account.Account != "bot7" || u.Login == nil || u.Login.Status != "confirmed" {
		t.Fatalf("%+v", u)
	}
	if u := w.User(8); u.Account != nil {
		t.Fatal("别的用户没有连接微信")
	}

	if err := w.Send(context.Background(), "wx_u1", Outbound{Text: "早呀"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Send(context.Background(), "wx_u1", Outbound{File: &File{Name: "结果.csv", Data: []byte("x,y\n")}}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	msg := sent[0]["msg"].(map[string]any)
	fileMsg := mustJSON(sent[1]["msg"])
	uploaded := cdn["up"]
	mu.Unlock()
	if msg["context_token"] != "ctx1" || msg["to_user_id"] != "wx_u1" || !strings.Contains(mustJSON(msg), "早呀") {
		t.Fatalf("%v", msg)
	}
	if !strings.Contains(fileMsg, `"file_name":"结果.csv"`) || !strings.Contains(fileMsg, `"type":4`) {
		t.Fatal(fileMsg)
	}
	if len(uploaded)%16 != 0 || len(uploaded) == 0 {
		t.Fatal("上传的文件应该是加密过的")
	}
	if err := w.Send(context.Background(), "nobody", Outbound{Text: "x"}); err == nil {
		t.Fatal("没发过消息的人不能主动发")
	}

	// 登录态（含 context_token）保存在数据目录，重启后还能用。
	waitFor(t, func() bool {
		a := NewWeixin(dir, nil).accounts[7]
		return a != nil && a.state.SyncBuf == "b1"
	})
	w2 := NewWeixin(dir, nil)
	if a := w2.accounts[7]; a.state.Contexts["wx_u1"] != "ctx1" || a.state.Token != "tok7" || a.state.OwnerPeer != "wx_u1" {
		t.Fatalf("%+v", a.state)
	}
	if list := w2.Accounts(); len(list) != 1 || list[0].UserID != 7 {
		t.Fatalf("%+v", list)
	}

	// 管理员踢下线：删除登录态，告诉用户原因。
	w.Logout(ctx, 7, "管理员断开了你的微信连接")
	if v := <-logouts; v != "7wx_u1" {
		t.Fatal(v)
	}
	if u := w.User(7); u.Account != nil || u.Notice == "" {
		t.Fatalf("%+v", u)
	}
	if len(NewWeixin(dir, nil).Accounts()) != 0 {
		t.Fatal("退出后不应再保存登录态")
	}

	// 旧版本管理员扫码的登录态继续可用。
	legacy := t.TempDir()
	b, _ := json.Marshal(weixinState{Token: "old", AccountID: "bot0"})
	_ = os.WriteFile(filepath.Join(legacy, "weixin.json"), b, 0o600)
	if list := NewWeixin(legacy, nil).Accounts(); len(list) != 1 || !list[0].Legacy {
		t.Fatalf("%+v", list)
	}

	// 媒体加解密。
	if out, err := aesECBDecrypt(key, aesECBEncrypt(key, []byte("hello"))); err != nil || string(out) != "hello" {
		t.Fatal("aes")
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestTelegram(t *testing.T) {
	var mu sync.Mutex
	var sentText string
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/bot123:abc/") {
			w.WriteHeader(404)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch strings.TrimPrefix(r.URL.Path, "/bot123:abc/") {
		case "getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"username":"buugoo_bot"}}`))
		case "getUpdates":
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":5,"message":{"chat":{"id":99,"type":"private"},"from":{"first_name":"Ann"},"text":"hi"}},{"update_id":6,"message":{"chat":{"id":-1,"type":"group"},"from":{"first_name":"G"},"text":"group"}}]}`))
				return
			}
			if !strings.Contains(string(body), `"offset":7`) {
				t.Errorf("offset 应前进: %s", body)
			}
			time.Sleep(50 * time.Millisecond)
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		case "sendMessage":
			mu.Lock()
			sentText = string(body)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
		case "sendChatAction":
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	defer srv.Close()
	tg, err := NewTelegram("123:abc", srv.URL, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan Inbound, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tg.Run(ctx, func(_ context.Context, in Inbound) { got <- in })
	in := <-got
	if in.PeerID != "99" || in.PeerName != "Ann" || in.Text != "hi" {
		t.Fatalf("%+v", in)
	}
	waitFor(t, func() bool { return tg.Status().Account == "@buugoo_bot" })
	if err := tg.Send(context.Background(), "99", Outbound{Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(sentText, `"chat_id":"99"`) || !strings.Contains(sentText, "hello") {
		t.Fatal(sentText)
	}
	select {
	case in := <-got:
		t.Fatalf("群消息不应处理: %+v", in)
	default:
	}
}
