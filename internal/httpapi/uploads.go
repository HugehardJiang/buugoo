package httpapi

import (
	"errors"
	"mime"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/HugehardJiang/buugoo/internal/store"
)

func (s *server) uploadLimits() any {
	if s.Uploads == nil {
		return nil
	}
	return map[string]any{"max_file_bytes": s.Uploads.MaxFileBytes(), "max_files": s.Uploads.MaxFiles()}
}

// uploadFile 处理 POST /api/v1/uploads（multipart/form-data，字段名 file）。
// 上传后文件处于未发送状态，随聊天请求的 attachment_ids 发送；24 小时内没有发送会被清理。
func (s *server) uploadFile(c echo.Context) error {
	fh, err := c.FormFile("file")
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return &apiError{Status: http.StatusRequestEntityTooLarge, Code: "invalid_file",
				Message: "文件太大，单个文件不能超过 " + strconv.FormatInt(s.Uploads.MaxFileBytes()>>20, 10) + " MB"}
		}
		return badRequest("请选择要上传的文件")
	}
	f, err := fh.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	a, err := s.Uploads.Save(c.Request().Context(), currentUser(c).ID, fh.Filename, f)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, toAttachment(a))
}

// getUpload 返回附件文件。图片内联展示，其余文件作为下载。
func (s *server) getUpload(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	a, err := s.Store.Attachments().Get(c.Request().Context(), currentUser(c).ID, id)
	if err != nil {
		return err
	}
	if a.DeletedAt != nil {
		return &apiError{Status: http.StatusNotFound, Code: "file_deleted", Message: "文件已删除"}
	}
	f, err := s.Uploads.Open(a)
	if err != nil {
		return echo.ErrNotFound
	}
	defer f.Close()
	h := c.Response().Header()
	disposition := "attachment"
	ctype := "application/octet-stream"
	if a.Kind == store.AttachmentImage {
		disposition, ctype = "inline", a.MIME
	}
	h.Set(echo.HeaderContentDisposition, mime.FormatMediaType(disposition, map[string]string{"filename": a.Filename}))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, max-age=86400")
	return c.Stream(http.StatusOK, ctype, f)
}

// deleteUpload 删除一个还没有发送的附件（用户在输入框中移除文件）。
func (s *server) deleteUpload(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	ctx, u := c.Request().Context(), currentUser(c)
	list, err := s.Store.Attachments().ListPending(ctx, u.ID, []uint64{id})
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return store.ErrNotFound
	}
	if err := s.Uploads.Remove(ctx, list); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
