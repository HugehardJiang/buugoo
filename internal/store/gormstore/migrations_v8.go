package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v8：模型的上下文长度；对话记录上下文用了多少、是否需要在下一条消息前压缩。
// 以下为 v8 的快照结构体，不得修改。

type v8ModelConfig struct {
	ContextTokens int `gorm:"not null;default:0"`
}

func (v8ModelConfig) TableName() string { return "model_configs" }

type v8Conversation struct {
	ContextUsed    int  `gorm:"not null;default:0"`
	CompactPending bool `gorm:"not null;default:false"`
}

func (v8Conversation) TableName() string { return "conversations" }

func migrationV8() *gormigrate.Migration {
	cols := []struct {
		model any
		names []string
	}{
		{&v8ModelConfig{}, []string{"ContextTokens"}},
		{&v8Conversation{}, []string{"ContextUsed", "CompactPending"}},
	}
	return &gormigrate.Migration{
		ID: "202610250008_context",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, c := range cols {
				for _, col := range c.names {
					if !m.HasColumn(c.model, col) {
						if err := m.AddColumn(c.model, col); err != nil {
							return err
						}
					}
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, c := range cols {
				for _, col := range c.names {
					if m.HasColumn(c.model, col) {
						if err := m.DropColumn(c.model, col); err != nil {
							return err
						}
					}
				}
			}
			return nil
		},
	}
}
