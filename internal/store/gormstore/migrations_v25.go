package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v25：附加包（只加权限、不改额度的套餐，可以和时长套餐叠加）。以下为 v25 的快照结构体，不得修改。

type v25UserAddon struct {
	ID             uint64 `gorm:"primaryKey"`
	UserID         uint64 `gorm:"index;not null"`
	PlanID         uint64 `gorm:"not null"`
	PlanName       string `gorm:"size:64;not null"`
	APIAccess      bool   `gorm:"not null;default:false"`
	TaskAccess     bool   `gorm:"not null;default:false"`
	SandboxNetwork bool   `gorm:"not null;default:false"`
	ExpiresAt      tsPtr
	CreatedAt      ts
	UpdatedAt      ts
}

func (v25UserAddon) TableName() string { return "user_addons" }

func migrationV25() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202611090025_user_addons",
		Migrate: func(tx *gorm.DB) error {
			return autoMigrate(tx, &v25UserAddon{})
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropTable(&v25UserAddon{})
		},
	}
}
