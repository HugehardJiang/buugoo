package gormstore

import (
	"context"
	"fmt"
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"

	"github.com/HugehardJiang/buugoo/internal/config"
)

// 迁移规则：
//   - 每个迁移使用自己的快照结构体（见 migrations_v1.go），不得引用 store 包中会演进的模型；
//   - 已发布的迁移不得修改，只能追加新的迁移；
//   - 迁移必须能同时在 MySQL 与 SQLite 上运行，优先使用 GORM Migrator 而非手写 SQL。

const migrationsTable = "schema_migrations"

func migrations() []*gormigrate.Migration {
	return []*gormigrate.Migration{
		migrationV1Init(),
		migrationV2Models(),
		migrationV3Attachments(),
		migrationV4Emotions(),
		migrationV5PresetsFiles(),
		migrationV6(),
		migrationV7(),
		migrationV8(),
		migrationV9(),
		migrationV10(),
		migrationV11(),
		migrationV12(),
	}
}

func (s *DB) migrator(ctx context.Context) *gormigrate.Gormigrate {
	db := s.db.WithContext(ctx)
	if s.driver == config.DriverMySQL {
		// Session 让后续链式调用各自克隆 Statement，避免 gormigrate 的 Table() 污染迁移用的 db。
		db = db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci").Session(&gorm.Session{})
	}
	opts := *gormigrate.DefaultOptions
	opts.TableName = migrationsTable
	// MySQL 的 DDL 会隐式提交，包在事务里也无法回滚；为了两种数据库行为一致，
	// 统一不使用事务，由每个迁移自身保证可重复执行（AutoMigrate 本身是幂等的）。
	// 更新前的自动备份负责兜底。
	opts.UseTransaction = false
	return gormigrate.New(db, &opts, migrations())
}

// Migrate 执行所有未执行的迁移。
func (s *DB) Migrate(ctx context.Context) error {
	if err := s.migrator(ctx).Migrate(); err != nil {
		return fmt.Errorf("数据库迁移: %w", err)
	}
	return nil
}

// MigrationStatus 是一个迁移的执行状态。
type MigrationStatus struct {
	ID      string
	Applied bool
}

// Status 返回所有迁移的执行状态。
func (s *DB) Status(ctx context.Context) ([]MigrationStatus, error) {
	applied := map[string]bool{}
	if s.db.Migrator().HasTable(migrationsTable) {
		var ids []string
		if err := s.db.WithContext(ctx).Table(migrationsTable).Pluck("id", &ids).Error; err != nil {
			return nil, fmt.Errorf("读取迁移记录: %w", err)
		}
		for _, id := range ids {
			applied[id] = true
		}
	}
	var out []MigrationStatus
	for _, m := range migrations() {
		out = append(out, MigrationStatus{ID: m.ID, Applied: applied[m.ID]})
	}
	return out, nil
}

// Pending 报告是否存在未执行的迁移。
func (s *DB) Pending(ctx context.Context) (bool, error) {
	st, err := s.Status(ctx)
	if err != nil {
		return false, err
	}
	for _, m := range st {
		if !m.Applied {
			return true, nil
		}
	}
	return false, nil
}

// autoMigrate 是迁移中使用的辅助函数。
func autoMigrate(tx *gorm.DB, models ...any) error {
	for _, m := range models {
		if err := tx.AutoMigrate(m); err != nil {
			return fmt.Errorf("创建表 %T: %w", m, err)
		}
	}
	return nil
}

// 时间字段类型别名，便于快照结构体书写。
type (
	ts    = time.Time
	tsPtr = *time.Time
)
