package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/cosplay"
	"github.com/HugehardJiang/buugoo/internal/store"
)

func TestCosplayConversation(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, config.QuotaConfig{})
	cs := cosplay.New(e.db.Gorm(), 30000)
	e.svc.characters = cs
	e.writer.Start(ctx)
	// 先在普通对话里留下一条记忆。
	if _, err := e.svc.Send(ctx, e.user, SendInput{Content: "我对花生过敏"}, &recordSink{}); err != nil {
		t.Fatal(err)
	}
	e.svc.Wait()
	closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := e.writer.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	_, total, _ := e.db.Memories().List(ctx, e.user.ID, store.MemoryFilter{}, 10, 0)
	if total != 1 {
		t.Fatalf("应该有 1 条记忆，实际 %d", total)
	}
	card, err := cs.Create(ctx, cosplay.User{ID: e.user.ID}, cosplay.Input{Name: "小鲸鱼", Prompt: "# 角色卡：小鲸鱼\n你是一条爱吃白饭的鲸鱼，被叫大肥鱼会炸毛。"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := cs.Create(ctx, cosplay.User{ID: e.user.ID + 100}, cosplay.Input{Name: "别人的卡", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}

	// 不能用别人的卡、不能和临时对话一起用、记忆方式要有效。
	if _, err := e.svc.CreateConversation(ctx, e.user, ConversationPatch{CharacterID: &other.ID}); !errors.Is(err, ErrCharacterNotFound) {
		t.Fatalf("别人的卡: %v", err)
	}
	if _, err := e.svc.CreateConversation(ctx, e.user, ConversationPatch{CharacterID: &card.ID, Temporary: true}); !errors.Is(err, ErrCharacterCombo) {
		t.Fatalf("临时对话: %v", err)
	}
	if _, err := e.svc.CreateConversation(ctx, e.user, ConversationPatch{CharacterID: &card.ID, CharacterMemory: "all"}); !errors.Is(err, ErrCharacterMemory) {
		t.Fatalf("记忆方式: %v", err)
	}

	send := func(conv *store.Conversation, text string) (*recordSink, string, string) {
		t.Helper()
		sink := &recordSink{}
		if _, err := e.svc.Send(ctx, e.user, SendInput{ConversationID: conv.ID, Content: text}, sink); err != nil {
			t.Fatal(err)
		}
		e.svc.Wait()
		req := e.chat.reqs[len(e.chat.reqs)-1]
		return sink, req.Messages[0].Content, req.Messages[len(req.Messages)-1].Content
	}

	// 默认是「能看记忆，不写记忆」：系统提示词是角色卡，不是布谷的人格；能想起花生过敏，但不写新记忆。
	conv, err := e.svc.CreateConversation(ctx, e.user, ConversationPatch{CharacterID: &card.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !conv.Cosplay() || conv.CharacterName != "小鲸鱼" || conv.CharacterMemory != store.CharacterMemoryRead {
		t.Fatalf("%+v", conv)
	}
	sink, sys, last := send(conv, "大肥鱼，晚饭吃花生酱拌面可以吗")
	if !strings.Contains(sys, "你在扮演「小鲸鱼」") || !strings.Contains(sys, "爱吃白饭的鲸鱼") || strings.Contains(sys, "# 你是布谷") {
		t.Fatalf("系统提示词: %s", sys)
	}
	if strings.Contains(sys, "扮演别人") {
		t.Fatalf("不应有布谷的「拒绝扮演别人」原则: %s", sys)
	}
	if len(sink.mems) != 1 || !strings.Contains(last, "花生过敏") || !strings.Contains(last, "不会写进这些记忆") {
		t.Fatalf("应参考记忆: mems=%+v last=%s", sink.mems, last)
	}
	if _, total, _ := e.db.Memories().List(ctx, e.user.ID, store.MemoryFilter{}, 10, 0); total != 1 {
		t.Fatalf("不应写记忆，现在有 %d 条", total)
	}
	got, _ := e.db.Conversations().Get(ctx, e.user.ID, conv.ID)
	if got.Title == "" {
		t.Fatal("Cosplay 对话照常起标题")
	}
	if c, err := e.svc.latestConversation(ctx, e.user); err != nil || c.Cosplay() {
		t.Fatal("主动找对方时不能落到 Cosplay 对话里", c, err)
	}

	// 完全隔离：不检索记忆。
	off, err := e.svc.CreateConversation(ctx, e.user, ConversationPatch{CharacterID: &card.ID, CharacterMemory: store.CharacterMemoryOff})
	if err != nil {
		t.Fatal(err)
	}
	sink, _, last = send(off, "那花生呢")
	if len(sink.mems) != 0 || strings.Contains(last, "花生过敏") || !strings.Contains(last, "不参考对方以前的聊天") {
		t.Fatalf("不应参考记忆: mems=%+v last=%s", sink.mems, last)
	}

	// 卡删掉后，用它建的对话照样能聊；但不能再用它建新对话。
	if err := cs.Delete(ctx, cosplay.User{ID: e.user.ID}, card.ID); err != nil {
		t.Fatal(err)
	}
	if _, sys, _ := send(conv, "还在吗"); !strings.Contains(sys, "爱吃白饭的鲸鱼") {
		t.Fatal("删卡后对话应照常扮演")
	}
	if _, err := e.svc.CreateConversation(ctx, e.user, ConversationPatch{CharacterID: &card.ID}); !errors.Is(err, ErrCharacterNotFound) {
		t.Fatalf("删掉的卡不能再建对话: %v", err)
	}
}

func TestCosplayFullMemory(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, config.QuotaConfig{})
	cs := cosplay.New(e.db.Gorm(), 30000)
	e.svc.characters = cs
	e.writer.Start(ctx)
	card, _ := cs.Create(ctx, cosplay.User{ID: e.user.ID}, cosplay.Input{Name: "小鲸鱼", Prompt: "你是小鲸鱼"})
	conv, err := e.svc.CreateConversation(ctx, e.user, ConversationPatch{CharacterID: &card.ID, CharacterMemory: store.CharacterMemoryFull})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Send(ctx, e.user, SendInput{ConversationID: conv.ID, Content: "我对花生过敏"}, &recordSink{}); err != nil {
		t.Fatal(err)
	}
	e.svc.Wait()
	closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := e.writer.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := e.db.Memories().List(ctx, e.user.ID, store.MemoryFilter{}, 10, 0); total != 1 {
		t.Fatalf("「全部互通」应该写记忆，现在有 %d 条", total)
	}
}
