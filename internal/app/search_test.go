package app

import (
	"testing"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/tools"
)

// 填了专用的 DeepSeek 搜索 Key 时，用其他模型聊天也走 DeepSeek 搜索；没填时只有 DeepSeek 模型才用。
func TestSearchProviderDeepSeekKey(t *testing.T) {
	other := &models.Resolved{}
	other.BaseURL, other.APIKey = "https://api.siliconflow.cn/v1", "sf-key"
	ds := &models.Resolved{}
	ds.BaseURL, ds.APIKey = "https://api.deepseek.com/v1", "ds-key"

	cfg := config.Default()
	cfg.Tools.Search.TavilyAPIKey = "tv-key"
	p := newToolProvider(cfg, testLogger())
	if s, _ := p.search(other); s == nil {
		t.Fatal("应退回 Tavily")
	} else if _, isDS := s.(*tools.DeepSeek); isDS {
		t.Fatal("没填专用 Key 时其他模型不应走 DeepSeek")
	}
	if s, _ := p.search(ds); s == nil {
		t.Fatal("DeepSeek 模型应能搜索")
	} else if _, isDS := s.(*tools.DeepSeek); !isDS {
		t.Fatalf("DeepSeek 模型应走 DeepSeek 搜索，得到 %T", s)
	}

	cfg.Tools.Search.DeepSeekAPIKey = "search-key"
	s, f := p.search(other)
	if _, isDS := s.(*tools.DeepSeek); !isDS {
		t.Fatalf("填了专用 Key 后应走 DeepSeek 搜索，得到 %T", s)
	}
	if _, isDS := f.(*tools.DeepSeek); !isDS {
		t.Fatalf("读网页也应走 DeepSeek，得到 %T", f)
	}
}
