package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/HugehardJiang/buugoo/internal/client"
)

func TestModelRendersHistoryAndStream(t *testing.T) {
	m := New(Options{Client: client.New("http://x", ""), Name: "布谷", Server: "http://x", GlamourStyle: "notty"})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	now := time.Now()
	m.Update(historyMsg{msgs: []client.Message{
		{ID: 1, Role: "user", Content: "你好呀", CreatedAt: now, Status: "complete"},
		{ID: 2, Role: "assistant", Content: "嗨！", CreatedAt: now, Status: "complete"},
	}})
	v := m.View()
	for _, want := range []string{"布谷", "今天", "你好呀", "嗨！"} {
		if !strings.Contains(v, want) {
			t.Fatalf("界面缺少 %q:\n%s", want, v)
		}
	}

	// 模拟一轮流式回复。
	m.entries = append(m.entries, entry{role: "user", content: "猫", at: now, status: "complete"}, entry{role: "assistant", at: now})
	m.Update(streamMsg{client.StreamEvent{Type: "memories", Memories: []client.UsedMemory{{ID: 1, Content: "用户养猫", Source: "stated"}}}})
	m.Update(streamMsg{client.StreamEvent{Type: "delta", Text: "团子还好吗"}})
	m.Update(streamEnd{})
	v = m.View()
	if !strings.Contains(v, "想起了 1 件事") || !strings.Contains(v, "团子还好吗") {
		t.Fatalf("流式内容未渲染:\n%s", v)
	}
	if m.entries[len(m.entries)-1].status != "complete" {
		t.Fatal("流结束后状态应为 complete")
	}
}

func TestDayLabel(t *testing.T) {
	now := time.Now()
	if dayLabel(now) != "今天" || dayLabel(now.AddDate(0, 0, -1)) != "昨天" {
		t.Fatal("今天/昨天")
	}
	if got := dayLabel(time.Date(2001, 2, 3, 0, 0, 0, 0, time.Local)); got != "2001年2月3日" {
		t.Fatal(got)
	}
}
