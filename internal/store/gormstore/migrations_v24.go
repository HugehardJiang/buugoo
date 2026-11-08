package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v24：后台长任务；套餐里的「包含后台任务」「后台任务的沙箱可以联网」。以下为 v24 的快照结构体，不得修改。

type v24BgTask struct {
	ID             uint64  `gorm:"primaryKey"`
	UserID         uint64  `gorm:"index:idx_bgtask_user,priority:1;not null"`
	ConversationID uint64  `gorm:"not null;default:0"`
	Title          string  `gorm:"size:120;not null;default:''"`
	Goal           string  `gorm:"type:text"`
	Status         string  `gorm:"size:16;index;not null"`
	BudgetTokens   int64   `gorm:"not null;default:0"`
	UsedTokens     int64   `gorm:"not null;default:0"`
	Rounds         int     `gorm:"not null;default:0"`
	ModelID        *uint64 ``
	Network        bool    `gorm:"not null;default:false"`
	Steps          string  `gorm:"type:mediumtext"`
	Transcript     string  `gorm:"type:mediumtext"`
	Result         string  `gorm:"type:mediumtext"`
	MessageID      *uint64 ``
	Error          string  `gorm:"size:500;not null;default:''"`
	CreatedAt      ts      `gorm:"index:idx_bgtask_user,priority:2"`
	StartedAt      tsPtr
	FinishedAt     tsPtr
	UpdatedAt      ts
}

func (v24BgTask) TableName() string { return "bg_tasks" }

type v24Plan struct {
	TaskAccess     bool `gorm:"not null;default:false"`
	SandboxNetwork bool `gorm:"not null;default:false"`
}

func (v24Plan) TableName() string { return "plans" }

type v24Subscription struct {
	TaskAccess     bool `gorm:"not null;default:false"`
	SandboxNetwork bool `gorm:"not null;default:false"`
}

func (v24Subscription) TableName() string { return "subscriptions" }

var v24Cols = []string{"TaskAccess", "SandboxNetwork"}

func migrationV24() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202611080024_bg_tasks",
		Migrate: func(tx *gorm.DB) error {
			if err := autoMigrate(tx, &v24BgTask{}); err != nil {
				return err
			}
			if err := addColumns(tx.Migrator(), &v24Plan{}, v24Cols...); err != nil {
				return err
			}
			return addColumns(tx.Migrator(), &v24Subscription{}, v24Cols...)
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, x := range []any{&v24Plan{}, &v24Subscription{}} {
				for _, col := range v24Cols {
					if m.HasColumn(x, col) {
						if err := m.DropColumn(x, col); err != nil {
							return err
						}
					}
				}
			}
			return m.DropTable(&v24BgTask{})
		},
	}
}
