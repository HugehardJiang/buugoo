package dbmove

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	mysqldrv "github.com/go-sql-driver/mysql"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func TestParseTimeAndConvert(t *testing.T) {
	if got, ok := parseTime("2026-10-28 05:11:17.467106271+00:00"); !ok || got.Nanosecond() != 467106271 {
		t.Fatalf("%v %v", got, ok)
	}
	if got, ok := parseTime("2026-10-28 13:11:17+08:00"); !ok || got.Hour() != 5 || got.Location() != time.UTC {
		t.Fatalf("%v %v", got, ok)
	}
	for _, s := range []string{"你好", "2026-10-28", "2026-10-28 abc", "BG-XXXX-XXXX-XXXX-XXXX"} {
		if _, ok := parseTime(s); ok {
			t.Errorf("%q 不是时间", s)
		}
	}
	b := []byte{1, 2}
	c := convert(b).([]byte)
	b[0] = 9
	if c[0] != 1 {
		t.Fatal("[]byte 要复制")
	}
	if err := checkVersion("5.6.51"); err == nil {
		t.Fatal("5.6 太旧")
	}
	if checkVersion("8.4.3") != nil || checkVersion("10.11.6-MariaDB") != nil || checkVersion("5.7.44-log") != nil {
		t.Fatal("这些版本可以")
	}
}

