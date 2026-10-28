package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeepSeek 调用 DeepSeek 的 Search 与 Fetch 接口（POST /v1/tools/search、/v1/tools/fetch），
// 使用与聊天相同的 Bearer Key。
type DeepSeek struct {
	base string
	key  string
	http *http.Client
}

// DeepSeekBase 是 DeepSeek 官方接口的地址（专用的搜索 Key 用它）。
const DeepSeekBase = "https://api.deepseek.com"

// NewDeepSeek 在模型地址属于 DeepSeek（api.deepseek.com）且有 Key 时返回客户端，否则返回 nil。
func NewDeepSeek(modelBaseURL, key string) *DeepSeek {
	u, err := url.Parse(modelBaseURL)
	if err != nil || key == "" || !strings.EqualFold(u.Hostname(), "api.deepseek.com") {
		return nil
	}
	return &DeepSeek{base: u.Scheme + "://" + u.Host, key: key, http: &http.Client{Timeout: 60 * time.Second}}
}

// newDeepSeekAt 用于测试，指定接口地址。
func newDeepSeekAt(base, key string) *DeepSeek {
	return &DeepSeek{base: strings.TrimRight(base, "/"), key: key, http: &http.Client{Timeout: 10 * time.Second}}
}

func (d *DeepSeek) post(ctx context.Context, path string, body, out any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.base+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+d.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败：%w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		msg := strings.TrimSpace(string(raw))
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		switch resp.StatusCode {
		case http.StatusPaymentRequired:
			msg = "DeepSeek 账户余额不足"
		case http.StatusNotFound:
			if path == "/v1/tools/fetch" {
				msg = "无法获取这个网页的内容（网址不可用、被站点拦截或没有正文）"
			}
		}
		return fmt.Errorf("HTTP %d：%s", resp.StatusCode, msg)
	}
	return json.Unmarshal(raw, out)
}

type searchResult struct {
	Title    string  `json:"title"`
	URL      string  `json:"url"`
	Snippet  string  `json:"snippet"`
	Summary  string  `json:"summary"`
	SiteName *string `json:"site_name"`
	Date     *string `json:"date"`
}

// Search 实现 Searcher。
func (d *DeepSeek) Search(ctx context.Context, query string, count int) ([]SearchHit, error) {
	var out struct {
		Results []searchResult `json:"results"`
	}
	if err := d.post(ctx, "/v1/tools/search", map[string]any{"query": query, "count": count}, &out); err != nil {
		return nil, err
	}
	hits := make([]SearchHit, 0, len(out.Results))
	for _, r := range out.Results {
		h := SearchHit{Title: r.Title, URL: r.URL, Summary: r.Summary}
		if h.Summary == "" {
			h.Summary = r.Snippet
		}
		if r.Date != nil {
			h.Date = *r.Date
		}
		hits = append(hits, h)
	}
	return hits, nil
}

// Fetch 实现 Fetcher。
func (d *DeepSeek) Fetch(ctx context.Context, url string) (Page, error) {
	var out Page
	err := d.post(ctx, "/v1/tools/fetch", map[string]any{"url": url}, &out)
	return out, err
}
