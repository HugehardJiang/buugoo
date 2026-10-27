package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v11：备忘录。以下为 v11 的快照结构体，不得修改。

type v11MemoPage struct {
	ID        uint64 `gorm:"primaryKey"`
	UserID    uint64 `gorm:"index;not null"`
	Title     string `gorm:"size:64;not null"`
	Content   string `gorm:"type:text"`
	CreatedAt ts
	UpdatedAt ts
}

func (v11MemoPage) TableName() string { return "memo_pages" }

func migrationV11() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610270011_memo_pages",
		Migrate: func(tx *gorm.DB) error {
			return autoMigrate(tx, &v11MemoPage{})
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropTable(&v11MemoPage{})
		},
	}
}
