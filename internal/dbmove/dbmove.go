// Package dbmove 把 SQLite 里的全部数据一键搬到 MySQL：管理后台填好 MySQL 的地址和账号，先测试连接，
// 再开始迁移。迁移期间网站暂停写入（维护中），在同一个只读快照里逐表复制，核对每张表的行数，
// 全部一致后把配置切换到 MySQL 并重启服务。任何一步失败都会清掉 MySQL 里已经复制的数据、恢复写入，原来的 SQLite 不受影响。
package dbmove

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mysqldrv "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/settings"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

// ErrMaintenance 表示正在迁移数据库，暂停写入。
var ErrMaintenance = errors.New("正在迁移数据库，暂时不能保存，请几分钟后再试")

// UserError 是填写的内容或者环境不满足迁移条件。
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

// Target 是要迁移到的 MySQL。
type Target struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
}

func (t *Target) normalize() error {
	t.Host, t.User, t.Database = strings.TrimSpace(t.Host), strings.TrimSpace(t.User), strings.TrimSpace(t.Database)
	if t.Host == "" {
		t.Host = "127.0.0.1"
	}
	if t.Port == 0 {
		t.Port = 3306
	}
	if t.Database == "" {
		t.Database = "buugoo"
	}
	switch {
	case t.User == "":
		return &UserError{"请填写 MySQL 用户名"}
	case t.Port < 1 || t.Port > 65535:
		return &UserError{"端口不对"}
	case strings.ContainsAny(t.Host, "/ ") || strings.ContainsAny(t.Database, "/`'\" "):
		return &UserError{"地址或数据库名里有不支持的字符"}
	}
	return nil
}

// DSN 返回连接串（含密码，只写进设置文件，不要打日志）。
func (t Target) DSN() string {
	c := mysqldrv.NewConfig()
	c.User, c.Passwd, c.Net = t.User, t.Password, "tcp"
	c.Addr = net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	c.DBName = t.Database
	c.ParseTime = true
	c.Params = map[string]string{"charset": "utf8mb4"}
	return c.FormatDSN()
}

// 迁移的阶段。
const (
	StageIdle    = "idle"
	StageRunning = "running"
	StageDone    = "done"
	StageFailed  = "failed"
)

// TableProgress 是一张表的复制进度。
type TableProgress struct {
	Name   string `json:"name"`
	Rows   int64  `json:"rows"`
	Copied int64  `json:"copied"`
}

// Status 是迁移的状态，网页轮询显示进度。
type Status struct {
	Stage string `json:"stage"`
	// Step 是正在做的事（中文一句话）。
	Step      string          `json:"step"`
	Tables    []TableProgress `json:"tables"`
	Rows      int64           `json:"rows"`
	Copied    int64           `json:"copied"`
	Error     string          `json:"error,omitempty"`
	StartedAt *time.Time      `json:"started_at,omitempty"`
	EndedAt   *time.Time      `json:"ended_at,omitempty"`
	// Target 是迁移到的地址（不含密码）。
	Target string `json:"target,omitempty"`
}

// Mover 执行迁移。同一时间只能有一次迁移。
type Mover struct {
	src *gormstore.DB
	cfg *config.Config
	log *slog.Logger

	paused atomic.Bool

	mu     sync.Mutex
	status Status
}

// New 创建 Mover，并在源数据库上挂上「维护中不许写」的检查。
func New(src *gormstore.DB, cfg *config.Config, log *slog.Logger) *Mover {
	if log == nil {
		log = slog.Default()
	}
	m := &Mover{src: src, cfg: cfg, log: log.With("component", "dbmove"), status: Status{Stage: StageIdle}}
	m.guard(src.Gorm())
	return m
}

