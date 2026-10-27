package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// 回复失败时，原因（打过码）存进消息里，诊断接口能看到服务器信息。
func TestDiagnostics(t *testing.T) {
	ta := newTestApp(t, nil)

	_, b := ta.do(t, "POST", "/api/v1/chat", "", map[string]any{"content": "触发上游错误", "stream": false})
	if strings.Contains(string(b), "sk-abcdef1234567890") {
		t.Fatalf("响应里漏了密钥: %s", b)
	}

	_, b = ta.do(t, "GET", "/api/v1/messages?limit=10", "", nil)
	var hist struct {
		Messages []struct{ Role, Status, Error string }
	}
	_ = json.Unmarshal(b, &hist)
	var failed string
	for _, m := range hist.Messages {
		if m.Role == "assistant" && m.Error != "" {
			failed = m.Error
		}
	}
	if failed == "" || strings.Contains(failed, "sk-abcdef1234567890") || !strings.Contains(failed, "****") {
		t.Fatalf("消息里的失败原因: %q (%s)", failed, b)
	}

	resp, b := ta.do(t, "GET", "/api/v1/diagnostics", "", nil)
	var d struct {
		Server map[string]any
		Recent []any
	}
	if resp.StatusCode != 200 || json.Unmarshal(b, &d) != nil || d.Server["version"] == nil || d.Recent == nil {
		t.Fatalf("diagnostics: %d %s", resp.StatusCode, b)
	}
	if resp, _ := ta.do(t, "GET", "/api/v1/console/diagnostics", "", nil); resp.StatusCode != 401 {
		t.Fatalf("没登录后台看服务器诊断: %d", resp.StatusCode)
	}
}

// 头像每年只能重新生成 5 次；昵称可以改。
func TestProfileAvatar(t *testing.T) {
	ta := newTestApp(t, nil)
	_, b := ta.do(t, "GET", "/api/v1/me/profile", "", nil)
	var p struct {
		User struct {
			AvatarSeed string `json:"avatar_seed"`
		}
		AvatarLeft int `json:"avatar_left"`
	}
	if json.Unmarshal(b, &p) != nil || p.User.AvatarSeed == "" || p.AvatarLeft != 5 {
		t.Fatalf("profile: %s", b)
	}
	seeds := map[string]bool{p.User.AvatarSeed: true}
	for i := 4; i >= 0; i-- {
		resp, b := ta.do(t, "POST", "/api/v1/me/avatar", "", nil)
		if resp.StatusCode != 200 || json.Unmarshal(b, &p) != nil || p.AvatarLeft != i || seeds[p.User.AvatarSeed] {
			t.Fatalf("第 %d 次换头像: %d %s", 5-i, resp.StatusCode, b)
		}
		seeds[p.User.AvatarSeed] = true
	}
	if resp, b := ta.do(t, "POST", "/api/v1/me/avatar", "", nil); resp.StatusCode != 429 || !strings.Contains(string(b), "avatar_limit") {
		t.Fatalf("超过 5 次: %d %s", resp.StatusCode, b)
	}
	_, b = ta.do(t, "GET", "/api/v1/me", "", nil)
	if !strings.Contains(string(b), p.User.AvatarSeed) {
		t.Fatalf("/me 应返回新头像: %s", b)
	}

	if resp, _ := ta.do(t, "PUT", "/api/v1/me/profile", "", map[string]string{"display_name": "小明"}); resp.StatusCode != 200 {
		t.Fatalf("改昵称: %d", resp.StatusCode)
	}
	if resp, _ := ta.do(t, "PUT", "/api/v1/me/profile", "", map[string]string{"display_name": strings.Repeat("长", 31)}); resp.StatusCode != 400 {
		t.Fatalf("昵称太长应拒绝: %d", resp.StatusCode)
	}
	_, b = ta.do(t, "GET", "/api/v1/me", "", nil)
	if !strings.Contains(string(b), `"display_name":"小明"`) {
		t.Fatalf("昵称: %s", b)
	}
}
