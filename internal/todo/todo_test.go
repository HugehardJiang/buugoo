package todo

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
	"github.com/HugehardJiang/buugoo/internal/tools"
)

var shanghai = time.FixedZone("CST", 8*3600)

func newService(t *testing.T) *Service {
	t.Helper()
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "t.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return New(db.Gorm(), shanghai)
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

func TestService(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	if _, err := s.Create(ctx, 1, "  ", "", ""); err == nil {
		t.Fatal("空内容应失败")
	}
	if _, err := s.Create(ctx, 1, "交论文", "", "下周五"); err == nil {
		t.Fatal("时间格式不对应失败")
	}
	a, _ := s.Create(ctx, 1, "写论文", "", "2026-11-05")
	b, _ := s.Create(ctx, 1, "交实验报告", "带上数据", "2026-10-30 18:00")
	c, _ := s.Create(ctx, 1, "学吉他", "", "")
	_, _ = s.Create(ctx, 2, "别人的", "", "")
	if !a.AllDay || b.AllDay || b.DueAt.In(shanghai).Hour() != 18 {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
	if _, err := s.SetDone(ctx, 1, a.ID, true); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List(ctx, 1)
	// 没做完的按截止时间排、没有截止时间的在后，做完的最后。
	if len(list) != 3 || list[0].ID != b.ID || list[1].ID != c.ID || list[2].ID != a.ID || list[2].DoneAt == nil {
		t.Fatalf("%+v", list)
	}
	empty := ""
	if got, _ := s.Update(ctx, 1, b.ID, Patch{Due: &empty}); got.DueAt != nil {
		t.Fatal("应去掉截止时间")
	}
	if _, err := s.Update(ctx, 2, b.ID, Patch{Due: &empty}); err != ErrNotFound {
		t.Fatalf("别人的不能改: %v", err)
	}
	if n, _ := s.ClearDone(ctx, 1); n != 1 {
		t.Fatalf("cleared %d", n)
	}
}

func TestTools(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	now := func() time.Time { return time.Date(2026, 10, 28, 20, 0, 0, 0, shanghai) }
	turn1 := Tools(s, 1, "t1", now)
	if r := call(t, turn1, "todo_add", map[string]any{"title": "写大物实验报告", "due": "2026-10-30 18:00"}); !r.OK || !strings.Contains(r.Output, "10-30 18:00") {
		t.Fatalf("%+v", r)
	}
	call(t, turn1, "todo_add", map[string]any{"title": "复习高数"})
	call(t, turn1, "todo_add", map[string]any{"title": "复习线代"})
	r := call(t, turn1, "todo_list", map[string]any{})
	if !strings.Contains(r.Output, "没做完的有 3 项") || !strings.Contains(r.Output, "写大物实验报告") {
		t.Fatalf("%s", r.Output)
	}
	// 按内容找：含糊时要用编号。
	if r := call(t, turn1, "todo_done", map[string]any{"item": "复习"}); r.OK {
		t.Fatalf("含糊的应失败: %+v", r)
	}
	if r := call(t, turn1, "todo_done", map[string]any{"item": "大物"}); !r.OK || !strings.Contains(r.Output, "已打勾") {
		t.Fatalf("%+v", r)
	}
	if r := call(t, turn1, "todo_update", map[string]any{"item": "高数", "due": "2026-11-01"}); !r.OK || !strings.Contains(r.Output, "11-01") {
		t.Fatalf("%+v", r)
	}
	r = call(t, turn1, "todo_list", map[string]any{"include_done": true})
	if !strings.Contains(r.Output, "〔已完成〕") || !strings.Contains(r.Output, "没做完的有 2 项") {
		t.Fatalf("%s", r.Output)
	}
	// 删除：先问，同一轮不能确认，下一轮带确认码才删。
	r = call(t, turn1, "todo_delete", map[string]any{"item": "线代"})
	code := regexp.MustCompile(`confirm_code=([0-9a-f]+)`).FindStringSubmatch(r.Output)
	if code == nil {
		t.Fatalf("%s", r.Output)
	}
	if r := call(t, turn1, "todo_delete", map[string]any{"item": "线代", "confirm_code": code[1]}); r.OK {
		t.Fatal("同一轮不能确认")
	}
	if r := call(t, Tools(s, 2, "t2", now), "todo_delete", map[string]any{"item": "线代", "confirm_code": code[1]}); r.OK {
		t.Fatal("别人不能用这个确认码")
	}
	if r := call(t, Tools(s, 1, "t2", now), "todo_delete", map[string]any{"item": "线代", "confirm_code": code[1]}); !r.OK {
		t.Fatalf("%+v", r)
	}
	if list, _ := s.List(ctx, 1); len(list) != 2 {
		t.Fatalf("%+v", list)
	}
}
