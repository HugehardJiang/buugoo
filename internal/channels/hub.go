package channels

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/HugehardJiang/buugoo/internal/chat"
	"github.com/HugehardJiang/buugoo/internal/memory"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/uploads"
)

// ChatService 是对话服务（chat.Service）。
type ChatService interface {
	Send(ctx context.Context, u *store.User, in chat.SendInput, sink chat.Sink) (*chat.Result, error)
	CreateConversation(ctx context.Context, u *store.User, p chat.ConversationPatch) (*store.Conversation, error)
	Compact(ctx context.Context, u *store.User, convID uint64) (*chat.ContextStatus, error)
}

// 节奏。
const (
	// mergeWindow：对方连着发几条消息时，等这么久没有新消息再一起回复（像真人那样看完再回）。
	mergeWindow = 1500 * time.Millisecond
	mergeMax    = 8 * time.Second
	codeTTL     = 10 * time.Minute
	helpEvery   = 10 * time.Minute // 没绑定的人，绑定说明最多多久发一次
	workerIdle  = 5 * time.Minute
)

// Hub 连接各个聊天软件与对话服务：绑定、合并连发的消息、把回复拆成几条发出去、投递主动消息。
type Hub struct {
	store    store.Store
	chat     ChatService
	uploads  *uploads.Service
	name     func() string
	log      *slog.Logger
	channels map[string]Channel

	mu      sync.Mutex
	codes   map[string]bindCode // 绑定码 → 用户
	workers map[string]chan Inbound
	helped  map[string]time.Time
}

type bindCode struct {
	userID  uint64
	expires time.Time
}

// NewHub 创建 Hub。uploads 可以为空（收到的图片不保存）。
func NewHub(st store.Store, cs ChatService, up *uploads.Service, name func() string, log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{store: st, chat: cs, uploads: up, name: name, log: log.With("component", "channels"),
		channels: map[string]Channel{}, codes: map[string]bindCode{}, workers: map[string]chan Inbound{}, helped: map[string]time.Time{}}
}

// Add 加入一个聊天软件。在 Start 之前调用。
func (h *Hub) Add(c Channel) {
	h.channels[c.Name()] = c
	if w, ok := c.(*WeixinChannel); ok {
		w.OnLogin = func(ctx context.Context, userID uint64, peer string) {
			if _, err := h.bindOwner(ctx, userID, Weixin, peer, ""); err != nil {
				h.log.Error("绑定微信失败", "user_id", userID, "err", err)
			}
		}
		w.OnLogout = func(ctx context.Context, userID uint64, peer string) {
			if b, err := h.store.Bindings().GetByPeer(ctx, Weixin, peer); err == nil && b.UserID == userID {
				_ = h.store.Bindings().Delete(ctx, userID, b.ID)
			}
		}
	}
}

// Weixin 返回个人微信接入，没有开启时返回 nil。
func (h *Hub) Weixin() *WeixinChannel {
	w, _ := h.channels[Weixin].(*WeixinChannel)
	return w
}

// bindOwner 把用户自己登录的账号绑定到这个用户：已经绑定给别人时改绑过来（同一个微信先后被两个账号扫码）。
func (h *Hub) bindOwner(ctx context.Context, userID uint64, channel, peer, name string) (*store.ChannelBinding, error) {
	b, err := h.store.Bindings().GetByPeer(ctx, channel, peer)
	switch {
	case err == nil && b.UserID == userID:
		return b, nil
	case err == nil:
		if err := h.store.Bindings().Update(ctx, b.ID, map[string]any{"user_id": userID, "conversation_id": nil}); err != nil {
			return nil, err
		}
		b.UserID, b.ConversationID = userID, nil
		return b, nil
	case !errors.Is(err, store.ErrNotFound):
		return nil, err
	}
	b = &store.ChannelBinding{UserID: userID, Channel: channel, PeerID: peer, PeerName: name}
	if err := h.store.Bindings().Create(ctx, b); err != nil {
		return nil, err
	}
	h.log.Info("聊天软件已绑定", "channel", channel, "user_id", userID)
	return b, nil
}

// Channel 返回已加入的聊天软件，没有时返回 nil。
func (h *Hub) Channel(name string) Channel { return h.channels[name] }

// Enabled 返回已开启的聊天软件名。
func (h *Hub) Enabled() []string {
	var out []string
	for _, n := range []string{Weixin, QQ, Telegram} {
		if _, ok := h.channels[n]; ok {
			out = append(out, n)
		}
	}
	return out
}

