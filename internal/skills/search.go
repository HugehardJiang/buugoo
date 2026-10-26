package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RepoHit 是在 GitHub 上搜到的一个仓库。
type RepoHit struct {
	Name        string `json:"name"` // 用户/仓库
	URL         string `json:"url"`
	Description string `json:"description"`
	Stars       int    `json:"stars"`
	Updated     string `json:"updated"`
}

// SearchGitHub 在 GitHub 上按关键词搜可能含有 skill 的公开仓库（按星数排序）。
// 没有登录的请求每分钟只能搜 10 次。
func (m *Manager) SearchGitHub(ctx context.Context, query string, n int) ([]RepoHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, &UserError{Msg: "要有搜索关键词"}
	}
	if n <= 0 || n > 20 {
		n = 8
	}
	q := query
	if !strings.Contains(strings.ToLower(q), "skill") {
		q += " skill"
	}
	u := fmt.Sprintf("%s/search/repositories?q=%s&sort=stars&order=desc&per_page=%d", githubAPI, url.QueryEscape(q), n)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "buugoo")
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, &UserError{Msg: fmt.Sprintf("连不上 GitHub：%v", err)}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return nil, &UserError{Msg: "GitHub 暂时不让搜了（每分钟最多搜 10 次），过一会儿再试"}
	case resp.StatusCode != http.StatusOK:
		return nil, &UserError{Msg: fmt.Sprintf("GitHub 搜索失败：HTTP %d", resp.StatusCode)}
	}
	var body struct {
		Items []struct {
			FullName    string `json:"full_name"`
			HTMLURL     string `json:"html_url"`
			Description string `json:"description"`
			Stars       int    `json:"stargazers_count"`
			PushedAt    string `json:"pushed_at"`
		} `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return nil, errors.New("GitHub 返回的内容看不懂")
	}
	out := make([]RepoHit, 0, len(body.Items))
	for _, it := range body.Items {
		out = append(out, RepoHit{Name: it.FullName, URL: it.HTMLURL, Description: it.Description, Stars: it.Stars, Updated: dateOnly(it.PushedAt)})
	}
	return out, nil
}

func dateOnly(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// PreviewGitHub 下载 GitHub 上的仓库（或其中的文件夹），列出里面有哪些 skill，不安装。
func (m *Manager) PreviewGitHub(ctx context.Context, rawURL string) ([]Candidate, error) {
	g, err := ParseGitHub(rawURL)
	if err != nil {
		return nil, &UserError{Msg: err.Error()}
	}
	t, err := downloadGitHub(ctx, m.http, g)
	if err != nil {
		return nil, &UserError{Msg: err.Error()}
	}
	return discover(t, g.Path), nil
}
