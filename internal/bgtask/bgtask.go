// Package bgtask 是后台长任务：聊天里的模型遇到比较大的事（调研、批量处理文件、写报告）时提出任务，
// 对方确认后放进队列，在后台用更多轮工具慢慢做（聊天的模型像「路由」，后台的工作会话像「工人」），
// 做完把结果发回原来的对话和对方绑定的聊天软件。
//
// 这个包管任务的记录、排队、并发、预算、取消、中断后接着做；真正去做由 Executor（chat.Service）负责。
package bgtask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/HugehardJiang/buugoo/internal/chat"
	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/logx"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// 限制。
const (
	MaxTitleRunes = 40
	MaxGoalRunes  = 4000
	// maxPending 是每人最多有几个等确认的任务（防止模型乱提）。
	maxPending = 5
	// minBudget 是能设的最小预算。
	minBudget = 20_000
)

// ErrNotFound 表示没有这个任务。
var ErrNotFound = errors.New("没有这个后台任务")

// UserError 是操作不对。
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

// Access 报告用户能不能用后台任务，以及任务的沙箱能不能联网（按套餐，管理员总可以）。
type Access func(ctx context.Context, u *store.User) (allowed, network bool)

// Executor 真正去做一个任务（chat.Service）。
type Executor interface {
	ExecuteTask(ctx context.Context, u *store.User, t *store.BgTask, run chat.TaskRun) (*chat.ProactiveResult, string, error)
}

// Deliver 把做完的结果发到对方绑定的聊天软件（网页上的对话里已经有了）。
type Deliver func(ctx context.Context, u *store.User, res *chat.ProactiveResult)

// Service 管理后台任务。
type Service struct {
	db      *gorm.DB
	cfg     config.TasksConfig
	users   store.UserRepo
	access  Access
	exec    Executor
	deliver Deliver
	log     *slog.Logger
	now     func() time.Time

	mu      sync.Mutex
	running map[uint64]*runningTask
	wake    chan struct{}
}

type runningTask struct {
	userID   uint64
	cancel   context.CancelFunc
	canceled bool
}

// New 创建 Service。exec 和 deliver 可以之后用 SetExecutor、SetDeliver 设置。
func New(db *gorm.DB, cfg config.TasksConfig, users store.UserRepo, access Access, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{db: db, cfg: cfg, users: users, access: access, log: log.With("component", "bgtask"), now: time.Now,
		running: map[uint64]*runningTask{}, wake: make(chan struct{}, 1)}
}

// SetExecutor 设置真正去做任务的执行者。
func (s *Service) SetExecutor(e Executor) { s.exec = e }

// SetDeliver 设置做完后发到聊天软件的方法。
func (s *Service) SetDeliver(d Deliver) { s.deliver = d }

// Config 返回配置（网页上显示默认预算等）。
func (s *Service) Config() config.TasksConfig { return s.cfg }

// Allowed 报告用户能不能用后台任务。
func (s *Service) Allowed(ctx context.Context, u *store.User) bool {
	if !s.cfg.Enabled || s.access == nil {
		return false
	}
	ok, _ := s.access(ctx, u)
	return ok
}

func (s *Service) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) clampBudget(b int64) int64 {
	switch {
	case b <= 0:
		return s.cfg.DefaultBudget
	case b < minBudget:
		return minBudget
	case b > s.cfg.MaxBudget:
		return s.cfg.MaxBudget
	}
	return b
}

