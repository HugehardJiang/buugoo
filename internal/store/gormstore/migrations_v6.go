package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v6：长对话摘要（较早的消息整理成摘要放进提示词）。以下为 v6 的快照结构体，不得修改。

type v6Conversation struct {
	Summary      string `gorm:"size:16384"`
	SummaryUntil uint64 `gorm:"not null;default:0"`
}

func (v6Conversation) TableName() string { return "conversations" }

func migrationV6Summary() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610250006_conversation_summary",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, col := range []string{"Summary", "SummaryUntil"} {
				if !m.HasColumn(&v6Conversation{}, col) {
					if err := m.AddColumn(&v6Conversation{}, col); err != nil {
						return err
					}
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, col := range []string{"Summary", "SummaryUntil"} {
				if m.HasColumn(&v6Conversation{}, col) {
					if err := m.DropColumn(&v6Conversation{}, col); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
}
