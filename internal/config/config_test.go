package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadPrecedence(t *testing.T) {
	path := writeFile(t, `
mode: single
data_dir: /tmp/buugoo-test
llm:
  chat:
    model: from-file
    api_key: file-key
memory:
  retrieval:
    alpha: 2.5
`)
	cfg, err := load(path, "", func() []string {
		return []string{
			"BUUGOO_LLM__CHAT__API_KEY=env-key",
			"BUUGOO_SERVER__CORS_ORIGINS=https://a.com, https://b.com",
			"BUUGOO_CONFIG=/ignored",
			"OTHER_VAR=x",
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Chat.Model != "from-file" {
		t.Errorf("model = %q, 期望来自文件", cfg.LLM.Chat.Model)
	}
	if cfg.LLM.Chat.APIKey != "env-key" {
		t.Errorf("api_key = %q, 期望被环境变量覆盖", cfg.LLM.Chat.APIKey)
	}
	if cfg.Memory.Retrieval.Alpha != 2.5 || cfg.Memory.Retrieval.Gamma != 1.5 {
		t.Errorf("retrieval = %+v, 期望文件值与默认值合并", cfg.Memory.Retrieval)
	}
	if got := strings.Join(cfg.Server.CORSOrigins, "|"); got != "https://a.com|https://b.com" {
		t.Errorf("cors_origins = %q", got)
	}
	// utility 未配置时沿用 chat。
	if cfg.LLM.Utility.Model != "from-file" || cfg.LLM.Utility.APIKey != "env-key" {
		t.Errorf("utility 未继承 chat: %+v", cfg.LLM.Utility)
	}
	if cfg.LLM.Utility.Temperature != 0.2 {
		t.Errorf("utility temperature = %v, 期望保留自身默认值", cfg.LLM.Utility.Temperature)
	}
	if cfg.Database.DSN != filepath.Join("/tmp/buugoo-test", "buugoo.db") {
		t.Errorf("sqlite dsn = %q", cfg.Database.DSN)
	}
	if cfg.Path() != path {
		t.Errorf("Path() = %q", cfg.Path())
	}
}

func TestDurationFromEnv(t *testing.T) {
	cfg, err := load("", "", func() []string {
		return []string{"BUUGOO_DATA_DIR=/tmp/x", "BUUGOO_AUTH__TOKEN_TTL=2h"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.TokenTTL != 2*time.Hour {
		t.Errorf("token_ttl = %v", cfg.Auth.TokenTTL)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"bad mode", func(c *Config) { c.Mode = "x" }, "mode"},
		{"multi without secret", func(c *Config) { c.Mode = ModeMulti }, "jwt_secret"},
		{"bad registration", func(c *Config) { c.Auth.Registration = "x" }, "registration"},
		{"mysql without dsn", func(c *Config) { c.Database.Driver = DriverMySQL; c.Database.DSN = "" }, "mysql"},
		{"bad decay", func(c *Config) { c.Memory.Retrieval.Decay = 1.5 }, "decay"},
		{"topk > candidates", func(c *Config) { c.Memory.Retrieval.TopK = 500 }, "top_k"},
		{"bad timezone", func(c *Config) { c.Timezone = "Mars/Base" }, "timezone"},
		{"negative quota", func(c *Config) { c.Quota.DailyTokens = -1 }, "quota"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			c.Database.DSN = "/tmp/x.db"
			c.applyDerived()
			tc.mutate(c)
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, 期望包含 %q", err, tc.want)
			}
		})
	}
	ok := Default()
	ok.Database.DSN = "/tmp/x.db"
	ok.applyDerived()
	if err := ok.Validate(); err != nil {
		t.Fatalf("默认配置应合法: %v", err)
	}
}
