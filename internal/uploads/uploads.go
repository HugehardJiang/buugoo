// Package uploads 管理聊天中上传的图片与文档：识别类型、提取文档文字、按用户保存到上传目录、
// 统计用量，以及清理一直没有发送的文件。
package uploads

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// 上传错误。
var (
	ErrTooLarge    = errors.New("文件太大")
	ErrQuota       = errors.New("上传空间已用完")
	ErrUnsupported = errors.New("不支持这种文件")
	ErrNoText      = errors.New("没有从文件中提取到文字")
	ErrEmpty       = errors.New("文件是空的")
)

// UserError 是可以直接展示给用户的上传错误。
type UserError struct {
	Kind error
	Msg  string
}

func (e *UserError) Error() string { return e.Msg }
func (e *UserError) Unwrap() error { return e.Kind }

func userErr(kind error, format string, args ...any) error {
	return &UserError{Kind: kind, Msg: fmt.Sprintf(format, args...)}
}

// Service 管理上传文件。
type Service struct {
	repo store.AttachmentRepo
	cfg  config.UploadsConfig
	log  *slog.Logger
	now  func() time.Time
}

// New 创建上传服务。
func New(repo store.AttachmentRepo, cfg config.UploadsConfig, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, cfg: cfg, log: log.With("component", "uploads"), now: time.Now}
}

// MaxFileBytes 返回单个文件的大小上限。
func (s *Service) MaxFileBytes() int64 { return int64(s.cfg.MaxFileMB) << 20 }

// MaxFiles 返回一条消息最多附带的文件数。
func (s *Service) MaxFiles() int { return s.cfg.MaxFiles }

// DocChars 返回一个文档最多放进对话的字数。
func (s *Service) DocChars() int { return s.cfg.DocChars }

// imageTypes 是支持的图片格式（按文件内容判断，不看扩展名）。
var imageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// Save 保存一个上传的文件：读入（不超过大小上限）→ 识别类型 → 文档提取文字 → 检查用户空间 → 写入磁盘与数据库。
func (s *Service) Save(ctx context.Context, userID uint64, filename string, r io.Reader) (*store.Attachment, error) {
	filename = cleanName(filename)
	limit := s.MaxFileBytes()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("读取上传文件: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, userErr(ErrTooLarge, "文件太大，单个文件不能超过 %d MB", s.cfg.MaxFileMB)
	}
	if len(data) == 0 {
		return nil, userErr(ErrEmpty, "文件是空的")
	}

	a := &store.Attachment{UserID: userID, Filename: filename, Size: int64(len(data)), Source: store.AttachmentUploaded}
	sniffed := http.DetectContentType(data)
	if ext, ok := imageTypes[sniffed]; ok {
		a.Kind, a.MIME = store.AttachmentImage, sniffed
		a.Path = ext
	} else {
		text, mime, err := extractText(filename, data)
		if err != nil {
			return nil, err
		}
		a.Kind, a.MIME, a.Text = store.AttachmentDocument, mime, text
		a.Path = strings.ToLower(filepath.Ext(filename))
	}

	if err := s.checkQuota(ctx, userID, a.Size); err != nil {
		return nil, err
	}

	sum := sha256.Sum256(data)
	a.SHA256 = hex.EncodeToString(sum[:])
	rel, err := s.write(userID, a.Path, data)
	if err != nil {
		return nil, err
	}
	a.Path = rel
	if err := s.repo.Create(ctx, a); err != nil {
		_ = os.Remove(s.abs(rel))
		return nil, err
	}
	return a, nil
}

// SaveGenerated 保存代码生成的图片，直接关联到助手的回复。只接受 PNG/JPEG/GIF/WebP，计入用户的上传空间。
func (s *Service) SaveGenerated(ctx context.Context, userID, conversationID, messageID uint64, filename string, data []byte) (*store.Attachment, error) {
	if len(data) == 0 {
		return nil, userErr(ErrEmpty, "图片是空的")
	}
	if int64(len(data)) > s.MaxFileBytes() {
		return nil, userErr(ErrTooLarge, "图片太大，不能超过 %d MB", s.cfg.MaxFileMB)
	}
	sniffed := http.DetectContentType(data)
	ext, ok := imageTypes[sniffed]
	if !ok {
		return nil, userErr(ErrUnsupported, "%s 不是 PNG、JPEG、GIF 或 WebP 图片", cleanName(filename))
	}
	if err := s.checkQuota(ctx, userID, int64(len(data))); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	a := &store.Attachment{UserID: userID, ConversationID: &conversationID, MessageID: &messageID,
		Kind: store.AttachmentImage, Source: store.AttachmentGenerated, Filename: cleanName(filename), MIME: sniffed, Size: int64(len(data)),
		SHA256: hex.EncodeToString(sum[:])}
	rel, err := s.write(userID, ext, data)
	if err != nil {
		return nil, err
	}
	a.Path = rel
	if err := s.repo.Create(ctx, a); err != nil {
		_ = os.Remove(s.abs(rel))
		return nil, err
	}
	return a, nil
}

// checkQuota 检查用户的文件空间：已经用满时任何文件都不能再上传，否则检查加上这个文件后是否超出。
func (s *Service) checkQuota(ctx context.Context, userID uint64, size int64) error {
	u, err := s.Usage(ctx, userID)
	if err != nil || u.QuotaBytes == 0 {
		return err
	}
	if u.Full() {
		return userErr(ErrQuota, "文件空间已经用满（%s / %d MB），暂时不能上传文件。去「文件」里删除一些不需要的文件后就能继续使用", mb(u.UsedBytes), s.cfg.UserQuotaMB)
	}
	if u.UsedBytes+size > u.QuotaBytes {
		return userErr(ErrQuota, "文件空间不够了：还剩 %s，这个文件有 %s。去「文件」里删除一些不需要的文件后再试", mb(u.QuotaBytes-u.UsedBytes), mb(size))
	}
	return nil
}

