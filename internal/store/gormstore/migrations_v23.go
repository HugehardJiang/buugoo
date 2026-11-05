package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v23：Cosplay 角色卡的修改记录（自我进化改了提示词以后可以恢复）。以下为 v23 的快照结构体，不得修改。

type v23CharacterRevision struct {
	ID          uint64 `gorm:"primaryKey"`
	CharacterID uint64 `gorm:"index;not null"`
	UserID      uint64 `gorm:"index;not null"`
	Prompt      string `gorm:"type:mediumtext;not null"`
	Note        string `gorm:"size:200;not null;default:''"`
	CreatedAt   ts
}

func (v23CharacterRevision) TableName() string { return "character_revisions" }

func migrationV23() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202611050023_character_revisions",
		Migrate: func(tx *gorm.DB) error {
			return autoMigrate(tx, &v23CharacterRevision{})
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropTable(&v23CharacterRevision{})
		},
	}
}
