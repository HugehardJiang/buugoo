package opencode

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestFactorAndCNY(t *testing.T) {
	glm, _ := Builtin.Find("glm-5.2")
	if f := Builtin.Factor(glm, TierGo); f != 10.0/60 {
		t.Fatalf("go factor %v", f)
	}
	if f := Builtin.Factor(glm, TierPlus); f != 40.0/180 {
		t.Fatalf("plus factor %v", f)
	}
	// $1.40 × (10/60) × 7.2 = 1.68 元 / 百万 token
	if r := glm.Rate.CNY(Builtin.Factor(glm, TierGo), 7.2, 1); r.Input != 1.68 || r.Output != 5.28 {
		t.Fatalf("%+v", r)
	}
	free, _ := Builtin.Find("beam-free")
	if Builtin.Factor(free, TierGo) != 0 {
		t.Fatal("免费模型系数为 0")
	}
	if err := Builtin.validate(); err != nil {
		t.Fatal(err)
	}
	if NormalizeTier("plus") != TierPlus {
		t.Fatal("旧档位名")
	}
	if !IsOpenCode("https://opencode.ai/inference/go/openai/v1") || IsOpenCode("https://api.deepseek.com/v1") || IsOpenCode("https://opencode.ai.evil.com/v1") {
		t.Fatal("IsOpenCode")
	}
}

// 用官网文档 2026-10-29 的页面测试解析（这一天多了 Go Teams 一档）。
func TestParseDocs(t *testing.T) {
	page, err := os.ReadFile("testdata/go-2026-10-29.html")
	if err != nil {
		t.Fatal(err)
	}
	snap, err := Parse(string(page), time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(snap.Tiers, ",") != "go,go-plus,go-teams" || snap.Plans[TierTeams].Price != 20 || !snap.Plans[TierTeams].PerMember || snap.Plans[TierPlus].Price != 40 {
		t.Fatalf("plans %v %+v", snap.Tiers, snap.Plans)
	}
	if len(snap.Models) < 25 {
		t.Fatalf("只有 %d 个模型", len(snap.Models))
	}
	check := func(id string, f func(m Model) bool) {
		t.Helper()
		m, ok := snap.Find(id)
		if !ok || !f(m) {
			t.Errorf("%s: %+v", id, m)
		}
	}
	check("glm-5.2", func(m Model) bool {
		return m.Rate == Rate{1.40, 4.40, 0.26} && m.Limits[TierGo] == 60 && m.Limits[TierPlus] == 180 && m.Limits[TierTeams] == 65
	})
	check("deepseek-v4-pro", func(m Model) bool {
		return m.Rate.Input == 1.32 && m.OffPeak != nil && m.OffPeak.Input == 0.66 && m.Limits[TierGo] == 15
	})
	check("qwen3.7-plus", func(m Model) bool { return m.Rate.Input == 0.40 && m.Note == tieredNote })     // 取 ≤ 256K 那一档
	check("gemini-4-argon", func(m Model) bool { return m.Rate.Input == 2.00 && m.Rate.Output == 10.00 }) // 优惠价
	check("beam-free", func(m Model) bool { return m.Free })
	check("kimi-k3", func(m Model) bool { return m.Vision }) // 从内置目录带上
	if _, err := Parse("<html>改版了</html>", time.Now()); err == nil {
		t.Fatal("解析不出来时要报错")
	}
}
