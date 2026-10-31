package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func TestRecallChat(t *testing.T) {
	ctx := context.Background()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "r.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	u, _ := db.Users().EnsureOwner(ctx)
	conv := &store.Conversation{UserID: u.ID}
	if err := db.Conversations().Create(ctx, conv); err != nil {
		t.Fatal(err)
	}
	var src uint64
	for i := 0; i < 20; i++ {
		role := store.RoleUser
		if i%2 == 1 {
			role = store.RoleAssistant
		}
		text := "第" + string(rune('A'+i)) + "句" + strings.Repeat("话", 300)
		m := &store.Message{ConversationID: conv.ID, UserID: u.ID, Role: role, Content: text, Status: store.MessageComplete}
		if err := db.Messages().Create(ctx, m); err != nil {
			t.Fatal(err)
		}
		if i == 10 {
			src = m.ID
		}
	}
	mem := &store.Memory{UserID: u.ID, Type: "semantic", Content: "用户说过一件事", Source: store.SourceStated, SourceMessageID: &src, ConversationID: &conv.ID}
	if err := db.Memories().Create(ctx, mem); err != nil {
		t.Fatal(err)
	}
	other := &store.Memory{UserID: u.ID, Type: "semantic", Content: "整理出来的", Source: store.SourceStated}
	_ = db.Memories().Create(ctx, other)

	run := recallTools(db, u.ID, time.UTC)[0].Run
	args := func(id uint64) json.RawMessage { b, _ := json.Marshal(map[string]uint64{"memory_id": id}); return b }
	res := run(ctx, args(mem.ID))
	if !res.OK || !strings.Contains(res.Output, "第K句") || strings.Contains(res.Output, "第A句") || strings.Contains(res.Output, "第T句") {
		t.Fatalf("应只取出处前后几条：%s", res.Output)
	}
	if n := len([]rune(res.Output)); n > recallTotal+200 {
		t.Fatalf("返回太长：%d 字", n)
	}
	// 每轮只能翻一次。
	if res := run(ctx, args(mem.ID)); res.OK || !strings.Contains(res.Output, "已经翻看过") {
		t.Fatal(res.Output)
	}
	// 没有出处的记忆、别人的记忆都翻不到。
	if res := recallTools(db, u.ID, time.UTC)[0].Run(ctx, args(other.ID)); res.OK {
		t.Fatal("没有出处的记忆不应返回内容")
	}
	if res := recallTools(db, u.ID+99, time.UTC)[0].Run(ctx, args(mem.ID)); res.OK {
		t.Fatal("不能翻别人的聊天")
	}
	// 对话删掉了。
	_ = db.Conversations().Delete(ctx, u.ID, conv.ID)
	if res := recallTools(db, u.ID, time.UTC)[0].Run(ctx, args(mem.ID)); res.OK || !strings.Contains(res.Output, "删掉") {
		t.Fatal(res.Output)
	}
}
