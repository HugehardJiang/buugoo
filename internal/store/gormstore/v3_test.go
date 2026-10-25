package gormstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-gormigrate/gormigrate/v2"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// 从 v2 升级到 v3：已停用的 DeepSeek 旧模型名换成 deepseek-flash，其他模型不变；新列取默认值。
func TestUpgradeFromV2(t *testing.T) {
	ctx := context.Background()
	s, err := Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "up.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	opts := *gormigrate.DefaultOptions
	opts.TableName = migrationsTable
	opts.UseTransaction = false
	if err := gormigrate.New(s.db, &opts, migrations()[:2]).Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, m := range []struct{ base, model string }{
		{"https://api.deepseek.com/v1", "deepseek-chat"},
		{"https://api.openai.com/v1", "deepseek-chat"},
		{"https://api.deepseek.com/v1", "deepseek-v4-pro"},
	} {
		if err := s.db.Exec("INSERT INTO model_configs (owner_id, name, base_url, model, thinking_style, thinking_model, created_at, updated_at) VALUES (0, 'm', ?, ?, 'model_switch', 'deepseek-reasoner', ?, ?)",
			m.base, m.model, now, now).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	m1, _ := s.Models().Get(ctx, 1)
	m2, _ := s.Models().Get(ctx, 2)
	m3, _ := s.Models().Get(ctx, 3)
	if m1.Model != "deepseek-flash" || m1.ThinkingStyle != store.ThinkingStyleDeepSeek || !m1.Vision || !m1.Tools {
		t.Fatalf("m1=%+v", m1)
	}
	if m2.Model != "deepseek-chat" || m3.Model != "deepseek-v4-pro" || m3.Vision || !m3.Tools {
		t.Fatalf("其他模型不应改动: %+v %+v", m2, m3)
	}
}

func TestAttachmentRepo(t *testing.T) {
	forEachDriver(t, func(t *testing.T, s *DB) {
		ctx := context.Background()
		r := s.Attachments()
		mk := func(user uint64, size int64) *store.Attachment {
			a := &store.Attachment{UserID: user, Kind: store.AttachmentDocument, Filename: "a.txt", MIME: "text/plain",
				Size: size, Path: "p", SHA256: "x", Text: "内容"}
			if err := r.Create(ctx, a); err != nil {
				t.Fatal(err)
			}
			return a
		}
		a1, a2, other := mk(1, 10), mk(1, 20), mk(2, 5)
		if n, _ := r.TotalSize(ctx, 1); n != 30 {
			t.Fatalf("total=%d", n)
		}
		list, err := r.ListPending(ctx, 1, []uint64{a2.ID, a1.ID, other.ID})
		if err != nil || len(list) != 2 || list[0].ID != a2.ID {
			t.Fatalf("应按请求顺序返回自己的附件: %+v %v", list, err)
		}
		if err := r.Attach(ctx, 1, []uint64{a1.ID, other.ID}, 5, 6); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("不能关联他人的附件: %v", err)
		}
		if err := r.Attach(ctx, 1, []uint64{a1.ID}, 5, 6); err != nil {
			t.Fatal(err)
		}
		byMsg, _ := r.ListByMessages(ctx, 1, []uint64{6})
		byConv, _ := r.ListByConversation(ctx, 1, 5)
		if len(byMsg) != 1 || len(byConv) != 1 || *byMsg[0].MessageID != 6 {
			t.Fatalf("byMsg=%+v byConv=%+v", byMsg, byConv)
		}
		if _, err := r.Get(ctx, 2, a1.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("不能读取他人的附件")
		}
		if err := r.SetDescription(ctx, a1.ID, "描述"); err != nil {
			t.Fatal(err)
		}
		stale, _ := r.ListStale(ctx, time.Now().Add(time.Hour), 10)
		if len(stale) != 2 {
			t.Fatalf("未发送的附件应为 2 个: %+v", stale)
		}
		if err := r.Delete(ctx, []uint64{a1.ID, a2.ID, other.ID}); err != nil {
			t.Fatal(err)
		}
		if n, _ := r.TotalSize(ctx, 1); n != 0 {
			t.Fatalf("total=%d", n)
		}
	})
}
