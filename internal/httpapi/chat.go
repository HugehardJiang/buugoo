package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/HugehardJiang/buugoo/internal/logx"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/chat"
	"github.com/HugehardJiang/buugoo/internal/memory"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// chat 处理 POST /api/v1/chat。
//
// stream 为 true（默认）时返回 text/event-stream，事件依次为：
//
//	start     {"conversation": {...}, "user_message", "assistant_message_id", "model": {"id", "name"}}
//	memories  {"memories": [...]}  本轮使用的记忆及得分
//	reasoning {"text": "..."}      思考过程增量（开启思考时），可能有多个
//	tool      {"id", "name", "label", "status", "input", "summary", "detail"}  工具调用的开始（running）与结果（done/error）
//	delta     {"text": "..."}      回复增量，可能有多个
//	done      {"message": {...}, "usage": {...}}
//	error     {"error": {"code", "message"}}  出错时代替 done
//
// conversation_id 为空时使用最近的对话。stream 为 false 时一次性返回 JSON。
func (s *server) chat(c echo.Context) error {
	var req struct {
		ConversationID uint64   `json:"conversation_id"`
		Content        string   `json:"content"`
		AttachmentIDs  []uint64 `json:"attachment_ids"`
		Stream         *bool    `json:"stream"`
	}
	if err := bind(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	u := currentUser(c)
	in := chat.SendInput{ConversationID: req.ConversationID, Content: req.Content, AttachmentIDs: req.AttachmentIDs}

	if req.Stream != nil && !*req.Stream {
		res, err := s.Chat.Send(ctx, u, in, nopSink{})
		if err != nil {
			if res != nil && ctx.Err() == nil {
				return s.llmError(u.ID, err)
			}
			return err
		}
		return c.JSON(http.StatusOK, map[string]any{
			"conversation_id": res.Conversation.ID,
			"user_message":    userMessage(res.UserMessage, res.Attachments),
			"message":         toMessage(res.Reply),
			"memories":        toUsedMemories(res.Memories),
			"usage":           res.Usage,
		})
	}

	return s.streamReply(c, func(sink chat.Sink) (*chat.Result, error) { return s.Chat.Send(ctx, u, in, sink) })
}

// regenerate 处理 POST /api/v1/conversations/:id/regenerate：重新生成最后一条回复，事件和 /chat 一样。
func (s *server) regenerate(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	ctx, u := c.Request().Context(), currentUser(c)
	return s.streamReply(c, func(sink chat.Sink) (*chat.Result, error) { return s.Chat.Regenerate(ctx, u, id, sink) })
}

// deleteMessage 处理 DELETE /api/v1/conversations/:id/messages/:mid：删除一条消息。
func (s *server) deleteMessage(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	mid, err := strconv.ParseUint(c.Param("mid"), 10, 64)
	if err != nil || mid == 0 {
		return badRequest("消息 id 无效")
	}
	if err := s.Chat.DeleteMessage(c.Request().Context(), currentUser(c), id, mid); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// streamReply 用 SSE 推送一轮回复的生成过程。
func (s *server) streamReply(c echo.Context, run func(chat.Sink) (*chat.Result, error)) error {
	ctx, u := c.Request().Context(), currentUser(c)
	sse := newSSE(c.Response())
	res, err := run(sse)
	if !sse.started {
		// 尚未开始推送（参数错误、超出限额、连不上模型服务等），按普通 JSON 错误返回。
		if err != nil {
			if res != nil && ctx.Err() == nil {
				// 已保存消息说明是模型服务出错。
				return s.llmError(u.ID, err)
			}
			return err
		}
		return errors.New("chat: 未开始推送即结束")
	}
	defer sse.stop()
	if err != nil {
		if ctx.Err() == nil {
			_ = sse.send("error", map[string]any{"error": s.llmError(u.ID, err)})
		}
		return nil
	}
	return sse.send("done", map[string]any{"message": toMessage(res.Reply), "usage": res.Usage})
}

// llmError 把生成回复时的错误转换为 API 错误。模型服务的错误信息对排查有帮助，
// 直接透出给用户（不含密钥）。
func (s *server) llmError(userID uint64, err error) *apiError {
	s.log.Warn("对话生成失败", "user_id", userID, "err", err)
	ae := toAPIError(err)
	if ae.Code == "internal" {
		ae = &apiError{Status: http.StatusFailedDependency, Code: "llm_error", Message: "生成回复失败：" + err.Error()}
	}
	// 模型服务返回的错误里偶尔会带上请求头或密钥的片段，打码后再给用户看。
	cp := *ae
	cp.Message = logx.Redact(cp.Message)
	return &cp
}

type nopSink struct{}

func (nopSink) Start(*store.Conversation, *store.Message, *store.Message, *models.Resolved) error {
	return nil
}
func (nopSink) Memories([]memory.Scored) error { return nil }
func (nopSink) Reasoning(string) error         { return nil }
func (nopSink) Delta(string) error             { return nil }
func (nopSink) Tool(chat.ToolEvent) error      { return nil }

func userMessage(m *store.Message, atts []store.Attachment) messageDTO {
	d := toMessage(m)
	if len(atts) > 0 {
		d.Attachments = toAttachments(atts)
	}
	return d
}

// sseWriter 实现 chat.Sink，把事件写为 Server-Sent Events，并定时发送心跳。
type sseWriter struct {
	w       *echo.Response
	mu      sync.Mutex
	started bool
	done    chan struct{}
	once    sync.Once
}

var _ chat.Sink = (*sseWriter)(nil)

func newSSE(w *echo.Response) *sseWriter { return &sseWriter{w: w, done: make(chan struct{})} }

func (s *sseWriter) Start(conv *store.Conversation, userMsg, reply *store.Message, model *models.Resolved) error {
	h := s.w.Header()
	h.Set(echo.HeaderContentType, "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // 关闭 Nginx 缓冲，保证流式输出实时到达
	s.w.WriteHeader(http.StatusOK)
	s.started = true
	go s.heartbeat()
	return s.send("start", map[string]any{
		"conversation_id":      conv.ID,
		"conversation":         toConversation(conv),
		"user_message":         toMessage(userMsg),
		"assistant_message_id": reply.ID,
		"model":                map[string]any{"id": model.ID, "name": model.Name},
	})
}

func (s *sseWriter) Memories(used []memory.Scored) error {
	return s.send("memories", map[string]any{"memories": toUsedMemories(used)})
}

func (s *sseWriter) Reasoning(text string) error {
	return s.send("reasoning", map[string]string{"text": text})
}

func (s *sseWriter) Delta(text string) error {
	return s.send("delta", map[string]string{"text": text})
}

func (s *sseWriter) Tool(ev chat.ToolEvent) error {
	return s.send("tool", ev)
}

func (s *sseWriter) send(event string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("编码 SSE 数据: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, b); err != nil {
		return fmt.Errorf("写入 SSE: %w", err)
	}
	s.w.Flush()
	return nil
}

func (s *sseWriter) heartbeat() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.mu.Lock()
			_, err := s.w.Write([]byte(": ping\n\n"))
			if err == nil {
				s.w.Flush()
			}
			s.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (s *sseWriter) stop() { s.once.Do(func() { close(s.done) }) }