// guard 在迁移期间拒绝所有写入：迁移用的是只读快照，之后写进 SQLite 的数据不会被搬过去。
func (m *Mover) guard(db *gorm.DB) {
	check := func(tx *gorm.DB) {
		if m.paused.Load() {
			_ = tx.AddError(ErrMaintenance)
		}
	}
	cb := db.Callback()
	_ = cb.Create().Before("gorm:create").Register("dbmove:guard", check)
	_ = cb.Update().Before("gorm:update").Register("dbmove:guard", check)
	_ = cb.Delete().Before("gorm:delete").Register("dbmove:guard", check)
	_ = cb.Raw().Before("gorm:raw").Register("dbmove:guard", func(tx *gorm.DB) {
		// Exec 走 Raw；SELECT 不拦。
		if m.paused.Load() && !isSelect(tx.Statement.SQL.String()) {
			_ = tx.AddError(ErrMaintenance)
		}
	})
}

func isSelect(q string) bool {
	q = strings.ToUpper(strings.TrimSpace(q))
	return strings.HasPrefix(q, "SELECT") || strings.HasPrefix(q, "WITH") || strings.HasPrefix(q, "PRAGMA") || strings.HasPrefix(q, "EXPLAIN")
}

// Paused 报告现在是否暂停写入（迁移中或迁移完等重启）。
func (m *Mover) Paused() bool { return m.paused.Load() }

// Status 返回当前状态。
func (m *Mover) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status
	s.Tables = append([]TableProgress(nil), m.status.Tables...)
	return s
}

func (m *Mover) update(f func(s *Status)) {
	m.mu.Lock()
	f(&m.status)
	m.mu.Unlock()
}

// Check 报告能不能迁移（不能时返回原因）。
func (m *Mover) Check() error {
	switch {
	case m.src.Driver() != config.DriverSQLite:
		return &UserError{"现在用的已经是 MySQL 了"}
	case m.cfg.FromEnv("database.driver") || m.cfg.FromEnv("database.dsn"):
		return &UserError{"数据库是用环境变量（BUUGOO_DATABASE__…）设置的，网页上切换不了：请自己导出数据后修改环境变量"}
	}
	return nil
}

// TestResult 是测试连接的结果。
type TestResult struct {
	Version string `json:"version"`
	// Existing 是目标库里已经有的布谷数据表数量（都是空的才能迁移）。
	Existing int `json:"existing"`
	// SourceRows 是 SQLite 里要搬的总行数，SourceBytes 是数据库文件大小。
	SourceRows int64 `json:"source_rows"`
	Tables     int   `json:"tables"`
}

// Test 测试能不能连上 MySQL、目标库是不是空的。
func (m *Mover) Test(ctx context.Context, t Target) (*TestResult, error) {
	if err := m.Check(); err != nil {
		return nil, err
	}
	if err := t.normalize(); err != nil {
		return nil, err
	}
	db, err := openTarget(ctx, t)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	res := &TestResult{}
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&res.Version); err != nil {
		return nil, fmt.Errorf("读取 MySQL 版本: %w", err)
	}
	if err := checkVersion(res.Version); err != nil {
		return nil, err
	}
	tables, err := m.sourceTables(ctx)
	if err != nil {
		return nil, err
	}
	res.Tables = len(tables)
	for _, name := range tables {
		var n int64
		if err := m.src.Gorm().WithContext(ctx).Raw("SELECT COUNT(*) FROM " + quoteSQLite(name)).Scan(&n).Error; err != nil {
			return nil, err
		}
		res.SourceRows += n
		var exists int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?", name).Scan(&exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			continue
		}
		res.Existing++
		var rows int64
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteMySQL(name)).Scan(&rows); err != nil {
			return nil, err
		}
		if rows > 0 {
			return nil, &UserError{fmt.Sprintf("数据库「%s」里已经有布谷的数据（%s 表有 %d 行）。请换一个空的数据库，或者先清空它", t.Database, name, rows)}
		}
	}
	return res, nil
}