func mb(n int64) string {
	if n < 1<<20 {
		return fmt.Sprintf("%d KB", (n+1023)>>10)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

// Usage 是用户文件空间的用量。
type Usage struct {
	UsedBytes int64
	// QuotaBytes 为 0 表示不限。
	QuotaBytes int64
}

// Full 报告空间是否已经用满。
func (u Usage) Full() bool { return u.QuotaBytes > 0 && u.UsedBytes >= u.QuotaBytes }

// Usage 返回用户文件空间的用量。
func (s *Service) Usage(ctx context.Context, userID uint64) (Usage, error) {
	used, err := s.repo.TotalSize(ctx, userID)
	if err != nil {
		return Usage{}, err
	}
	return Usage{UsedBytes: used, QuotaBytes: int64(s.cfg.UserQuotaMB) << 20}, nil
}

// Delete 删除用户的文件（文件管理）：删掉磁盘上的文件、不再计入用量；已经发送过的保留记录，
// 聊天记录中显示「文件已删除」。返回实际删除的数量。
func (s *Service) Delete(ctx context.Context, userID uint64, ids []uint64) (int, error) {
	list, err := s.repo.ListByIDs(ctx, userID, ids)
	if err != nil {
		return 0, err
	}
	return s.deleteFiles(ctx, userID, list)
}

// DeleteBefore 删除用户在 before 之前上传的全部文件，返回删除的数量。
func (s *Service) DeleteBefore(ctx context.Context, userID uint64, before time.Time) (int, error) {
	list, err := s.repo.ListBefore(ctx, userID, before)
	if err != nil {
		return 0, err
	}
	return s.deleteFiles(ctx, userID, list)
}

func (s *Service) deleteFiles(ctx context.Context, userID uint64, list []store.Attachment) (int, error) {
	var sent []uint64
	var pending []store.Attachment
	for _, a := range list {
		if a.MessageID == nil {
			pending = append(pending, a)
			continue
		}
		if err := os.Remove(s.abs(a.Path)); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("删除文件失败", "path", a.Path, "err", err)
		}
		sent = append(sent, a.ID)
	}
	if err := s.repo.MarkDeleted(ctx, userID, sent, s.now()); err != nil {
		return 0, err
	}
	if err := s.Remove(ctx, pending); err != nil {
		return len(sent), err
	}
	return len(list), nil
}

// write 把文件写到 <dir>/<userID>/<年月>/<随机名><ext>，返回相对路径。
func (s *Service) write(userID uint64, ext string, data []byte) (string, error) {
	var rnd [12]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	rel := filepath.ToSlash(filepath.Join(strconv.FormatUint(userID, 10), s.now().Format("200601"), hex.EncodeToString(rnd[:])+ext))
	path := s.abs(rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("创建上传目录: %w", err)
	}
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return "", fmt.Errorf("保存上传文件: %w", err)
	}
	return rel, nil
}

func (s *Service) abs(rel string) string {
	return filepath.Join(s.cfg.Dir, filepath.FromSlash(rel))
}

// Open 打开附件文件。
func (s *Service) Open(a *store.Attachment) (*os.File, error) {
	return os.Open(s.abs(a.Path))
}

// DataURL 读取图片并编码为 data: URL，用于发送给能看图的模型。
func (s *Service) DataURL(a *store.Attachment) (string, error) {
	data, err := os.ReadFile(s.abs(a.Path))
	if err != nil {
		return "", fmt.Errorf("读取图片 %s: %w", a.Filename, err)
	}
	return "data:" + a.MIME + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// Remove 删除附件的文件与记录。文件删除失败只记录日志。
func (s *Service) Remove(ctx context.Context, list []store.Attachment) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uint64, len(list))
	for i, a := range list {
		ids[i] = a.ID
		if err := os.Remove(s.abs(a.Path)); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("删除上传文件失败", "path", a.Path, "err", err)
		}
	}
	return s.repo.Delete(ctx, ids)
}

// CleanupStale 删除上传超过 maxAge 仍未发送的文件，返回删除数量。
func (s *Service) CleanupStale(ctx context.Context, maxAge time.Duration) (int, error) {
	total := 0
	for {
		list, err := s.repo.ListStale(ctx, s.now().Add(-maxAge), 200)
		if err != nil || len(list) == 0 {
			return total, err
		}
		if err := s.Remove(ctx, list); err != nil {
			return total, err
		}
		total += len(list)
	}
}

// RunCleanup 在后台定期清理未发送的文件，直到 ctx 取消。
func (s *Service) RunCleanup(ctx context.Context) {
	run := func() {
		if n, err := s.CleanupStale(ctx, 24*time.Hour); err != nil {
			s.log.Warn("清理未发送的上传文件失败", "err", err)
		} else if n > 0 {
			s.log.Info("已清理未发送的上传文件", "count", n)
		}
	}
	run()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

// cleanName 去掉路径与控制字符，保留原始文件名用于展示。
func cleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == "/" {
		name = "未命名"
	}
	if utf8.RuneCountInString(name) > 120 {
		ext := filepath.Ext(name)
		r := []rune(strings.TrimSuffix(name, ext))
		name = string(r[:min(len(r), 100)]) + ext
	}
	return name
}
