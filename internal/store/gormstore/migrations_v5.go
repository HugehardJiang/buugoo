package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v5：用户自选的预设性格；附件记录来源（上传 / 代码生成），并支持在文件管理中删除（删除后保留记录，聊天记录中显示「文件已删除」）。
// 以下为 v5 的快照结构体，不得修改。

type v5User struct {
	PersonaPreset string `gorm:"size:32;not null;default:''"`
}

func (v5User) TableName() string { return "users" }

type v5Attachment struct {
	Source    string `gorm:"size:16;not null;default:''"`
	DeletedAt tsPtr  `gorm:"index"`
}

func (v5Attachment) TableName() string { return "attachments" }

func migrationV5PresetsFiles() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610250005_presets_files",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasColumn(&v5User{}, "PersonaPreset") {
				if err := m.AddColumn(&v5User{}, "PersonaPreset"); err != nil {
					return err
				}
			}
			if !m.HasColumn(&v5Attachment{}, "DeletedAt") {
				if err := m.AddColumn(&v5Attachment{}, "DeletedAt"); err != nil {
					return err
				}
				if err := m.CreateIndex(&v5Attachment{}, "DeletedAt"); err != nil {
					return err
				}
			}
			if !m.HasColumn(&v5Attachment{}, "Source") {
				if err := m.AddColumn(&v5Attachment{}, "Source"); err != nil {
					return err
				}
			}
			// v0.7 起助手回复中的附件都是代码生成的图片。
			if err := tx.Table("attachments").Where("message_id IN (?)", tx.Table("messages").Select("id").Where("role = ?", "assistant")).
				Update("source", "generated").Error; err != nil {
				return err
			}
			return tx.Table("attachments").Where("source = ?", "").Update("source", "upload").Error
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if m.HasColumn(&v5Attachment{}, "Source") {
				if err := m.DropColumn(&v5Attachment{}, "Source"); err != nil {
					return err
				}
			}
			if m.HasColumn(&v5Attachment{}, "DeletedAt") {
				_ = m.DropIndex(&v5Attachment{}, "DeletedAt")
				if err := m.DropColumn(&v5Attachment{}, "DeletedAt"); err != nil {
					return err
				}
			}
			if m.HasColumn(&v5User{}, "PersonaPreset") {
				return m.DropColumn(&v5User{}, "PersonaPreset")
			}
			return nil
		},
	}
}