// Statuses 返回各个聊天软件的连接状态。
func (h *Hub) Statuses() []Status {
	var out []Status
	for _, n := range h.Enabled() {
		out = append(out, h.channels[n].Status())
	}
	return out
}

// Start 在后台运行所有聊天软件，直到 ctx 取消。
func (h *Hub) Start(ctx context.Context) {
	for _, c := range h.channels {
		go c.Run(ctx, h.handle)
		h.log.Info("已开启聊天软件接入", "channel", c.Name())
	}
}

// NewCode 为用户生成绑定码（6 位数字，10 分钟有效），同一用户之前的绑定码作废。
func (h *Hub) NewCode(userID uint64) (string, time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	for c, b := range h.codes {
		if b.userID == userID || now.After(b.expires) {
			delete(h.codes, c)
		}
	}
	for {
		n, _ := rand.Int(rand.Reader, big.NewInt(900000))
		code := fmt.Sprintf("%06d", n.Int64()+100000)
		if _, used := h.codes[code]; !used {
			exp := now.Add(codeTTL)
			h.codes[code] = bindCode{userID: userID, expires: exp}
			return code, exp
		}
	}
}

func (h *Hub) useCode(code string) (uint64, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, ok := h.codes[code]
	if !ok || time.Now().After(b.expires) {
		return 0, false
	}
	delete(h.codes, code)
	return b.userID, true
}

// handle 把消息交给这个人的处理队列：同一个人的消息按顺序处理，连着发的几条合并成一轮。
func (h *Hub) handle(ctx context.Context, in Inbound) {
	if strings.TrimSpace(in.Text) == "" && len(in.Files) == 0 {
		return
	}
	key := in.Channel + ":" + in.PeerID
	h.mu.Lock()
	ch, ok := h.workers[key]
	if !ok {
		ch = make(chan Inbound, 32)
		h.workers[key] = ch
		go h.worker(context.WithoutCancel(ctx), key, ch)
	}
	h.mu.Unlock()
	select {
	case ch <- in:
	default:
		h.log.Warn("消息太多，丢弃一条", "channel", in.Channel)
	}
}

func (h *Hub) worker(ctx context.Context, key string, ch chan Inbound) {
	for {
		var first Inbound
		select {
		case first = <-ch:
		case <-time.After(workerIdle):
			h.mu.Lock()
			if len(ch) == 0 {
				delete(h.workers, key)
				h.mu.Unlock()
				return
			}
			h.mu.Unlock()
			continue
		}
		batch := []Inbound{first}
		deadline := time.After(mergeMax)
	collect:
		for {
			select {
			case in := <-ch:
				batch = append(batch, in)
			case <-time.After(mergeWindow):
				break collect
			case <-deadline:
				break collect
			}
		}
		merged := batch[0]
		for _, in := range batch[1:] {
			if in.Text != "" {
				merged.Text = strings.TrimSpace(merged.Text + "\n" + in.Text)
			}
			merged.Files = append(merged.Files, in.Files...)
			if in.PeerName != "" {
				merged.PeerName = in.PeerName
			}
		}
		h.process(ctx, merged)
	}
}

var codePattern = regexp.MustCompile(`^(?:绑定|bind|/bind)?\s*(\d{6})$`)

