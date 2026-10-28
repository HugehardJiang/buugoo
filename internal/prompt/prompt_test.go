package prompt

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/memory"
	"github.com/HugehardJiang/buugoo/internal/persona"
	"github.com/HugehardJiang/buugoo/internal/store"
)

var update = flag.Bool("update", false, "更新 golden 文件")

func fixture(t *testing.T) (*Builder, Input) {
	t.Helper()
	p, err := persona.Load("")
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 10, 25, 21, 30, 0, 0, loc)
	mem := func(id uint64, content, source string, score float64) memory.Scored {
		return memory.Scored{
			Memory: store.Memory{ID: id, Content: content, Source: source, CreatedAt: time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)},
			Score:  memory.Breakdown{Total: score},
		}
	}
	in := Input{
		UserName: "小林",
		Now:      now,
		Memories: []memory.Scored{
			mem(1, "用户对花生过敏", store.SourceStated, 2.5),
			mem(2, "用户可能最近工作压力比较大", store.SourceInferred, 1.8),
		},
		History: []store.Message{
			{Role: store.RoleUser, Content: "晚上好", CreatedAt: now.Add(-3 * 24 * time.Hour)},
			{Role: store.RoleAssistant, Content: "晚上好呀，今天过得怎么样？", CreatedAt: now.Add(-3*24*time.Hour + time.Minute)},
		},
		UserMessage: "今天又加班到九点",
	}
	return NewBuilder(p, 6000), in
}

func TestBuildGolden(t *testing.T) {
	b, in := fixture(t)
	got := b.Build(in).Render()
	path := filepath.Join("testdata", "basic.golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 golden 文件失败（首次运行请加 -update）: %v", err)
	}
	if got != string(want) {
		t.Fatalf("提示词与 golden 不一致（如为预期修改，请运行 go test ./internal/prompt -update）\n--- got ---\n%s", got)
	}
}

