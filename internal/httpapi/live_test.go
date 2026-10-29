package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// parseSSE 把 SSE 输出拆成（事件名, 数据）。
func parseSSE(t *testing.T, body string) (names []string, text string) {
	t.Helper()
	for _, block := range strings.Split(body, "\n\n") {
		var name, data string
		for _, line := range strings.Split(block, "\n") {
			if v, ok := strings.CutPrefix(line, "event: "); ok {
				name = v
			} else if v, ok := strings.CutPrefix(line, "data: "); ok {
				data = v
			}
		}
		if name == "" {
			continue
		}
		names = append(names, name)
		if name == "delta" {
			var d struct{ Text string }
			if err := json.Unmarshal([]byte(data), &d); err != nil {
				t.Fatal(err)
			}
			text += d.Text
		}
	}
	return names, text
}

func follow(t *testing.T, run *liveRun) string {
	t.Helper()
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)
	if err := (&server{}).followLive(c, run); err != nil {
		t.Fatal(err)
	}
	return rec.Body.String()
}

func TestLiveReplayAndFollow(t *testing.T) {
	run := &liveRun{userID: 1, convID: 2, cancel: func() {}}
	run.add("start", map[string]any{"assistant_message_id": 9})
	run.addText("delta", "你")
	run.addText("delta", "好")
	run.add("tool", map[string]any{"id": "t1"})
	run.addText("delta", "呀")

	// 一边生成一边看：中途接上的网页也能拿到完整的文字。
	done := make(chan string)
	go func() { done <- follow(t, run) }()
	for _, s := range []string{"，", "今天", "怎么样"} {
		run.addText("delta", s)
	}
	run.add("done", map[string]any{"message": map[string]any{"id": 9}})
	run.finish()
	names, text := parseSSE(t, <-done)
	if text != "你好呀，今天怎么样" || names[0] != "start" || names[len(names)-1] != "done" {
		t.Fatalf("%v %q", names, text)
	}

	// 生成完以后再接上：补发合并过的事件。
	names, text = parseSSE(t, follow(t, run))
	if strings.Join(names, ",") != "start,delta,tool,delta,done" || text != "你好呀，今天怎么样" {
		t.Fatalf("%v %q", names, text)
	}
}

func TestLiveHubClaim(t *testing.T) {
	h := newLiveHub()
	a := &liveRun{userID: 1, convID: 5}
	if !h.claim(a) || !h.busy(1, 5) || h.busy(2, 5) || h.get(2, 5) != nil {
		t.Fatal("登记后只有本人能看到")
	}
	if h.claim(&liveRun{userID: 1, convID: 5}) {
		t.Fatal("同一段对话同时只能有一个回复在生成")
	}
	a.finish()
	if h.busy(1, 5) || !h.claim(&liveRun{userID: 1, convID: 5}) {
		t.Fatal("生成完以后可以开始新的")
	}
}
