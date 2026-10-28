package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v15：消息记下缓存命中的 token 数。以下为 v15 的快照结构体，不得修改。

type v15Message struct {
	CachedTokens *int
}

func (v15Message) TableName() string { return "messages" }

func migrationV15() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610280015_cached_tokens",
		Migrate: func(tx *gorm.DB) error {
			return addColumns(tx.Migrator(), &v15Message{}, "CachedTokens")
		},
		Rollback: func(tx *gorm.DB) error {
			if m := tx.Migrator(); m.HasColumn(&v15Message{}, "CachedTokens") {
				return m.DropColumn(&v15Message{}, "CachedTokens")
			}
			return nil
		},
	}
}
