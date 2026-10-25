package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 搜索服务的接口地址（测试时替换）。
var (
	bochaURL       = "https://api.bochaai.com/v1/web-search"
	tavilyURL      = "https://api.tavily.com/search"
	tavilyExtract  = "https://api.tavily.com/extract"
	providerClient = &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
)

// postJSON 用 Bearer Key 发送 JSON 请求，非 200 时返回带服务名的错误。
func postJSON(ctx context.Context, service, endpoint, key string, body, out any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := providerClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求%s失败：%w", service, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		var e struct {
			Msg    string `json:"msg"`
			Detail any    `json:"detail"`
			Error  any    `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil {
			switch {
			case e.Msg != "":
				msg = e.Msg
			case e.Detail != nil:
				msg = fmt.Sprint(e.Detail)
			case e.Error != nil:
				msg = fmt.Sprint(e.Error)
			}
		}
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			msg = service + " 的 API Key 无效"
		case http.StatusPaymentRequired, 432, 433:
			msg = service + " 的额度用完了"
		case http.StatusTooManyRequests:
			msg = service + " 请求太频繁，请稍后再试"
		}
		return fmt.Errorf("HTTP %d：%s", resp.StatusCode, truncate(msg, 300))
	}
	return json.Unmarshal(raw, out)
}

// Bocha 是博查 Web Search API（国内服务）。只提供搜索。
type Bocha struct{ key string }

// NewBocha 创建博查搜索，key 为空时返回 nil。
func NewBocha(key string) *Bocha {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	return &Bocha{key: strings.TrimSpace(key)}
}

// Search 实现 Searcher。
func (b *Bocha) Search(ctx context.Context, query string, count int) ([]SearchHit, error) {
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			WebPages struct {
				Value []struct {
					Name          string `json:"name"`
					URL           string `json:"url"`
					Snippet       string `json:"snippet"`
					Summary       string `json:"summary"`
					DatePublished string `json:"datePublished"`
				} `json:"value"`
			} `json:"webPages"`
		} `json:"data"`
	}
	body := map[string]any{"query": query, "count": count, "summary": true, "freshness": "noLimit"}
	if err := postJSON(ctx, "博查", bochaURL, b.key, body, &out); err != nil {
		return nil, err
	}
	if out.Code != 0 && out.Code != 200 {
		return nil, fmt.Errorf("博查返回错误：%s", out.Msg)
	}
	hits := make([]SearchHit, 0, len(out.Data.WebPages.Value))
	for _, v := range out.Data.WebPages.Value {
		h := SearchHit{Title: v.Name, URL: v.URL, Summary: v.Summary, Date: v.DatePublished}
		if h.Summary == "" {
			h.Summary = v.Snippet
		}
		hits = append(hits, h)
	}
	return hits, nil
}

// Tavily 是 Tavily 搜索（为 AI 设计），同时提供读取网页正文（Extract）。
type Tavily struct{ key string }

// NewTavily 创建 Tavily 搜索，key 为空时返回 nil。
func NewTavily(key string) *Tavily {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	return &Tavily{key: strings.TrimSpace(key)}
}

// Search 实现 Searcher。
func (t *Tavily) Search(ctx context.Context, query string, count int) ([]SearchHit, error) {
	var out struct {
		Results []struct {
			Title         string `json:"title"`
			URL           string `json:"url"`
			Content       string `json:"content"`
			PublishedDate string `json:"published_date"`
		} `json:"results"`
	}
	body := map[string]any{"query": query, "max_results": count, "search_depth": "basic", "topic": "general"}
	if err := postJSON(ctx, "Tavily", tavilyURL, t.key, body, &out); err != nil {
		return nil, err
	}
	hits := make([]SearchHit, 0, len(out.Results))
	for _, r := range out.Results {
		hits = append(hits, SearchHit{Title: r.Title, URL: r.URL, Summary: r.Content, Date: r.PublishedDate})
	}
	return hits, nil
}

// Fetch 实现 Fetcher（Tavily Extract）。
func (t *Tavily) Fetch(ctx context.Context, url string) (Page, error) {
	var out struct {
		Results []struct {
			URL        string `json:"url"`
			RawContent string `json:"raw_content"`
		} `json:"results"`
		FailedResults []struct {
			Error string `json:"error"`
		} `json:"failed_results"`
	}
	if err := postJSON(ctx, "Tavily", tavilyExtract, t.key, map[string]any{"urls": []string{url}, "extract_depth": "basic"}, &out); err != nil {
		return Page{}, err
	}
	if len(out.Results) == 0 {
		msg := "无法获取这个网页的内容"
		if len(out.FailedResults) > 0 && out.FailedResults[0].Error != "" {
			msg += "：" + out.FailedResults[0].Error
		}
		return Page{}, fmt.Errorf("%s", msg)
	}
	return Page{Content: out.Results[0].RawContent}, nil
}
