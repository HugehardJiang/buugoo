package cosplay

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

func newService(t *testing.T, maxRunes int) *Service {
	t.Helper()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "t.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return New(db.Gorm(), maxRunes)
}

func TestParseHTMLCards(t *testing.T) {
	s := &Service{maxRunes: 30000}
	f, err := os.Open("testdata/cards.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cards, err := s.ParseHTML(f, "https://example.com/persona")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 {
		t.Fatalf("应该解析出 2 张卡，得到 %d 张：%+v", len(cards), cards)
	}
	a := cards[0]
	if a.Name != "测试鲸鱼 · 群友版" || a.Description != "一条爱吃白饭的鲸鱼。" || strings.Join(a.Tags, ",") != "群聊,傲娇" {
		t.Errorf("第一张卡：%+v", a)
	}
	if !strings.HasPrefix(a.Prompt, "# 角色卡：测试鲸鱼") || !strings.Contains(a.Prompt, `你是一条"小鲸鱼"`) {
		t.Errorf("提示词：%q", a.Prompt)
	}
	if strings.Contains(a.Prompt, "FILE:") || strings.Contains(a.Prompt, "persona.json") || strings.Contains(a.Prompt, "json 里的简介") {
		t.Errorf("提示词里不应该有文件头和 persona.json：%q", a.Prompt)
	}
	if a.Source != "https://example.com/persona" || a.Runes == 0 || a.Tokens == 0 {
		t.Errorf("来源和字数：%+v", a)
	}
	if cards[1].Name != "妈妈版" || cards[1].Prompt != "你是鲸鱼妈妈。" {
		t.Errorf("第二张卡：%+v", cards[1])
	}
}

func TestParseHTMLPre(t *testing.T) {
	s := &Service{maxRunes: 30000}
	page := "<html><body><h2>猫娘</h2><pre>" + strings.Repeat("你是一只猫娘。", 40) + "</pre><pre>短</pre></body></html>"
	cards, err := s.ParseHTML(strings.NewReader(page), "")
	if err != nil || len(cards) != 1 || cards[0].Name != "猫娘" {
		t.Fatalf("cards=%+v err=%v", cards, err)
	}
	if _, err := s.ParseHTML(strings.NewReader("<p>什么都没有</p>"), ""); err == nil {
		t.Fatal("没有提示词时应该报错")
	}
}

func TestParseText(t *testing.T) {
	s := &Service{maxRunes: 50}
	c, err := s.ParseText("# 角色卡：小鲸 —— 迷你卡\n\n你是小鲸。\r\n\r\n\r\n\r\n喜欢白饭。", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "小鲸 —— 迷你卡" || c.Prompt != "# 角色卡：小鲸 —— 迷你卡\n\n你是小鲸。\n\n喜欢白饭。" || c.TooLong {
		t.Errorf("%+v", c)
	}
	c, _ = s.ParseText(strings.Repeat("长", 51), "我的角色.txt")
	if !c.TooLong || c.Name != "我的角色" {
		t.Errorf("超长：%+v", c)
	}
	// SillyTavern V2 角色卡
	st := `{"spec":"chara_card_v2","data":{"name":"艾拉","description":"{{char}} 是 {{user}} 的邻居。","personality":"开朗","first_mes":"嗨，{{user}}！","creator_notes":"一个邻居\n第二行","tags":["日常"]}}`
	c, err = s.ParseText(st, "ella.json")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "艾拉" || c.Description != "一个邻居" || !strings.Contains(c.Prompt, "艾拉 是 对方 的邻居。") || !strings.Contains(c.Prompt, "嗨，对方！") || strings.Contains(c.Prompt, "{{") {
		t.Errorf("SillyTavern：%+v", c)
	}
	if _, err := s.ParseText("  ", ""); err == nil {
		t.Error("空内容应该报错")
	}
}

func TestCRUDAndPermissions(t *testing.T) {
	ctx := context.Background()
	s := newService(t, 100)
	alice, bob, admin := User{ID: 1}, User{ID: 2}, User{ID: 3, IsAdmin: true}

	if _, err := s.Create(ctx, alice, Input{Name: "x", Prompt: "p", Public: true}); err == nil {
		t.Fatal("普通用户不能发布公共卡")
	}
	if _, err := s.Create(ctx, alice, Input{Name: "x", Prompt: strings.Repeat("长", 101)}); err == nil {
		t.Fatal("超过上限应该报错")
	}
	mine, err := s.Create(ctx, alice, Input{Name: " 小 鲸 ", Description: "d", Prompt: "你是小鲸", Source: "javascript:alert(1)"})
	if err != nil {
		t.Fatal(err)
	}
	if mine.Name != "小 鲸" || mine.Source != "" || !mine.Mine {
		t.Errorf("%+v", mine)
	}
	pub, err := s.Create(ctx, admin, Input{Name: "公共鲸", Prompt: "你是公共鲸", Public: true})
	if err != nil {
		t.Fatal(err)
	}

	list, _ := s.List(ctx, alice)
	if len(list) != 2 || list[0].ID != mine.ID || list[1].ID != pub.ID || list[1].Mine {
		t.Fatalf("alice 的列表：%+v", list)
	}
	if list, _ := s.List(ctx, bob); len(list) != 1 || list[0].ID != pub.ID {
		t.Fatalf("bob 只能看到公共卡：%+v", list)
	}
	if _, err := s.Get(ctx, bob, mine.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob 不能读 alice 的卡：%v", err)
	}
	if _, err := s.Update(ctx, bob, mine.ID, Input{Name: "x", Prompt: "y"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob 不能改 alice 的卡：%v", err)
	}
	if _, err := s.Update(ctx, alice, pub.ID, Input{Name: "x", Prompt: "y", Public: true}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("alice 不能改公共卡：%v", err)
	}
	if err := s.Delete(ctx, alice, pub.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("alice 不能删公共卡：%v", err)
	}
	if _, err := s.Update(ctx, alice, mine.ID, Input{Name: "小鲸", Prompt: "你是小鲸2", Public: true}); err == nil {
		t.Fatal("普通用户不能把卡改成公共卡")
	}
	up, err := s.Update(ctx, alice, mine.ID, Input{Name: "小鲸", Prompt: "你是小鲸2"})
	if err != nil || up.Name != "小鲸" {
		t.Fatalf("%+v %v", up, err)
	}
	if _, err := s.Update(ctx, admin, pub.ID, Input{Name: "公共鲸2", Prompt: "p", Public: true}); err != nil {
		t.Fatalf("管理员可以改公共卡：%v", err)
	}

	// 删除后列表里没有了，但聊天时照样能读到。
	if err := s.Delete(ctx, alice, mine.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(ctx, alice); len(list) != 1 {
		t.Fatalf("删除后：%+v", list)
	}
	if c, err := s.Load(ctx, mine.ID); err != nil || c.Prompt != "你是小鲸2" {
		t.Fatalf("删掉的卡聊天时还要能读到：%+v %v", c, err)
	}
}

func TestFetchURLRejectsPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("你是猫")) }))
	defer srv.Close()
	s := &Service{maxRunes: 100, fetcher: newFetcher()}
	if _, err := s.FetchURL(context.Background(), srv.URL); err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("不能从内网地址导入：%v", err)
	}
	if _, err := s.FetchURL(context.Background(), "file:///etc/passwd"); err == nil {
		t.Fatal("只能是 http(s) 网址")
	}
}
