package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// SearchHit 是一条搜索结果。
type SearchHit struct {
	Title   string
	URL     string
	Summary string
	// Date 是发布日期（可以为空，格式不限，只取前 10 个字符）。
	Date string
}

// Searcher 是联网搜索服务（DeepSeek、博查、Tavily）。
type Searcher interface {
	Search(ctx context.Context, query string, count int) ([]SearchHit, error)
}

// Page 是读取到的网页。
type Page struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// Fetcher 读取网页正文。
type Fetcher interface {
	Fetch(ctx context.Context, url string) (Page, error)
}

// maxFetchChars 是网页正文返回给模型的最多字数。
const maxFetchChars = 12000

func searchHandler(s Searcher) handler {
	return func(ctx context.Context, args json.RawMessage) Result {
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
		hits, err := s.Search(ctx, in.Query, in.Count)
		if err != nil {
			res.Output, res.Summary = "搜索失败："+err.Error(), "搜索失败"
			return res
		}
		if len(hits) == 0 {
			res.OK, res.Output, res.Summary = true, "没有搜索到结果。", "没有结果"
			return res
		}
		if len(hits) > in.Count {
			hits = hits[:in.Count]
		}
		var model, detail strings.Builder
		for i, h := range hits {
			fmt.Fprintf(&model, "[%d] %s\n链接：%s\n", i+1, h.Title, h.URL)
			if len(h.Date) >= 10 {
				fmt.Fprintf(&model, "日期：%s\n", h.Date[:10])
			}
			fmt.Fprintf(&model, "摘要：%s\n\n", truncate(strings.TrimSpace(h.Summary), 600))
			fmt.Fprintf(&detail, "%d. %s\n%s\n", i+1, h.Title, h.URL)
		}
		res.OK = true
		res.Output = strings.TrimSpace(model.String())
		res.Summary = fmt.Sprintf("找到 %d 条结果", len(hits))
		res.Detail = strings.TrimSpace(detail.String())
		return res
	}
}

func fetchHandler(f Fetcher) handler {
	return func(ctx context.Context, args json.RawMessage) Result {
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
		p, err := f.Fetch(ctx, in.URL)
		if err != nil {
			res.Output, res.Summary = "读取失败："+err.Error(), "读取失败"
			return res
		}
		if strings.TrimSpace(p.Content) == "" {
			res.Output, res.Summary = "这个网页没有可读的正文", "没有正文"
			return res
		}
		res.OK = true
		res.Output = truncate(fmt.Sprintf("# %s\n\n%s", p.Title, p.Content), maxFetchChars)
		res.Summary = p.Title
		if res.Summary == "" {
			res.Summary = "已读取"
		}
		return res
	}
}