// Propose 提出一个任务。confirmed 为 false 时等对方在网页上确认（可以调预算）；
// 为 true 时（聊天软件里对方已经口头答应）直接排队。
func (s *Service) Propose(ctx context.Context, u *store.User, convID uint64, title, goal string, budget int64, confirmed bool) (*store.BgTask, error) {
	if !s.Allowed(ctx, u) {
		return nil, &UserError{"对方现在的套餐不包含后台任务"}
	}
	title = strings.Join(strings.Fields(title), " ")
	goal = strings.TrimSpace(goal)
	switch {
	case title == "" || goal == "":
		return nil, &UserError{"任务的标题和要做的事都要写"}
	case utf8.RuneCountInString(title) > MaxTitleRunes:
		title = string([]rune(title)[:MaxTitleRunes])
	}
	if utf8.RuneCountInString(goal) > MaxGoalRunes {
		return nil, &UserError{fmt.Sprintf("任务说明最多 %d 字", MaxGoalRunes)}
	}
	var pending int64
	if err := s.db.WithContext(ctx).Model(&store.BgTask{}).Where("user_id = ? AND status = ?", u.ID, store.TaskPending).Count(&pending).Error; err != nil {
		return nil, err
	}
	if pending >= maxPending {
		return nil, &UserError{"已经有好几个任务在等对方确认了，先让对方确认或取消"}
	}
	t := &store.BgTask{UserID: u.ID, ConversationID: convID, Title: title, Goal: goal, Status: store.TaskPending, BudgetTokens: s.clampBudget(budget)}
	if confirmed {
		_, network := s.access(ctx, u)
		t.Status, t.Network = store.TaskQueued, network
	}
	if err := s.db.WithContext(ctx).Create(t).Error; err != nil {
		return nil, err
	}
	if confirmed {
		s.poke()
	}
	return t, nil
}

func (s *Service) load(ctx context.Context, userID, id uint64) (*store.BgTask, error) {
	var t store.BgTask
	if err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

// setStatus 只在任务现在是 from 里的状态时改成 to（防止和后台同时改）。
func (s *Service) setStatus(ctx context.Context, id uint64, from []string, fields map[string]any) (bool, error) {
	res := s.db.WithContext(ctx).Model(&store.BgTask{}).Where("id = ? AND status IN ?", id, from).Updates(fields)
	return res.RowsAffected > 0, res.Error
}

// Confirm 对方确认开始（可以调预算），任务进入队列。
func (s *Service) Confirm(ctx context.Context, u *store.User, id uint64, budget int64) (*store.BgTask, error) {
	t, err := s.load(ctx, u.ID, id)
	if err != nil {
		return nil, err
	}
	if t.Status != store.TaskPending {
		return nil, &UserError{"这个任务已经确认过或者取消了"}
	}
	allowed, network := s.access(ctx, u)
	if !s.cfg.Enabled || !allowed {
		return nil, &UserError{"你现在的套餐不包含后台任务"}
	}
	if budget <= 0 {
		budget = t.BudgetTokens
	}
	ok, err := s.setStatus(ctx, id, []string{store.TaskPending}, map[string]any{"status": store.TaskQueued, "budget_tokens": s.clampBudget(budget), "network": network})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &UserError{"这个任务已经确认过或者取消了"}
	}
	s.poke()
	return s.load(ctx, u.ID, id)
}

// Cancel 取消任务：还没开始的直接取消，正在跑的停下来（已经花掉的照样计费）。
func (s *Service) Cancel(ctx context.Context, u *store.User, id uint64) error {
	t, err := s.load(ctx, u.ID, id)
	if err != nil {
		return err
	}
	switch t.Status {
	case store.TaskPending, store.TaskQueued, store.TaskInterrupted:
		_, err := s.setStatus(ctx, id, []string{t.Status}, map[string]any{"status": store.TaskCanceled, "finished_at": s.now()})
		return err
	case store.TaskRunning:
		s.mu.Lock()
		if r := s.running[id]; r != nil {
			r.canceled = true
			r.cancel()
		}
		s.mu.Unlock()
		return nil
	}
	return &UserError{"这个任务已经结束了"}
}

// Resume 让被服务器重启打断的任务接着做（从打断的地方继续）。
func (s *Service) Resume(ctx context.Context, u *store.User, id uint64) (*store.BgTask, error) {
	if !s.Allowed(ctx, u) {
		return nil, &UserError{"你现在的套餐不包含后台任务"}
	}
	ok, err := s.setStatus(ctx, id, []string{store.TaskInterrupted}, map[string]any{"status": store.TaskQueued})
	if err != nil {
		return nil, err
	}
	if !ok {
		if _, err := s.load(ctx, u.ID, id); err != nil {
			return nil, err
		}
		return nil, &UserError{"只有被打断的任务可以接着做"}
	}
	s.poke()
	return s.load(ctx, u.ID, id)
}

