package memo

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
	"github.com/HugehardJiang/buugoo/internal/tools"
)

func newService(t *testing.T) *Service {
	t.Helper()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "m.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return New(db.Gorm())
}

func call(t *testing.T, list []tools.Extra, name string, args any) tools.Result {
	t.Helper()
	b, _ := json.Marshal(args)
	for _, x := range list {
		if x.Def.Name == name {
			return x.Run(context.Background(), b)
		}
	}
	t.Fatalf("no tool %s", name)
	return tools.Result{}
}

func TestPages(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	p, err := s.Create(ctx, 1, "  购物  清单 ", "- 牛奶")
	if err != nil || p.Title != "购物 清单" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := s.Create(ctx, 1, "购物 清单", ""); err == nil {
		t.Error("重名应失败")
	}
	if _, err := s.Create(ctx, 2, "购物 清单", ""); err != nil {
		t.Error("不同用户可以重名")
	}
	if _, err := s.Get(ctx, 2, p.ID); err != ErrNotFound {
		t.Error("不能读别人的")
	}
	title := "买菜"
	if _, err := s.Update(ctx, 1, p.ID, &title, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Find(ctx, 1, "买菜"); got == nil || got.Content != "- 牛奶" {
		t.Fatalf("%+v", got)
	}
	if err := s.Delete(ctx, 2, p.ID); err != ErrNotFound {
		t.Error("不能删别人的")
	}
}

func TestTools(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	turn1 := Tools(s, 1, "t1")
	if r := call(t, turn1, "memo_list", map[string]any{}); !strings.Contains(r.Output, "空") {
		t.Errorf("list empty = %q", r.Output)
	}
	// 没有这一页就新建，之后追加。
	if r := call(t, turn1, "memo_write", map[string]any{"page": "购物清单", "content": "- 牛奶"}); !r.OK || !strings.Contains(r.Summary, "新建") {
		t.Fatalf("%+v", r)
	}
	call(t, turn1, "memo_write", map[string]any{"page": "购物清单", "content": "- 鸡蛋\n- 面包"})
	call(t, turn1, "memo_write", map[string]any{"page": "购物清单", "content": "# 周末", "mode": "prepend"})
	p, _ := s.Find(ctx, 1, "购物清单")
	if p.Content != "# 周末\n- 牛奶\n- 鸡蛋\n- 面包" {
		t.Fatalf("%q", p.Content)
	}
	if r := call(t, turn1, "memo_read", map[string]any{"page": "购物清单"}); r.Output != p.Content {
		t.Errorf("read = %q", r.Output)
	}
	if r := call(t, turn1, "memo_read", map[string]any{"page": "不存在"}); r.OK {
		t.Error("读不存在的页应失败")
	}
	// 修改：原文要唯一；替换成空要用删除。
	if r := call(t, turn1, "memo_edit", map[string]any{"page": "购物清单", "find": "鸡蛋", "replace": "鸡蛋 x2"}); !r.OK {
		t.Fatalf("%+v", r)
	}
	if r := call(t, turn1, "memo_edit", map[string]any{"page": "购物清单", "find": "- ", "replace": "* "}); r.OK {
		t.Error("不唯一的原文应失败")
	}
	if r := call(t, turn1, "memo_edit", map[string]any{"page": "购物清单", "find": "面包", "replace": ""}); r.OK {
		t.Error("替换成空应要求用删除")
	}

	// 删除：第一次只给确认码；同一轮里不能确认；下一轮带上确认码才删。
	r := call(t, turn1, "memo_delete", map[string]any{"page": "购物清单", "text": "面包"})
	code := regexp.MustCompile(`confirm_code=([0-9a-f]+)`).FindStringSubmatch(r.Output)
	if !r.OK || code == nil {
		t.Fatalf("%+v", r)
	}
	if r := call(t, turn1, "memo_delete", map[string]any{"page": "购物清单", "confirm_code": code[1]}); r.OK {
		t.Fatal("同一轮里不能确认")
	}
	turn2 := Tools(s, 1, "t2")
	if r := call(t, Tools(s, 2, "t2"), "memo_delete", map[string]any{"page": "购物清单", "confirm_code": code[1]}); r.OK {
		t.Fatal("别人不能用这个确认码")
	}
	if r := call(t, turn2, "memo_delete", map[string]any{"page": "购物清单", "confirm_code": code[1]}); !r.OK {
		t.Fatalf("%+v", r)
	}
	p, _ = s.Find(ctx, 1, "购物清单")
	if p.Content != "# 周末\n- 牛奶\n- 鸡蛋 x2" {
		t.Fatalf("%q", p.Content)
	}
	if r := call(t, turn2, "memo_delete", map[string]any{"page": "购物清单", "confirm_code": code[1]}); r.OK {
		t.Fatal("确认码只能用一次")
	}
	// 删除整页。
	r = call(t, turn2, "memo_delete", map[string]any{"page": "购物清单"})
	code = regexp.MustCompile(`confirm_code=([0-9a-f]+)`).FindStringSubmatch(r.Output)
	if r := call(t, Tools(s, 1, "t3"), "memo_delete", map[string]any{"page": "x", "confirm_code": code[1]}); !r.OK {
		t.Fatalf("%+v", r)
	}
	if _, err := s.Find(ctx, 1, "购物清单"); err != ErrNotFound {
		t.Error("整页应删除")
	}
}

func TestRemoveText(t *testing.T) {
	cases := [][3]string{
		{"- a\n- b\n- c", "b", "- a\n- c"},
		{"- a\n- b", "- b", "- a"},
		{"今天买了苹果和梨", "和梨", "今天买了苹果"},
		{"x\n\nmid\n\ny", "mid", "x\n\ny"},
	}
	for _, c := range cases {
		if got := removeText(c[0], c[1]); got != c[2] {
			t.Errorf("removeText(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}
