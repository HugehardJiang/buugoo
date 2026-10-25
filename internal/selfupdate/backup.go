package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	mysqldrv "github.com/go-sql-driver/mysql"

	"github.com/HugehardJiang/buugoo/internal/config"
	"github.com/HugehardJiang/buugoo/internal/store/gormstore"
)

// 备份类型。
const (
	BackupSQLite = "sqlite"
	BackupMySQL  = "mysql"
	BackupNone   = "none" // MySQL 且没有 mysqldump，用户确认后跳过
)

// Backup 是一次数据库备份。
type Backup struct {
	Kind string
	Path string
	// DBPath 是 SQLite 数据库文件路径（恢复时使用）。
	DBPath string
}

// ErrBackupDeclined 表示用户拒绝在没有备份的情况下继续。
var ErrBackupDeclined = errors.New("没有完成数据库备份，已取消更新")

// BackupDatabase 在 <data_dir>/backups 下备份数据库。
// SQLite 使用 VACUUM INTO 生成一致性快照；MySQL 调用 mysqldump，找不到时询问是否继续。
func BackupDatabase(ctx context.Context, cfg *config.Config, label string, confirm func(string) bool) (*Backup, error) {
	dir := filepath.Join(cfg.DataDir, "backups")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("创建备份目录: %w", err)
	}
	stamp := time.Now().Format("20060102-150405")
	switch cfg.Database.Driver {
	case config.DriverSQLite:
		dbPath := gormstore.SQLitePath(cfg.Database.DSN)
		if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
			return &Backup{Kind: BackupNone}, nil // 还没有数据库，无需备份
		}
		out := filepath.Join(dir, fmt.Sprintf("buugoo-%s-%s.db", stamp, label))
		db, err := gormstore.Open(cfg.Database, nil)
		if err != nil {
			return nil, fmt.Errorf("打开数据库: %w", err)
		}
		defer db.Close()
		if err := db.Gorm().WithContext(ctx).Exec("VACUUM INTO ?", out).Error; err != nil {
			return nil, fmt.Errorf("备份 SQLite 数据库: %w", err)
		}
		return &Backup{Kind: BackupSQLite, Path: out, DBPath: dbPath}, nil
	case config.DriverMySQL:
		out := filepath.Join(dir, fmt.Sprintf("buugoo-%s-%s.sql", stamp, label))
		if _, err := exec.LookPath("mysqldump"); err != nil {
			msg := "未找到 mysqldump，无法自动备份 MySQL 数据库。请先手动备份（例如在宝塔面板中备份数据库），确认已备份后继续"
			if confirm == nil || !confirm(msg) {
				return nil, ErrBackupDeclined
			}
			return &Backup{Kind: BackupNone}, nil
		}
		if err := mysqldump(ctx, cfg.Database.DSN, out); err != nil {
			return nil, err
		}
		return &Backup{Kind: BackupMySQL, Path: out}, nil
	default:
		return nil, fmt.Errorf("不支持的数据库驱动 %q", cfg.Database.Driver)
	}
}

func mysqldump(ctx context.Context, dsn, out string) error {
	c, err := mysqldrv.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("解析 mysql dsn: %w", err)
	}
	args := []string{"--single-transaction", "--routines", "--no-tablespaces", "--default-character-set=utf8mb4", "-u", c.User}
	switch c.Net {
	case "unix":
		args = append(args, "--socket", c.Addr)
	default:
		host, port, err := net.SplitHostPort(c.Addr)
		if err != nil {
			host, port = c.Addr, "3306"
		}
		args = append(args, "-h", host, "-P", port)
	}
	args = append(args, "--result-file", out, c.DBName)
	cmd := exec.CommandContext(ctx, "mysqldump", args...)
	// 密码通过环境变量传入，不出现在进程参数中。
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+c.Passwd)
	if b, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(out)
		return fmt.Errorf("mysqldump 失败: %w: %s", err, strings.TrimSpace(string(b)))
	}
	return os.Chmod(out, 0o600)
}

// RestoreSQLite 用备份覆盖 SQLite 数据库（服务必须已停止）。
func RestoreSQLite(b *Backup) error {
	if b == nil || b.Kind != BackupSQLite {
		return errors.New("没有可恢复的 SQLite 备份")
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(b.DBPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	tmp := b.DBPath + ".restore"
	if err := copyFile(b.Path, tmp); err != nil {
		return fmt.Errorf("恢复数据库: %w", err)
	}
	if err := os.Rename(tmp, b.DBPath); err != nil {
		return fmt.Errorf("恢复数据库: %w", err)
	}
	return nil
}
