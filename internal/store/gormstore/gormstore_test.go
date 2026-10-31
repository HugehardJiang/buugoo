package gormstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// forEachDriver 在 SQLite 上运行测试；设置了 BUUGOO_TEST_MYSQL_DSN 时同时在 MySQL 上运行。
// MySQL 测试会清空目标库中的所有 Buugoo 表，请使用专用的测试库。
func forEachDriver(t *testing.T, fn func(t *testing.T, s *DB)) {
	t.Run("sqlite", func(t *testing.T) {
		s, err := Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "t.db")}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		if err := s.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		fn(t, s)
	})
	dsn := os.Getenv("BUUGOO_TEST_MYSQL_DSN")
	if dsn == "" {
		return
	}
	t.Run("mysql", func(t *testing.T) {
		s, err := Open(config.DatabaseConfig{Driver: config.DriverMySQL, DSN: dsn, MaxOpenConns: 10, MaxIdleConns: 5}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		for _, m := range migrations() {
			if err := m.Rollback(s.db); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.db.Migrator().DropTable(MigrationsTable); err != nil {
			t.Fatal(err)
		}
		if err := s.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		fn(t, s)
	})
}

func TestMigrateIdempotent(t *testing.T) {
	forEachDriver(t, func(t *testing.T, s *DB) {
		ctx := context.Background()
		if err := s.Migrate(ctx); err != nil {
			t.Fatalf("重复迁移失败: %v", err)
		}
		pending, err := s.Pending(ctx)
		if err != nil || pending {
			t.Fatalf("pending=%v err=%v", pending, err)
		}
	})
}

func TestUsersAndInvites(t *testing.T) {
	forEachDriver(t, func(t *testing.T, s *DB) {
		ctx := context.Background()
		owner, err := s.Users().EnsureOwner(ctx)
		if err != nil || owner.ID != store.OwnerID || !owner.IsAdmin {
			t.Fatalf("owner=%+v err=%v", owner, err)
		}
		if _, err := s.Users().EnsureOwner(ctx); err != nil {
			t.Fatal(err)
		}
		u := &store.User{Username: "alice", PasswordHash: "h"}
		if err := s.Users().Create(ctx, u); err != nil {
			t.Fatal(err)
		}
		if err := s.Users().Create(ctx, &store.User{Username: "alice"}); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("重复用户名应返回 ErrConflict，得到 %v", err)
		}
		if err := s.Users().UpdatePassword(ctx, u.ID, "h2"); err != nil {
			t.Fatal(err)
		}
		got, err := s.Users().GetByUsername(ctx, "alice")
		if err != nil || got.TokenVersion != 1 || got.PasswordHash != "h2" {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		if _, err := s.Users().GetByID(ctx, 9999); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("应返回 ErrNotFound，得到 %v", err)
		}

		now := time.Now().UTC()
		inv := &store.Invite{Code: "CODE1", CreatedBy: owner.ID, MaxUses: 2}
		if err := s.Invites().Create(ctx, inv); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if _, err := s.Invites().Redeem(ctx, "CODE1", now); err != nil {
				t.Fatalf("第 %d 次使用: %v", i+1, err)
			}
		}
		if _, err := s.Invites().Redeem(ctx, "CODE1", now); !errors.Is(err, store.ErrInviteInvalid) {
			t.Fatalf("超出次数应失败，得到 %v", err)
		}
		past := now.Add(-time.Hour)
		if err := s.Invites().Create(ctx, &store.Invite{Code: "OLD", CreatedBy: owner.ID, MaxUses: 0, ExpiresAt: &past}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Invites().Redeem(ctx, "OLD", now); !errors.Is(err, store.ErrInviteInvalid) {
			t.Fatalf("过期邀请码应失败，得到 %v", err)
		}

		// 事务回滚：消耗邀请码后失败，次数不应增加。
		if err := s.Invites().Create(ctx, &store.Invite{Code: "TX", CreatedBy: owner.ID, MaxUses: 1}); err != nil {
			t.Fatal(err)
		}
		txErr := s.Tx(ctx, func(tx store.Store) error {
			if _, err := tx.Invites().Redeem(ctx, "TX", now); err != nil {
				return err
			}
			return tx.Users().Create(ctx, &store.User{Username: "alice"}) // 冲突
		})
		if !errors.Is(txErr, store.ErrConflict) {
			t.Fatalf("事务应因冲突失败，得到 %v", txErr)
		}
		if _, err := s.Invites().Redeem(ctx, "TX", now); err != nil {
			t.Fatalf("回滚后邀请码应仍可用: %v", err)
		}
	})
}

