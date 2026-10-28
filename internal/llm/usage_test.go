package llm

import (
	"encoding/json"
	"testing"
)

func TestUsageCachedTokens(t *testing.T) {
	cases := map[string]int{
		// DeepSeek
		`{"prompt_tokens":1000,"completion_tokens":20,"prompt_cache_hit_tokens":800,"prompt_cache_miss_tokens":200}`: 800,
		// OpenAI 格式
		`{"prompt_tokens":1000,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":640}}`: 640,
		// 没有缓存信息
		`{"prompt_tokens":1000,"completion_tokens":20}`: 0,
		// 自己保存的格式
		`{"prompt_tokens":1000,"completion_tokens":20,"cached_tokens":300}`: 300,
		// 不合理的值截到 PromptTokens 以内
		`{"prompt_tokens":10,"completion_tokens":20,"prompt_cache_hit_tokens":99}`: 10,
	}
	for in, want := range cases {
		var u Usage
		if err := json.Unmarshal([]byte(in), &u); err != nil || u.CachedTokens != want || u.CompletionTokens != 20 {
			t.Errorf("%s → %+v %v", in, u, err)
		}
	}
	var sum Usage
	sum.Add(Usage{PromptTokens: 10, CompletionTokens: 2, CachedTokens: 5})
	sum.Add(Usage{PromptTokens: 10, CompletionTokens: 2, CachedTokens: 3})
	if sum != (Usage{PromptTokens: 20, CompletionTokens: 4, CachedTokens: 8}) {
		t.Errorf("sum = %+v", sum)
	}
}
