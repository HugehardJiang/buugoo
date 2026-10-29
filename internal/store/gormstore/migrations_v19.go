package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v19：以前在网页上添加模型时「最大输出 0（用服务的默认值）」会被存成 1024（数据库默认值），
// 回复说一点就被截断。把这些改回 0（不限，由服务决定）。

func migrationV19() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610290019_unlimited_max_tokens",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec("UPDATE model_configs SET max_tokens = 0 WHERE max_tokens = 1024").Error
		},
		Rollback: func(*gorm.DB) error { return nil },
	}
}
