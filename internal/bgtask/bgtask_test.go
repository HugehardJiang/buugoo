package bgtask

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/chat"
	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

type fakeExec struct {
	mu      sync.Mutex
	started chan uint64
	release chan struct{}
	runs    []chat.TaskRun
	fail    error
}

func (f *fakeExec) ExecuteTask(ctx context.Context, u *store.User, t *store.BgTask, run chat.TaskRun) (*chat.ProactiveResult, string, error) {
	f.mu.Lock()
	f.runs = append(f.runs, run)
	f.mu.Unlock()
	run.Progress.Step(chat.ToolEvent{ID: "s1", Name: "web_search", Status: "done"})
	run.Progress.Round(1234, 1, []llm.Message{{Role: llm.RoleUser, Content: "x"}})
	f.started <- t.ID
	select {
	case <-f.release:
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	if f.fail != nil {
		return nil, "", f.fail
	}
	return &chat.ProactiveResult{Message: &store.Message{ID: 77}}, "做完了：" + t.Title, nil
}

func newDB(t *testing.T) *gormstore.DB {
	cfg := config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "t.db")}
	if dsn := os.Getenv("BUUGOO_TEST_MYSQL_DSN"); dsn != "" {
		cfg = config.DatabaseConfig{Driver: config.DriverMySQL, DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 2}
	}
	db, err := gormstore.Open(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cfg.Driver == config.DriverMySQL {
		db.Gorm().Exec("DELETE FROM bg_tasks")
		db.Gorm().Exec("DELETE FROM users")
	}
	return db
}

func waitStatus(t *testing.T, s *Service, u *store.User, id uint64, want string) *View {
	t.Helper()
	for i := 0; i < 200; i++ {
		v, err := s.Get(context.Background(), u, id)
		if err == nil && v.Status == want {
			return v
		}
		time.Sleep(20 * time.Millisecond)
	}
	v, _ := s.Get(context.Background(), u, id)
	t.Fatalf("任务 %d 应该是 %s，实际 %+v", id, want, v)
	return nil
}