// View 是给网页看的任务（带步骤）。
type View struct {
	store.BgTask
	Steps []chat.ToolEvent `json:"steps"`
	// Position 是排队时前面还有几个。
	Position int `json:"position,omitempty"`
}

func (s *Service) view(t store.BgTask) View {
	v := View{BgTask: t}
	if t.Steps != "" {
		_ = json.Unmarshal([]byte(t.Steps), &v.Steps)
	}
	if v.Steps == nil {
		v.Steps = []chat.ToolEvent{}
	}
	return v
}

// List 列出用户最近的任务（最新的在前，不带步骤）。
func (s *Service) List(ctx context.Context, u *store.User, limit int) ([]store.BgTask, error) {
	var rows []store.BgTask
	err := s.db.WithContext(ctx).Omit("steps", "transcript").Where("user_id = ?", u.ID).Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// Get 返回一个任务（带步骤）。
func (s *Service) Get(ctx context.Context, u *store.User, id uint64) (*View, error) {
	t, err := s.load(ctx, u.ID, id)
	if err != nil {
		return nil, err
	}
	v := s.view(*t)
	if t.Status == store.TaskQueued {
		var n int64
		s.db.WithContext(ctx).Model(&store.BgTask{}).Where("status = ? AND id < ?", store.TaskQueued, t.ID).Count(&n)
		v.Position = int(n)
	}
	return &v, nil
}

// Start 在后台调度任务，直到 ctx 结束。启动时把上次没跑完的任务标成「被打断」。
func (s *Service) Start(ctx context.Context) {
	if err := s.db.WithContext(ctx).Model(&store.BgTask{}).Where("status = ?", store.TaskRunning).
		Updates(map[string]any{"status": store.TaskInterrupted, "error": "服务器重启，任务被打断了，可以接着做"}).Error; err != nil {
		s.log.Warn("标记被打断的任务失败", "err", err)
	}
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		s.dispatch(ctx)
		select {
		case <-ctx.Done():
			s.mu.Lock()
			for _, r := range s.running {
				r.cancel()
			}
			s.mu.Unlock()
			return
		case <-tick.C:
		case <-s.wake:
		}
	}
}

// dispatch 从队列里取任务来跑：总数和每人的并发都有上限。
func (s *Service) dispatch(ctx context.Context) {
	if s.exec == nil || !s.cfg.Enabled {
		return
	}
	s.mu.Lock()
	free := s.cfg.MaxRunning - len(s.running)
	perUser := map[uint64]int{}
	for _, r := range s.running {
		perUser[r.userID]++
	}
	s.mu.Unlock()
	if free <= 0 {
		return
	}
	var queued []store.BgTask
	if err := s.db.WithContext(ctx).Omit("steps", "transcript", "result").Where("status = ?", store.TaskQueued).Order("id").Limit(100).Find(&queued).Error; err != nil {
		s.log.Warn("读取任务队列失败", "err", err)
		return
	}
	for _, t := range queued {
		if free <= 0 {
			return
		}
		if perUser[t.UserID] >= s.cfg.PerUser {
			continue
		}
		ok, err := s.setStatus(ctx, t.ID, []string{store.TaskQueued}, map[string]any{"status": store.TaskRunning, "started_at": s.now(), "error": ""})
		if err != nil || !ok {
			continue
		}
		free--
		perUser[t.UserID]++
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.MaxDuration+5*time.Minute)
		r := &runningTask{userID: t.UserID, cancel: cancel}
		s.mu.Lock()
		s.running[t.ID] = r
		s.mu.Unlock()
		go s.run(runCtx, t.ID, r)
	}
}

