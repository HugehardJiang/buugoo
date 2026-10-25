package settings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HugehardJiang/buugoo/internal/config"
)

func setup(t *testing.T, extraEnv ...string) (string, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := filepath.Join(dir, "data")
	content := "data_dir: " + data + "\nllm:\n  chat:\n    api_key: sk-file\n    model: deepseek-flash\nquota:\n  daily_messages: 5\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, kv := range extraEnv {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	cfg, err := config.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	return path, cfg
}

func field(t *testing.T, fields []Field, key string) Field {
	t.Helper()
	for _, f := range fields {
		if f.Key == key {
			return f
		}
	}
	t.Fatalf("没有设置项 %s", key)
	return Field{}
}

func TestFieldsAndSave(t *testing.T) {
	path, cfg := setup(t, "BUUGOO_LOG__LEVEL=debug")
	fields, err := Fields(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if f := field(t, fields, "llm.chat.api_key"); f.Type != "secret" || f.Value != true {
		t.Fatalf("密钥只显示是否已设置: %+v", f)
	}
	if f := field(t, fields, "admin.password"); !f.Locked || f.Value != true {
		t.Fatalf("管理员密码应锁定: %+v", f)
	}
	if f := field(t, fields, "mode"); f.Type != "select" || f.Label != "运行模式" {
		t.Fatalf("mode: %+v", f)
	}
	if f := field(t, fields, "tools.python.timeout"); f.Type != "duration" || f.Value != "30s" {
		t.Fatalf("timeout: %+v", f)
	}
	if !field(t, fields, "log.level").Env {
		t.Fatal("应标出被环境变量覆盖的设置")
	}

	// 校验失败不写入。
	for _, bad := range []map[string]any{
		{"mode": "cluster"},
		{"tools.python.timeout": "半分钟"},
		{"quota.daily_messages": "很多"},
		{"admin.password": "123"},
		{"nope.nope": 1},
		{"emotion.temper": 9},
	} {
		var ve *ValidationError
		if _, err := Save(cfg, bad); !errors.As(err, &ve) {
			t.Errorf("%v 应校验失败: %v", bad, err)
		}
	}
	if _, err := os.Stat(cfg.SettingsPath()); !os.IsNotExist(err) {
		t.Fatal("校验失败时不应写入")
	}

	changed, err := Save(cfg, map[string]any{
		"quota.daily_messages": 20.0, "tools.python.timeout": "45s", "llm.chat.api_key": "",
		"emotion.temper": 1.5, "server.cors_origins": "https://a.example, https://b.example", "log.level": "warn",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 5 {
		t.Fatalf("留空的密钥不应算作修改: %v", changed)
	}
	if info, _ := os.Stat(cfg.SettingsPath()); info.Mode().Perm() != 0o600 {
		t.Fatalf("设置文件权限应为 0600: %v", info.Mode())
	}
	fields, _ = Fields(cfg)
	if f := field(t, fields, "quota.daily_messages"); !f.Pending {
		t.Fatalf("保存后、重启前应为待生效: %+v", f)
	}

	// 重启（重新加载）后生效：网页设置覆盖配置文件，环境变量仍然优先，未修改的密钥保持不变。
	cfg2, err := config.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Quota.DailyMessages != 20 || cfg2.Tools.Python.Timeout.String() != "45s" || cfg2.Emotion.Temper != 1.5 ||
		len(cfg2.Server.CORSOrigins) != 2 || cfg2.LLM.Chat.APIKey != "sk-file" || cfg2.Log.Level != "debug" {
		t.Fatalf("cfg2=%+v %+v %+v", cfg2.Quota, cfg2.Tools.Python, cfg2.Server)
	}
	fields, _ = Fields(cfg2)
	if f := field(t, fields, "quota.daily_messages"); f.Pending {
		t.Fatal("重启后不应再是待生效")
	}

	// 修改密钥，然后恢复为配置文件中的设置。
	if _, err := Save(cfg2, map[string]any{"llm.chat.api_key": "sk-web"}); err != nil {
		t.Fatal(err)
	}
	if cfg3, _ := config.Load(path, ""); cfg3.LLM.Chat.APIKey != "sk-web" {
		t.Fatal("网页修改的密钥应生效")
	}
	if err := Discard(cfg2, []string{"llm.chat.api_key"}); err != nil {
		t.Fatal(err)
	}
	if cfg4, _ := config.Load(path, ""); cfg4.LLM.Chat.APIKey != "sk-file" || cfg4.Quota.DailyMessages != 20 {
		t.Fatal("恢复后应回到配置文件中的值，其他网页设置保留")
	}
}

// 每个设置项都要在后台的目录里有位置、有中文名；已经不用的设置不显示。
func TestLayoutComplete(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	fields, err := Fields(cfg)
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]bool{}
	for _, g := range Groups {
		groups[g.Key] = true
	}
	for _, f := range fields {
		if f.Group == "other" {
			t.Errorf("%s 没有放进 layout", f.Key)
		}
		if !groups[f.Group] {
			t.Errorf("%s 的分类 %s 不存在", f.Key, f.Group)
		}
		if f.Label == f.Key {
			t.Errorf("%s 没有中文名", f.Key)
		}
		if hidden[f.Key] {
			t.Errorf("%s 不应显示", f.Key)
		}
	}
	for k := range places {
		found := false
		for _, f := range fields {
			found = found || f.Key == k
		}
		if !found {
			t.Errorf("layout 里的 %s 不是设置项", k)
		}
	}
}
