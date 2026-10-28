package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v16：待办清单。以下为 v16 的快照结构体，不得修改。

type v16Todo struct {
	ID        uint64 `gorm:"primaryKey"`
	UserID    uint64 `gorm:"index:idx_todo_user;not null"`
	Title     string `gorm:"size:255;not null"`
	Note      string `gorm:"type:text"`
	DueAt     *ts    `gorm:"index:idx_todo_due,priority:2"`
	AllDay    bool   `gorm:"not null;default:false"`
	Done      bool   `gorm:"index:idx_todo_due,priority:1;not null;default:false"`
	DoneAt    *ts
	NudgedAt  *ts
	CreatedAt ts
	UpdatedAt ts
}

func (v16Todo) TableName() string { return "todos" }

func migrationV16() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610280016_todos",
		Migrate: func(tx *gorm.DB) error {
			return autoMigrate(tx, &v16Todo{})
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropTable(&v16Todo{})
		},
	}
}
