package uploads

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func newService(t *testing.T, quotaMB int) (*Service, *gormstore.DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(dir, "u.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	up := filepath.Join(dir, "uploads")
	return New(db.Attachments(), config.UploadsConfig{Dir: up, MaxFileMB: 1, MaxFiles: 4, UserQuotaMB: quotaMB, DocChars: 100}, nil), db, up
}

func pngBytes(t *testing.T) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func docxBytes(t *testing.T) []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, _ := zw.Create("word/document.xml")
	fmt.Fprint(w, `<?xml version="1.0"?><w:document xmlns:w="w"><w:body>`+
		`<w:p><w:r><w:t>第一段</w:t><w:tab/><w:t>内容</w:t></w:r></w:p><w:p><w:r><w:t>第二段</w:t></w:r></w:p></w:body></w:document>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pdfBytes 生成只有一行文字的最小 PDF（带正确的 xref 偏移）。
func pdfBytes(text string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		"", // 内容流
		"<< /Type /Font /Subtype /Type1 /BaseFont /Courier /Encoding /WinAnsiEncoding /FirstChar 32 /LastChar 126 /Widths [" +
			strings.TrimSpace(strings.Repeat("600 ", 95)) + "] >>",
	}
	stream := fmt.Sprintf("BT /F1 12 Tf 20 100 Td (%s) Tj ET", text)
	objs[3] = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream)
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

func TestSaveKinds(t *testing.T) {
	ctx := context.Background()
	s, _, dir := newService(t, 0)
	gbk, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("你好，布谷"))

	cases := []struct {
		name, file string
		data       []byte
		kind, text string
	}{
		{"png 按内容识别", "photo.txt", pngBytes(t), store.AttachmentImage, ""},
		{"utf8 文本", "note.md", []byte("\xef\xbb\xbf# 标题\n正文"), store.AttachmentDocument, "# 标题\n正文"},
		{"gbk 文本", "old.txt", gbk, store.AttachmentDocument, "你好，布谷"},
		{"docx", "doc.docx", docxBytes(t), store.AttachmentDocument, "第一段\t内容\n第二段"},
		{"pdf", "a.pdf", pdfBytes("Hello Buugoo"), store.AttachmentDocument, "Hello Buugoo"},
	}
	for _, tc := range cases {
		a, err := s.Save(ctx, 7, tc.file, bytes.NewReader(tc.data))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if a.Kind != tc.kind || a.Text != tc.text || a.ID == 0 || a.Size != int64(len(tc.data)) {
			t.Fatalf("%s: %+v", tc.name, a)
		}
		if !strings.HasPrefix(a.Path, "7/") {
			t.Fatalf("%s: 文件应按用户分目录: %s", tc.name, a.Path)
		}
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(a.Path)))
		if err != nil || !bytes.Equal(got, tc.data) {
			t.Fatalf("%s: 文件内容不一致: %v", tc.name, err)
		}
	}
	if a, _ := s.Save(ctx, 7, "x.png", bytes.NewReader(pngBytes(t))); a != nil {
		u, err := s.DataURL(a)
		if err != nil || !strings.HasPrefix(u, "data:image/png;base64,") {
			t.Fatalf("DataURL: %q %v", u[:min(len(u), 30)], err)
		}
	}

	for _, bad := range []struct {
		file string
		data []byte
		kind error
	}{
		{"bin.exe", []byte{0x4d, 0x5a, 0, 1, 2}, ErrUnsupported},
		{"old.doc", []byte("whatever"), ErrUnsupported},
		{"empty.txt", nil, ErrEmpty},
		{"blank.txt", []byte("   \n "), ErrNoText},
		{"big.txt", bytes.Repeat([]byte("a"), 1<<20+1), ErrTooLarge},
	} {
		_, err := s.Save(ctx, 7, bad.file, bytes.NewReader(bad.data))
		var ue *UserError
		if !errors.As(err, &ue) || !errors.Is(err, bad.kind) {
			t.Errorf("%s: 应返回 %v，得到 %v", bad.file, bad.kind, err)
		}
	}

	// 能运行代码时，读不出文字的文件也可以上传（交给代码处理）；空文件、太大的照样拒绝。
	s.SetAnyFile(true)
	a, err := s.Save(ctx, 7, "表格.xlsx", bytes.NewReader([]byte("PK\x03\x04\x00\x00 zip")))
	if err != nil || a.Kind != store.AttachmentFile || !strings.HasSuffix(a.Path, ".xlsx") {
		t.Fatalf("%+v %v", a, err)
	}
	if _, err := s.Save(ctx, 7, "empty.bin", bytes.NewReader(nil)); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
	if a, err := s.Save(ctx, 7, "weird.$%", bytes.NewReader([]byte{0, 1, 2})); err != nil || strings.Contains(a.Path, "$") {
		t.Fatalf("%+v %v", a, err)
	}
	// 发给用户的文件：图片按图片保存，其他文件只能下载。
	png := pngBytes(t)
	if a, err := s.SaveSent(ctx, 7, 1, 2, "图.png", png); err != nil || a.Kind != store.AttachmentImage || a.Source != store.AttachmentGenerated {
		t.Fatalf("%+v %v", a, err)
	}
	if a, err := s.SaveSent(ctx, 7, 1, 2, "结果.csv", []byte("a,b")); err != nil || a.Kind != store.AttachmentFile || *a.MessageID != 2 {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestQuotaAndCleanup(t *testing.T) {
	ctx := context.Background()
	s, db, dir := newService(t, 1)
	big := bytes.Repeat([]byte("字"), (600<<10)/3)
	a, err := s.Save(ctx, 1, "a.txt", bytes.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, 1, "b.txt", bytes.NewReader(big)); !errors.Is(err, ErrQuota) {
		t.Fatalf("应超出空间: %v", err)
	}
	// 其他用户不受影响。
	if _, err := s.Save(ctx, 2, "b.txt", bytes.NewReader(big)); err != nil {
		t.Fatal(err)
	}

	// 关联到消息后不会被清理；未发送的超过时限会被清理。
	if err := db.Attachments().Attach(ctx, 1, []uint64{a.ID}, 3, 9); err != nil {
		t.Fatal(err)
	}
	if err := db.Attachments().Attach(ctx, 1, []uint64{a.ID}, 3, 9); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("已发送的附件不能再次关联")
	}
	s.now = func() time.Time { return time.Now().Add(48 * time.Hour) }
	n, err := s.CleanupStale(ctx, 24*time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, err := db.Attachments().Get(ctx, 1, a.ID); err != nil {
		t.Fatal("已发送的附件不应被清理")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "2"))
	for _, e := range entries {
		files, _ := os.ReadDir(filepath.Join(dir, "2", e.Name()))
		if len(files) != 0 {
			t.Fatalf("文件应被删除: %v", files)
		}
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"../../etc/passwd":                "passwd",
		`C:\Users\a\图片.png`:               "图片.png",
		"a\x00b\n.txt":                    "ab.txt",
		"":                                "未命名",
		strings.Repeat("长", 150) + ".pdf": strings.Repeat("长", 100) + ".pdf",
	} {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFileManagement(t *testing.T) {
	ctx := context.Background()
	s, db, _ := newService(t, 1)
	big := bytes.Repeat([]byte("字"), (600<<10)/3)
	old, err := s.Save(ctx, 1, "old.txt", bytes.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Attachments().Attach(ctx, 1, []uint64{old.ID}, 3, 9)
	img, err := s.SaveGenerated(ctx, 1, 3, 10, "chart.png", pngBytes(t))
	if err != nil || img.Source != store.AttachmentGenerated || old.Source != store.AttachmentUploaded {
		t.Fatalf("img=%+v err=%v", img, err)
	}
	pending, _ := s.Save(ctx, 1, "p.txt", strings.NewReader("还没发送"))

	list, total, err := db.Attachments().ListFiles(ctx, 1, "", 10, 0)
	if err != nil || total != 2 || len(list) != 2 || list[0].ID != img.ID || list[1].Text != "" {
		t.Fatalf("文件列表只含已发送的，按时间倒序，不读文字: %+v total=%d err=%v", list, total, err)
	}
	if _, n, _ := db.Attachments().ListFiles(ctx, 1, store.AttachmentImage, 10, 0); n != 1 {
		t.Fatalf("按类型筛选: %d", n)
	}

	// 删除已发送的文件：文件删掉、记录保留并标记删除、不再计入用量。
	u0, _ := s.Usage(ctx, 1)
	n, err := s.Delete(ctx, 1, []uint64{old.ID, 9999})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	got, err := db.Attachments().Get(ctx, 1, old.ID)
	if err != nil || got.DeletedAt == nil || got.Text != "" {
		t.Fatalf("应保留记录并标记删除: %+v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(s.cfg.Dir, got.Path)); !os.IsNotExist(err) {
		t.Fatal("文件应删除")
	}
	u1, _ := s.Usage(ctx, 1)
	if u1.UsedBytes != u0.UsedBytes-old.Size {
		t.Fatalf("用量 %d → %d", u0.UsedBytes, u1.UsedBytes)
	}
	if n, _ := s.Delete(ctx, 2, []uint64{img.ID}); n != 0 {
		t.Fatal("不能删除别人的文件")
	}

	// 删除 7 天前的全部文件：未发送的直接删除记录。
	s.now = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	n, err = s.DeleteBefore(ctx, 1, s.now().AddDate(0, 0, -7))
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, err := db.Attachments().Get(ctx, 1, pending.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("未发送的文件应直接删除")
	}
	if u, _ := s.Usage(ctx, 1); u.UsedBytes != 0 {
		t.Fatalf("用量应清零: %+v", u)
	}
}

func TestQuotaFullBlocksAllUploads(t *testing.T) {
	ctx := context.Background()
	s, db, _ := newService(t, 1)
	// 管理员调低了限额等情况下，用量可能已经超过上限：此时连很小的文件也不能上传。
	big := bytes.Repeat([]byte("a"), 900<<10)
	if _, err := s.Save(ctx, 1, "a.txt", bytes.NewReader(big)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, 1, "b.txt", bytes.NewReader(big)); !errors.Is(err, ErrQuota) || !strings.Contains(err.Error(), "还剩") {
		t.Fatalf("空间不够: %v", err)
	}
	if err := db.Attachments().Create(ctx, &store.Attachment{UserID: 1, Kind: store.AttachmentDocument, Filename: "x", MIME: "text/plain", Size: 200 << 10, Path: "x", SHA256: "x"}); err != nil {
		t.Fatal(err)
	}
	u, _ := s.Usage(ctx, 1)
	if !u.Full() {
		t.Fatalf("应已满: %+v", u)
	}
	if _, err := s.Save(ctx, 1, "tiny.txt", strings.NewReader("hi")); !errors.Is(err, ErrQuota) || !strings.Contains(err.Error(), "用满") {
		t.Fatalf("用满后不能上传任何文件: %v", err)
	}
	if _, err := s.SaveGenerated(ctx, 1, 1, 1, "a.png", pngBytes(t)); !errors.Is(err, ErrQuota) {
		t.Fatalf("用满后代码生成的图片也不保存: %v", err)
	}
}