func newSource(t *testing.T) (*config.Config, *gormstore.DB) {
	t.Helper()
	dir := t.TempDir()
	yaml := "data_dir: " + dir + "\ndatabase:\n  driver: sqlite\n  dsn: " + filepath.Join(dir, "buugoo.db") + "\n"
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	db, err := gormstore.Open(cfg.Database, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return cfg, db
}

func TestCheckRefusesMySQLAndPausedWrites(t *testing.T) {
	ctx := context.Background()
	cfg, db := newSource(t)
	m := New(db, cfg, nil)
	if err := m.Check(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Users().EnsureOwner(ctx); err != nil {
		t.Fatal(err)
	}
	m.paused.Store(true)
	if _, err := db.Users().GetByID(ctx, 1); err != nil {
		t.Fatalf("暂停时还能读: %v", err)
	}
	err := db.Conversations().Create(ctx, &store.Conversation{UserID: 1})
	if !errors.Is(err, ErrMaintenance) {
		t.Fatalf("暂停时不能写: %v", err)
	}
	if err := db.Gorm().Exec("UPDATE users SET display_name = 'x'").Error; !errors.Is(err, ErrMaintenance) {
		t.Fatalf("Exec 也要拦: %v", err)
	}
	m.paused.Store(false)
	if err := db.Conversations().Create(ctx, &store.Conversation{UserID: 1}); err != nil {
		t.Fatal(err)
	}
}

// TestMoveToMySQL 需要 MySQL：BUUGOO_TEST_MYSQL_DSN=root:t@tcp(127.0.0.1:13306)/buugoo_test
func TestMoveToMySQL(t *testing.T) {
	dsn := os.Getenv("BUUGOO_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("没有设置 BUUGOO_TEST_MYSQL_DSN")
	}
	ctx := context.Background()
	mc, err := mysqldrv.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	host, portS, _ := net.SplitHostPort(mc.Addr)
	port, _ := strconv.Atoi(portS)
	target := Target{Host: host, Port: port, User: mc.User, Password: mc.Passwd, Database: "buugoo_move_test"}
	// 每次从一个不存在的库开始（顺便测试自动建库）。
	admin := target
	admin.Database = ""
	adb, err := sql.Open("mysql", admin.DSN())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adb.Exec("DROP DATABASE IF EXISTS buugoo_move_test"); err != nil {
		t.Fatal(err)
	}
	_ = adb.Close()

	cfg, db := newSource(t)
	u, err := db.Users().EnsureOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conv := &store.Conversation{UserID: u.ID, Title: "测试"}
	if err := db.Conversations().Create(ctx, conv); err != nil {
		t.Fatal(err)
	}
	var lastMsg *store.Message
	for i := 0; i < 450; i++ { // 超过一批
		msg := &store.Message{ConversationID: conv.ID, UserID: u.ID, Role: store.RoleUser, Content: "第 " + strconv.Itoa(i) + " 条 🐦 " + strings.Repeat("长", i%50), Status: store.MessageComplete}
		if err := db.Messages().Create(ctx, msg); err != nil {
			t.Fatal(err)
		}
		lastMsg = msg
	}
	emb := []byte{0, 1, 2, 3, 255, 254}
	if err := db.Memories().Create(ctx, &store.Memory{UserID: u.ID, Type: "semantic", Content: "喜欢猫", Embedding: emb, EmbeddingDim: 1, Source: "stated"}); err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	if err := db.Gorm().Create(&store.Todo{UserID: u.ID, Title: "写报告", DueAt: &due}).Error; err != nil {
		t.Fatal(err)
	}

	m := New(db, cfg, nil)
	res, err := m.Test(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if res.SourceRows < 452 || res.Existing != 0 {
		t.Fatalf("%+v", res)
	}
	done := make(chan struct{})
	if err := m.Start(target, func() { close(done) }); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(target, nil); err == nil {
		t.Fatal("不能同时迁移两次")
	}
	if err := db.Conversations().Create(ctx, &store.Conversation{UserID: u.ID}); !errors.Is(err, ErrMaintenance) {
		t.Fatalf("迁移中不能写: %v", err)
	}
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatalf("超时: %+v", m.Status())
	}
	st := m.Status()
	if st.Stage != StageDone || st.Copied != st.Rows || !m.Paused() {
		t.Fatalf("%+v", st)
	}

	// 配置已切换。
	saved, _ := os.ReadFile(cfg.SettingsPath())
	if !strings.Contains(string(saved), "mysql") || !strings.Contains(string(saved), "buugoo_move_test") {
		t.Fatalf("settings: %s", saved)
	}
	// 用 MySQL 读回来，内容和时间都对。
	dst, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverMySQL, DSN: target.DSN()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	if err := dst.Migrate(ctx); err != nil {
		t.Fatalf("迁移后再启动要能正常跑迁移: %v", err)
	}
	msgs, err := dst.Messages().Recent(ctx, u.ID, conv.ID, 1)
	if err != nil || len(msgs) != 1 {
		t.Fatal(err, msgs)
	}
	if msgs[0].ID != lastMsg.ID || msgs[0].Content != lastMsg.Content || !msgs[0].CreatedAt.Equal(lastMsg.CreatedAt.Truncate(time.Microsecond)) && msgs[0].CreatedAt.Sub(lastMsg.CreatedAt).Abs() > time.Second {
		t.Fatalf("got %+v want %+v", msgs[0], lastMsg)
	}
	var mem store.Memory
	if err := dst.Gorm().First(&mem).Error; err != nil || !bytes.Equal(mem.Embedding, emb) {
		t.Fatalf("embedding %v %v", mem.Embedding, err)
	}
	var td store.Todo
	if err := dst.Gorm().First(&td).Error; err != nil || td.DueAt == nil || !td.DueAt.Equal(due) {
		t.Fatalf("todo %+v %v", td, err)
	}
	// 自增编号接着往后排。
	nc := &store.Conversation{UserID: u.ID}
	if err := dst.Conversations().Create(ctx, nc); err != nil || nc.ID <= conv.ID {
		t.Fatalf("new id %d err %v", nc.ID, err)
	}
	// 再迁一次到同一个库：已经有数据，拒绝。
	m2 := New(db, cfg, nil)
	m.paused.Store(false)
	if _, err := m2.Test(ctx, target); err == nil || !strings.Contains(err.Error(), "已经有布谷的数据") {
		t.Fatalf("err=%v", err)
	}
}
