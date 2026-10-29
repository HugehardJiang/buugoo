package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/chat"
	"github.com/HugehardJiang/buugoo/internal/memory"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// 后台生成：回复在服务器上生成，不跟着网页的连接走。手机切到别的应用、网页断线或切到别的对话时，
// 回复照样生成完、存下来；回到这段对话时用 GET /conversations/:id/live 接上（先补发已经生成的部分，
// 再接着推送）。只有点「停止」（POST /conversations/:id/stop）才会中断。

// liveKeep 是回复生成完后还保留多久（刚好在结束时重连的网页还能收到结果）。
const liveKeep = 30 * time.Second

// liveEvent 是推送过的一个事件；文字和思考增量（delta、reasoning）连续的合并在 text 里。
type liveEvent struct {
	name string
	data []byte
	text []byte
}

func isText(name string) bool { return name == "delta" || name == "reasoning" }

// liveRun 是一段正在生成的回复：记下推送过的事件，供重连的网页补发。
type liveRun struct {
	userID, convID uint64
	cancel         context.CancelFunc

	mu     sync.Mutex
	events []liveEvent
	subs   map[chan struct{}]struct{}
	done   bool
}

func (r *liveRun) addText(name, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n := len(r.events); n > 0 && r.events[n-1].name == name {
		r.events[n-1].text = append(r.events[n-1].text, text...)
	} else {
		r.events = append(r.events, liveEvent{name: name, text: []byte(text)})
	}
	r.notify()
}

// add 记下一个事件。
func (r *liveRun) add(name string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, liveEvent{name: name, data: b})
	r.notify()
}

// snapshot 返回现在的事件（只读副本）和是否已经结束。
func (r *liveRun) snapshot() ([]liveEvent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]liveEvent, len(r.events))
	for i, ev := range r.events {
		ev.text = ev.text[:len(ev.text):len(ev.text)]
		out[i] = ev
	}
	return out, r.done
}

func (r *liveRun) finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done = true
	r.notify()
}

// notify 叫醒等着的订阅者（调用时持有 mu）。
func (r *liveRun) notify() {
	for ch := range r.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (r *liveRun) subscribe() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	r.mu.Lock()
	if r.subs == nil {
		r.subs = map[chan struct{}]struct{}{}
	}
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		delete(r.subs, ch)
		r.mu.Unlock()
	}
}

// liveSink 实现 chat.Sink：事件只记进 liveRun，不直接写给网页，所以网页断开不影响生成。
type liveSink struct {
	run     *liveRun
	hub     *liveHub
	started chan struct{}
}

func (s *liveSink) Start(conv *store.Conversation, userMsg, reply *store.Message, model *models.Resolved) error {
	s.run.convID = conv.ID
	if !s.hub.claim(s.run) {
		return errBusy
	}
	s.run.add("start", map[string]any{
		"conversation_id":      conv.ID,
		"conversation":         toConversation(conv),
		"user_message":         toMessage(userMsg),
		"assistant_message_id": reply.ID,
		"model":                map[string]any{"id": model.ID, "name": model.Name},
	})
	close(s.started)
	return nil
}

func (s *liveSink) Memories(used []memory.Scored) error {
	s.run.add("memories", map[string]any{"memories": toUsedMemories(used)})
	return nil
}
func (s *liveSink) Reasoning(text string) error {
	s.run.addText("reasoning", text)
	return nil
}
func (s *liveSink) Delta(text string) error {
	s.run.addText("delta", text)
	return nil
}
func (s *liveSink) Tool(ev chat.ToolEvent) error {
	s.run.add("tool", ev)
	return nil
}

var errBusy = &apiError{Status: http.StatusConflict, Code: "busy", Message: "上一条回复还在生成，等它说完或者先点停止"}

// liveHub 记下每段对话正在生成的回复（每段对话同时只有一个）。
type liveHub struct {
	mu   sync.Mutex
	runs map[uint64]*liveRun
}

func newLiveHub() *liveHub { return &liveHub{runs: map[uint64]*liveRun{}} }

// claim 登记一段回复；这段对话已经有回复在生成时返回 false。
func (h *liveHub) claim(r *liveRun) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.runs[r.convID]; ok && old != r {
		old.mu.Lock()
		busy := !old.done
		old.mu.Unlock()
		if busy {
			return false
		}
	}
	h.runs[r.convID] = r
	return true
}

