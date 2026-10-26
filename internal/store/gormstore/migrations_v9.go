package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v9：旧版本中聊天软件绑定的对话没有标记来源（conversations.channel），按绑定补上。
func migrationV9() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610260009_backfill_channel",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE conversations SET channel = (
				SELECT b.channel FROM channel_bindings b WHERE b.conversation_id = conversations.id ORDER BY b.id LIMIT 1
			) WHERE channel = '' AND id IN (SELECT conversation_id FROM channel_bindings WHERE conversation_id IS NOT NULL)`).Error
		},
		Rollback: func(*gorm.DB) error { return nil },
	}
}