// openTarget 连接 MySQL；数据库不存在时尝试创建。
func openTarget(ctx context.Context, t Target) (*sql.DB, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := sql.Open("mysql", t.DSN())
	if err != nil {
		return nil, err
	}
	err = db.PingContext(cctx)
	var me *mysqldrv.MySQLError
	if errors.As(err, &me) && me.Number == 1049 { // 数据库不存在：试着建一个
		_ = db.Close()
		admin := t
		admin.Database = ""
		adb, err2 := sql.Open("mysql", admin.DSN())
		if err2 != nil {
			return nil, err2
		}
		_, err2 = adb.ExecContext(cctx, "CREATE DATABASE IF NOT EXISTS "+quoteMySQL(t.Database)+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
		_ = adb.Close()
		if err2 != nil {
			return nil, &UserError{fmt.Sprintf("数据库「%s」不存在，自动创建也失败了（%v）。请在 MySQL（或宝塔「数据库」）里先建好它", t.Database, friendly(err2))}
		}
		if db, err = sql.Open("mysql", t.DSN()); err != nil {
			return nil, err
		}
		err = db.PingContext(cctx)
	}
	if err != nil {
		_ = db.Close()
		return nil, &UserError{"连不上 MySQL：" + friendly(err)}
	}
	return db, nil
}

// friendly 把常见的 MySQL 错误翻成大白话。
func friendly(err error) string {
	var me *mysqldrv.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case 1045:
			return "用户名或密码不对"
		case 1044, 1142:
			return "这个账号没有权限访问这个数据库"
		}
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "connection refused"):
		return "连接被拒绝，检查地址和端口，以及 MySQL 有没有在运行"
	case strings.Contains(s, "i/o timeout"), errors.Is(err, context.DeadlineExceeded):
		return "连接超时，检查地址、端口和防火墙"
	case strings.Contains(s, "no such host"):
		return "找不到这个地址"
	}
	return s
}

func checkVersion(v string) error {
	if strings.Contains(strings.ToLower(v), "mariadb") {
		return nil
	}
	major, _ := strconv.Atoi(strings.SplitN(v, ".", 2)[0])
	minor := 0
	if parts := strings.SplitN(v, ".", 3); len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if major < 5 || (major == 5 && minor < 7) {
		return &UserError{"MySQL 版本太旧（" + v + "），至少需要 5.7，推荐 8.0 以上"}
	}
	return nil
}

func quoteSQLite(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }
func quoteMySQL(name string) string  { return "`" + strings.ReplaceAll(name, "`", "``") + "`" }

