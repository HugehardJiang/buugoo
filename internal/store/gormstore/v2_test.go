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

// 从 v1 升级到 v2：已有的对话和消息保留，新列取默认值。
func TestUpgradeFromV1(t *testing.T) {
	ctx := context.Background()
	s, err := Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "up.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	opts := *gormigrate.DefaultOptions
	opts.TableName = migrationsTable
	opts.UseTransaction = false
	if err := gormigrate.New(s.db, &opts, migrations()[:1]).Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Exec("INSERT INTO conversations (user_id, title, created_at, updated_at) VALUES (1, '旧对话', ?, ?)", time.Now(), time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Exec("INSERT INTO messages (conversation_id, user_id, role, content, created_at) VALUES (1, 1, 'user', '旧消息', ?)", time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := s.Conversations().Get(ctx, 1, 1)
	if err != nil || c.Title != "旧对话" || c.Thinking != store.ThinkingOff || c.ModelID != nil {
		t.Fatalf("c=%+v err=%v", c, err)
	}
	msgs, err := s.Messages().Recent(ctx, 1, 1, 10)
	if err != nil || len(msgs) != 1 || msgs[0].Content != "旧消息" || msgs[0].Reasoning != "" {
		t.Fatalf("msgs=%+v err=%v", msgs, err)
	}
}

func TestConversationsAndModels(t *testing.T) {
	forEachDriver(t, func(t *testing.T, s *DB) {
		ctx := context.Background()
		c1 := &store.Conversation{UserID: 1, Title: "一"}
		if err := s.Conversations().Create(ctx, c1); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1100 * time.Millisecond) // MySQL datetime 精度为秒
		c2 := &store.Conversation{UserID: 1, Title: "二"}
		_ = s.Conversations().Create(ctx, c2)
		_ = s.Conversations().Create(ctx, &store.Conversation{UserID: 2, Title: "别人的"})
		list, err := s.Conversations().List(ctx, 1)
		if err != nil || len(list) != 2 || list[0].ID != c2.ID {
			t.Fatalf("list=%+v err=%v", list, err)
		}
		if p, _ := s.Conversations().Primary(ctx, 1); p.ID != c2.ID {
			t.Fatal("Primary 应返回最近使用的对话")
		}

		m := &store.ModelConfig{OwnerID: 1, Name: "我的", BaseURL: "https://x/v1", Model: "m", ThinkingStyle: store.ThinkingStyleNone}
		if err := s.Models().Create(ctx, m); err != nil {
			t.Fatal(err)
		}
		pub := &store.ModelConfig{OwnerID: store.PublicOwner, Name: "公共", BaseURL: "https://y/v1", Model: "p", ThinkingStyle: store.ThinkingStyleEffort}
		_ = s.Models().Create(ctx, pub)
		_ = s.Models().Create(ctx, &store.ModelConfig{OwnerID: 2, Name: "别人", BaseURL: "https://z/v1", Model: "o"})
		vis, err := s.Models().ListVisible(ctx, 1)
		if err != nil || len(vis) != 2 {
			t.Fatalf("应只看到公共模型和自己的模型: %+v %v", vis, err)
		}
		mid := m.ID
		if err := s.Conversations().Update(ctx, 1, c1.ID, map[string]any{"model_id": mid, "thinking": store.ThinkingHigh, "title": "改名"}); err != nil {
			t.Fatal(err)
		}
		if err := s.Conversations().Update(ctx, 2, c1.ID, map[string]any{"title": "x"}); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("不能修改别人的对话")
		}
		got, _ := s.Conversations().Get(ctx, 1, c1.ID)
		if got.ModelID == nil || *got.ModelID != mid || got.Thinking != store.ThinkingHigh || got.Title != "改名" {
			t.Fatalf("got=%+v", got)
		}
		if err := s.Conversations().ClearModel(ctx, mid); err != nil {
			t.Fatal(err)
		}
		got, _ = s.Conversations().Get(ctx, 1, c1.ID)
		if got.ModelID != nil {
			t.Fatal("删除模型后对话应改回默认模型")
		}
		if err := s.Models().SetDefault(ctx, 1, mid); err != nil {
			t.Fatal(err)
		}
		if err := s.Models().SetDefault(ctx, 1, pub.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("不能把别人的模型设为自己的默认模型")
		}
		m.Name = "改过"
		if err := s.Models().Update(ctx, m); err != nil {
			t.Fatal(err)
		}
		if g, _ := s.Models().Get(ctx, mid); g.Name != "改过" || !g.IsDefault {
			t.Fatalf("g=%+v", g)
		}

		msg := &store.Message{ConversationID: c1.ID, UserID: 1, Role: store.RoleAssistant}
		_ = s.Messages().Create(ctx, msg)
		msg.Reasoning, msg.Content = "先想想", "答案"
		if err := s.Messages().Finish(ctx, msg); err != nil {
			t.Fatal(err)
		}
		if err := s.Conversations().Delete(ctx, 1, c1.ID); err != nil {
			t.Fatal(err)
		}
		if ms, _ := s.Messages().Recent(ctx, 1, c1.ID, 10); len(ms) != 0 {
			t.Fatal("删除对话应删除其消息")
		}
		if err := s.Conversations().Delete(ctx, 1, c1.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
	})
}
