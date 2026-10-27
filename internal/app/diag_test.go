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
