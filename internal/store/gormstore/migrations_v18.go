package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v18：临时对话（不出现在对话列表、不写记忆和情绪，结束后删除）。以下为 v18 的快照结构体，不得修改。

type v18Conversation struct {
	Temporary  bool `gorm:"not null;default:false"`
	UseContext bool `gorm:"not null;default:false"`
}

func (v18Conversation) TableName() string { return "conversations" }

func migrationV18() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610290018_temporary_conversations",
		Migrate: func(tx *gorm.DB) error {
			return addColumns(tx.Migrator(), &v18Conversation{}, "Temporary", "UseContext")
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, col := range []string{"Temporary", "UseContext"} {
				if m.HasColumn(&v18Conversation{}, col) {
					if err := m.DropColumn(&v18Conversation{}, col); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
}
