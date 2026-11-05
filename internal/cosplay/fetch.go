package cosplay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html/charset"

	"github.com/HugehardJiang/buugoo/internal/netguard"
)

// maxFetchBytes 是从网址导入时最多下载多少（aigengtu.com/persona 整页约 0.6 MB）。
const maxFetchBytes = 8 << 20

type fetcher struct{ client *http.Client }

func newFetcher() *fetcher {
	return &fetcher{client: &http.Client{
		Timeout: 30 * time.Second,
		// 只能访问公网地址，防止借导入功能访问服务器内网。
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

// FetchURL 打开网址，解析出里面的角色卡（网页里的卡片、<pre> 全文，或者直接是文本 / JSON 文件）。
func (s *Service) FetchURL(ctx context.Context, raw string) ([]Candidate, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, &UserError{"请填写完整的网址，例如 https://aigengtu.com/persona"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, &UserError{"网址不对"}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BuugooBot/1.0; +https://github.com/HugehardJiang/buugoo)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,application/json;q=0.9,*/*;q=0.5")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.6")
	resp, err := s.fetcher.client.Do(req)
	if err != nil {
		if errors.Is(err, netguard.ErrPrivateAddress) {
			return nil, &UserError{"不能从内网地址导入"}
		}
		return nil, &UserError{fmt.Sprintf("打不开这个网址：%v", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &UserError{fmt.Sprintf("网址返回了 HTTP %d", resp.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
	if err != nil {
		return nil, &UserError{fmt.Sprintf("读取网页失败：%v", err)}
	}
	if len(body) > maxFetchBytes {
		return nil, &UserError{"这个网页太大了"}
	}
	ctype, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	source := resp.Request.URL.String()
	if ctype == "" || ctype == "text/html" || ctype == "application/xhtml+xml" {
		r, err := charset.NewReader(bytes.NewReader(body), resp.Header.Get("Content-Type"))
		if err != nil {
			return nil, &UserError{"网页编码无法识别"}
		}
		return s.ParseHTML(r, source)
	}
	if !strings.HasPrefix(ctype, "text/") && ctype != "application/json" {
		return nil, &UserError{"这个网址不是网页或文本（PNG 角色卡请先导出成 JSON）"}
	}
	if !utf8.Valid(body) {
		return nil, &UserError{"文件编码无法识别，请用 UTF-8"}
	}
	c, err := s.ParseText(string(body), u.Path)
	if err != nil {
		return nil, err
	}
	c.Source = source
	return []Candidate{c}, nil
}
