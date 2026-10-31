package selfupdate

import (
	"os"
	"path/filepath"
	"testing"
)

// 同一秒里备份两次不能撞名（回滚后马上再更新时会这样）。
func TestFreePath(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "buugoo-20261031-022151-0.0.1")
	first := freePath(base, ".db")
	if first != base+".db" {
		t.Fatal(first)
	}
	_ = os.WriteFile(first, nil, 0o600)
	second := freePath(base, ".db")
	if second != base+"-2.db" {
		t.Fatal(second)
	}
	_ = os.WriteFile(second, nil, 0o600)
	if third := freePath(base, ".db"); third != base+"-3.db" {
		t.Fatal(third)
	}
}
