package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v17：公共模型可以只给授权用户用（OpenCode Go 等）。以下为 v17 的快照结构体，不得修改。

type v17ModelConfig struct {
	Restricted bool `gorm:"not null;default:false"`
}

func (v17ModelConfig) TableName() string { return "model_configs" }

type v17User struct {
	RestrictedModels bool `gorm:"not null;default:false"`
}

func (v17User) TableName() string { return "users" }

func migrationV17() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610290017_restricted_models",
		Migrate: func(tx *gorm.DB) error {
			if err := addColumns(tx.Migrator(), &v17ModelConfig{}, "Restricted"); err != nil {
				return err
			}
			return addColumns(tx.Migrator(), &v17User{}, "RestrictedModels")
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if m.HasColumn(&v17ModelConfig{}, "Restricted") {
				if err := m.DropColumn(&v17ModelConfig{}, "Restricted"); err != nil {
					return err
				}
			}
			if m.HasColumn(&v17User{}, "RestrictedModels") {
				return m.DropColumn(&v17User{}, "RestrictedModels")
			}
			return nil
		},
	}
}