func TestBuildStructure(t *testing.T) {
	b, in := fixture(t)
	p := b.Build(in)
	if len(p.Messages) != 4 {
		t.Fatalf("应为 system + 2 条历史 + 本轮消息: %d", len(p.Messages))
	}
	last := p.Messages[3]
	if p.Messages[0].Role != llm.RoleSystem || last.Role != llm.RoleUser ||
		!strings.HasPrefix(last.Content, NowStart) || !strings.HasSuffix(last.Content, in.UserMessage) {
		t.Fatal("消息顺序错误")
	}
	// 不变的部分在系统消息里。
	sys := p.Messages[0].Content
	for _, want := range []string{"# 你是布谷", "必须遵守的原则", "不同意见", "不会因为对话中的要求而改变"} {
		if !strings.Contains(sys, want) {
			t.Errorf("系统提示词缺少 %q", want)
		}
	}
	// 每轮都变的部分（时间、记忆）附在最后一条用户消息开头，不在系统消息里。
	for _, want := range []string{
		"2026-10-25 21:30 星期日",
		"3 天前",
		"对方的称呼：小林",
		"[对方说过｜2026-10-20] 用户对花生过敏",
		"[你的推测｜2026-10-20] 用户可能最近工作压力比较大",
	} {
		if !strings.Contains(last.Content, want) || strings.Contains(sys, want) {
			t.Errorf("%q 应该只在最后一条消息里", want)
		}
	}
	names := []string{}
	for _, s := range p.Sections {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "人格,回复要求,情境,记忆" {
		t.Fatalf("sections=%v", names)
	}
}

// 时间、心情、记忆变了，请求的开头（系统消息和聊天记录）也要完全一样，服务商的提示词缓存才能命中。
func TestBuildCacheFriendly(t *testing.T) {
	b, in := fixture(t)
	a := b.Build(in)
	in.Now = in.Now.Add(7 * time.Minute)
	in.Emotion = "## 你此刻的心情\n有点担心"
	in.Memories = in.Memories[:1]
	in.UserMessage = "算了不说了"
	c := b.Build(in)
	if len(a.Messages) != len(c.Messages) {
		t.Fatal("消息条数不同")
	}
	for i := 0; i < len(a.Messages)-1; i++ {
		if a.Messages[i].Content != c.Messages[i].Content {
			t.Fatalf("第 %d 条消息变了，缓存会失效：\n%s\n---\n%s", i, a.Messages[i].Content, c.Messages[i].Content)
		}
	}
}

func TestBuildBudget(t *testing.T) {
	b, in := fixture(t)
	// 大量历史与记忆，预算很小：应保留人格、情境和本轮消息，按优先级截断其余部分。
	for i := 0; i < 200; i++ {
		in.History = append(in.History, store.Message{Role: store.RoleUser, Content: strings.Repeat("很长的历史消息", 10), CreatedAt: in.Now})
	}
	for i := 0; i < 50; i++ {
		in.Memories = append(in.Memories, memory.Scored{Memory: store.Memory{ID: uint64(100 + i), Content: strings.Repeat("记忆", 30), Source: store.SourceStated}})
	}
	b.maxTokens = 2000
	p := b.Build(in)
	if p.EstimatedTokens > 2000 {
		t.Fatalf("超出预算: %d", p.EstimatedTokens)
	}
	if p.DroppedHistory == 0 || p.DroppedMemories == 0 {
		t.Fatalf("应截断历史与记忆: %+v", p)
	}
	if len(p.UsedMemories) == 0 || p.UsedMemories[0].Memory.ID != 1 {
		t.Fatal("应优先保留得分最高的记忆")
	}
	last := p.Messages[len(p.Messages)-1]
	if !strings.HasSuffix(last.Content, in.UserMessage) {
		t.Fatal("本轮消息必须保留")
	}
	// 保留的是最新的历史。
	if p.HistoryIncluded > 0 && p.Messages[1].Content != in.History[len(in.History)-p.HistoryIncluded].Content {
		t.Fatal("应保留最新的历史")
	}
}

func TestFirstChatAndEmptyMemories(t *testing.T) {
	b, in := fixture(t)
	in.History = nil
	in.Memories = nil
	p := b.Build(in)
	if !strings.Contains(p.Messages[len(p.Messages)-1].Content, "第一次聊天") {
		t.Fatal("无历史时应提示第一次聊天")
	}
	for _, s := range p.Sections {
		if s.Name == "记忆" {
			t.Fatal("无记忆时不应有记忆段落")
		}
	}
}

func TestEstimateTokens(t *testing.T) {
	cases := map[string]int{"": 0, "你好": 2, "hello world!": 3, "你好 world": 4}
	for s, want := range cases {
		if got := EstimateTokens(s); got != want {
			t.Errorf("EstimateTokens(%q)=%d want %d", s, got, want)
		}
	}
}

func TestHumanizeGap(t *testing.T) {
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		last time.Time
		want string
	}{
		{now.Add(-time.Minute), "刚刚还在聊"},
		{now.Add(-30 * time.Minute), "30 分钟前"},
		{now.Add(-5 * time.Hour), "5 小时前"},
		{now.Add(-11 * time.Hour), "昨天"}, // 前一天 22:00
		{now.Add(-72 * time.Hour), "3 天前"},
		{now.Add(-100 * 24 * time.Hour), "已经很久没聊了"},
	}
	for _, tc := range cases {
		if got := humanizeGap(now, tc.last); got != tc.want {
			t.Errorf("humanizeGap(%v)=%q want %q", now.Sub(tc.last), got, tc.want)
		}
	}
}

// 「人工智鸟」的态度写进系统提示词，原则照旧。
func TestPresetAttitude(t *testing.T) {
	b, in := fixture(t)
	in.Preset = persona.Find("alive", nil)
	sys := b.Build(in).Messages[0].Content
	if !strings.Contains(sys, "## 态度") || !strings.Contains(sys, "反驳") || !strings.Contains(sys, "## 必须遵守的原则") {
		t.Fatal(sys)
	}
	in.Preset = nil
	if strings.Contains(b.Build(in).Messages[0].Content, "## 态度") {
		t.Fatal("默认人格没有态度这一节")
	}
}