func (h *Hub) process(ctx context.Context, in Inbound) {
	c := h.channels[in.Channel]
	if c == nil {
		return
	}
	text := strings.TrimSpace(in.Text)
	b, err := h.store.Bindings().GetByPeer(ctx, in.Channel, in.PeerID)
	if in.Owner != 0 && (errors.Is(err, store.ErrNotFound) || (err == nil && b.UserID != in.Owner)) {
		b, err = h.bindOwner(ctx, in.Owner, in.Channel, in.PeerID, in.PeerName)
	}
	if errors.Is(err, store.ErrNotFound) {
		h.unbound(ctx, c, in, text)
		return
	}
	if err != nil {
		h.log.Error("读取绑定失败", "err", err)
		return
	}
	u, err := h.store.Users().GetByID(ctx, b.UserID)
	if err != nil {
		h.log.Error("读取用户失败", "user_id", b.UserID, "err", err)
		return
	}
	now := time.Now()
	fields := map[string]any{"last_message_at": now}
	if in.PeerName != "" && in.PeerName != b.PeerName {
		fields["peer_name"] = in.PeerName
	}
	_ = h.store.Bindings().Update(ctx, b.ID, fields)

	switch strings.ToLower(text) {
	case "/new", "新对话":
		// 每个聊天软件只有一段对话（网页上在「外部聊天」里），换话题直接聊就行；聊多了会自动整理。
		h.say(ctx, c, in.PeerID, "在这里我们一直是同一段对话，想换话题直接说就行\n聊得多了我会自动把之前的整理成要点，也可以发「压缩」让我现在整理")
		return
	case "/unbind", "解绑", "解除绑定":
		if in.Owner != 0 {
			h.say(ctx, c, in.PeerID, "这是你自己扫码连接的微信，要断开的话去网页「聊天软件」页里点「断开」")
			return
		}
		_ = h.store.Bindings().Delete(ctx, u.ID, b.ID)
		h.say(ctx, c, in.PeerID, "已经解绑啦，想再聊的话重新发绑定码就行")
		return
	case "/compact", "压缩", "压缩上下文":
		if b.ConversationID == nil {
			h.say(ctx, c, in.PeerID, "还没聊几句呢，不用压缩")
			return
		}
		st, err := h.chat.Compact(ctx, u, *b.ConversationID)
		switch {
		case err != nil && (errors.Is(err, chat.ErrNothingToCompact) || errors.Is(err, chat.ErrCompacting) || errors.Is(err, chat.ErrSummaryDisabled)):
			h.say(ctx, c, in.PeerID, err.Error())
		case err != nil:
			h.log.Warn("压缩上下文失败", "err", err)
			h.say(ctx, c, in.PeerID, "压缩失败了，稍后再试试")
		default:
			h.say(ctx, c, in.PeerID, fmt.Sprintf("好，之前聊的我整理成要点记下了，现在上下文用了大约 %d%%", st.Percent))
		}
		return
	case "/help", "帮助":
		h.say(ctx, c, in.PeerID, "直接和我聊天就行，发图片、文件也可以\n发「压缩」整理之前的聊天，发「解绑」解除绑定")
		return
	}
	h.reply(ctx, c, b, u, in)
}

func (h *Hub) unbound(ctx context.Context, c Channel, in Inbound, text string) {
	if m := codePattern.FindStringSubmatch(text); m != nil {
		userID, ok := h.useCode(m[1])
		if !ok {
			h.say(ctx, c, in.PeerID, "这个绑定码不对或者过期了，在网页「聊天软件」页里重新生成一个吧")
			return
		}
		b := &store.ChannelBinding{UserID: userID, Channel: in.Channel, PeerID: in.PeerID, PeerName: in.PeerName}
		if err := h.store.Bindings().Create(ctx, b); err != nil {
			h.log.Error("保存绑定失败", "err", err)
			h.say(ctx, c, in.PeerID, "绑定失败了，稍后再试试")
			return
		}
		h.log.Info("聊天软件已绑定", "channel", in.Channel, "user_id", userID)
		h.say(ctx, c, in.PeerID, "绑好啦！以后在这里也能找我聊天，网页上聊过的事我都记得")
		return
	}
	key := in.Channel + ":" + in.PeerID
	h.mu.Lock()
	last, ok := h.helped[key]
	if ok && time.Since(last) < helpEvery {
		h.mu.Unlock()
		return
	}
	h.helped[key] = time.Now()
	h.mu.Unlock()
	h.say(ctx, c, in.PeerID, fmt.Sprintf("你好呀，我是%s\n先在网页「聊天软件」页里生成一个绑定码，再把那 6 位数字发给我，就能在这里聊天啦", h.botName()))
}

func (h *Hub) botName() string {
	if h.name != nil {
		return h.name()
	}
	return "布谷"
}

// conversation 返回绑定使用的对话，没有或被删除时新建一段。
func (h *Hub) conversation(ctx context.Context, b *store.ChannelBinding, u *store.User) (uint64, error) {
	if b.ConversationID != nil {
		if conv, err := h.store.Conversations().Get(ctx, u.ID, *b.ConversationID); err == nil {
			// 旧版本建的对话没有标记来源：补上，网页上就能分到「外部聊天」里。
			if conv.Channel == "" {
				_ = h.store.Conversations().Update(ctx, u.ID, conv.ID, map[string]any{"channel": b.Channel})
			}
			return *b.ConversationID, nil
		}
	}
	conv, err := h.chat.CreateConversation(ctx, u, chat.ConversationPatch{Channel: b.Channel})
	if err != nil {
		return 0, err
	}
	if err := h.store.Bindings().Update(ctx, b.ID, map[string]any{"conversation_id": conv.ID}); err != nil {
		return 0, err
	}
	b.ConversationID = &conv.ID
	return conv.ID, nil
}

