package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v20：app_meta.value 原来是 VARCHAR(4096)，MySQL 上存不下 OpenCode 的模型价格表
// （报 Error 1406 Data too long），改成 MEDIUMTEXT。SQLite 不限制长度，不用改。

func migrationV20() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202611050020_meta_value_mediumtext",
		Migrate: func(tx *gorm.DB) error {
			if tx.Dialector.Name() != "mysql" {
				return nil
			}
			return tx.Exec("ALTER TABLE app_meta MODIFY `value` MEDIUMTEXT NOT NULL").Error
		},
		Rollback: func(*gorm.DB) error { return nil },
	}
}
