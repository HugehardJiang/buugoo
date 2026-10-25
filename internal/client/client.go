// Package client 是 Buugoo HTTP API 的 Go 客户端，供 buugoo chat 使用（以后的 App 可参考）。
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client 是 API 客户端。
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New 创建客户端。baseURL 形如 http://127.0.0.1:8080。
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 60 * time.Second}},
	}
}

// Error 是服务端返回的错误。
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// IsUnauthorized 报告错误是否为未登录或令牌失效。
func IsUnauthorized(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusUnauthorized
}

// Meta 是站点信息。
type Meta struct {
	Name             string `json:"name"`
	Version          string `json:"version"`
	Mode             string `json:"mode"`
	PasswordRequired bool   `json:"password_required"`
	Registration     string `json:"registration"`
	EmbeddingReady   bool   `json:"embedding_ready"`
}

// User 是用户信息。
type User struct {
	ID          uint64 `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
}

// Session 是登录结果。
type Session struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      User      `json:"user"`
}

// Message 是一条消息。
type Message struct {
	ID        uint64    `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// UsedMemory 是本轮使用的记忆。
type UsedMemory struct {
	ID      uint64 `json:"id"`
	Content string `json:"content"`
	Source  string `json:"source"`
	Score   struct {
		Recency    float64 `json:"recency"`
		Importance float64 `json:"importance"`
		Relevance  float64 `json:"relevance"`
		Total      float64 `json:"total"`
	} `json:"score"`
}

// Memory 是一条记忆。
type Memory struct {
	ID          uint64    `json:"id"`
	Type        string    `json:"type"`
	Content     string    `json:"content"`
	Importance  int       `json:"importance"`
	Source      string    `json:"source"`
	AccessCount int       `json:"access_count"`
	CreatedAt   time.Time `json:"created_at"`
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+"/api/v1"+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("连接 %s 失败（服务是否已启动？）: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return readError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("解析响应: %w", err)
	}
	return nil
}

func readError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	e := &Error{Status: resp.StatusCode, Code: "http_error", Message: fmt.Sprintf("请求失败（HTTP %d）", resp.StatusCode)}
	var parsed struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &parsed) == nil && parsed.Error.Message != "" {
		e.Code, e.Message = parsed.Error.Code, parsed.Error.Message
	}
	return e
}

// Meta 获取站点信息。
func (c *Client) Meta(ctx context.Context) (*Meta, error) {
	var m Meta
	return &m, c.do(ctx, http.MethodGet, "/meta", nil, &m)
}

// Login 登录。single 模式 username 为空。
func (c *Client) Login(ctx context.Context, username, password string) (*Session, error) {
	var s Session
	if err := c.do(ctx, http.MethodPost, "/auth/login", map[string]string{"username": username, "password": password}, &s); err != nil {
		return nil, err
	}
	c.Token = s.Token
	return &s, nil
}

// Me 获取当前用户。
func (c *Client) Me(ctx context.Context) (*User, error) {
	var r struct {
		User User `json:"user"`
	}
	return &r.User, c.do(ctx, http.MethodGet, "/me", nil, &r)
}

// HasModel 报告当前用户是否有可用的聊天模型。旧版本服务端不返回该字段时视为有。
func (c *Client) HasModel(ctx context.Context) (bool, error) {
	var r struct {
		HasModel *bool `json:"has_model"`
	}
	if err := c.do(ctx, http.MethodGet, "/me", nil, &r); err != nil {
		return false, err
	}
	return r.HasModel == nil || *r.HasModel, nil
}

// Messages 分页获取最近一段对话的历史（按时间正序）。
func (c *Client) Messages(ctx context.Context, beforeID uint64, limit int) ([]Message, bool, error) {
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if beforeID > 0 {
		q.Set("before_id", strconv.FormatUint(beforeID, 10))
	}
	var r struct {
		Messages []Message `json:"messages"`
		HasMore  bool      `json:"has_more"`
	}
	err := c.do(ctx, http.MethodGet, "/messages?"+q.Encode(), nil, &r)
	return r.Messages, r.HasMore, err
}

// Memories 列出记忆。
func (c *Client) Memories(ctx context.Context, query string, limit int) ([]Memory, int, error) {
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if query != "" {
		q.Set("q", query)
	}
	var r struct {
		Memories []Memory `json:"memories"`
		Total    int      `json:"total"`
	}
	err := c.do(ctx, http.MethodGet, "/memories?"+q.Encode(), nil, &r)
	return r.Memories, r.Total, err
}

// DeleteMemory 删除记忆。
func (c *Client) DeleteMemory(ctx context.Context, id uint64) error {
	return c.do(ctx, http.MethodDelete, "/memories/"+strconv.FormatUint(id, 10), nil, nil)
}

// DebugPrompt 获取本轮会发送给模型的提示词（文本格式）。
func (c *Client) DebugPrompt(ctx context.Context, q string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/debug/prompt?format=text&q="+url.QueryEscape(q), nil)
	if err != nil {
		return "", err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", readError(resp)
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

// StreamEvent 是聊天流中的一个事件。
type StreamEvent struct {
	Type     string // start | memories | reasoning | delta | done
	Text     string
	Memories []UsedMemory
	Message  *Message
}

// Chat 发送消息，并通过 onEvent 回调推送流式事件。正常结束时返回最终的助手消息。
func (c *Client) Chat(ctx context.Context, content string, onEvent func(StreamEvent)) (*Message, error) {
	b, _ := json.Marshal(map[string]any{"content": content, "stream": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/chat", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 %s 失败: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, readError(resp)
	}
	return parseStream(resp.Body, onEvent)
}

func parseStream(r io.Reader, onEvent func(StreamEvent)) (*Message, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	event := ""
	var data strings.Builder
	var final *Message
	flush := func() error {
		defer func() { event = ""; data.Reset() }()
		if data.Len() == 0 {
			return nil
		}
		raw := []byte(data.String())
		switch event {
		case "start":
			onEvent(StreamEvent{Type: "start"})
		case "memories":
			var p struct {
				Memories []UsedMemory `json:"memories"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return fmt.Errorf("解析 memories 事件: %w", err)
			}
			onEvent(StreamEvent{Type: "memories", Memories: p.Memories})
		case "delta", "reasoning":
			var p struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return fmt.Errorf("解析 %s 事件: %w", event, err)
			}
			onEvent(StreamEvent{Type: event, Text: p.Text})
		case "done":
			var p struct {
				Message Message `json:"message"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return fmt.Errorf("解析 done 事件: %w", err)
			}
			final = &p.Message
			onEvent(StreamEvent{Type: "done", Message: final})
		case "error":
			var p struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return fmt.Errorf("解析 error 事件: %w", err)
			}
			return &Error{Status: http.StatusBadGateway, Code: p.Error.Code, Message: p.Error.Message}
		}
		return nil
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if err := flush(); err != nil {
				return nil, err
			}
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("读取回复: %w", err)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if final == nil {
		return nil, errors.New("连接意外中断，回复可能不完整")
	}
	return final, nil
}
