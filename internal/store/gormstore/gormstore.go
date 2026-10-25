// Package gormstore 用 GORM 实现 store.Store，同时支持 MySQL 与 SQLite。
package gormstore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	mysqldrv "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store"
)

// DB 是 store.Store 的 GORM 实现。
type DB struct {
	db     *gorm.DB
	driver string
}

var _ store.Store = (*DB)(nil)

// Open 根据配置打开数据库连接。不会执行迁移，迁移请调用 Migrate。
func Open(cfg config.DatabaseConfig, log *slog.Logger) (*DB, error) {
	var dialector gorm.Dialector
	switch cfg.Driver {
	case config.DriverSQLite:
		dsn, err := sqliteDSN(cfg.DSN)
		if err != nil {
			return nil, err
		}
		dialector = sqlite.Open(dsn)
	case config.DriverMySQL:
		dsn, err := mysqlDSN(cfg.DSN)
		if err != nil {
			return nil, err
		}
		dialector = mysql.Open(dsn)
	default:
		return nil, fmt.Errorf("不支持的数据库驱动 %q", cfg.Driver)
	}

	gcfg := &gorm.Config{
		TranslateError: true,
		NowFunc:        func() time.Time { return time.Now().UTC() },
		Logger:         newGormLogger(log, cfg.LogSQL),
	}
	db, err := gorm.Open(dialector, gcfg)
	if err != nil {
		return nil, fmt.Errorf("连接数据库: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取连接池: %w", err)
	}
	if cfg.Driver == config.DriverSQLite {
		// SQLite 只允许一个写者；WAL + busy_timeout + 立即事务可以避免大部分锁冲突，
		// 但连接数仍需保守。
		sqlDB.SetMaxOpenConns(4)
	} else {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(time.Hour)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("连接数据库: %w", err)
	}
	return &DB{db: db, driver: cfg.Driver}, nil
}

// sqliteDSN 为 SQLite 文件路径补上必要的参数，并确保所在目录存在。
func sqliteDSN(dsn string) (string, error) {
	if dsn == "" {
		return "", errors.New("sqlite 数据库路径为空")
	}
	path, query, _ := strings.Cut(dsn, "?")
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return "", fmt.Errorf("创建数据库目录: %w", err)
		}
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		return "", fmt.Errorf("解析 sqlite dsn 参数: %w", err)
	}
	pragmas := strings.Join(q["_pragma"], ",")
	add := func(name, value string) {
		if !strings.Contains(pragmas, name) {
			q.Add("_pragma", name+"("+value+")")
		}
	}
	add("busy_timeout", "10000")
	add("foreign_keys", "1")
	if path != ":memory:" {
		add("journal_mode", "WAL")
		add("synchronous", "NORMAL")
	}
	if q.Get("_txlock") == "" {
		q.Set("_txlock", "immediate")
	}
	return path + "?" + q.Encode(), nil
}

// SQLitePath 从 DSN 中取出数据库文件路径（供备份使用）。
func SQLitePath(dsn string) string {
	path, _, _ := strings.Cut(dsn, "?")
	return strings.TrimPrefix(path, "file:")
}

// mysqlDSN 强制设置 parseTime 与 UTC 时区，保证两种数据库时间语义一致。
func mysqlDSN(dsn string) (string, error) {
	c, err := mysqldrv.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("解析 mysql dsn: %w", err)
	}
	c.ParseTime = true
	c.Loc = time.UTC
	if c.Collation == "" {
		c.Collation = "utf8mb4_unicode_ci"
	}
	if c.Params == nil {
		c.Params = map[string]string{}
	}
	if _, ok := c.Params["charset"]; !ok {
		c.Params["charset"] = "utf8mb4"
	}
	// 统一会话时区为 UTC。
	c.Params["time_zone"] = "'+00:00'"
	return c.FormatDSN(), nil
}

// Driver 实现 store.Store。
func (s *DB) Driver() string { return s.driver }

// Gorm 返回底层 *gorm.DB（仅供迁移与测试使用）。
func (s *DB) Gorm() *gorm.DB { return s.db }

// Ping 实现 store.Store。
func (s *DB) Ping(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Close 实现 store.Store。
func (s *DB) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Tx 实现 store.Store。
func (s *DB) Tx(ctx context.Context, fn func(tx store.Store) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&DB{db: tx, driver: s.driver})
	})
}

func (s *DB) Users() store.UserRepo                 { return userRepo{s.db} }
func (s *DB) Invites() store.InviteRepo             { return inviteRepo{s.db} }
func (s *DB) Conversations() store.ConversationRepo { return conversationRepo{s.db} }
func (s *DB) Messages() store.MessageRepo           { return messageRepo{s.db} }
func (s *DB) Models() store.ModelRepo               { return modelRepo{s.db} }
func (s *DB) Attachments() store.AttachmentRepo     { return attachmentRepo{s.db} }
func (s *DB) Memories() store.MemoryRepo            { return memoryRepo{s.db} }
func (s *DB) Usage() store.UsageRepo                { return usageRepo{s.db} }
func (s *DB) Jobs() store.JobRepo                   { return jobRepo{s.db} }
func (s *DB) Meta() store.MetaRepo                  { return metaRepo{s.db} }

// mapErr 把 GORM 错误映射为 store 包定义的错误。
func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return store.ErrNotFound
	case errors.Is(err, gorm.ErrDuplicatedKey):
		return fmt.Errorf("%w: %v", store.ErrConflict, err)
	default:
		return err
	}
}

// gormLogger 把 GORM 日志接入 slog。
type gormLogger struct {
	log     *slog.Logger
	logSQL  bool
	slowSQL time.Duration
}

func newGormLogger(log *slog.Logger, logSQL bool) logger.Interface {
	if log == nil {
		log = slog.Default()
	}
	return &gormLogger{log: log.With("component", "db"), logSQL: logSQL, slowSQL: time.Second}
}

func (l *gormLogger) LogMode(logger.LogLevel) logger.Interface { return l }
func (l *gormLogger) Info(ctx context.Context, msg string, args ...any) {
	l.log.InfoContext(ctx, fmt.Sprintf(msg, args...))
}
func (l *gormLogger) Warn(ctx context.Context, msg string, args ...any) {
	l.log.WarnContext(ctx, fmt.Sprintf(msg, args...))
}
func (l *gormLogger) Error(ctx context.Context, msg string, args ...any) {
	l.log.ErrorContext(ctx, fmt.Sprintf(msg, args...))
}
func (l *gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && !errors.Is(err, gorm.ErrDuplicatedKey):
		sql, rows := fc()
		l.log.ErrorContext(ctx, "sql error", "err", err, "elapsed", elapsed, "rows", rows, "sql", sql)
	case elapsed > l.slowSQL:
		sql, rows := fc()
		l.log.WarnContext(ctx, "slow sql", "elapsed", elapsed, "rows", rows, "sql", sql)
	case l.logSQL:
		sql, rows := fc()
		l.log.DebugContext(ctx, "sql", "elapsed", elapsed, "rows", rows, "sql", sql)
	}
}
