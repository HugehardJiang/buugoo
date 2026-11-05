package cosplay

import (
	"context"
	"strings"
	"testing"
)

func TestRevise(t *testing.T) {
	ctx := context.Background()
	s := newService(t, 300)
	alice, admin := User{ID: 1}, User{ID: 3, IsAdmin: true}
	mine, _ := s.Create(ctx, alice, Input{Name: "小鲸", Prompt: "你是小鲸。\n说话爱用括号。"})

	// 追加：第一次加上小标题，之后接着往下写。
	c, copied, err := s.Revise(ctx, alice, mine.ID, Edit{Mode: EditAppend, Text: "少用括号", Note: "少用括号"})
	if err != nil || copied || !strings.HasSuffix(c.Prompt, EvolveHeading+"\n- 少用括号") {
		t.Fatalf("%q %v %v", c.Prompt, copied, err)
	}
	c, _, _ = s.Revise(ctx, alice, mine.ID, Edit{Mode: EditAppend, Text: "叫我哥哥", Note: "称呼"})
	if strings.Count(c.Prompt, EvolveHeading) != 1 || !strings.HasSuffix(c.Prompt, "- 少用括号\n- 叫我哥哥") {
		t.Fatalf("%q", c.Prompt)
	}
	// 替换：原文要一字不差且只出现一次。
	if _, _, err := s.Revise(ctx, alice, mine.ID, Edit{Mode: EditReplace, Old: "不存在的话", Text: "x"}); err == nil {
		t.Fatal("没找到原文要报错")
	}
	c, _, err = s.Revise(ctx, alice, mine.ID, Edit{Mode: EditReplace, Old: "说话爱用括号。", Text: "", Note: "删掉括号设定"})
	if err != nil || strings.Contains(c.Prompt, "爱用括号") {
		t.Fatalf("%q %v", c.Prompt, err)
	}
	// 超过上限不改。
	if _, _, err := s.Revise(ctx, alice, mine.ID, Edit{Mode: EditAppend, Text: strings.Repeat("长", 400)}); err == nil {
		t.Fatal("超过上限要报错")
	}

	// 修改记录：最新的在前，能恢复，恢复前的样子也记下。
	revs, err := s.Revisions(ctx, alice, mine.ID)
	if err != nil || len(revs) != 3 || revs[0].Note != "删掉括号设定" || revs[2].Note != "少用括号" {
		t.Fatalf("%+v %v", revs, err)
	}
	card, err := s.Restore(ctx, alice, mine.ID, revs[2].ID)
	if err != nil || card.Runes != len([]rune("你是小鲸。\n说话爱用括号。")) {
		t.Fatalf("%+v %v", card, err)
	}
	if got, _ := s.Get(ctx, alice, mine.ID); got.Prompt != "你是小鲸。\n说话爱用括号。" {
		t.Fatalf("恢复后：%q", got.Prompt)
	}
	if revs, _ := s.Revisions(ctx, alice, mine.ID); len(revs) != 4 || revs[0].Note != "恢复之前的样子" {
		t.Fatalf("%+v", revs)
	}
	// 别人看不到也改不了。
	if _, err := s.Revisions(ctx, User{ID: 2}, mine.ID); err == nil {
		t.Fatal("别人不能看修改记录")
	}
	if _, _, err := s.Revise(ctx, User{ID: 2}, mine.ID, Edit{Mode: EditAppend, Text: "x"}); err != ErrNotFound {
		t.Fatalf("别人不能改：%v", err)
	}

	// 公共卡：复制一张自己的再改，原卡不变。
	pub, _ := s.Create(ctx, admin, Input{Name: "公共鲸", Prompt: "你是公共鲸。", Public: true})
	c, copied, err = s.Revise(ctx, alice, pub.ID, Edit{Mode: EditAppend, Text: "叫我主人", Note: "称呼"})
	if err != nil || !copied || c.UserID != alice.ID || c.ID == pub.ID || !strings.Contains(c.Prompt, "叫我主人") || !strings.Contains(c.Description, "公共鲸") {
		t.Fatalf("%+v %v %v", c, copied, err)
	}
	if orig, _ := s.Get(ctx, admin, pub.ID); orig.Prompt != "你是公共鲸。" {
		t.Fatal("公共卡不能被改掉")
	}
	if revs, _ := s.Revisions(ctx, alice, c.ID); len(revs) != 1 || revs[0].Prompt != "你是公共鲸。" {
		t.Fatalf("复制的卡要留着原版：%+v", revs)
	}

	// 管理员自己的公共卡，聊天里自我进化也不改掉大家用的那张，同样另存副本。
	c, copied, err = s.Revise(ctx, admin, pub.ID, Edit{Mode: EditAppend, Text: "管理员的改动"})
	if err != nil || !copied || c.Public || c.UserID != admin.ID {
		t.Fatalf("%+v %v %v", c, copied, err)
	}
	if orig, _ := s.Get(ctx, admin, pub.ID); orig.Prompt != "你是公共鲸。" {
		t.Fatal("公共卡不能被自我进化改掉")
	}

	// 手动修改提示词也记一条。
	if _, err := s.Update(ctx, alice, mine.ID, Input{Name: "小鲸", Prompt: "全新的设定"}); err != nil {
		t.Fatal(err)
	}
	if revs, _ := s.Revisions(ctx, alice, mine.ID); revs[0].Note != "手动修改前" {
		t.Fatalf("%+v", revs[0])
	}
	// 只留最近 MaxRevisions 条。
	for i := 0; i < MaxRevisions+5; i++ {
		if _, _, err := s.Revise(ctx, alice, mine.ID, Edit{Mode: EditReplace, Old: "设定", Text: "设定"}); err != nil {
			t.Fatal(err)
		}
	}
	if revs, _ := s.Revisions(ctx, alice, mine.ID); len(revs) != MaxRevisions {
		t.Fatalf("应只留 %d 条，实际 %d", MaxRevisions, len(revs))
	}
}