func TestTasks(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	alice := &store.User{Username: "alice", PasswordHash: "x"}
	bob := &store.User{Username: "bob", PasswordHash: "x"}
	for _, u := range []*store.User{alice, bob} {
		if err := db.Users().Create(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	allowed := map[uint64]bool{alice.ID: true}
	cfg := config.Default().Tasks
	cfg.MaxRunning, cfg.PerUser = 2, 1
	s := New(db.Gorm(), cfg, db.Users(), func(_ context.Context, u *store.User) (bool, bool) { return allowed[u.ID], true }, nil)
	ex := &fakeExec{started: make(chan uint64, 10), release: make(chan struct{})}
	s.SetExecutor(ex)
	var delivered []uint64
	var dmu sync.Mutex
	s.SetDeliver(func(_ context.Context, u *store.User, _ *chat.ProactiveResult) {
		dmu.Lock()
		delivered = append(delivered, u.ID)
		dmu.Unlock()
	})

	// 没有套餐的不能提。
	if _, err := s.Propose(ctx, bob, 1, "x", "y", 0, false); err == nil {
		t.Fatal("没有套餐不能提任务")
	}
	t1, err := s.Propose(ctx, alice, 1, "调研 A", "详细调研 A", 0, false)
	if err != nil || t1.Status != store.TaskPending || t1.BudgetTokens != cfg.DefaultBudget {
		t.Fatalf("%+v %v", t1, err)
	}
	// 别人看不到、确认不了。
	if _, err := s.Confirm(ctx, bob, t1.ID, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("别人不能确认：%v", err)
	}
	// 预算会被限制在范围内。
	if v, err := s.Confirm(ctx, alice, t1.ID, 999_999_999); err != nil || v.Status != store.TaskQueued || v.BudgetTokens != cfg.MaxBudget || !v.Network {
		t.Fatalf("%+v %v", v, err)
	}
	if _, err := s.Confirm(ctx, alice, t1.ID, 0); err == nil {
		t.Fatal("不能确认两次")
	}
	// 聊天软件里口头答应过的直接排队。
	t2, _ := s.Propose(ctx, alice, 1, "调研 B", "详细调研 B", 50_000, true)
	if t2.Status != store.TaskQueued {
		t.Fatalf("%+v", t2)
	}

	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { s.Start(runCtx); close(done) }()
	// 每人同时只跑一个：先跑 t1，t2 排队。
	if id := <-ex.started; id != t1.ID {
		t.Fatalf("先跑 %d，实际 %d", t1.ID, id)
	}
	v := waitStatus(t, s, alice, t1.ID, store.TaskRunning)
	if len(v.Steps) != 1 || v.UsedTokens != 1234 {
		t.Fatalf("进展：%+v", v)
	}
	if v2, _ := s.Get(ctx, alice, t2.ID); v2.Status != store.TaskQueued {
		t.Fatalf("t2 应该还在排队：%+v", v2)
	}
	ex.release <- struct{}{}
	v = waitStatus(t, s, alice, t1.ID, store.TaskDone)
	if v.Result != "做完了：调研 A" || v.MessageID == nil || *v.MessageID != 77 || v.Transcript != "" {
		t.Fatalf("%+v", v)
	}
	// t1 做完后 t2 开始；取消正在跑的 t2。
	if id := <-ex.started; id != t2.ID {
		t.Fatalf("接着跑 %d，实际 %d", t2.ID, id)
	}
	if err := s.Cancel(ctx, alice, t2.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, s, alice, t2.ID, store.TaskCanceled)
	dmu.Lock()
	if len(delivered) != 1 || delivered[0] != alice.ID {
		t.Fatalf("只有做完的发出去：%+v", delivered)
	}
	dmu.Unlock()

	// 执行出错：记下错误。
	ex.fail = errors.New("模型服务出错了")
	t3, _ := s.Propose(ctx, alice, 1, "调研 C", "C", 0, true)
	s.poke()
	<-ex.started
	ex.release <- struct{}{}
	if v := waitStatus(t, s, alice, t3.ID, store.TaskFailed); v.Error != "模型服务出错了" {
		t.Fatalf("%+v", v)
	}
	stop()
	<-done

	// 服务器重启：跑到一半的任务标成「被打断」，可以接着做（保留工作过程）。
	if err := db.Gorm().Model(&store.BgTask{}).Where("id = ?", t3.ID).Updates(map[string]any{"status": store.TaskRunning, "transcript": `[{"role":"user","content":"x"}]`}).Error; err != nil {
		t.Fatal(err)
	}
	s2 := New(db.Gorm(), cfg, db.Users(), s.access, nil)
	ctx2, stop2 := context.WithCancel(ctx)
	go s2.Start(ctx2)
	waitStatus(t, s2, alice, t3.ID, store.TaskInterrupted)
	stop2()
	if _, err := s2.Resume(ctx, alice, t3.ID); err != nil {
		t.Fatal(err)
	}
	if v, _ := s2.Get(ctx, alice, t3.ID); v.Status != store.TaskQueued || v.Transcript == "" {
		t.Fatalf("接着做要保留工作过程：%+v", v)
	}
	if _, err := s2.Resume(ctx, alice, t1.ID); err == nil {
		t.Fatal("做完的不能接着做")
	}
	// 每人最多 5 个等确认的。
	for i := 0; i < maxPending; i++ {
		if _, err := s.Propose(ctx, alice, 1, "t", "g", 0, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Propose(ctx, alice, 1, "t", "g", 0, false); err == nil {
		t.Fatal("等确认的太多了")
	}
	if list, _ := s.List(ctx, alice, 100); len(list) != 8 || list[0].Steps != "" {
		t.Fatalf("列表：%d", len(list))
	}
}