func (h *Hub) reply(ctx context.Context, c Channel, b *store.ChannelBinding, u *store.User, in Inbound) {
	convID, err := h.conversation(ctx, b, u)
	if err != nil {
		h.log.Error("准备对话失败", "err", err)
		return
	}
	var attIDs []uint64
	for _, f := range in.Files {
		if h.uploads == nil {
			break
		}
		a, err := h.uploads.Save(ctx, u.ID, f.Name, bytes.NewReader(f.Data))
		if err != nil {
			h.log.Warn("保存收到的文件失败", "err", err)
			var ue *uploads.UserError
			if errors.As(err, &ue) {
				what := "文件"
				if f.IsImage() {
					what = "图片"
				}
				h.say(ctx, c, in.PeerID, what+"没收到："+ue.Msg)
			}
			continue
		}
		attIDs = append(attIDs, a.ID)
	}
	if strings.TrimSpace(in.Text) == "" && len(attIDs) == 0 {
		return
	}
	stop := c.Typing(ctx, in.PeerID)
	res, err := h.chat.Send(ctx, u, chat.SendInput{ConversationID: convID, Content: in.Text, AttachmentIDs: attIDs}, nopSink{})
	stop()
	if err != nil && (res == nil || res.Reply == nil || res.Reply.Content == "") {
		h.say(ctx, c, in.PeerID, friendlyError(err))
		return
	}
	h.sendReply(ctx, c, in.PeerID, u, res.Reply.Content, res.Tools)
}

// sendReply 把回复拆成几条发出去，中间停顿一下；代码生成的图片和文件放在最后发。
func (h *Hub) sendReply(ctx context.Context, c Channel, peer string, u *store.User, content string, tools []chat.ToolEvent) {
	for i, part := range SplitBubbles(content) {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(typingDelay(part)) * time.Millisecond):
			}
		}
		h.say(ctx, c, peer, PlainText(part))
	}
	if h.uploads == nil {
		return
	}
	for _, t := range tools {
		for _, f := range append(append([]chat.ToolImage{}, t.Images...), t.Files...) {
			a, err := h.store.Attachments().Get(ctx, u.ID, f.ID)
			if err != nil {
				continue
			}
			data, err := os.ReadFile(h.uploads.Path(a))
			if err != nil {
				continue
			}
			if err := c.Send(ctx, peer, Outbound{File: &File{Name: a.Filename, Data: data}}); err != nil {
				h.log.Warn("发送文件失败", "channel", c.Name(), "err", err)
				h.say(ctx, c, peer, a.Filename+" 没发出去，可以到网页上下载")
			}
		}
	}
}

func (h *Hub) say(ctx context.Context, c Channel, peer, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if err := c.Send(ctx, peer, Outbound{Text: text}); err != nil {
		h.log.Warn("发送消息失败", "channel", c.Name(), "err", err)
	}
}

// Deliver 把主动消息发到聊天软件（消息已经保存在对话里，网页上也能看到）。
// everywhere 为 true（提醒、定时任务）时发到用户绑定的所有聊天软件，不管是在哪里设的；
// 否则（主动关心）只发到这段对话绑定的聊天软件，对话没有绑定时只留在网页上。
func (h *Hub) Deliver(ctx context.Context, u *store.User, res *chat.ProactiveResult, everywhere bool) {
	if res == nil || res.Conversation == nil || res.Message == nil {
		return
	}
	list, err := h.store.Bindings().ListByUser(ctx, u.ID)
	if err != nil {
		return
	}
	for i := range list {
		b := &list[i]
		c := h.channels[b.Channel]
		if c == nil {
			continue
		}
		if !everywhere && (b.ConversationID == nil || *b.ConversationID != res.Conversation.ID) {
			continue
		}
		h.sendReply(ctx, c, b.PeerID, u, res.Message.Content, res.Tools)
		if !everywhere {
			return
		}
	}
}

// friendlyError 把对话出错的原因说给聊天软件里的人听。
func friendlyError(err error) string {
	var qe *quota.ExceededError
	var ue *uploads.UserError
	switch {
	case errors.As(err, &qe):
		return "今天聊得有点多啦，" + qe.Error()
	case errors.Is(err, models.ErrNoModel):
		return "我这边还没配置好聊天模型，请管理员在网页「设置 → 模型」里添加一个"
	case errors.As(err, &ue):
		return ue.Msg
	default:
		return "我这边出了点问题，稍后再找我吧"
	}
}

type nopSink struct{}

func (nopSink) Start(*store.Conversation, *store.Message, *store.Message, *models.Resolved) error {
	return nil
}
func (nopSink) Memories([]memory.Scored) error { return nil }
func (nopSink) Reasoning(string) error         { return nil }
func (nopSink) Delta(string) error             { return nil }
func (nopSink) Tool(chat.ToolEvent) error      { return nil }
