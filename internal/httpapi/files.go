package httpapi

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/uploads"
)

// maxDeleteIDs 是一次多选删除的文件数上限。
const maxDeleteIDs = 500

type usageDTO struct {
	UsedBytes      int64 `json:"used_bytes"`
	WorkspaceBytes int64 `json:"workspace_bytes"` // 其中运行代码保存在对话工作目录里的部分
	QuotaBytes     int64 `json:"quota_bytes"`     // 0 表示不限
	Full           bool  `json:"full"`
}

func toUsage(u uploads.Usage) usageDTO {
	return usageDTO{UsedBytes: u.UsedBytes, WorkspaceBytes: u.WorkspaceBytes, QuotaBytes: u.QuotaBytes, Full: u.Full()}
}

type fileDTO struct {
	attachmentDTO
	Source            string    `json:"source"`
	CreatedAt         time.Time `json:"created_at"`
	ConversationID    *uint64   `json:"conversation_id"`
	ConversationTitle string    `json:"conversation_title"`
}

// usage 处理 GET /api/v1/files/usage。
func (s *server) usage(c echo.Context) error {
	u, err := s.Uploads.Usage(c.Request().Context(), currentUser(c).ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toUsage(u))
}

// listFiles 处理 GET /api/v1/files：用户上传的文件与对话中代码生成的图片，按时间倒序。
func (s *server) listFiles(c echo.Context) error {
	limit, err := queryInt(c, "limit", 60, 1, 200)
	if err != nil {
		return err
	}
	offset, err := queryInt(c, "offset", 0, 0, 1<<30)
	if err != nil {
		return err
	}
	kind := c.QueryParam("kind")
	if kind != "" && kind != store.AttachmentImage && kind != store.AttachmentDocument {
		return badRequest("kind 只能是 image 或 document")
	}
	ctx, u := c.Request().Context(), currentUser(c)
	list, total, err := s.Store.Attachments().ListFiles(ctx, u.ID, kind, limit, offset)
	if err != nil {
		return err
	}
	usage, err := s.Uploads.Usage(ctx, u.ID)
	if err != nil {
		return err
	}
	titles := map[uint64]string{}
	if len(list) > 0 {
		convs, err := s.Store.Conversations().List(ctx, u.ID)
		if err != nil {
			return err
		}
		for _, cv := range convs {
			titles[cv.ID] = cv.Title
		}
	}
	out := make([]fileDTO, len(list))
	for i := range list {
		a := &list[i]
		out[i] = fileDTO{attachmentDTO: toAttachment(a), Source: a.Source, CreatedAt: a.CreatedAt, ConversationID: a.ConversationID}
		if a.ConversationID != nil {
			out[i].ConversationTitle = titles[*a.ConversationID]
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"files": out, "total": total, "usage": toUsage(usage)})
}

// deleteFiles 处理 POST /api/v1/files/delete：{"ids": [...]} 删除选中的文件，或 {"older_than_days": 7} 删除 N 天以前的全部文件。
func (s *server) deleteFiles(c echo.Context) error {
	var req struct {
		IDs           []uint64 `json:"ids"`
		OlderThanDays int      `json:"older_than_days"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest("请求格式不正确")
	}
	ctx, u := c.Request().Context(), currentUser(c)
	var n int
	var err error
	switch {
	case req.OlderThanDays > 0 && len(req.IDs) == 0:
		if req.OlderThanDays > 3650 {
			return badRequest("天数太大")
		}
		n, err = s.Uploads.DeleteBefore(ctx, u.ID, time.Now().AddDate(0, 0, -req.OlderThanDays))
	case len(req.IDs) > 0 && req.OlderThanDays == 0:
		if len(req.IDs) > maxDeleteIDs {
			return badRequest("一次最多删除 500 个文件")
		}
		n, err = s.Uploads.Delete(ctx, u.ID, req.IDs)
	default:
		return badRequest("请选择要删除的文件")
	}
	if err != nil {
		return err
	}
	usage, err := s.Uploads.Usage(ctx, u.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"deleted": n, "usage": toUsage(usage)})
}
