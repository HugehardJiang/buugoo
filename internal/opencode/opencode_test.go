package opencode

import "testing"

func TestFactorAndCNY(t *testing.T) {
	glm, _ := Find("glm-5.2")
	if f := glm.Factor(TierGo); f != 10.0/60 {
		t.Fatalf("go factor %v", f)
	}
	if f := glm.Factor(TierPlus); f != 40.0/180 {
		t.Fatalf("plus factor %v", f)
	}
	// $1.40 × (10/60) × 7.2 = 1.68 元 / 百万 token
	if r := glm.Rate.CNY(glm.Factor(TierGo), 7.2, 1); r.Input != 1.68 || r.Output != 5.28 {
		t.Fatalf("%+v", r)
	}
	free, _ := Find("beam-free")
	if free.Factor(TierGo) != 0 {
		t.Fatal("免费模型系数为 0")
	}
	seen := map[string]bool{}
	for _, m := range Catalog {
		if seen[m.ID] {
			t.Fatalf("重复的模型 %s", m.ID)
		}
		seen[m.ID] = true
		if !m.Free && (m.Limits[TierGo] <= 0 || m.Limits[TierPlus] < m.Limits[TierGo] || m.Rate.Input <= 0) {
			t.Errorf("%s 的数据不对: %+v", m.ID, m)
		}
	}
	if !IsOpenCode("https://opencode.ai/inference/go/openai/v1") || IsOpenCode("https://api.deepseek.com/v1") || IsOpenCode("https://opencode.ai.evil.com/v1") {
		t.Fatal("IsOpenCode")
	}
}
