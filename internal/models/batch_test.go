package models

import (
	"context"
	"testing"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
)

func TestBatchUpdateAndDelete(t *testing.T) {
	ctx := context.Background()
	svc, db := newService(t, config.ModeMulti, "")
	admin, alice, bob := User{ID: 1, IsAdmin: true}, User{ID: 2}, User{ID: 3}
	pubIn := input("pub", "k")
	pubIn.Public = true
	pub, _ := svc.Create(ctx, admin, pubIn)
	a1, _ := svc.Create(ctx, alice, input("a1", "k"))
	a2, _ := svc.Create(ctx, alice, input("a2", "k"))
	b1, _ := svc.Create(ctx, bob, input("b1", "k"))

	yes, ctxLen, effort := true, 200000, store.ThinkingStyleEffort
	res, err := svc.BatchUpdate(ctx, alice, []uint64{a1.ID, a2.ID, b1.ID, pub.ID}, Patch{Vision: &yes, ContextTokens: &ctxLen, ThinkingStyle: &effort})
	if err != nil || res.Done != 2 || len(res.Failed) != 2 {
		t.Fatalf("alice 只能改自己的两个：%+v %v", res, err)
	}
	for _, id := range []uint64{a1.ID, a2.ID} {
		m, _ := db.Models().Get(ctx, id)
		if !m.Vision || m.ContextTokens != 200000 || m.ThinkingStyle != effort || m.Name == "" {
			t.Fatalf("改后：%+v", m)
		}
		key, _ := svc.box.Open(m.APIKey)
		if key != "k" {
			t.Fatal("批量修改不应动 Key")
		}
	}
	if m, _ := db.Models().Get(ctx, b1.ID); m.Vision {
		t.Fatal("别人的模型不应被改")
	}
	// 「换用推理模型」没填推理模型名：每个都失败并说明原因。
	sw := store.ThinkingStyleSwitchMod
	res, _ = svc.BatchUpdate(ctx, alice, []uint64{a1.ID}, Patch{ThinkingStyle: &sw})
	if res.Done != 0 || len(res.Failed) != 1 || res.Failed[0].Name != "a1" || res.Failed[0].Error == "" {
		t.Fatalf("应校验失败：%+v", res)
	}
	if _, err := svc.BatchUpdate(ctx, alice, []uint64{a1.ID}, Patch{}); err == nil {
		t.Fatal("什么都不改应报错")
	}
	// 管理员批量设仅限授权用户。
	res, _ = svc.BatchUpdate(ctx, admin, []uint64{pub.ID}, Patch{Restricted: &yes})
	if m, _ := db.Models().Get(ctx, pub.ID); res.Done != 1 || !m.Restricted {
		t.Fatalf("管理员改公共模型：%+v %+v", res, m)
	}
	// 批量删除：只删得掉自己的。
	res, err = svc.BatchDelete(ctx, alice, []uint64{a1.ID, a2.ID, b1.ID})
	if err != nil || res.Done != 2 || len(res.Failed) != 1 {
		t.Fatalf("批量删除：%+v %v", res, err)
	}
	if _, err := db.Models().Get(ctx, b1.ID); err != nil {
		t.Fatal("bob 的模型不应被删")
	}
}
