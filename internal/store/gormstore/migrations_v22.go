package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v22：强制弹窗通知、API 接入（API Key、套餐里的「可以用 API」）。以下为 v22 的快照结构体，不得修改。

type v22Notice struct {
	ID        uint64 `gorm:"primaryKey"`
	Title     string `gorm:"size:120;not null"`
	Content   string `gorm:"type:text"`
	Seconds   int    `gorm:"not null;default:10"`
	CreatedAt ts
	UpdatedAt ts
}

func (v22Notice) TableName() string { return "notices" }

type v22NoticeRead struct {
	UserID   uint64 `gorm:"primaryKey;autoIncrement:false"`
	NoticeID uint64 `gorm:"primaryKey;autoIncrement:false;index"`
	ShownAt  ts
	ReadAt   tsPtr
}

func (v22NoticeRead) TableName() string { return "notice_reads" }

type v22APIKey struct {
	ID         uint64 `gorm:"primaryKey"`
	UserID     uint64 `gorm:"index;not null"`
	Name       string `gorm:"size:64;not null;default:''"`
	Hash       string `gorm:"size:64;uniqueIndex;not null"`
	Hint       string `gorm:"size:32;not null;default:''"`
	LastUsedAt tsPtr
	CreatedAt  ts
}

func (v22APIKey) TableName() string { return "api_keys" }

type v22Plan struct {
	APIAccess bool `gorm:"not null;default:false"`
}

func (v22Plan) TableName() string { return "plans" }

type v22Subscription struct {
	APIAccess bool `gorm:"not null;default:false"`
}

func (v22Subscription) TableName() string { return "subscriptions" }

func migrationV22() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202611050022_notices_api",
		Migrate: func(tx *gorm.DB) error {
			if err := autoMigrate(tx, &v22Notice{}, &v22NoticeRead{}, &v22APIKey{}); err != nil {
				return err
			}
			if err := addColumns(tx.Migrator(), &v22Plan{}, "APIAccess"); err != nil {
				return err
			}
			return addColumns(tx.Migrator(), &v22Subscription{}, "APIAccess")
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, x := range []any{&v22Plan{}, &v22Subscription{}} {
				if m.HasColumn(x, "APIAccess") {
					if err := m.DropColumn(x, "APIAccess"); err != nil {
						return err
					}
				}
			}
			return m.DropTable(&v22APIKey{}, &v22NoticeRead{}, &v22Notice{})
		},
	}
}
