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

func (d *DeepSeek) search(ctx context.Context, args json.RawMessage) Result {
	var in struct {
		Query string `json:"query"`
		Count int    `json:"count"`
	}
	_ = json.Unmarshal(args, &in)
	in.Query = strings.TrimSpace(in.Query)
	res := Result{Input: in.Query}
	if in.Query == "" {
		res.Output, res.Summary = "搜索词不能为空", "搜索词为空"
		return res
	}
	if r := []rune(in.Query); len(r) > 128 {
		in.Query = string(r[:128])
	}
	if in.Count <= 0 || in.Count > 10 {
		in.Count = 5
	}
	var out struct {
		Results []searchResult `json:"results"`
	}
	if err := d.post(ctx, "/v1/tools/search", map[string]any{"query": in.Query, "count": in.Count}, &out); err != nil {
		res.Output, res.Summary = "搜索失败："+err.Error(), "搜索失败"
		return res
	}
	if len(out.Results) == 0 {
		res.OK, res.Output, res.Summary = true, "没有搜索到结果。", "没有结果"
		return res
	}
	var model, detail strings.Builder
	for i, r := range out.Results {
		date := ""
		if r.Date != nil && len(*r.Date) >= 10 {
			date = (*r.Date)[:10]
		}
		summary := r.Summary
		if summary == "" {
			summary = r.Snippet
		}
		fmt.Fprintf(&model, "[%d] %s\n链接：%s\n", i+1, r.Title, r.URL)
		if date != "" {
			fmt.Fprintf(&model, "日期：%s\n", date)
		}
		fmt.Fprintf(&model, "摘要：%s\n\n", summary)
		fmt.Fprintf(&detail, "%d. %s\n%s\n", i+1, r.Title, r.URL)
	}
	res.OK = true
	res.Output = strings.TrimSpace(model.String())
	res.Summary = fmt.Sprintf("找到 %d 条结果", len(out.Results))
	res.Detail = strings.TrimSpace(detail.String())
	return res
}

// maxFetchChars 是网页正文返回给模型的最多字数。
const maxFetchChars = 12000

func (d *DeepSeek) fetch(ctx context.Context, args json.RawMessage) Result {
	var in struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(args, &in)
	in.URL = strings.TrimSpace(in.URL)
	res := Result{Input: in.URL}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		res.Output, res.Summary = "网址无效：需要 http(s) 开头的完整网址", "网址无效"
		return res
	}
	var out struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := d.post(ctx, "/v1/tools/fetch", map[string]any{"url": in.URL}, &out); err != nil {
		res.Output, res.Summary = "读取失败："+err.Error(), "读取失败"
		return res
	}
	res.OK = true
	res.Output = truncate(fmt.Sprintf("# %s\n\n%s", out.Title, out.Content), maxFetchChars)
	res.Summary = out.Title
	if res.Summary == "" {
		res.Summary = "已读取"
	}
	return res
}
