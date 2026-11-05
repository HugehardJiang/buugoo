package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v21：Cosplay 角色卡，以及对话里选的角色和记忆方式。以下为 v21 的快照结构体，不得修改。

type v21Character struct {
	ID          uint64 `gorm:"primaryKey"`
	UserID      uint64 `gorm:"index;not null"`
	Public      bool   `gorm:"index;not null;default:false"`
	Name        string `gorm:"size:64;not null"`
	Description string `gorm:"size:600;not null;default:''"`
	Prompt      string `gorm:"type:mediumtext;not null"`
	Source      string `gorm:"size:512;not null;default:''"`
	CreatedAt   ts
	UpdatedAt   ts
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func (v21Character) TableName() string { return "characters" }

type v21Conversation struct {
	CharacterID     *uint64 `gorm:"index"`
	CharacterName   string  `gorm:"size:64;not null;default:''"`
	CharacterMemory string  `gorm:"size:8;not null;default:''"`
}

func (v21Conversation) TableName() string { return "conversations" }

var v21ConvCols = []string{"CharacterID", "CharacterName", "CharacterMemory"}

func migrationV21() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202611050021_characters",
		Migrate: func(tx *gorm.DB) error {
			if err := autoMigrate(tx, &v21Character{}); err != nil {
				return err
			}
			return addColumns(tx.Migrator(), &v21Conversation{}, v21ConvCols...)
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, col := range v21ConvCols {
				if m.HasColumn(&v21Conversation{}, col) {
					if err := m.DropColumn(&v21Conversation{}, col); err != nil {
						return err
					}
				}
			}
			return m.DropTable(&v21Character{})
		},
	}
}
