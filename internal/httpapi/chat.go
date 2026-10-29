package httpapi

import (
	"context"
	"github.com/HugehardJiang/buugoo/internal/logx"
	"net/http"
	"strconv"

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

	return s.startLive(c, req.ConversationID, func(ctx context.Context, sink chat.Sink) (*chat.Result, error) {
		return s.Chat.Send(ctx, u, in, sink)
	})
}

// regenerate 处理 POST /api/v1/conversations/:id/regenerate：重新生成最后一条回复，事件和 /chat 一样。
func (s *server) regenerate(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	u := currentUser(c)
	return s.startLive(c, id, func(ctx context.Context, sink chat.Sink) (*chat.Result, error) {
		return s.Chat.Regenerate(ctx, u, id, sink)
	})
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
