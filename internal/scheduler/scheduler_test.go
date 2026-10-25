package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

type testJob struct {
	name, spec string
	fn         func(context.Context) error
}

func (j testJob) Name() string                  { return j.name }
func (j testJob) Spec() string                  { return j.spec }
func (j testJob) Run(ctx context.Context) error { return j.fn(ctx) }

func newDB(t *testing.T) *gormstore.DB {
	db, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverSQLite, DSN: filepath.Join(t.TempDir(), "s.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRegisterValidation(t *testing.T) {
	s := New(newDB(t).Jobs(), time.UTC, nil)
	ok := testJob{"a", "0 4 * * *", func(context.Context) error { return nil }}
	if err := s.Register(ok); err != nil {
		t.Fatal(err)
	}
	if err := s.Register(ok); err == nil {
		t.Fatal("重复注册应报错")
	}
	if err := s.Register(testJob{"b", "not a cron", nil}); err == nil {
		t.Fatal("无效 cron 应报错")
	}
}

func TestRunRecordsAndRecovers(t *testing.T) {
	db := newDB(t)
	s := New(db.Jobs(), time.UTC, nil)
	ctx := context.Background()
	_ = s.Register(testJob{"ok", "@daily", func(context.Context) error { return nil }})
	_ = s.Register(testJob{"fail", "@daily", func(context.Context) error { return errors.New("boom") }})
	_ = s.Register(testJob{"panic", "@daily", func(context.Context) error { panic("oops") }})

	if err := s.RunNow(ctx, "ok"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Jobs().LastSucceeded(ctx, "ok"); err != nil {
		t.Fatalf("应记录成功运行: %v", err)
	}
	if err := s.RunNow(ctx, "fail"); err == nil {
		t.Fatal("应返回任务错误")
	}
	if err := s.RunNow(ctx, "panic"); err == nil {
		t.Fatal("panic 应转为错误")
	}
	var runs []store.JobRun
	db.Gorm().Order("id").Find(&runs)
	if len(runs) != 3 || runs[1].Status != store.JobFailed || runs[1].Error != "boom" || runs[2].Status != store.JobFailed || runs[2].FinishedAt == nil {
		t.Fatalf("runs=%+v", runs)
	}
	if err := s.RunNow(ctx, "missing"); err == nil {
		t.Fatal("未知任务应报错")
	}
}

func TestNoOverlapAndSchedule(t *testing.T) {
	s := New(newDB(t).Jobs(), time.UTC, nil)
	release := make(chan struct{})
	var calls atomic.Int32
	_ = s.Register(testJob{"slow", "@every 1s", func(context.Context) error {
		calls.Add(1)
		<-release
		return nil
	}})
	s.Start(context.Background())
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatal("定时任务应被触发")
	}
	if err := s.RunNow(context.Background(), "slow"); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("运行中的任务不应重叠执行: %v", err)
	}
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
