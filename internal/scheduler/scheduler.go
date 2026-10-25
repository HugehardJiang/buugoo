// Package scheduler 是后台定时任务框架：按 cron 表达式（配置时区）触发任务，
// 防止同一任务重叠执行，捕获 panic，并把每次运行记录到 job_runs 表。
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/HugehardJiang/buugoo/internal/store"
)

// Job 是一个定时任务。
type Job interface {
	// Name 是任务的唯一名称，记录在 job_runs.job_name。
	Name() string
	// Spec 是标准 5 段 cron 表达式（分 时 日 月 周），也支持 @daily、@every 1h 等写法。
	Spec() string
	Run(ctx context.Context) error
}

// ErrAlreadyRunning 表示任务上一次运行尚未结束。
var ErrAlreadyRunning = errors.New("任务正在运行")

// Scheduler 管理定时任务。
type Scheduler struct {
	cron *cron.Cron
	runs store.JobRepo
	log  *slog.Logger
	now  func() time.Time

	mu      sync.Mutex
	jobs    map[string]Job
	running map[string]bool
	ctx     context.Context
	wg      sync.WaitGroup
}

// New 创建调度器。cron 表达式按 loc 解释。
func New(runs store.JobRepo, loc *time.Location, log *slog.Logger) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{
		cron: cron.New(cron.WithLocation(loc)),
		runs: runs, log: log.With("component", "scheduler"), now: time.Now,
		jobs: map[string]Job{}, running: map[string]bool{}, ctx: context.Background(),
	}
}

// Register 注册任务。必须在 Start 之前调用。
func (s *Scheduler) Register(job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.jobs[job.Name()]; dup {
		return fmt.Errorf("任务 %q 重复注册", job.Name())
	}
	if _, err := s.cron.AddFunc(job.Spec(), func() { s.trigger(job) }); err != nil {
		return fmt.Errorf("任务 %q 的 cron 表达式 %q 无效: %w", job.Name(), job.Spec(), err)
	}
	s.jobs[job.Name()] = job
	return nil
}

// Start 开始调度。ctx 会传给每次任务运行。
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
	s.cron.Start()
	for _, e := range s.cron.Entries() {
		s.log.Debug("定时任务下次运行", "next", e.Next)
	}
}

// Stop 停止调度并等待正在运行的任务结束（ctx 到期则放弃等待）。
func (s *Scheduler) Stop(ctx context.Context) error {
	stopCtx := s.cron.Stop()
	done := make(chan struct{})
	go func() {
		<-stopCtx.Done()
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("等待定时任务结束: %w", ctx.Err())
	}
}

// RunNow 立即同步运行一个任务（不影响定时调度）。
func (s *Scheduler) RunNow(ctx context.Context, name string) error {
	s.mu.Lock()
	job, ok := s.jobs[name]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("未知任务 %q", name)
	}
	return s.run(ctx, job)
}

func (s *Scheduler) trigger(job Job) {
	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()
	if err := s.run(ctx, job); err != nil && !errors.Is(err, ErrAlreadyRunning) {
		s.log.Error("定时任务失败", "job", job.Name(), "err", err)
	}
}

func (s *Scheduler) run(ctx context.Context, job Job) (err error) {
	name := job.Name()
	s.mu.Lock()
	if s.running[name] {
		s.mu.Unlock()
		s.log.Warn("上一次运行尚未结束，跳过本次", "job", name)
		return ErrAlreadyRunning
	}
	s.running[name] = true
	s.wg.Add(1)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.running, name)
		s.mu.Unlock()
		s.wg.Done()
	}()

	start := s.now().UTC()
	rec, recErr := s.runs.Start(ctx, name, start)
	if recErr != nil {
		s.log.Error("记录任务开始失败", "job", name, "err", recErr)
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("任务 panic: %v\n%s", p, debug.Stack())
		}
		status, msg := store.JobSucceeded, ""
		if err != nil {
			status, msg = store.JobFailed, err.Error()
		}
		if rec != nil {
			if ferr := s.runs.Finish(context.WithoutCancel(ctx), rec.ID, status, msg, s.now().UTC()); ferr != nil {
				s.log.Error("记录任务结束失败", "job", name, "err", ferr)
			}
		}
		s.log.Info("定时任务结束", "job", name, "status", status, "elapsed", s.now().Sub(start).Round(time.Millisecond))
	}()
	s.log.Info("定时任务开始", "job", name)
	return job.Run(ctx)
}
