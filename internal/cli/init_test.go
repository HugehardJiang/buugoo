package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HugehardJiang/buugoo/internal/config"
)

// 生成的配置文件必须能被正常加载并通过校验。
func TestRenderedConfigLoads(t *testing.T) {
	dir := t.TempDir()
	for _, mode := range []string{config.ModeSingle, config.ModeMulti} {
		p := filepath.Join(dir, mode+".yaml")
		if err := os.WriteFile(p, []byte(renderConfig(mode, dir, "127.0.0.1:8080", "0123456789abcdef0123456789abcdef", strings.Repeat("ab", 32))), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(p, "")
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if cfg.Mode != mode || cfg.DataDir != dir || cfg.Database.DSN != filepath.Join(dir, "buugoo.db") {
			t.Errorf("%s: 意外的配置 %+v", mode, cfg)
		}
	}
}
