package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v13：随机头像。以下为 v13 的快照结构体，不得修改。

type v13User struct {
	AvatarSeed   string `gorm:"size:32;not null;default:''"`
	AvatarYear   int    `gorm:"not null;default:0"`
	AvatarRegens int    `gorm:"not null;default:0"`
}

func (v13User) TableName() string { return "users" }

var v13Columns = []string{"AvatarSeed", "AvatarYear", "AvatarRegens"}

func migrationV13() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610270013_avatar",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, col := range v13Columns {
				if !m.HasColumn(&v13User{}, col) {
					if err := m.AddColumn(&v13User{}, col); err != nil {
						return err
					}
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, col := range v13Columns {
				if m.HasColumn(&v13User{}, col) {
					if err := m.DropColumn(&v13User{}, col); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
}
