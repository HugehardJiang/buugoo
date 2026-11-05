package notice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

// TestNotices 在 SQLite 上运行；设置了 BUUGOO_TEST_MYSQL_DSN 时也在 MySQL 上运行（会清空通知表）。
func TestNotices(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		testNotices(t, config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "t.db")})
	})
	if dsn := os.Getenv("BUUGOO_TEST_MYSQL_DSN"); dsn != "" {
		t.Run("mysql", func(t *testing.T) {
			testNotices(t, config.DatabaseConfig{Driver: config.DriverMySQL, DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 2})
		})
	}
}

func testNotices(t *testing.T, cfg config.DatabaseConfig) {
	ctx := context.Background()
	db, err := gormstore.Open(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	db.Gorm().Exec("DELETE FROM notice_reads")
	db.Gorm().Exec("DELETE FROM notices")
	s := New(db.Gorm())
	now := time.Now()
	s.now = func() time.Time { return now }

	if _, err := s.Create(ctx, Input{Title: " ", Seconds: 10}); err == nil {
		t.Fatal("标题不能为空")
	}
	if _, err := s.Create(ctx, Input{Title: "x", Seconds: 61}); err == nil {
		t.Fatal("秒数超范围")
	}
	a, err := s.Create(ctx, Input{Title: "系统维护", Content: "今晚 23:00 维护", Seconds: 10})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.Create(ctx, Input{Title: "新功能", Content: "Cosplay", Seconds: 0})

	p, err := s.Pending(ctx, 7)
	if err != nil || len(p) != 2 || p[0].ID != a.ID {
		t.Fatalf("待读：%+v %v", p, err)
	}
	// 没看够 10 秒不能关；刷新页面（再取一次）也不会重新计时。
	now = now.Add(5 * time.Second)
	if err := s.MarkRead(ctx, 7, a.ID); err == nil || !strings.Contains(err.Error(), "再看") {
		t.Fatalf("没看够应该拒绝：%v", err)
	}
	if _, err := s.Pending(ctx, 7); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Second)
	if err := s.MarkRead(ctx, 7, a.ID); err != nil {
		t.Fatalf("看够了应该能关：%v", err)
	}
	// 0 秒的马上能关。
	if err := s.MarkRead(ctx, 7, b.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Pending(ctx, 7); len(p) != 0 {
		t.Fatalf("关掉后不再弹：%+v", p)
	}
	// 没弹出过的不能直接标已读。
	if err := s.MarkRead(ctx, 8, a.ID); err == nil {
		t.Fatal("没弹出过不能关")
	}
	// 之后的用户（例如新注册的）照样会弹。
	if p, _ := s.Pending(ctx, 9); len(p) != 2 {
		t.Fatalf("新用户也要弹：%+v", p)
	}
	h, _ := s.History(ctx, 7)
	if len(h) != 2 || h[0].ID != b.ID || h[0].ReadAt == nil || h[1].ReadAt == nil {
		t.Fatalf("历史：%+v", h)
	}
	list, _ := s.AdminList(ctx)
	if len(list) != 2 || list[1].Reads != 1 {
		t.Fatalf("后台列表：%+v", list)
	}
	// 修改并「重新弹一次」：所有人重新弹。
	if _, err := s.Update(ctx, a.ID, Input{Title: "系统维护（改期）", Seconds: 3}, true); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Pending(ctx, 7); len(p) != 1 || p[0].Title != "系统维护（改期）" {
		t.Fatalf("重新弹：%+v", p)
	}
	if err := s.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Pending(ctx, 7); len(p) != 0 {
		t.Fatalf("撤回后不再弹：%+v", p)
	}
	if err := s.Delete(ctx, a.ID); err != ErrNotFound {
		t.Fatal(err)
	}
}
