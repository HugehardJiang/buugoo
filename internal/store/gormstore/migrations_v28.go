package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v28：官网首页编排与站点配置（cms_site_settings 表）。以下为 v28 的快照结构体，不得修改。

type v28CmsSiteSetting struct {
	Key       string `gorm:"column:key;size:64;primaryKey"`
	ValueJSON string `gorm:"column:value_json;type:mediumtext;not null"`
	UpdatedAt ts
}

func (v28CmsSiteSetting) TableName() string { return "cms_site_settings" }

func migrationV28() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202611280028_cms_site_settings",
		Migrate: func(tx *gorm.DB) error {
			return autoMigrate(tx, &v28CmsSiteSetting{})
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropTable(&v28CmsSiteSetting{})
		},
	}
}
