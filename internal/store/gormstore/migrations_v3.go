package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v3：聊天附件（图片与文档）、模型的看图与工具调用能力、消息的工具调用记录；把已停用的 DeepSeek 旧模型名换成 deepseek-flash。
// 以下为 v3 的快照结构体，不得修改。

type v3Attachment struct {
	ID             uint64  `gorm:"primaryKey"`
	UserID         uint64  `gorm:"index;not null"`
	ConversationID *uint64 `gorm:"index"`
	MessageID      *uint64 `gorm:"index"`
	Kind           string  `gorm:"size:16;not null"`
	Filename       string  `gorm:"size:255;not null"`
	MIME           string  `gorm:"column:mime;size:128;not null"`
	Size           int64   `gorm:"not null"`
	Path           string  `gorm:"size:512;not null"`
	SHA256         string  `gorm:"column:sha256;size:64;not null"`
	Text           string  `gorm:"size:16777215"`
	Description    string  `gorm:"size:8192"`
	CreatedAt      ts
}

func (v3Attachment) TableName() string { return "attachments" }

type v3ModelConfig struct {
	Vision bool `gorm:"not null;default:false"`
	Tools  bool `gorm:"not null;default:true"`
}

func (v3ModelConfig) TableName() string { return "model_configs" }

type v3Message struct {
	ToolLog string `gorm:"size:65536"`
}

func (v3Message) TableName() string { return "messages" }

func migrationV3Attachments() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610250003_attachments_vision",
		Migrate: func(tx *gorm.DB) error {
			if err := autoMigrate(tx, &v3Attachment{}); err != nil {
				return err
			}
			for _, col := range []string{"Vision", "Tools"} {
				if !tx.Migrator().HasColumn(&v3ModelConfig{}, col) {
					if err := tx.Migrator().AddColumn(&v3ModelConfig{}, col); err != nil {
						return err
					}
				}
			}
			if !tx.Migrator().HasColumn(&v3Message{}, "ToolLog") {
				if err := tx.Migrator().AddColumn(&v3Message{}, "ToolLog"); err != nil {
					return err
				}
			}
			// DeepSeek 已停用 deepseek-chat / deepseek-reasoner，网页中添加的这类模型改用 deepseek-flash（支持思考与图片）。
			return tx.Table("model_configs").
				Where("base_url LIKE ? AND model IN ?", "%api.deepseek.com%", []string{"deepseek-chat", "deepseek-reasoner"}).
				Updates(map[string]any{"model": "deepseek-flash", "thinking_style": "deepseek", "thinking_model": "", "vision": true}).Error
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, col := range []string{"Vision", "Tools"} {
				if m.HasColumn(&v3ModelConfig{}, col) {
					if err := m.DropColumn(&v3ModelConfig{}, col); err != nil {
						return err
					}
				}
			}
			if m.HasColumn(&v3Message{}, "ToolLog") {
				if err := m.DropColumn(&v3Message{}, "ToolLog"); err != nil {
					return err
				}
			}
			return m.DropTable(&v3Attachment{})
		},
	}
}