// progress 把任务的进展写进数据库（步骤、用量、工作过程），网页上轮询看。
type progress struct {
	s      *Service
	id     uint64
	mu     sync.Mutex
	steps  []chat.ToolEvent
	last   time.Time
	budget int64
}

func (p *progress) Step(ev chat.ToolEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	replaced := false
	for i := range p.steps {
		if p.steps[i].ID == ev.ID {
			p.steps[i], replaced = ev, true
		}
	}
	if !replaced {
		p.steps = append(p.steps, ev)
	}
	// 步骤开始和结束都写，但最多每秒一次（结束时会再写一次完整的）。
	if time.Since(p.last) < time.Second && ev.Status != "running" {
		return
	}
	p.last = time.Now()
	p.saveSteps()
}

func (p *progress) saveSteps() {
	b, _ := json.Marshal(p.steps)
	_ = p.s.db.Model(&store.BgTask{}).Where("id = ?", p.id).Update("steps", string(b)).Error
}

func (p *progress) Round(used int64, rounds int, transcript []llm.Message) {
	b, _ := json.Marshal(transcript)
	_ = p.s.db.Model(&store.BgTask{}).Where("id = ?", p.id).Updates(map[string]any{
		"used_tokens": used, "rounds": rounds, "transcript": string(b)}).Error
	p.mu.Lock()
	p.saveSteps()
	p.mu.Unlock()
}

func (s *Service) run(ctx context.Context, id uint64, r *runningTask) {
	defer func() {
		r.cancel()
		s.mu.Lock()
		delete(s.running, id)
		s.mu.Unlock()
		s.poke()
	}()
	bg := context.WithoutCancel(ctx)
	fail := func(status, msg string) {
		_, _ = s.setStatus(bg, id, []string{store.TaskRunning}, map[string]any{"status": status, "error": msg, "finished_at": s.now()})
	}
	var t store.BgTask
	if err := s.db.WithContext(ctx).First(&t, id).Error; err != nil {
		fail(store.TaskFailed, "读取任务失败")
		return
	}
	u, err := s.users.GetByID(ctx, t.UserID)
	if err != nil || u.DisabledAt != nil {
		fail(store.TaskFailed, "账号不存在或已停用")
		return
	}
	if !s.Allowed(ctx, u) {
		fail(store.TaskFailed, "套餐不再包含后台任务")
		return
	}
	p := &progress{s: s, id: id, budget: t.BudgetTokens}
	if t.Steps != "" {
		_ = json.Unmarshal([]byte(t.Steps), &p.steps)
	}
	// 留几分钟收尾：到点前就不再调用工具，让模型交出结果，而不是硬生生被打断。
	start := s.now()
	soft := start.Add(s.cfg.MaxDuration - min(s.cfg.MaxDuration/5, 3*time.Minute))
	res, report, err := s.exec.ExecuteTask(ctx, u, &t, chat.TaskRun{
		MaxRounds: s.cfg.MaxRounds, Deadline: soft, Network: t.Network, Progress: p})
	p.mu.Lock()
	p.saveSteps()
	p.mu.Unlock()
	s.mu.Lock()
	canceled := r.canceled
	s.mu.Unlock()
	switch {
	case canceled:
		fail(store.TaskCanceled, "")
		return
	case err != nil:
		s.log.Warn("后台任务失败", "task_id", id, "user_id", u.ID, "err", err)
		msg := logx.Redact(err.Error())
		if r := []rune(msg); len(r) > 400 {
			msg = string(r[:400])
		}
		fail(store.TaskFailed, msg)
		return
	}
	fields := map[string]any{"status": store.TaskDone, "result": report, "finished_at": s.now(), "transcript": ""}
	if res != nil && res.Message != nil {
		fields["message_id"] = res.Message.ID
	}
	_, _ = s.setStatus(bg, id, []string{store.TaskRunning}, fields)
	s.log.Info("后台任务完成", "task_id", id, "user_id", u.ID)
	if s.deliver != nil && res != nil {
		s.deliver(bg, u, res)
	}
}