// sourceTables 返回 SQLite 里要搬的表（迁移记录表由目标库自己生成）。
func (m *Mover) sourceTables(ctx context.Context) ([]string, error) {
	var names []string
	err := m.src.Gorm().WithContext(ctx).Raw(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`).Scan(&names).Error
	out := names[:0]
	for _, n := range names {
		if n != gormstore.MigrationsTable {
			out = append(out, n)
		}
	}
	return out, err
}

// Start 在后台开始迁移，马上返回。onDone 在全部成功、配置已经切换后调用（重启服务）。
func (m *Mover) Start(t Target, onDone func()) error {
	if err := m.Check(); err != nil {
		return err
	}
	if err := t.normalize(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.status.Stage == StageRunning || m.status.Stage == StageDone {
		m.mu.Unlock()
		return &UserError{"迁移已经在进行或已经完成"}
	}
	now := time.Now()
	m.status = Status{Stage: StageRunning, Step: "准备中", StartedAt: &now, Target: net.JoinHostPort(t.Host, strconv.Itoa(t.Port)) + "/" + t.Database}
	m.mu.Unlock()
	m.paused.Store(true)
	m.log.Warn("开始把数据迁移到 MySQL，暂停写入", "target", m.status.Target)
	go func() {
		err := m.run(context.Background(), t)
		end := time.Now()
		if err != nil {
			m.paused.Store(false)
			m.log.Error("迁移到 MySQL 失败，已恢复写入", "err", err)
			msg := err.Error()
			var ue *UserError
			if !errors.As(err, &ue) {
				msg = friendlyRun(err)
			}
			m.update(func(s *Status) { s.Stage, s.Error, s.EndedAt = StageFailed, msg, &end })
			return
		}
		m.update(func(s *Status) { s.Stage, s.Step, s.EndedAt = StageDone, "完成，正在重启", &end })
		m.log.Warn("已迁移到 MySQL，配置已切换，重启后生效")
		if onDone != nil {
			onDone()
		}
	}()
	return nil
}

func friendlyRun(err error) string {
	var me *mysqldrv.MySQLError
	if errors.As(err, &me) {
		return friendly(err) + "（" + me.Error() + "）"
	}
	return err.Error()
}

// 每批最多多少行、多少字节（远小于 MySQL 默认 max_allowed_packet）。
const (
	batchRows  = 200
	batchBytes = 2 << 20
)

func (m *Mover) run(ctx context.Context, t Target) (err error) {
	// 1. 等正在进行的写入结束：暂停后新的写入都会被拒绝，给已经开始的事务一点时间提交。
	m.update(func(s *Status) { s.Step = "暂停写入，等正在保存的数据存完" })
	time.Sleep(2 * time.Second)

	// 2. 在 MySQL 里建表（和平时启动一样跑一遍数据库迁移）。
	m.update(func(s *Status) { s.Step = "在 MySQL 里建表" })
	pre, err := openTarget(ctx, t)
	if err != nil {
		return err
	}
	_ = pre.Close()
	dst, err := gormstore.Open(config.DatabaseConfig{Driver: config.DriverMySQL, DSN: t.DSN(), MaxOpenConns: 2, MaxIdleConns: 2}, m.log)
	if err != nil {
		return &UserError{"连不上 MySQL：" + friendly(err)}
	}
	defer dst.Close()
	if err := dst.Migrate(ctx); err != nil {
		return fmt.Errorf("在 MySQL 里建表: %w", err)
	}
	tables, err := m.sourceTables(ctx)
	if err != nil {
		return err
	}

	// 3. 在同一个只读快照里逐表复制。
	srcSQL, err := m.src.Gorm().DB()
	if err != nil {
		return err
	}
	snap, err := srcSQL.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer snap.Rollback()
	progress := make([]TableProgress, len(tables))
	var total int64
	for i, name := range tables {
		progress[i].Name = name
		if err := snap.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteSQLite(name)).Scan(&progress[i].Rows); err != nil {
			return err
		}
		total += progress[i].Rows
	}
	m.update(func(s *Status) { s.Tables, s.Rows = progress, total })

	dstSQL, err := dst.Gorm().DB()
	if err != nil {
		return err
	}
	conn, err := dstSQL.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		return err
	}
	// 失败时清掉已经复制的数据，下次可以重新来。
	defer func() {
		if err != nil {
			for _, name := range tables {
				_, _ = conn.ExecContext(context.WithoutCancel(ctx), "DELETE FROM "+quoteMySQL(name))
			}
		}
	}()
	for _, name := range tables {
		var n int64
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteMySQL(name)).Scan(&n); err != nil {
			return &UserError{fmt.Sprintf("MySQL 里没有 %s 表：%v", name, err)}
		}
		if n > 0 {
			return &UserError{fmt.Sprintf("MySQL 的 %s 表里已经有数据，请换一个空的数据库", name)}
		}
	}
	for i, name := range tables {
		m.update(func(s *Status) { s.Step = "复制 " + name })
		if err := m.copyTable(ctx, snap, conn, name, func(n int64) {
			m.update(func(s *Status) { s.Tables[i].Copied += n; s.Copied += n })
		}); err != nil {
			return fmt.Errorf("复制 %s: %w", name, err)
		}
	}

	// 4. 核对每张表的行数。
	m.update(func(s *Status) { s.Step = "核对数据" })
	for _, p := range progress {
		var n int64
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteMySQL(p.Name)).Scan(&n); err != nil {
			return err
		}
		if n != p.Rows {
			return &UserError{fmt.Sprintf("核对失败：%s 表 SQLite 有 %d 行，MySQL 有 %d 行", p.Name, p.Rows, n)}
		}
	}
	if _, err := conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1"); err != nil {
		return err
	}

	// 5. 切换配置（写进数据目录里的设置文件，权限 600），重启后生效。原来的 SQLite 文件保留。
	m.update(func(s *Status) { s.Step = "切换配置" })
	if _, err := settings.Save(m.cfg, map[string]any{"database.driver": config.DriverMySQL, "database.dsn": t.DSN()}); err != nil {
		return fmt.Errorf("保存配置: %w", err)
	}
	return nil
}

// copyTable 把一张表按主键顺序分批复制过去。
func (m *Mover) copyTable(ctx context.Context, src *sql.Tx, dst *sql.Conn, name string, onBatch func(int64)) error {
	// 只复制两边都有的列（两边由同一套迁移建表，正常情况下完全一样）。
	dstCols, err := columns(ctx, dst, "SELECT * FROM "+quoteMySQL(name)+" LIMIT 0")
	if err != nil {
		return err
	}
	rows, err := src.QueryContext(ctx, "SELECT * FROM "+quoteSQLite(name)+" ORDER BY rowid")
	if err != nil {
		return err
	}
	defer rows.Close()
	srcCols, err := rows.Columns()
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, c := range dstCols {
		want[strings.ToLower(c)] = true
	}
	var keep []int
	var cols []string
	for i, c := range srcCols {
		if want[strings.ToLower(c)] {
			keep = append(keep, i)
			cols = append(cols, quoteMySQL(c))
		}
	}
	if len(cols) == 0 {
		return nil
	}
	head := "INSERT INTO " + quoteMySQL(name) + " (" + strings.Join(cols, ", ") + ") VALUES "
	tuple := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + ")"

	var (
		args  []any
		count int
		size  int
	)
	flush := func() error {
		if count == 0 {
			return nil
		}
		q := head + strings.TrimSuffix(strings.Repeat(tuple+", ", count), ", ")
		if _, err := dst.ExecContext(ctx, q, args...); err != nil {
			return err
		}
		onBatch(int64(count))
		args, count, size = args[:0], 0, 0
		return nil
	}
	vals := make([]any, len(srcCols))
	ptrs := make([]any, len(srcCols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		for _, i := range keep {
			v := convert(vals[i])
			size += valueSize(v)
			args = append(args, v)
		}
		count++
		if count >= batchRows || size >= batchBytes {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return flush()
}

func columns(ctx context.Context, c *sql.Conn, q string) ([]string, error) {
	rows, err := c.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return rows.Columns()
}

// convert 把 SQLite 读出来的值转成 MySQL 能存的：时间统一按 UTC 存，[]byte 复制一份（Scan 的缓冲区会被复用）。
func convert(v any) any {
	switch x := v.(type) {
	case time.Time:
		return x.UTC()
	case []byte:
		return append([]byte(nil), x...)
	case string:
		// 少数时间列在 SQLite 里是按字符串存的（带时区后缀），MySQL 的 DATETIME 不认，转成 UTC 时间。
		if t, ok := parseTime(x); ok {
			return t
		}
		return x
	}
	return v
}

var timeLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999Z07:00",
}

func parseTime(s string) (time.Time, bool) {
	if len(s) < 20 || len(s) > 40 || s[4] != '-' || s[7] != '-' || (s[10] != ' ' && s[10] != 'T') {
		return time.Time{}, false
	}
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func valueSize(v any) int {
	switch x := v.(type) {
	case string:
		return len(x)
	case []byte:
		return len(x)
	}
	return 8
}