func TestFork(t *testing.T) {
	ctx := context.Background()
	s := newService(t, 300)
	alice, bob, admin := User{ID: 1}, User{ID: 2}, User{ID: 3, IsAdmin: true}
	pub, _ := s.Create(ctx, admin, Input{Name: "公共鲸", Prompt: "你是公共鲸。", Public: true})
	// 普通用户不能直接改公共卡。
	if _, err := s.Update(ctx, alice, pub.ID, Input{Name: "x", Prompt: "y", Public: true}); err != ErrForbidden {
		t.Fatalf("%v", err)
	}
	// alice 和 bob 各有一段用公共卡的对话。
	db := s.db
	for _, uid := range []uint64{alice.ID, alice.ID, bob.ID} {
		if err := db.Exec("INSERT INTO conversations (user_id, title, thinking, created_at, updated_at, character_id, character_name, character_memory) VALUES (?, '', 'off', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, ?, '公共鲸', 'read')", uid, pub.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	card, err := s.Fork(ctx, alice, pub.ID, Input{Name: "我的鲸", Prompt: "你是我的鲸。", Public: true})
	if err != nil || !card.Mine || card.Public || card.Name != "我的鲸" {
		t.Fatalf("%+v %v", card, err)
	}
	count := func(uid, cid uint64) (n int64) {
		db.Table("conversations").Where("user_id = ? AND character_id = ?", uid, cid).Count(&n)
		return
	}
	if count(alice.ID, card.ID) != 2 || count(alice.ID, pub.ID) != 0 || count(bob.ID, pub.ID) != 1 {
		t.Fatal("只换 alice 自己的对话")
	}
	if orig, _ := s.Get(ctx, admin, pub.ID); orig.Prompt != "你是公共鲸。" {
		t.Fatal("公共卡不变")
	}
	if revs, _ := s.Revisions(ctx, alice, card.ID); len(revs) != 1 {
		t.Fatalf("副本要留着原版：%+v", revs)
	}
	if _, err := s.Fork(ctx, alice, card.ID, Input{Name: "x", Prompt: "y"}); err == nil {
		t.Fatal("自己的卡不用另存")
	}
}
