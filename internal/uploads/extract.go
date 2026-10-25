package uploads

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"rsc.io/pdf"
)

// maxStoredText 是保存的文档文字上限（字节）。放进对话时再按 doc_chars 截断。
const maxStoredText = 1 << 20

// maxPDFPages 是提取文字的最大页数，避免超大 PDF 占用太多时间。
const maxPDFPages = 300

// SupportedHint 是支持的文件类型说明，用于错误提示。
const SupportedHint = "图片（JPG、PNG、GIF、WebP）和文档（PDF、Word .docx、TXT、Markdown、CSV、JSON、代码等纯文本文件）"

// extractText 从文档中提取文字，返回文字与 MIME 类型。
func extractText(filename string, data []byte) (string, string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	var (
		text string
		mime string
		err  error
	)
	switch {
	case bytes.HasPrefix(data, []byte("%PDF-")):
		mime = "application/pdf"
		text, err = pdfText(data)
		if err == nil && strings.TrimSpace(text) == "" {
			return "", "", userErr(ErrNoText, "没有从这个 PDF 中提取到文字（可能是扫描件或图片），可以截图后作为图片发送")
		}
	case ext == ".docx" && bytes.HasPrefix(data, []byte("PK")):
		mime = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		text, err = docxText(data)
	case ext == ".doc" || ext == ".xls" || ext == ".ppt":
		return "", "", userErr(ErrUnsupported, "暂不支持旧版 Office 格式（%s），请另存为 .docx 或 PDF 后再上传", ext)
	default:
		var ok bool
		text, ok = plainText(data)
		if !ok {
			return "", "", userErr(ErrUnsupported, "不支持这种文件。可以上传%s", SupportedHint)
		}
		mime = "text/plain; charset=utf-8"
	}
	if err != nil {
		return "", "", userErr(ErrUnsupported, "无法读取文件内容：%v", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", userErr(ErrNoText, "没有从文件中提取到文字")
	}
	if len(text) > maxStoredText {
		text = truncateBytes(text, maxStoredText)
	}
	return text, mime, nil
}

// plainText 把纯文本文件解码为 UTF-8：优先 UTF-8（去掉 BOM），否则尝试 GB18030（兼容 GBK）。
// 含有 NUL 字节或解码后大量乱码的视为二进制文件。
func plainText(data []byte) (string, bool) {
	if bytes.IndexByte(data, 0) >= 0 {
		return "", false
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if utf8.Valid(data) {
		return string(data), true
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data)
	if err != nil || !utf8.Valid(decoded) || strings.Count(string(decoded), "�") > len(decoded)/100 {
		return "", false
	}
	return string(decoded), true
}

// pdfText 逐页提取 PDF 中的文字，按行拼接。PDF 解析库遇到损坏的文件可能 panic，这里转换为错误。
func pdfText(data []byte) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("PDF 文件无法解析")
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("PDF 文件无法解析（可能已加密或损坏）")
	}
	var b strings.Builder
	for i := 1; i <= min(r.NumPage(), maxPDFPages); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		var lastY, lastEnd float64
		first := true
		for _, t := range p.Content().Text {
			switch {
			case first:
			case t.Y < lastY-t.FontSize*0.5 || t.Y > lastY+t.FontSize*0.5:
				b.WriteByte('\n')
			case t.X > lastEnd+t.FontSize*0.15 && t.S != " ":
				// 字形之间有明显间隔：PDF 中的空格常常不是字符，而是位移。
				b.WriteByte(' ')
			}
			b.WriteString(t.S)
			lastY, lastEnd, first = t.Y, t.X+t.W, false
		}
		b.WriteString("\n\n")
		if b.Len() > maxStoredText {
			break
		}
	}
	return b.String(), nil
}

// docxText 从 Word 文档的 word/document.xml 中提取段落文字。
func docxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("Word 文件无法解析")
	}
	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
			break
		}
	}
	if doc == nil {
		return "", fmt.Errorf("不是有效的 Word 文档")
	}
	rc, err := doc.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	dec := xml.NewDecoder(io.LimitReader(rc, 64<<20))
	var b strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("Word 文件无法解析")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				b.WriteByte('\t')
			case "br", "cr":
				b.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				b.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
		if b.Len() > maxStoredText {
			break
		}
	}
	return b.String(), nil
}

// truncateBytes 在不超过 n 字节的前提下按完整字符截断。
func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// Truncate 截取前 n 个字，返回截断后的文字与是否截断。
func Truncate(s string, n int) (string, bool) {
	if utf8.RuneCountInString(s) <= n {
		return s, false
	}
	r := []rune(s)
	return string(r[:n]), true
}
