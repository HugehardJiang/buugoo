package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"github.com/HugehardJiang/buugoo/internal/netguard"
)

// maxPageBytes 是直接读取网页时最多下载的字节数。
const maxPageBytes = 3 << 20

// Direct 由服务器直接下载网页并提取正文，用于搜索服务不提供读取网页时（例如博查）。
// 只能访问公网地址，防止借此访问服务器内网。
type Direct struct{ client *http.Client }

// NewDirect 创建直接读取网页的 Fetcher。
func NewDirect() *Direct {
	return &Direct{client: &http.Client{
		Timeout:   25 * time.Second,
		Transport: netguard.PublicTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("重定向次数过多")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("不支持的网址")
			}
			return nil
		},
	}}
}

// Fetch 实现 Fetcher。
func (d *Direct) Fetch(ctx context.Context, url string) (Page, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Page{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BuugooBot/1.0; +https://github.com/HugehardJiang/buugoo)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.5")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.6")
	resp, err := d.client.Do(req)
	if err != nil {
		if errors.Is(err, netguard.ErrPrivateAddress) {
			return Page{}, netguard.ErrPrivateAddress
		}
		return Page{}, fmt.Errorf("打不开这个网页：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Page{}, fmt.Errorf("网页返回 HTTP %d", resp.StatusCode)
	}
	ctype, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	body := io.LimitReader(resp.Body, maxPageBytes)
	switch {
	case ctype == "" || ctype == "text/html" || ctype == "application/xhtml+xml":
		r, err := charset.NewReader(body, resp.Header.Get("Content-Type"))
		if err != nil {
			return Page{}, err
		}
		return extractHTML(r)
	case strings.HasPrefix(ctype, "text/") || ctype == "application/json":
		b, err := io.ReadAll(body)
		if err != nil {
			return Page{}, err
		}
		if !utf8.Valid(b) {
			return Page{}, errors.New("网页编码无法识别")
		}
		return Page{Content: string(b)}, nil
	default:
		return Page{}, fmt.Errorf("这不是网页（%s），读取不了", ctype)
	}
}

// skipTags 是不包含正文的标签。
var skipTags = map[string]bool{"script": true, "style": true, "noscript": true, "nav": true, "footer": true, "header": true,
	"aside": true, "form": true, "svg": true, "iframe": true, "template": true, "button": true, "select": true}

// blockTags 结束时换行。
var blockTags = map[string]bool{"p": true, "div": true, "br": true, "li": true, "tr": true, "section": true, "article": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "pre": true, "blockquote": true, "table": true, "ul": true, "ol": true}

// extractHTML 提取标题与正文文字：去掉脚本、导航等，合并多余空白。
func extractHTML(r io.Reader) (Page, error) {
	z := html.NewTokenizer(r)
	var title, text strings.Builder
	skip, inTitle := 0, false
	for {
		switch z.Next() {
		case html.ErrorToken:
			if err := z.Err(); err != io.EOF {
				return Page{}, err
			}
			return Page{Title: strings.TrimSpace(title.String()), Content: tidy(text.String())}, nil
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			tag := string(name)
			switch {
			case skipTags[tag]:
				if tag != "br" {
					skip++
				}
			case tag == "title":
				inTitle = true
			case blockTags[tag]:
				text.WriteByte('\n')
				if strings.HasPrefix(tag, "h") && len(tag) == 2 {
					text.WriteString(strings.Repeat("#", int(tag[1]-'0')) + " ")
				} else if tag == "li" {
					text.WriteString("- ")
				}
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			switch {
			case skipTags[tag] && skip > 0:
				skip--
			case tag == "title":
				inTitle = false
			case blockTags[tag]:
				text.WriteByte('\n')
			}
		case html.TextToken:
			t := string(z.Text())
			if inTitle {
				title.WriteString(t)
			} else if skip == 0 {
				text.WriteString(strings.Join(strings.Fields(t), " "))
				text.WriteByte(' ')
			}
		}
	}
}

// tidy 去掉行首尾空白与多余空行。
func tidy(s string) string {
	var out []string
	blank := false
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "-" || strings.Trim(line, "#") == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		out = append(out, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