func (h *liveHub) get(userID, convID uint64) *liveRun {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.runs[convID]
	if r == nil || r.userID != userID {
		return nil
	}
	return r
}

// busy 报告这段对话是不是有回复正在生成。
func (h *liveHub) busy(userID, convID uint64) bool {
	r := h.get(userID, convID)
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.done
}

func (h *liveHub) release(r *liveRun) {
	time.AfterFunc(liveKeep, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.runs[r.convID] == r {
			delete(h.runs, r.convID)
		}
	})
}

// startLive 在后台生成一段回复，等它开始推送（或者开始前就失败）后返回。
func (s *server) startLive(c echo.Context, convID uint64, gen func(ctx context.Context, sink chat.Sink) (*chat.Result, error)) error {
	u := currentUser(c)
	if convID != 0 && s.live.busy(u.ID, convID) {
		return errBusy
	}
	// 不跟着请求取消：网页断开后照样生成完。
	ctx, cancel := context.WithCancel(context.WithoutCancel(c.Request().Context()))
	run := &liveRun{userID: u.ID, cancel: cancel}
	sink := &liveSink{run: run, hub: s.live, started: make(chan struct{})}
	failed := make(chan error, 1)
	go func() {
		defer cancel()
		res, err := gen(ctx, sink)
		select {
		case <-sink.started:
		default:
			failed <- err
			return
		}
		switch {
		case res != nil && res.Reply != nil && (err == nil || ctx.Err() != nil):
			// 正常结束或点了停止：都把存下来的回复发过去。
			run.add("done", map[string]any{"message": toMessage(res.Reply), "usage": res.Usage})
		case err != nil:
			run.add("error", map[string]any{"error": s.llmError(u.ID, err)})
		}
		run.finish()
		s.live.release(run)
	}()
	select {
	case <-sink.started:
		return s.followLive(c, run)
	case err := <-failed:
		// 尚未开始推送（参数错误、超出限额、连不上模型服务等），按普通 JSON 错误返回。
		if err == nil {
			err = fmt.Errorf("chat: 未开始推送即结束")
		}
		if ae, ok := err.(*apiError); ok {
			return ae
		}
		if toAPIError(err).Code == "internal" {
			return s.llmError(u.ID, err)
		}
		return err
	}
}

// followLive 把一段回复的事件推给网页：先补发已有的，再接着推，直到生成完或网页断开。
func (s *server) followLive(c echo.Context, run *liveRun) error {
	h := c.Response().Header()
	h.Set(echo.HeaderContentType, "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // 关闭 Nginx 缓冲，保证流式输出实时到达
	c.Response().WriteHeader(http.StatusOK)
	w := c.Response()

	ch, unsub := run.subscribe()
	defer unsub()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	// sent 是已经发完的事件数；partial 是 events[sent]（还在变长的文字事件）已经发出去的字节数。
	sent, partial := 0, 0
	for {
		events, done := run.snapshot()
		for i := sent; i < len(events); i++ {
			ev := events[i]
			data := ev.data
			if isText(ev.name) {
				if len(ev.text) > partial {
					data, _ = json.Marshal(map[string]string{"text": string(ev.text[partial:])})
				} else {
					data = nil
				}
			}
			if data != nil {
				if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.name, data); err != nil {
					return nil
				}
			}
			if isText(ev.name) && i == len(events)-1 {
				// 最后一个文字事件之后还会变长，下次从这里接着发。
				sent, partial = i, len(ev.text)
			} else {
				sent, partial = i+1, 0
			}
		}
		w.Flush()
		if done {
			return nil
		}
		select {
		case <-c.Request().Context().Done():
			return nil
		case <-ch:
		case <-ping.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return nil
			}
			w.Flush()
		}
	}
}

// liveReply 处理 GET /conversations/:id/live：接上正在生成的回复，没有时返回 204。
func (s *server) liveReply(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	run := s.live.get(currentUser(c).ID, id)
	if run == nil {
		return c.NoContent(http.StatusNoContent)
	}
	return s.followLive(c, run)
}

// stopReply 处理 POST /conversations/:id/stop：中断正在生成的回复（已经生成的部分会保存）。
func (s *server) stopReply(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if run := s.live.get(currentUser(c).ID, id); run != nil {
		run.cancel()
	}
	return c.NoContent(http.StatusNoContent)
}
