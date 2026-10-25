package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v2：网页中管理的模型、多对话（每段对话可选模型与思考等级）、保存思考过程。
// 以下为 v2 的快照结构体，不得修改。

type v2ModelConfig struct {
	ID            uint64  `gorm:"primaryKey"`
	OwnerID       uint64  `gorm:"index;not null;default:0"`
	Name          string  `gorm:"size:64;not null"`
	BaseURL       string  `gorm:"size:512;not null"`
	APIKey        string  `gorm:"column:api_key_enc;size:2048;not null;default:''"`
	Model         string  `gorm:"size:128;not null"`
	ThinkingStyle string  `gorm:"size:32;not null;default:'none'"`
	ThinkingModel string  `gorm:"size:128;not null;default:''"`
	Temperature   float64 `gorm:"not null;default:0.8"`
	MaxTokens     int     `gorm:"not null;default:1024"`
	IsDefault     bool    `gorm:"not null;default:false"`
	CreatedAt     ts
	UpdatedAt     ts
}

func (v2ModelConfig) TableName() string { return "model_configs" }

type v2Conversation struct {
	ID            uint64  `gorm:"primaryKey"`
	UserID        uint64  `gorm:"index:idx_conv_user_last,priority:1;not null"`
	Title         string  `gorm:"size:255;not null;default:''"`
	ModelID       *uint64 ``
	Thinking      string  `gorm:"size:16;not null;default:'off'"`
	CreatedAt     ts
	UpdatedAt     ts
	LastMessageAt tsPtr `gorm:"index:idx_conv_user_last,priority:2"`
}

func (v2Conversation) TableName() string { return "conversations" }

type v2Message struct {
	ID               uint64  `gorm:"primaryKey"`
	ConversationID   uint64  `gorm:"index:idx_msg_conv,priority:1;not null"`
	UserID           uint64  `gorm:"index;not null"`
	Role             string  `gorm:"size:16;not null"`
	Content          string  `gorm:"size:65536;not null"`
	Model            string  `gorm:"size:128;not null;default:''"`
	PromptTokens     int     `gorm:"not null;default:0"`
	CompletionTokens int     `gorm:"not null;default:0"`
	Status           string  `gorm:"size:16;not null;default:'complete'"`
	Reasoning        string  `gorm:"size:65536"`
	ModelID          *uint64 ``
	CreatedAt        ts
}

func (v2Message) TableName() string { return "messages" }

func migrationV2Models() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610250002_models_conversations",
		Migrate: func(tx *gorm.DB) error {
			// AutoMigrate 只会新增缺少的表和列，不会删除已有数据。
			return autoMigrate(tx, &v2ModelConfig{}, &v2Conversation{}, &v2Message{})
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, c := range []struct {
				model any
				col   string
			}{{&v2Conversation{}, "ModelID"}, {&v2Conversation{}, "Thinking"}, {&v2Message{}, "Reasoning"}, {&v2Message{}, "ModelID"}} {
				if m.HasColumn(c.model, c.col) {
					if err := m.DropColumn(c.model, c.col); err != nil {
						return err
					}
				}
			}
			return m.DropTable(&v2ModelConfig{})
		},
	}
}