func TestConversationsAndMessages(t *testing.T) {
	forEachDriver(t, func(t *testing.T, s *DB) {
		ctx := context.Background()
		c1, err := s.Conversations().Primary(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		c2, err := s.Conversations().Primary(ctx, 1)
		if err != nil || c1.ID != c2.ID {
			t.Fatalf("主对话应唯一: %v %v %v", c1.ID, c2.ID, err)
		}
		other, _ := s.Conversations().Primary(ctx, 2)
		if _, err := s.Conversations().Get(ctx, 1, other.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("不能读取他人对话: %v", err)
		}
		var ids []uint64
		for i := 0; i < 5; i++ {
			m := &store.Message{ConversationID: c1.ID, UserID: 1, Role: store.RoleUser, Content: string(rune('a' + i))}
			if err := s.Messages().Create(ctx, m); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, m.ID)
		}
		recent, err := s.Messages().Recent(ctx, 1, c1.ID, 3)
		if err != nil || len(recent) != 3 || recent[0].Content != "c" || recent[2].Content != "e" {
			t.Fatalf("recent=%v err=%v", recent, err)
		}
		before, err := s.Messages().Before(ctx, 1, c1.ID, ids[2], 10)
		if err != nil || len(before) != 2 || before[1].Content != "b" {
			t.Fatalf("before=%v err=%v", before, err)
		}
		if got, _ := s.Messages().Recent(ctx, 2, c1.ID, 10); len(got) != 0 {
			t.Fatalf("不能读取他人消息")
		}
		m := recent[2]
		m.Content, m.Status, m.CompletionTokens = "done", store.MessageInterrupted, 7
		if err := s.Messages().Finish(ctx, &m); err != nil {
			t.Fatal(err)
		}
		if err := s.Conversations().Touch(ctx, 1, c1.ID, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestMemories(t *testing.T) {
	forEachDriver(t, func(t *testing.T, s *DB) {
		ctx := context.Background()
		mk := func(user uint64, content, model string) *store.Memory {
			m := &store.Memory{UserID: user, Type: store.MemorySemantic, Content: content,
				Embedding: []byte{1, 2, 3, 4}, EmbeddingModel: model, EmbeddingDim: 1,
				Importance: 6, Source: store.SourceStated}
			if err := s.Memories().Create(ctx, m); err != nil {
				t.Fatal(err)
			}
			return m
		}
		a := mk(1, "喜欢猫", "m1")
		b := mk(1, "住在沈阳", "m1")
		mk(1, "旧模型记忆", "m0")
		mk(2, "别人的记忆", "m1")

		rows, err := s.Memories().LoadEmbeddings(ctx, 1, "m1")
		if err != nil || len(rows) != 2 {
			t.Fatalf("应只加载同一模型的 2 条向量: %v %v", rows, err)
		}
		list, total, err := s.Memories().List(ctx, 1, store.MemoryFilter{Query: "沈阳"}, 10, 0)
		if err != nil || total != 1 || list[0].ID != b.ID || list[0].Embedding != nil {
			t.Fatalf("list=%v total=%d err=%v", list, total, err)
		}
		if err := s.Memories().TouchAccessed(ctx, 1, []uint64{a.ID}, 0.5, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		got, err := s.Memories().Get(ctx, 1, a.ID)
		if err != nil || got.AccessCount != 1 || got.Strength != 1.5 || got.LastAccessedAt == nil {
			t.Fatalf("touch 未生效: %+v %v", got, err)
		}
		if err := s.Memories().Delete(ctx, 2, a.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("不能删除他人记忆: %v", err)
		}
		if err := s.Memories().Delete(ctx, 1, a.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.Memories().Delete(ctx, 1, a.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("重复删除应返回 ErrNotFound: %v", err)
		}
		// 删除后内容与向量必须被清空。
		var raw store.Memory
		if err := s.db.Unscoped().First(&raw, a.ID).Error; err != nil {
			t.Fatal(err)
		}
		if raw.Content != "" || raw.Embedding != nil || !raw.DeletedAt.Valid {
			t.Fatalf("删除未清空内容: %+v", raw)
		}
		if rows, _ := s.Memories().LoadEmbeddings(ctx, 1, "m1"); len(rows) != 1 {
			t.Fatalf("已删除记忆不应参与检索: %v", rows)
		}

		stale, err := s.Memories().ListForReembed(ctx, 0, "m1", true, 10)
		if err != nil || len(stale) != 1 || stale[0].Content != "旧模型记忆" {
			t.Fatalf("stale=%v err=%v", stale, err)
		}
		if err := s.Memories().UpdateEmbedding(ctx, stale[0].ID, []byte{9, 9, 9, 9}, "m1", 1); err != nil {
			t.Fatal(err)
		}
		counts, err := s.Memories().CountByModel(ctx)
		if err != nil || counts["m1"] != 3 {
			t.Fatalf("counts=%v err=%v", counts, err)
		}
		if err := s.Memories().RecordUsage(ctx, []store.MemoryUsage{{MessageID: 1, MemoryID: b.ID, UserID: 1, Score: 0.9}}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestUsageConcurrentAdd(t *testing.T) {
	forEachDriver(t, func(t *testing.T, s *DB) {
		ctx := context.Background()
		var wg sync.WaitGroup
		errs := make(chan error, 20)
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- s.Usage().Add(ctx, 1, "2026-10-25", 1, 10, 5)
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		u, err := s.Usage().Get(ctx, 1, "2026-10-25")
		if err != nil || u.Messages != 20 || u.TotalTokens() != 300 {
			t.Fatalf("usage=%+v err=%v", u, err)
		}
		empty, err := s.Usage().Get(ctx, 1, "2000-01-01")
		if err != nil || empty.Messages != 0 {
			t.Fatalf("empty=%+v err=%v", empty, err)
		}
	})
}

func TestJobsAndMeta(t *testing.T) {
	forEachDriver(t, func(t *testing.T, s *DB) {
		ctx := context.Background()
		if _, err := s.Jobs().LastSucceeded(ctx, "x"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
		run, err := s.Jobs().Start(ctx, "x", time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Jobs().Finish(ctx, run.ID, store.JobSucceeded, "", time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		if last, err := s.Jobs().LastSucceeded(ctx, "x"); err != nil || last.ID != run.ID {
			t.Fatalf("last=%v err=%v", last, err)
		}
		if _, ok, err := s.Meta().Get(ctx, "k"); ok || err != nil {
			t.Fatal("不存在的键")
		}
		for _, v := range []string{"v1", "v2"} {
			if err := s.Meta().Set(ctx, "k", v); err != nil {
				t.Fatal(err)
			}
		}
		if v, ok, err := s.Meta().Get(ctx, "k"); !ok || err != nil || v != "v2" {
			t.Fatalf("v=%q ok=%v err=%v", v, ok, err)
		}
	})
}

func TestMessageCacheStats(t *testing.T) {
	forEachDriver(t, func(t *testing.T, s *DB) {
		ctx := context.Background()
		c, err := s.Conversations().Primary(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if last, total, err := s.Messages().CacheStats(ctx, 1, c.ID); err != nil || last != nil || total != nil {
			t.Fatalf("没有数据时应为空: %v %v %v", last, total, err)
		}
		ptr := func(n int) *int { return &n }
		for _, m := range []store.Message{
			{PromptTokens: 1000, CachedTokens: ptr(500)},
			{PromptTokens: 1000, CachedTokens: ptr(900)},
			{PromptTokens: 1000}, // 服务商没返回缓存信息，不算进去
		} {
			m.ConversationID, m.UserID, m.Role, m.Content = c.ID, 1, store.RoleAssistant, "x"
			if err := s.Messages().Create(ctx, &m); err != nil {
				t.Fatal(err)
			}
		}
		last, total, err := s.Messages().CacheStats(ctx, 1, c.ID)
		if err != nil || last == nil || *last != 90 || total == nil || *total != 70 {
			t.Fatalf("last=%v total=%v err=%v", last, total, err)
		}
	})
}

func TestStaleEmptyConversations(t *testing.T) {
	forEachDriver(t, func(t *testing.T, s *DB) {
		ctx := context.Background()
		empty := &store.Conversation{UserID: 1}
		used := &store.Conversation{UserID: 1}
		wx := &store.Conversation{UserID: 1, Channel: "weixin"}
		for _, c := range []*store.Conversation{empty, used, wx} {
			if err := s.Conversations().Create(ctx, c); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Messages().Create(ctx, &store.Message{ConversationID: used.ID, UserID: 1, Role: store.RoleUser, Content: "hi", Status: store.MessageComplete}); err != nil {
			t.Fatal(err)
		}
		list, err := s.Conversations().StaleEmpty(ctx, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].ID != empty.ID {
			t.Fatalf("应只找到空的网页对话: %+v", list)
		}
	})
}
